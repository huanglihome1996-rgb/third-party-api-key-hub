package callback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

type TokenSink interface {
	Save(context.Context, string, Token) error
}

type TokenExchanger interface {
	Exchange(context.Context, AppConfig, string) (Token, error)
}

type HandlerConfig struct {
	Apps          map[string]AppConfig
	States        *StateStore
	Exchanger     TokenExchanger
	Sink          TokenSink
	AuthorizePath string
	CallbackPath  string
}

func NewHandler(cfg HandlerConfig) (http.Handler, error) {
	if len(cfg.Apps) == 0 || cfg.States == nil || cfg.Exchanger == nil || cfg.Sink == nil {
		return nil, errors.New("callback handler requires apps, state store, exchanger and sink")
	}
	if cfg.AuthorizePath == "" {
		cfg.AuthorizePath = "/authorize"
	}
	if cfg.CallbackPath == "" {
		cfg.CallbackPath = "/callbacks/tiktok-shop"
	}
	if !validPath(cfg.AuthorizePath) || !validPath(cfg.CallbackPath) || cfg.AuthorizePath == cfg.CallbackPath {
		return nil, errors.New("callback paths must be distinct absolute paths")
	}
	for id, app := range cfg.Apps {
		if id == "" || app.ID != id || app.AppKey == "" || app.AppSecret == "" || app.AuthorizeURL == "" || app.TokenURL == "" || app.RedirectURL == "" {
			return nil, fmt.Errorf("invalid callback app %q", id)
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		switch r.URL.Path {
		case "/healthz":
			if r.Method != http.MethodGet {
				writeFailure(w, http.StatusMethodNotAllowed)
				return
			}
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		case cfg.AuthorizePath:
			handleAuthorize(w, r, cfg)
		case cfg.CallbackPath:
			handleCallback(w, r, cfg)
		default:
			writeFailure(w, http.StatusNotFound)
		}
	}), nil
}

func handleAuthorize(w http.ResponseWriter, r *http.Request, cfg HandlerConfig) {
	if r.Method != http.MethodGet || r.URL.RawQuery == "" {
		writeFailure(w, http.StatusBadRequest)
		return
	}
	appID, ok := singleQuery(r.URL.Query(), "app_id")
	app, exists := cfg.Apps[appID]
	if !ok || !exists {
		writeFailure(w, http.StatusNotFound)
		return
	}
	state, err := cfg.States.Issue(appID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError)
		return
	}
	u, err := url.Parse(app.AuthorizeURL)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError)
		return
	}
	q := u.Query()
	for key, value := range app.AuthorizeParams {
		q.Set(key, value)
	}
	if q.Get("app_key") == "" && q.Get("service_id") == "" {
		q.Set("app_key", app.AppKey)
	}
	q.Set("state", state)
	q.Set("redirect_uri", app.RedirectURL)
	u.RawQuery = q.Encode()
	writeJSON(w, http.StatusOK, map[string]string{"app_id": app.ID, "authorization_url": u.String()})
}

func handleCallback(w http.ResponseWriter, r *http.Request, cfg HandlerConfig) {
	if r.Method != http.MethodGet {
		writeFailure(w, http.StatusMethodNotAllowed)
		return
	}
	state, stateOK := singleQuery(r.URL.Query(), "state")
	appID, validState := cfg.States.Consume(state)
	if !stateOK || !validState {
		writeCallbackResult(w, http.StatusBadRequest, false)
		return
	}
	code, codeOK := singleQuery(r.URL.Query(), "code")
	if !codeOK || code == "" || r.URL.Query().Get("error") != "" {
		writeCallbackResult(w, http.StatusBadRequest, false)
		return
	}
	app, ok := cfg.Apps[appID]
	if !ok {
		writeCallbackResult(w, http.StatusBadRequest, false)
		return
	}
	token, err := cfg.Exchanger.Exchange(r.Context(), app, code)
	if err != nil || token.AccessToken == "" {
		writeCallbackResult(w, http.StatusBadGateway, false)
		return
	}
	if err := cfg.Sink.Save(r.Context(), appID, token); err != nil {
		writeCallbackResult(w, http.StatusInternalServerError, false)
		return
	}
	writeCallbackResult(w, http.StatusOK, true)
}

func singleQuery(values url.Values, key string) (string, bool) {
	items, ok := values[key]
	if !ok || len(items) != 1 || items[0] == "" {
		return "", false
	}
	return items[0], true
}

func writeCallbackResult(w http.ResponseWriter, status int, success bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if success {
		_, _ = io.WriteString(w, "<!doctype html><title>授权处理完成</title><p>授权处理完成，请返回原系统查看状态。</p>")
		return
	}
	_, _ = io.WriteString(w, "<!doctype html><title>授权处理失败</title><p>授权处理失败，请返回原系统查看状态。</p>")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeFailure(w http.ResponseWriter, status int) {
	writeJSON(w, status, map[string]string{"error": "request_invalid"})
}

// HTTPTokenExchanger implements TikTok Shop's server-to-server authorization
// code exchange. The app secret is used only in this outbound request.
type HTTPTokenExchanger struct {
	Client  *http.Client
	MaxBody int64
}

func (e HTTPTokenExchanger) Exchange(ctx context.Context, app AppConfig, code string) (Token, error) {
	u, err := url.Parse(app.TokenURL)
	if err != nil {
		return Token{}, err
	}
	q := u.Query()
	q.Set("app_key", app.AppKey)
	q.Set("app_secret", app.AppSecret)
	q.Set("auth_code", code)
	q.Set("grant_type", "authorized_code")
	u.RawQuery = q.Encode()
	client := e.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	clientCopy := *client
	if clientCopy.CheckRedirect == nil {
		clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	}
	client = &clientCopy
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Token{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return Token{}, err
	}
	defer resp.Body.Close()
	maxBody := e.MaxBody
	if maxBody <= 0 {
		maxBody = 1 << 20
	}
	var out tokenEnvelope
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&out); err != nil {
		return Token{}, err
	}
	if resp.StatusCode/100 != 2 || out.Code != 0 || out.Data.AccessToken == "" {
		return Token{}, fmt.Errorf("token exchange failed")
	}
	return out.Data, nil
}

type tokenEnvelope struct {
	Code int   `json:"code"`
	Data Token `json:"data"`
}
