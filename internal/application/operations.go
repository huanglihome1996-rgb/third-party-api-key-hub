package application

import (
	"context"
	"errors"
	v1 "github.com/third-party-api-key-hub/internal/contract/v1"
	"github.com/third-party-api-key-hub/internal/domain"
	"time"
)

// Operations is trusted service configuration. Client inputs never select paths.
type Operations interface {
	Backup(context.Context, int64, bool, func() bool) (v1.BackupResult, error)
	Retention(context.Context, time.Time, func() bool) (v1.RetentionResult, error)
}

func (e *Engine) ConfigureOperations(operations Operations) {
	e.gate.Lock()
	defer e.gate.Unlock()
	e.operations = operations
}
func isMaintenance(op string) bool {
	return op == "operations.backup" || op == "operations.retention" || op == "recovery.open"
}
func (e *Engine) recordAuditGap() {
	e.degraded.Store(true)
	if repo, ok := e.repo.(interface{ RecordAuditGap(context.Context) error }); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		_ = repo.RecordAuditGap(ctx)
	}
}
func (e *Engine) recoveryStatus() v1.RecoveryStatusResult {
	return v1.RecoveryStatusResult{Revision: e.state.Revision, RecoveryClosed: e.state.RecoveryClosed, AuditGap: e.state.AuditGap || e.degraded.Load(), Mode: "fixture"}
}
func (e *Engine) operate(ctx context.Context, r v1.OperationRequest, actor Actor) (any, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if r.Operation == "recovery.open" {
		var in v1.RecoveryOpenInput
		if payload(r.Payload, &in, "expected_revision", "accounts_verified", "ownership_verified", "values_verified", "credentials") != nil || !in.AccountsVerified || !in.OwnershipVerified || !in.ValuesVerified || in.ExpectedRevision != e.state.Revision || !e.state.RecoveryClosed {
			return nil, v1.Fail(v1.ErrInputInvalid)
		}
		if len(in.Credentials) != len(e.state.Credentials) {
			return nil, v1.Fail(v1.ErrInputInvalid)
		}
		seen := map[string]bool{}
		for _, ref := range in.Credentials {
			k := ckey(ref.Scope, ref.AppID, ref.ExternalShopID, ref.Type)
			c, ok := e.state.Credentials[k]
			if !ok || seen[k] || c.Generation != ref.ExpectedGeneration || ref.Reason != "" {
				return nil, v1.Fail(v1.ErrInputInvalid)
			}
			seen[k] = true
		}
		if err := ValidateRecoveryState(e.state, e.protector, e.profiles); err != nil {
			return nil, mapError(err)
		}
		if !e.authorize(actor).Manage {
			return nil, v1.Fail(v1.ErrPermissionDenied)
		}
		if ctx.Err() != nil {
			return nil, v1.Fail(v1.ErrInternal)
		}
		candidate := clone(e.state)
		candidate.RecoveryClosed = false
		candidate.Revision++
		if err := e.repo.Save(ctx, candidate); err != nil {
			return nil, v1.Fail(v1.ErrInternal)
		}
		e.state = candidate
		return e.recoveryStatus(), nil
	}
	var in struct{}
	if payload(r.Payload, &in) != nil {
		return nil, v1.Fail(v1.ErrInputInvalid)
	}
	if !e.authorize(actor).Manage {
		return nil, v1.Fail(v1.ErrPermissionDenied)
	}
	if r.Operation == "recovery.status" {
		return e.recoveryStatus(), nil
	}
	if e.operations == nil {
		return nil, e.operationFailure(ctx, errors.New("operations unavailable"))
	}
	if r.Operation == "operations.backup" {
		res, err := e.operations.Backup(ctx, e.state.Revision, e.state.AuditGap || e.degraded.Load(), func() bool { return e.authorize(actor).Manage })
		if err != nil {
			return nil, e.operationFailure(ctx, err)
		}
		return res, nil
	}
	res, err := e.operations.Retention(ctx, time.Now().UTC(), func() bool { return e.authorize(actor).Manage })
	if err != nil {
		return nil, e.operationFailure(ctx, err)
	}
	return res, nil
}

func (e *Engine) operationFailure(ctx context.Context, err error) error {
	if !errors.Is(err, domain.ErrPermission) && ctx.Err() == nil {
		e.operationsDegraded.Store(true)
		e.operationsFailures.Add(1)
	}
	return mapError(err)
}

// ValidateRecoveryState decrypts ALL logical credentials, including disabled and
// expired entries, without returning values or changing their lifecycle evidence.
func ValidateRecoveryState(s State, p Protector, profiles ProviderRegistry) error {
	return validateRecoveryState(s, p, profiles, true)
}

// ValidateRestoreState permits legal intermediate mappings/credential sets.
// Restore always closes reads; only recovery.open requires complete business sets.
func ValidateRestoreState(s State, p Protector, profiles ProviderRegistry) error {
	return validateRecoveryState(s, p, profiles, false)
}
func validateRecoveryState(s State, p Protector, profiles ProviderRegistry, complete bool) error {
	e := &Engine{profiles: profiles}
	s = normalize(s)
	appIdentities, shopIdentities, internalOwners := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for k, a := range s.Apps {
		identity := a.Platform + "|" + a.ProviderAppID
		if k != a.ID || !id(a.ID) || !id(a.ProviderAppID) || a.RowRevision <= 0 || appIdentities[identity] {
			return domain.ErrInvalid
		}
		appIdentities[identity] = true
		if _, ok := e.profile(a.Platform); !ok {
			return domain.ErrInvalid
		}
	}
	for k, sh := range s.Shops {
		identity := sh.Platform + "|" + sh.ProviderShopID
		if k != sh.ID || !id(sh.ID) || !id(sh.ProviderShopID) || sh.RowRevision <= 0 || shopIdentities[identity] {
			return domain.ErrInvalid
		}
		shopIdentities[identity] = true
		if _, ok := e.profile(sh.Platform); !ok {
			return domain.ErrInvalid
		}
		if sh.BrandID != "" {
			if _, ok := s.Brands[sh.BrandID]; !ok {
				return domain.ErrMappingIncomplete
			}
		}
	}
	for k, b := range s.Brands {
		if k != b.ID || !id(b.ID) || b.RowRevision <= 0 {
			return domain.ErrInvalid
		}
		if b.InternalShopID != "" {
			if internalOwners[b.InternalShopID] {
				return domain.ErrInvalid
			}
			internalOwners[b.InternalShopID] = true
			if _, ok := s.Internals[b.InternalShopID]; !ok {
				return domain.ErrMappingIncomplete
			}
		}
	}
	for k, i := range s.Internals {
		if k != i.ID || !id(i.ID) || i.RowRevision <= 0 {
			return domain.ErrInvalid
		}
	}
	for k, c := range s.Credentials {
		if k != ckey(c.Scope, c.AppID, c.ExternalShopID, c.Type) || c.Generation <= 0 || c.RowRevision <= 0 || e.checkTarget(&s, c.Scope, c.AppID, c.ExternalShopID, c.Type) != nil {
			return domain.ErrInvalid
		}
		v, err := p.Open("current-credential", currentTarget(c.Scope, c.AppID, c.ExternalShopID), c.Type, c.Ciphertext, c.Generation)
		if err != nil || v == "" {
			return errors.New("recovery binding rejected")
		}
	}
	for k, i := range s.Installations {
		if k != ikey(i.AppID, i.ExternalShopID) || i.RowRevision <= 0 {
			return domain.ErrInvalid
		}
		a, aok := s.Apps[i.AppID]
		sh, sok := s.Shops[i.ExternalShopID]
		if !aok || !sok || a.Platform != sh.Platform {
			return domain.ErrMappingMismatch
		}
		if !complete {
			continue
		}
		if err := completeMapping(&s, sh.ID); err != nil {
			return err
		}
		profile, ok := e.profile(a.Platform)
		if !ok {
			return domain.ErrInvalid
		}
		for _, d := range profile.Credentials {
			if d.Purpose != "business" || !d.Required {
				continue
			}
			sid := ""
			if d.Scope == "shop" {
				sid = sh.ID
			}
			if _, ok := s.Credentials[ckey(d.Scope, a.ID, sid, d.Type)]; !ok {
				return domain.ErrMissing
			}
		}
	}
	return nil
}
