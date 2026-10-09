package application

import (
	"context"
	v1 "github.com/third-party-api-key-hub/internal/contract/v1"
	"github.com/third-party-api-key-hub/internal/domain"
	"sort"
	"time"
)

func keys[T any](m map[string]T) []string {
	k := make([]string, 0, len(m))
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return k
}
func (e *Engine) read(ctx context.Context, r v1.OperationRequest) (any, error) {
	s := &e.state
	switch r.Operation {
	case "credential.query":
		if s.RecoveryClosed {
			return nil, domain.ErrDisabled
		}
		var in v1.QueryInput
		if payload(r.Payload, &in, "by") != nil {
			return nil, domain.ErrInvalid
		}
		return e.query(s, in)
	case "credential.status":
		var in v1.ListInput
		if payload(r.Payload, &in) != nil || !optionalID(in.AppID) || !optionalID(in.ExternalShopID) {
			return nil, domain.ErrInvalid
		}
		if in.AppID != "" {
			if _, ok := s.Apps[in.AppID]; !ok {
				return nil, domain.ErrNotFound
			}
		}
		if in.ExternalShopID != "" {
			if _, ok := s.Shops[in.ExternalShopID]; !ok {
				return nil, domain.ErrNotFound
			}
		}
		items := []v1.StatusEntry{}
		for _, k := range keys(s.Credentials) {
			c := s.Credentials[k]
			if in.AppID != "" && c.AppID != in.AppID || in.ExternalShopID != "" && c.ExternalShopID != in.ExternalShopID {
				continue
			}
			st := status(c)
			items = append(items, v1.StatusEntry{Scope: c.Scope, AppID: c.AppID, ExternalShopID: c.ExternalShopID, Type: st.Type, Generation: st.Generation, RowRevision: st.RowRevision, Available: st.Available, Disabled: st.Disabled, ConfirmedByOperator: st.ConfirmedByOperator, ExpiryKnown: st.ExpiryKnown, ExpiresAt: st.ExpiresAt})
		}
		start, end, limit := pageBounds(len(items), in.Offset, in.Limit)
		return v1.StatusResult{Statuses: items[start:end], Revision: s.Revision, Total: len(items), Offset: in.Offset, Limit: limit}, nil
	case "app.list", "shop.list", "brand.list", "internal_shop.list", "installation.list":
		var in v1.ListInput
		if payload(r.Payload, &in) != nil || !optionalID(in.AppID) || !optionalID(in.ExternalShopID) {
			return nil, domain.ErrInvalid
		}
		if r.Operation != "installation.list" && (in.AppID != "" || in.ExternalShopID != "") {
			return nil, domain.ErrInvalid
		}
		switch r.Operation {
		case "app.list":
			a := []domain.App{}
			for _, k := range keys(s.Apps) {
				a = append(a, s.Apps[k])
			}
			return listPage("apps", a, s.Revision, in), nil
		case "shop.list":
			a := []domain.ExternalShop{}
			for _, k := range keys(s.Shops) {
				a = append(a, s.Shops[k])
			}
			return listPage("shops", a, s.Revision, in), nil
		case "brand.list":
			a := []domain.Brand{}
			for _, k := range keys(s.Brands) {
				a = append(a, s.Brands[k])
			}
			return listPage("brands", a, s.Revision, in), nil
		case "internal_shop.list":
			a := []domain.InternalShop{}
			for _, k := range keys(s.Internals) {
				a = append(a, s.Internals[k])
			}
			return listPage("internal_shops", a, s.Revision, in), nil
		case "installation.list":
			a := []domain.Installation{}
			for _, k := range keys(s.Installations) {
				i := s.Installations[k]
				if in.AppID != "" && i.AppID != in.AppID || in.ExternalShopID != "" && i.ExternalShopID != in.ExternalShopID {
					continue
				}
				a = append(a, i)
			}
			return listPage("installations", a, s.Revision, in), nil
		}
	case "health":
		var in struct{}
		if payload(r.Payload, &in) != nil {
			return nil, domain.ErrInvalid
		}
		return v1.HealthResult{Revision: s.Revision, AuditDegraded: e.degraded.Load() || s.AuditGap, OperationsDegraded: e.operationsDegraded.Load(), RecoveryClosed: s.RecoveryClosed, Mode: "fixture", Deferred: []string{"linux_acceptance", "openbao", "tiktok"}}, nil
	case "audit.list":
		var in v1.AuditListInput
		if payload(r.Payload, &in) != nil || in.Offset < 0 || in.Limit < 0 || in.Limit > 1000 {
			return nil, domain.ErrInvalid
		}
		var from, to *time.Time
		var err error
		if in.From != nil {
			from, err = expiry(true, in.From)
			if err != nil {
				return nil, err
			}
		}
		if in.To != nil {
			to, err = expiry(true, in.To)
			if err != nil {
				return nil, err
			}
		}
		if from != nil && to != nil && from.After(*to) {
			return nil, domain.ErrInvalid
		}
		if in.Result != "" && in.Result != "success" && in.Result != "failure" {
			return nil, domain.ErrInvalid
		}
		reader, ok := e.audit.(AuditReader)
		if !ok {
			return nil, domain.ErrMissing
		}
		events, err := reader.List(ctx)
		if err != nil {
			return nil, err
		}
		a := []AuditEvent{}
		for _, v := range events {
			if from != nil && v.Time.Before(*from) || to != nil && v.Time.After(*to) || in.Operation != "" && v.Operation != in.Operation || in.Target != "" && v.Target != in.Target || in.Result != "" && v.Result != in.Result || in.OperationID != "" && v.OperationID != in.OperationID {
				continue
			}
			a = append(a, v)
		}
		sort.SliceStable(a, func(i, j int) bool { return a[i].Time.Before(a[j].Time) })
		total := len(a)
		start, end, limit := pageBounds(total, in.Offset, in.Limit)
		return AuditListResult{Events: a[start:end], Total: total, Offset: in.Offset, Limit: limit}, nil
	}
	return nil, domain.ErrInvalid
}
func status(c domain.Credential) v1.CredentialStatus {
	return v1.CredentialStatus{Type: c.Type, Generation: c.Generation, RowRevision: c.RowRevision, Available: c.Available(time.Now().UTC()) == nil, Disabled: c.Disabled, ConfirmedByOperator: c.ConfirmedGeneration == c.Generation, ExpiryKnown: c.ExpiryKnown, ExpiresAt: formatTime(c.ExpiresAt)}
}
func formatTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339Nano)
	return &s
}
func (e *Engine) query(s *State, in v1.QueryInput) (any, error) {
	if !optionalID(in.AppID) || !optionalID(in.ExternalShopID) || !optionalID(in.BrandID) || !optionalID(in.InternalShopID) {
		return nil, domain.ErrInvalid
	}
	if in.AppID != "" {
		if _, ok := s.Apps[in.AppID]; !ok {
			return nil, domain.ErrNotFound
		}
	}
	shops := []string{}
	switch in.By {
	case "external_shop":
		if !id(in.ExternalShopID) || in.BrandID != "" || in.InternalShopID != "" {
			return nil, domain.ErrInvalid
		}
		if _, ok := s.Shops[in.ExternalShopID]; !ok {
			return nil, domain.ErrNotFound
		}
		shops = append(shops, in.ExternalShopID)
	case "brand":
		if !id(in.BrandID) || in.InternalShopID != "" {
			return nil, domain.ErrInvalid
		}
		if _, ok := s.Brands[in.BrandID]; !ok {
			return nil, domain.ErrNotFound
		}
		for k, v := range s.Shops {
			if v.BrandID == in.BrandID {
				shops = append(shops, k)
			}
		}
	case "internal_shop":
		if !id(in.InternalShopID) || in.BrandID != "" {
			return nil, domain.ErrInvalid
		}
		if _, ok := s.Internals[in.InternalShopID]; !ok {
			return nil, domain.ErrNotFound
		}
		for k, v := range s.Shops {
			b, ok := s.Brands[v.BrandID]
			if ok && b.InternalShopID == in.InternalShopID {
				shops = append(shops, k)
			}
		}
	default:
		return nil, domain.ErrInvalid
	}
	sort.Strings(shops)
	if in.ExternalShopID != "" && in.By != "external_shop" {
		if _, ok := s.Shops[in.ExternalShopID]; !ok {
			return nil, domain.ErrNotFound
		}
		found := false
		for _, k := range shops {
			if k == in.ExternalShopID {
				found = true
			}
		}
		if !found {
			return nil, domain.ErrMappingMismatch
		}
		shops = []string{in.ExternalShopID}
	}
	if len(shops) == 0 {
		return nil, domain.ErrMappingIncomplete
	}
	if len(shops) > 1 {
		return nil, domain.ErrSelection
	}
	shop := s.Shops[shops[0]]
	apps := []string{}
	for _, i := range s.Installations {
		if i.ExternalShopID == shop.ID {
			apps = append(apps, i.AppID)
		}
	}
	sort.Strings(apps)
	if in.AppID != "" {
		found := false
		for _, app := range apps {
			if app == in.AppID {
				found = true
			}
		}
		if !found {
			return nil, domain.ErrMappingMismatch
		}
		apps = []string{in.AppID}
	}
	if len(apps) == 0 {
		return nil, domain.ErrNotFound
	}
	if len(apps) > 1 {
		return nil, domain.ErrSelection
	}
	app := s.Apps[apps[0]]
	if app.Platform != shop.Platform {
		return nil, domain.ErrMappingMismatch
	}
	if err := completeMapping(s, shop.ID); err != nil {
		return nil, err
	}
	brand := s.Brands[shop.BrandID]
	profile, ok := e.profile(app.Platform)
	if !ok {
		return nil, errorsInternal()
	}
	all := []domain.Credential{}
	for _, d := range profile.Credentials {
		if d.Purpose != "business" || !d.Required {
			continue
		}
		sid := ""
		if d.Scope == "shop" {
			sid = shop.ID
		}
		c, ok := s.Credentials[ckey(d.Scope, app.ID, sid, d.Type)]
		if !ok {
			return nil, domain.ErrMissing
		}
		all = append(all, c)
	}
	for _, c := range all {
		if c.Disabled {
			return nil, domain.ErrDisabled
		}
	}
	now := time.Now().UTC()
	for _, c := range all {
		if c.ExpiryKnown && (c.ExpiresAt == nil || !c.ExpiresAt.After(now)) {
			return nil, domain.ErrExpired
		}
	}
	for _, c := range all {
		if err := c.Available(now); err != nil {
			return nil, err
		}
	}
	values := make([]string, len(all))
	for i, c := range all {
		v, err := e.protector.Open("current-credential", currentTarget(c.Scope, c.AppID, c.ExternalShopID), c.Type, c.Ciphertext, c.Generation)
		if err != nil || v == "" {
			return nil, errorsInternal()
		}
		values[i] = v
	}
	// Time passes during protection calls; the final result uses a fresh check.
	now = time.Now().UTC()
	for _, c := range all {
		if err := c.Available(now); err != nil {
			return nil, err
		}
	}
	out := v1.QueryResult{Identity: v1.Identity{AppID: app.ID, Platform: app.Platform, ProviderAppID: app.ProviderAppID, ExternalShopID: shop.ID, ProviderShopID: shop.ProviderShopID, BrandID: brand.ID, InternalShopID: brand.InternalShopID}}
	out.Credentials.App = []v1.CredentialValue{}
	out.Credentials.Shop = []v1.CredentialValue{}
	out.Status.App = []v1.CredentialStatus{}
	out.Status.Shop = []v1.CredentialStatus{}
	for i, c := range all {
		value := v1.CredentialValue{Type: c.Type, Value: values[i]}
		if c.Scope == "app" {
			out.Credentials.App = append(out.Credentials.App, value)
			out.Status.App = append(out.Status.App, status(c))
		} else {
			out.Credentials.Shop = append(out.Credentials.Shop, value)
			out.Status.Shop = append(out.Status.Shop, status(c))
		}
	}
	return out, nil
}
func errorsInternal() error { return v1.Fail(v1.ErrInternal) }

func pageBounds(total, offset, limit int) (int, int, int) {
	if limit == 0 {
		limit = 100
	}
	start := offset
	if start > total {
		start = total
	}
	count := total - start
	if count > limit {
		count = limit
	}
	return start, start + count, limit
}
func listPage[T any](key string, items []T, revision int64, in v1.ListInput) map[string]any {
	start, end, limit := pageBounds(len(items), in.Offset, in.Limit)
	return map[string]any{key: items[start:end], "revision": revision, "total": len(items), "offset": in.Offset, "limit": limit}
}
