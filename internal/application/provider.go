package application

// ProviderRegistry is trusted service configuration, never request data.
type ProviderRegistry interface {
	Profile(platform string) (ProviderProfile, bool)
}
type CredentialDefinition struct {
	Scope, Type, Purpose string
	Required             bool
}
type ProviderProfile struct{ Credentials []CredentialDefinition }
type StaticProviderRegistry map[string]ProviderProfile

func (r StaticProviderRegistry) Profile(platform string) (ProviderProfile, bool) {
	p, ok := r[platform]
	p.Credentials = append([]CredentialDefinition{}, p.Credentials...)
	return p, ok
}
func FixtureProfiles() ProviderRegistry {
	return StaticProviderRegistry{"fixture": {Credentials: []CredentialDefinition{{Scope: "app", Type: "client_secret", Purpose: "business", Required: true}, {Scope: "shop", Type: "shop_token", Purpose: "business", Required: true}, {Scope: "app", Type: "refresh_token", Purpose: "management"}, {Scope: "shop", Type: "refresh_token", Purpose: "management"}}}}
}
func (e *Engine) profile(platform string) (ProviderProfile, bool) {
	p, ok := e.profiles.Profile(platform)
	if !ok || len(p.Credentials) == 0 {
		return ProviderProfile{}, false
	}
	seen := map[string]bool{}
	required := false
	for _, d := range p.Credentials {
		key := d.Scope + "|" + d.Type
		if (d.Scope != "app" && d.Scope != "shop") || !id(d.Type) || (d.Purpose != "business" && d.Purpose != "management") || d.Purpose == "management" && d.Required || seen[key] {
			return ProviderProfile{}, false
		}
		seen[key] = true
		required = required || d.Required
	}
	if !required {
		return ProviderProfile{}, false
	}
	return p, true
}
func (e *Engine) validType(s *State, scope, app, typ string) bool {
	a, ok := s.Apps[app]
	if !ok {
		return true
	}
	p, ok := e.profile(a.Platform)
	if !ok {
		return false
	}
	for _, d := range p.Credentials {
		if d.Scope == scope && d.Type == typ {
			return true
		}
	}
	return false
}
