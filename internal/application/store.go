package application

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	v1 "github.com/third-party-api-key-hub/internal/contract/v1"
	"github.com/third-party-api-key-hub/internal/domain"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

type Actor struct {
	UID          uint32
	Name         string
	Read, Manage bool
}
type State struct {
	Apps          map[string]domain.App
	Shops         map[string]domain.ExternalShop
	Brands        map[string]domain.Brand
	Internals     map[string]domain.InternalShop
	Installations map[string]domain.Installation
	Credentials   map[string]domain.Credential
	Revision      int64
	NextOperation uint64
}
type Repository interface {
	Load(context.Context) (State, error)
	Save(context.Context, State) error
}
type Protector interface {
	Seal(purpose, target, typ, value string, generation int64) (string, error)
	Open(purpose, target, typ, cipher string, generation int64) (string, error)
}
type AuditEvent struct {
	Time        time.Time `json:"time"`
	UID         uint32    `json:"uid"`
	ActorName   string    `json:"actor_name"`
	Operation   string    `json:"operation"`
	Target      string    `json:"target"`
	Result      string    `json:"result"`
	Error       string    `json:"error,omitempty"`
	OperationID string    `json:"operation_id"`
}
type Audit interface {
	Record(context.Context, AuditEvent) error
}
type AuditReader interface {
	List(context.Context) ([]AuditEvent, error)
}
type Engine struct {
	mu        sync.RWMutex
	state     State
	repo      Repository
	protector Protector
	audit     Audit
	authorize func(Actor) Actor
	profiles  ProviderRegistry
	degraded  atomic.Bool
}

func EmptyState() State {
	return State{Apps: map[string]domain.App{}, Shops: map[string]domain.ExternalShop{}, Brands: map[string]domain.Brand{}, Internals: map[string]domain.InternalShop{}, Installations: map[string]domain.Installation{}, Credentials: map[string]domain.Credential{}}
}
func normalize(s State) State {
	if s.Apps == nil {
		s.Apps = map[string]domain.App{}
	}
	if s.Shops == nil {
		s.Shops = map[string]domain.ExternalShop{}
	}
	if s.Brands == nil {
		s.Brands = map[string]domain.Brand{}
	}
	if s.Internals == nil {
		s.Internals = map[string]domain.InternalShop{}
	}
	if s.Installations == nil {
		s.Installations = map[string]domain.Installation{}
	}
	if s.Credentials == nil {
		s.Credentials = map[string]domain.Credential{}
	}
	return s
}
func clone(s State) State {
	n := EmptyState()
	n.Revision = s.Revision
	n.NextOperation = s.NextOperation
	for k, v := range s.Apps {
		n.Apps[k] = v
	}
	for k, v := range s.Shops {
		n.Shops[k] = v
	}
	for k, v := range s.Brands {
		n.Brands[k] = v
	}
	for k, v := range s.Internals {
		n.Internals[k] = v
	}
	for k, v := range s.Installations {
		n.Installations[k] = v
	}
	for k, v := range s.Credentials {
		if v.ExpiresAt != nil {
			t := *v.ExpiresAt
			v.ExpiresAt = &t
		}
		n.Credentials[k] = v
	}
	return n
}
func NewEngine(repo Repository, protector Protector, audit Audit, authorize func(Actor) Actor, profiles ...ProviderRegistry) (*Engine, error) {
	if repo == nil || protector == nil || audit == nil || authorize == nil {
		return nil, v1.Fail(v1.ErrInternal)
	}
	s, err := repo.Load(context.Background())
	if err != nil {
		return nil, v1.Fail(v1.ErrInternal)
	}
	registry := FixtureProfiles()
	if len(profiles) > 1 || len(profiles) == 1 && profiles[0] == nil {
		return nil, v1.Fail(v1.ErrInternal)
	}
	if len(profiles) == 1 {
		registry = profiles[0]
	}
	return &Engine{state: normalize(clone(s)), repo: repo, protector: protector, audit: audit, authorize: authorize, profiles: registry}, nil
}
func (e *Engine) AuditDegraded() bool { return e.degraded.Load() }
func isRead(op string) bool           { return op == "credential.query" }
func isMutation(op string) bool {
	switch op {
	case "app.register", "shop.register", "brand.register", "internal_shop.register", "shop.map_brand", "shop.unmap_brand", "brand.map_internal_shop", "brand.unmap_internal_shop", "installation.create", "credential.import", "credential.confirm", "credential.mark_invalid", "credential.disable", "credential.restore":
		return true
	}
	return false
}

// Execute is the sole public business entry point. A candidate state is published only after durable commit.
func (e *Engine) Execute(ctx context.Context, req v1.OperationRequest, actor Actor) (result any, err error) {
	event := AuditEvent{Time: time.Now().UTC(), UID: actor.UID, ActorName: actor.Name, Operation: "unknown", Target: "unknown", Result: "failure", OperationID: "op-" + rand.Text()}
	defer func() {
		if err == nil {
			event.Result = "success"
		} else {
			event.Error = string(v1.SafeError(err).Error)
		}
		if e.audit != nil {
			aCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 500*time.Millisecond)
			defer cancel()
			if aerr := e.audit.Record(aCtx, event); aerr != nil {
				e.degraded.Store(true)
			}
		}
	}()
	actor = e.authorize(actor)
	if isRead(req.Operation) {
		if !actor.Read {
			return nil, v1.Fail(v1.ErrPermissionDenied)
		}
	} else if !actor.Manage {
		return nil, v1.Fail(v1.ErrPermissionDenied)
	}
	if v1.ValidateOperationRequest(req) != nil {
		return nil, v1.Fail(v1.ErrInputInvalid)
	}
	event.Operation = req.Operation
	if v1.ValidateOperationPayload(req) != nil {
		return nil, v1.Fail(v1.ErrInputInvalid)
	}
	event.Target = auditTarget(req.Payload)
	if ctx.Err() != nil {
		return nil, v1.Fail(v1.ErrInternal)
	}
	if isMutation(req.Operation) {
		e.mu.Lock()
		defer e.mu.Unlock()
		candidate := clone(e.state)
		candidate.NextOperation++
		op := fmt.Sprintf("op-%d-%s", candidate.NextOperation, rand.Text())
		event.OperationID = op
		res, derr := e.mutate(ctx, &candidate, req, op)
		if derr != nil {
			return nil, mapError(derr)
		}
		actor = e.authorize(actor)
		if !actor.Manage {
			return nil, v1.Fail(v1.ErrPermissionDenied)
		}
		if ctx.Err() != nil {
			return nil, v1.Fail(v1.ErrInternal)
		}
		candidate.Revision++
		if err = e.repo.Save(ctx, clone(candidate)); err != nil {
			return nil, v1.Fail(v1.ErrInternal)
		}
		e.state = candidate
		res.OperationID = op
		res.Revision = candidate.Revision
		return res, nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	res, derr := e.read(ctx, req)
	if derr != nil {
		return nil, mapError(derr)
	}
	actor = e.authorize(actor)
	if isRead(req.Operation) {
		if !actor.Read {
			return nil, v1.Fail(v1.ErrPermissionDenied)
		}
	} else if !actor.Manage {
		return nil, v1.Fail(v1.ErrPermissionDenied)
	}
	if ctx.Err() != nil {
		return nil, v1.Fail(v1.ErrInternal)
	}
	return res, nil
}
func mapError(err error) error {
	switch {
	case errors.Is(err, domain.ErrPermission):
		return v1.Fail(v1.ErrPermissionDenied)
	case errors.Is(err, domain.ErrInvalid), errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrGeneration), errors.Is(err, domain.ErrRecovery):
		return v1.Fail(v1.ErrInputInvalid)
	case errors.Is(err, domain.ErrNotFound):
		return v1.Fail(v1.ErrNotFound)
	case errors.Is(err, domain.ErrSelection):
		return v1.Fail(v1.ErrSelectionRequired)
	case errors.Is(err, domain.ErrMappingIncomplete):
		return v1.Fail(v1.ErrMappingIncomplete)
	case errors.Is(err, domain.ErrMappingMismatch):
		return v1.Fail(v1.ErrMappingMismatch)
	case errors.Is(err, domain.ErrMissing):
		return v1.Fail(v1.ErrCredentialMissing)
	case errors.Is(err, domain.ErrExpired):
		return v1.Fail(v1.ErrCredentialExpired)
	case errors.Is(err, domain.ErrDisabled):
		return v1.Fail(v1.ErrCredentialDisabled)
	}
	return v1.Fail(v1.ErrInternal)
}
func payload(raw json.RawMessage, dst any, required ...string) error {
	if v1.DecodeStrict(bytes.NewReader(raw), dst) != nil || v1.RequireFields(raw, required...) != nil {
		return domain.ErrInvalid
	}
	return nil
}
func id(s string) bool {
	if len(s) == 0 || len(s) > 128 || strings.Contains(s, "|") {
		return false
	}
	for _, c := range s {
		if unicode.IsSpace(c) || unicode.IsControl(c) {
			return false
		}
	}
	return true
}
func optionalID(s string) bool { return s == "" || id(s) }
func validName(s string) bool {
	if len(s) > 256 {
		return false
	}
	for _, c := range s {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}
func ikey(app, shop string) string                 { return app + "|" + shop }
func ckey(scope, app, shop, typ string) string     { return scope + "|" + app + "|" + shop + "|" + typ }
func currentTarget(scope, app, shop string) string { return scope + "|" + app + "|" + shop }
func (e *Engine) checkTarget(s *State, scope, app, shop, typ string) error {
	if !id(app) || (scope != "app" && scope != "shop") || !e.validType(s, scope, app, typ) || scope == "app" && shop != "" || scope == "shop" && !id(shop) {
		return domain.ErrInvalid
	}
	if _, ok := s.Apps[app]; !ok {
		return domain.ErrNotFound
	}
	if scope == "shop" {
		if _, ok := s.Installations[ikey(app, shop)]; !ok {
			return domain.ErrNotFound
		}
	}
	return nil
}
func bumpApp(s *State, app string) { a := s.Apps[app]; a.RowRevision++; s.Apps[app] = a }
func bumpCredential(s *State, k string, c domain.Credential) {
	s.Credentials[k] = c
	bumpApp(s, c.AppID)
	if c.Scope == "shop" {
		k := ikey(c.AppID, c.ExternalShopID)
		i := s.Installations[k]
		i.RowRevision++
		s.Installations[k] = i
	}
}
func (e *Engine) mutate(ctx context.Context, s *State, r v1.OperationRequest, op string) (v1.MutationResult, error) {
	out := v1.MutationResult{}
	switch r.Operation {
	case "app.register":
		var in v1.RegisterAppInput
		if payload(r.Payload, &in, "app_id", "platform", "provider_app_id") != nil || !id(in.AppID) || !id(in.ProviderAppID) || !validName(in.Name) {
			return out, domain.ErrInvalid
		}
		if _, profileOK := e.profile(in.Platform); !profileOK {
			return out, domain.ErrInvalid
		}
		if _, ok := s.Apps[in.AppID]; ok {
			return out, domain.ErrConflict
		}
		for _, a := range s.Apps {
			if a.Platform == in.Platform && a.ProviderAppID == in.ProviderAppID {
				return out, domain.ErrConflict
			}
		}
		s.Apps[in.AppID] = domain.App{ID: in.AppID, Platform: in.Platform, ProviderAppID: in.ProviderAppID, Name: in.Name, RowRevision: 1}
		out.RowRevision = 1
	case "shop.register":
		var in v1.RegisterShopInput
		if payload(r.Payload, &in, "external_shop_id", "platform", "provider_shop_id") != nil || !id(in.ExternalShopID) || !id(in.ProviderShopID) || !validName(in.Name) {
			return out, domain.ErrInvalid
		}
		if _, ok := e.profile(in.Platform); !ok {
			return out, domain.ErrInvalid
		}
		if _, ok := s.Shops[in.ExternalShopID]; ok {
			return out, domain.ErrConflict
		}
		for _, sh := range s.Shops {
			if sh.Platform == in.Platform && sh.ProviderShopID == in.ProviderShopID {
				return out, domain.ErrConflict
			}
		}
		s.Shops[in.ExternalShopID] = domain.ExternalShop{ID: in.ExternalShopID, Platform: in.Platform, ProviderShopID: in.ProviderShopID, Name: in.Name, RowRevision: 1}
		out.RowRevision = 1
	case "brand.register":
		var in v1.RegisterBrandInput
		if payload(r.Payload, &in, "brand_id") != nil || !id(in.BrandID) || !validName(in.Name) {
			return out, domain.ErrInvalid
		}
		if _, ok := s.Brands[in.BrandID]; ok {
			return out, domain.ErrConflict
		}
		s.Brands[in.BrandID] = domain.Brand{ID: in.BrandID, Name: in.Name, RowRevision: 1}
		out.RowRevision = 1
	case "internal_shop.register":
		var in v1.RegisterInternalShopInput
		if payload(r.Payload, &in, "internal_shop_id") != nil || !id(in.InternalShopID) || !validName(in.Name) {
			return out, domain.ErrInvalid
		}
		if _, ok := s.Internals[in.InternalShopID]; ok {
			return out, domain.ErrConflict
		}
		s.Internals[in.InternalShopID] = domain.InternalShop{ID: in.InternalShopID, Name: in.Name, RowRevision: 1}
		out.RowRevision = 1
	case "shop.map_brand", "shop.unmap_brand":
		var in v1.MapInput
		if payload(r.Payload, &in, "external_shop_id", "expected_revision") != nil || !id(in.ExternalShopID) || in.InternalShopID != "" {
			return out, domain.ErrInvalid
		}
		sh, ok := s.Shops[in.ExternalShopID]
		if !ok {
			return out, domain.ErrNotFound
		}
		if in.ExpectedRevision != sh.RowRevision {
			return out, domain.ErrGeneration
		}
		if r.Operation == "shop.map_brand" {
			if !id(in.BrandID) {
				return out, domain.ErrInvalid
			}
			if _, ok := s.Brands[in.BrandID]; !ok {
				return out, domain.ErrNotFound
			}
			sh.BrandID = in.BrandID
		} else {
			if in.BrandID != "" {
				return out, domain.ErrInvalid
			}
			sh.BrandID = ""
		}
		sh.RowRevision++
		s.Shops[sh.ID] = sh
		out.RowRevision = sh.RowRevision
	case "brand.map_internal_shop", "brand.unmap_internal_shop":
		var in v1.MapInput
		if payload(r.Payload, &in, "brand_id", "expected_revision") != nil || !id(in.BrandID) || in.ExternalShopID != "" {
			return out, domain.ErrInvalid
		}
		b, ok := s.Brands[in.BrandID]
		if !ok {
			return out, domain.ErrNotFound
		}
		if in.ExpectedRevision != b.RowRevision {
			return out, domain.ErrGeneration
		}
		old := b.InternalShopID
		if r.Operation == "brand.map_internal_shop" {
			if !id(in.InternalShopID) {
				return out, domain.ErrInvalid
			}
			if _, ok := s.Internals[in.InternalShopID]; !ok {
				return out, domain.ErrNotFound
			}
			for bid, x := range s.Brands {
				if bid != b.ID && x.InternalShopID == in.InternalShopID {
					return out, domain.ErrConflict
				}
			}
			b.InternalShopID = in.InternalShopID
		} else {
			if in.InternalShopID != "" {
				return out, domain.ErrInvalid
			}
			b.InternalShopID = ""
		}
		b.RowRevision++
		s.Brands[b.ID] = b
		for _, iid := range []string{old, b.InternalShopID} {
			if iid != "" {
				i := s.Internals[iid]
				i.RowRevision++
				s.Internals[iid] = i
			}
		}
		out.RowRevision = b.RowRevision
	case "installation.create":
		var in v1.InstallationInput
		if payload(r.Payload, &in, "app_id", "external_shop_id", "expected_revision") != nil || !id(in.AppID) || !id(in.ExternalShopID) {
			return out, domain.ErrInvalid
		}
		a, ok := s.Apps[in.AppID]
		if !ok {
			return out, domain.ErrNotFound
		}
		sh, ok := s.Shops[in.ExternalShopID]
		if !ok {
			return out, domain.ErrNotFound
		}
		if a.Platform != sh.Platform {
			return out, domain.ErrMappingMismatch
		}
		if a.RowRevision != in.ExpectedRevision {
			return out, domain.ErrGeneration
		}
		k := ikey(in.AppID, in.ExternalShopID)
		if _, ok := s.Installations[k]; ok {
			return out, domain.ErrConflict
		}
		s.Installations[k] = domain.Installation{AppID: in.AppID, ExternalShopID: in.ExternalShopID, RowRevision: 1}
		bumpApp(s, in.AppID)
		out.RowRevision = 1
	case "credential.import":
		var in v1.ImportCredentialInput
		if payload(r.Payload, &in, "scope", "app_id", "type", "value", "replace", "expiry_known", "expires_at", "confirm") != nil || in.Value == "" || len(in.Value) > 65536 {
			return out, domain.ErrInvalid
		}
		if in.Scope == "shop" {
			if !id(in.AppID) || !id(in.ExternalShopID) || !e.validType(s, in.Scope, in.AppID, in.Type) {
				return out, domain.ErrInvalid
			}
			a, ok := s.Apps[in.AppID]
			if !ok {
				return out, domain.ErrNotFound
			}
			sh, ok := s.Shops[in.ExternalShopID]
			if !ok {
				return out, domain.ErrNotFound
			}
			if a.Platform != sh.Platform {
				return out, domain.ErrMappingMismatch
			}
			if _, ok := s.Installations[ikey(in.AppID, in.ExternalShopID)]; !ok {
				s.Installations[ikey(in.AppID, in.ExternalShopID)] = domain.Installation{AppID: in.AppID, ExternalShopID: in.ExternalShopID, RowRevision: 1}
			}
		}
		if err := e.checkTarget(s, in.Scope, in.AppID, in.ExternalShopID, in.Type); err != nil {
			return out, err
		}
		exp, err := expiry(in.ExpiryKnown, in.ExpiresAt)
		if err != nil {
			return out, err
		}
		k := ckey(in.Scope, in.AppID, in.ExternalShopID, in.Type)
		old, exists := s.Credentials[k]
		if exists {
			if !in.Replace || in.ExpectedGeneration <= 0 || in.ExpectedGeneration != old.Generation {
				return out, domain.ErrGeneration
			}
		} else if in.Replace || in.ExpectedGeneration != 0 {
			return out, domain.ErrConflict
		}
		g, rev := int64(1), int64(1)
		if exists {
			g = old.Generation + 1
			rev = old.RowRevision + 1
		}
		cipher, err := e.protector.Seal("current-credential", currentTarget(in.Scope, in.AppID, in.ExternalShopID), in.Type, in.Value, g)
		if err != nil {
			return out, err
		}
		c := domain.Credential{Scope: in.Scope, AppID: in.AppID, ExternalShopID: in.ExternalShopID, Type: in.Type, Ciphertext: cipher, Generation: g, RowRevision: rev, ExpiryKnown: in.ExpiryKnown, ExpiresAt: exp, ValueOrigin: "manual_import"}
		if exists {
			c.ValueOrigin = "manual_replace"
			c.Disabled = old.Disabled
			c.DisabledGeneration = old.DisabledGeneration
			if c.Disabled {
				c.RecoveryBasisGeneration = g
				c.RecoveryBasisOperationID = op
			}
		}
		if in.Confirm {
			c.ConfirmedGeneration = g
		}
		bumpCredential(s, k, c)
		out.Generation = g
		out.RowRevision = rev
	case "credential.confirm", "credential.mark_invalid", "credential.disable", "credential.restore":
		var in v1.CredentialRefInput
		required := []string{"scope", "app_id", "type"}
		if r.Operation != "credential.disable" {
			required = append(required, "expected_generation")
		}
		if payload(r.Payload, &in, required...) != nil {
			return out, domain.ErrInvalid
		}
		if err := e.checkTarget(s, in.Scope, in.AppID, in.ExternalShopID, in.Type); err != nil {
			return out, err
		}
		k := ckey(in.Scope, in.AppID, in.ExternalShopID, in.Type)
		c, ok := s.Credentials[k]
		if !ok {
			return out, domain.ErrNotFound
		}
		if r.Operation != "credential.disable" && (in.ExpectedGeneration <= 0 || in.ExpectedGeneration != c.Generation) {
			return out, domain.ErrGeneration
		}
		if r.Operation != "credential.mark_invalid" && in.Reason != "" {
			return out, domain.ErrInvalid
		}
		switch r.Operation {
		case "credential.confirm":
			c.ConfirmedGeneration = c.Generation
		case "credential.disable":
			if in.ExpectedGeneration != 0 {
				return out, domain.ErrInvalid
			}
			c.Disabled = true
			c.DisabledGeneration = c.Generation
			c.RecoveryBasisGeneration = 0
			c.RecoveryBasisOperationID = ""
		case "credential.mark_invalid":
			if in.Reason != "rejected" && in.Reason != "revoked" && in.Reason != "invalid" {
				return out, domain.ErrInvalid
			}
			c.ValidationGeneration = c.Generation
			c.ValidationResult = "invalid"
			c.Disabled = true
			c.DisabledGeneration = c.Generation
			c.RecoveryBasisGeneration = 0
			c.RecoveryBasisOperationID = ""
		case "credential.restore":
			if !c.CanRestore() {
				return out, domain.ErrRecovery
			}
			if err := restoreMapping(s, c); err != nil {
				return out, err
			}
			v := c
			v.Disabled = false
			if err := v.Available(time.Now().UTC()); err != nil {
				return out, err
			}
			if _, err := e.protector.Open("current-credential", currentTarget(c.Scope, c.AppID, c.ExternalShopID), c.Type, c.Ciphertext, c.Generation); err != nil {
				return out, err
			}
			c.Disabled = false
			c.DisabledGeneration = 0
			c.RecoveryBasisGeneration = 0
			c.RecoveryBasisOperationID = ""
		}
		c.RowRevision++
		bumpCredential(s, k, c)
		out.Generation = c.Generation
		out.RowRevision = c.RowRevision
	}
	return out, nil
}
func expiry(known bool, s *string) (*time.Time, error) {
	if !known {
		if s != nil {
			return nil, domain.ErrInvalid
		}
		return nil, nil
	}
	if s == nil {
		return nil, domain.ErrInvalid
	}
	t, e := time.Parse(time.RFC3339, *s)
	if e != nil || !strings.HasSuffix(*s, "Z") {
		return nil, domain.ErrInvalid
	}
	t = t.UTC()
	return &t, nil
}
func completeMapping(s *State, shop string) error {
	sh, ok := s.Shops[shop]
	if !ok {
		return domain.ErrNotFound
	}
	if sh.BrandID == "" {
		return domain.ErrMappingIncomplete
	}
	b, ok := s.Brands[sh.BrandID]
	if !ok || b.InternalShopID == "" {
		return domain.ErrMappingIncomplete
	}
	if _, ok := s.Internals[b.InternalShopID]; !ok {
		return domain.ErrMappingIncomplete
	}
	return nil
}
func restoreMapping(s *State, c domain.Credential) error {
	if c.Scope == "shop" {
		return completeMapping(s, c.ExternalShopID)
	}
	found := false
	for _, i := range s.Installations {
		if i.AppID == c.AppID {
			found = true
			if err := completeMapping(s, i.ExternalShopID); err != nil {
				return err
			}
		}
	}
	if !found {
		return domain.ErrMappingIncomplete
	}
	return nil
}

type AuditListResult struct {
	Events []AuditEvent `json:"events"`
	Total  int          `json:"total"`
	Offset int          `json:"offset"`
	Limit  int          `json:"limit"`
}

func auditTarget(raw json.RawMessage) string {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return "unknown"
	}
	parts := []string{}
	for _, k := range []string{"app_id", "external_shop_id", "brand_id", "internal_shop_id"} {
		if b, ok := m[k]; ok {
			var value string
			if json.Unmarshal(b, &value) == nil && id(value) {
				parts = append(parts, k+":"+value)
			}
		}
	}
	if len(parts) == 0 {
		return "unknown"
	}
	return strings.Join(parts, ",")
}
