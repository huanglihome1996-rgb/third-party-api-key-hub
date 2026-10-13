package callback

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
)

// AppConfig is the resolved, in-memory configuration for one installed App.
// AppSecret is deliberately absent from the JSON shape below; it can only be
// resolved from an environment variable or a restricted file at startup.
type AppConfig struct {
	ID              string
	AppKey          string
	AppSecret       string
	AuthorizeURL    string
	TokenURL        string
	RedirectURL     string
	AuthorizeParams map[string]string
}

type Config struct {
	ListenAddr        string
	AuthorizePath     string
	CallbackPath      string
	StateStorePath    string
	TokenStorePath    string
	TokenStoreKeyFile string
	Apps              map[string]AppConfig
}

type fileConfig struct {
	ListenAddr        string                   `json:"listen_addr"`
	AuthorizePath     string                   `json:"authorize_path"`
	CallbackPath      string                   `json:"callback_path"`
	StateStorePath    string                   `json:"state_store_path"`
	TokenStorePath    string                   `json:"token_store_path"`
	TokenStoreKeyFile string                   `json:"token_store_key_file"`
	Apps              map[string]fileAppConfig `json:"apps"`
}

type fileAppConfig struct {
	AppKey          string            `json:"app_key"`
	AppSecret       string            `json:"app_secret"`
	AppSecretEnv    string            `json:"app_secret_env"`
	AppSecretFile   string            `json:"app_secret_file"`
	AuthorizeURL    string            `json:"authorize_url"`
	TokenURL        string            `json:"token_url"`
	RedirectURL     string            `json:"redirect_url"`
	AuthorizeParams map[string]string `json:"authorize_params"`
}

// LoadConfig parses the callback configuration. The env argument is useful for
// tests; nil reads the process environment. Secrets are never accepted inline.
func LoadConfig(r io.Reader, env map[string]string) (Config, error) {
	var in fileConfig
	dec := json.NewDecoder(io.LimitReader(r, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return Config{}, fmt.Errorf("invalid callback config: %w", err)
	}
	if len(in.Apps) == 0 {
		return Config{}, errors.New("callback config must define at least one app")
	}
	listen := in.ListenAddr
	if listen == "" {
		listen = "127.0.0.1:9080"
	}
	authorizePath := in.AuthorizePath
	if authorizePath == "" {
		authorizePath = "/authorize"
	}
	callbackPath := in.CallbackPath
	if callbackPath == "" {
		callbackPath = "/callbacks/tiktok-shop"
	}
	if !validPath(authorizePath) || !validPath(callbackPath) || authorizePath == callbackPath {
		return Config{}, errors.New("authorize_path and callback_path must be distinct absolute paths")
	}

	apps := make(map[string]AppConfig, len(in.Apps))
	for id, raw := range in.Apps {
		if !validID(id) || raw.AppKey == "" || raw.AppSecret != "" {
			return Config{}, fmt.Errorf("invalid app configuration for %q", id)
		}
		secret, err := resolveSecret(raw, env)
		if err != nil {
			return Config{}, fmt.Errorf("app %q: %w", id, err)
		}
		for name, value := range map[string]string{"authorize_url": raw.AuthorizeURL, "token_url": raw.TokenURL, "redirect_url": raw.RedirectURL} {
			if err := validateURL(value, name); err != nil {
				return Config{}, fmt.Errorf("app %q: %w", id, err)
			}
		}
		params := make(map[string]string, len(raw.AuthorizeParams))
		for key, value := range raw.AuthorizeParams {
			if key == "state" || key == "redirect_uri" || key == "app_secret" || strings.TrimSpace(value) == "" {
				return Config{}, fmt.Errorf("app %q: invalid authorize_params", id)
			}
			params[key] = value
		}
		apps[id] = AppConfig{ID: id, AppKey: raw.AppKey, AppSecret: secret, AuthorizeURL: raw.AuthorizeURL, TokenURL: raw.TokenURL, RedirectURL: raw.RedirectURL, AuthorizeParams: params}
	}
	return Config{ListenAddr: listen, AuthorizePath: authorizePath, CallbackPath: callbackPath, StateStorePath: in.StateStorePath, TokenStorePath: in.TokenStorePath, TokenStoreKeyFile: in.TokenStoreKeyFile, Apps: apps}, nil
}

func resolveSecret(raw fileAppConfig, env map[string]string) (string, error) {
	if (raw.AppSecretEnv == "") == (raw.AppSecretFile == "") {
		return "", errors.New("set exactly one of app_secret_env or app_secret_file")
	}
	if raw.AppSecretEnv != "" {
		var value string
		var ok bool
		if env == nil {
			value, ok = os.LookupEnv(raw.AppSecretEnv)
		} else {
			value, ok = env[raw.AppSecretEnv]
		}
		if !ok || strings.TrimSpace(value) == "" {
			return "", errors.New("configured secret environment variable is empty")
		}
		return value, nil
	}
	info, err := os.Stat(raw.AppSecretFile)
	if err != nil {
		return "", fmt.Errorf("read secret file: %w", err)
	}
	if info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return "", errors.New("secret file must be a regular file with mode 0600 or stricter")
	}
	b, err := os.ReadFile(raw.AppSecretFile)
	if err != nil {
		return "", fmt.Errorf("read secret file: %w", err)
	}
	value := strings.TrimSpace(string(b))
	if value == "" {
		return "", errors.New("secret file is empty")
	}
	return value, nil
}

func validateURL(value, name string) error {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("%s must be an absolute http(s) URL", name)
	}
	return nil
}

func validPath(value string) bool {
	return strings.HasPrefix(value, "/") && !strings.Contains(value, "?") && !strings.Contains(value, "#")
}

func validID(value string) bool {
	return value != "" && !strings.ContainsAny(value, "|/\\?&#\t\r\n ")
}
