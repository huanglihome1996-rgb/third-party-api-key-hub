package callback

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type tokenSinkFixture struct {
	appID string
	token Token
}

func (s *tokenSinkFixture) Save(_ context.Context, appID string, token Token) error {
	s.appID, s.token = appID, token
	return nil
}

type exchangerFixture struct {
	appID, secret, code string
	calls               int
}

type failingSink struct{}

func (failingSink) Save(context.Context, string, Token) error { return context.DeadlineExceeded }

type failingExchanger struct{}

func (failingExchanger) Exchange(context.Context, AppConfig, string) (Token, error) {
	return Token{}, context.DeadlineExceeded
}

func (e *exchangerFixture) Exchange(_ context.Context, app AppConfig, code string) (Token, error) {
	e.calls++
	e.appID, e.secret, e.code = app.ID, app.AppSecret, code
	return Token{AccessToken: "access-secret", RefreshToken: "refresh-secret", ExpiresIn: 3600}, nil
}

func TestHandlerRoutesEachStateToItsOwnAppWithoutLeakingSecrets(t *testing.T) {
	states := NewStateStore(timeForTest())
	exchanger := &exchangerFixture{}
	sink := &tokenSinkFixture{}
	h, err := NewHandler(HandlerConfig{
		Apps: map[string]AppConfig{
			"app-a": {ID: "app-a", AppKey: "key-a", AppSecret: "secret-a", AuthorizeURL: "https://auth.example/a", TokenURL: "https://token.example/a", RedirectURL: "https://callback.example/a"},
			"app-b": {ID: "app-b", AppKey: "key-b", AppSecret: "secret-b", AuthorizeURL: "https://auth.example/b", TokenURL: "https://token.example/b", RedirectURL: "https://callback.example/b"},
		},
		States:        states,
		Exchanger:     exchanger,
		Sink:          sink,
		AuthorizePath: "/authorize",
		CallbackPath:  "/callbacks/tiktok-shop",
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	defer ts.Close()

	var auth struct {
		AuthorizationURL string `json:"authorization_url"`
	}
	resp, err := http.Get(ts.URL + "/authorize?app_id=app-b")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authorize status = %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&auth); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	u, err := url.Parse(auth.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	state := u.Query().Get("state")
	if u.Query().Get("app_key") != "key-b" || state == "" {
		t.Fatalf("bad authorization URL: %s", auth.AuthorizationURL)
	}

	resp, err = http.Get(ts.URL + "/callbacks/tiktok-shop?state=" + url.QueryEscape(state) + "&code=one-time-code")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "授权处理完成") {
		t.Fatalf("callback response = %d %q", resp.StatusCode, body)
	}
	for _, secret := range []string{"secret-a", "secret-b", "access-secret", "refresh-secret", "one-time-code"} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("response leaked %q", secret)
		}
	}
	if exchanger.appID != "app-b" || exchanger.secret != "secret-b" || sink.appID != "app-b" {
		t.Fatalf("wrong app binding: exchanger=%#v sink=%#v", exchanger, sink)
	}

	resp, err = http.Get(ts.URL + "/callbacks/tiktok-shop?state=" + url.QueryEscape(state) + "&code=one-time-code")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK || exchanger.calls != 1 {
		t.Fatalf("state replay accepted or exchanged twice: status=%d calls=%d", resp.StatusCode, exchanger.calls)
	}
}

func TestHandlerRejectsUnknownAppAndMissingCode(t *testing.T) {
	h, err := NewHandler(HandlerConfig{Apps: map[string]AppConfig{"a": {ID: "a", AppKey: "k", AppSecret: "s", AuthorizeURL: "https://a", TokenURL: "https://t", RedirectURL: "https://r"}}, States: NewStateStore(timeForTest()), Exchanger: &exchangerFixture{}, Sink: &tokenSinkFixture{}, AuthorizePath: "/authorize", CallbackPath: "/callbacks/tiktok-shop"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	defer ts.Close()
	for path := range map[string]bool{"/authorize?app_id=unknown": true, "/callbacks/tiktok-shop?state=unknown": true} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode < 400 {
			t.Fatalf("%s returned %d", path, resp.StatusCode)
		}
	}
}

func TestHTTPTokenExchangerSendsTheSelectedAppCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("app_key") != "key-b" || q.Get("app_secret") != "secret-b" || q.Get("auth_code") != "code-b" || q.Get("grant_type") != "authorized_code" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"access_token": "a", "refresh_token": "r", "expires_in": 3600}})
	}))
	defer server.Close()
	token, err := (HTTPTokenExchanger{Client: server.Client()}).Exchange(context.Background(), AppConfig{ID: "app-b", AppKey: "key-b", AppSecret: "secret-b", TokenURL: server.URL}, "code-b")
	if err != nil || token.AccessToken != "a" || token.RefreshToken != "r" {
		t.Fatalf("exchange = %#v, %v", token, err)
	}
}

func TestHandlerHealthAndFailurePaths(t *testing.T) {
	app := AppConfig{ID: "a", AppKey: "k", AppSecret: "s", AuthorizeURL: "https://a", TokenURL: "https://t", RedirectURL: "https://r"}
	state := NewStateStore(timeForTest())
	h, err := NewHandler(HandlerConfig{Apps: map[string]AppConfig{"a": app}, States: state, Exchanger: failingExchanger{}, Sink: failingSink{}, AuthorizePath: "/authorize", CallbackPath: "/callback"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	defer ts.Close()
	for _, path := range []string{"/healthz", "/missing"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if path == "/healthz" && resp.StatusCode != http.StatusOK {
			t.Fatalf("health status = %d", resp.StatusCode)
		}
	}
	request, err := http.NewRequest(http.MethodPost, ts.URL+"/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("health POST status = %d", resp.StatusCode)
	}
	stateValue, err := state.Issue("a")
	if err != nil {
		t.Fatal(err)
	}
	resp, err = http.Get(ts.URL + "/callback?state=" + url.QueryEscape(stateValue) + "&code=x")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("exchange failure status = %d", resp.StatusCode)
	}
}

func TestHandlerReturnsServerErrorWhenSinkFails(t *testing.T) {
	state := NewStateStore(timeForTest())
	h, err := NewHandler(HandlerConfig{Apps: map[string]AppConfig{"a": {ID: "a", AppKey: "k", AppSecret: "s", AuthorizeURL: "https://a", TokenURL: "https://t", RedirectURL: "https://r"}}, States: state, Exchanger: &exchangerFixture{}, Sink: failingSink{}, AuthorizePath: "/authorize", CallbackPath: "/callback"})
	if err != nil {
		t.Fatal(err)
	}
	value, err := state.Issue("a")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/callback?state=" + url.QueryEscape(value) + "&code=x")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("sink failure status = %d", resp.StatusCode)
	}
}

func TestNewHandlerRejectsInvalidDependencies(t *testing.T) {
	if _, err := NewHandler(HandlerConfig{}); err == nil {
		t.Fatal("accepted empty handler config")
	}
}

func TestHTTPTokenExchangerRejectsInvalidResponses(t *testing.T) {
	for name, body := range map[string]string{
		"malformed":      "not-json",
		"provider-error": `{"code":12,"data":{"access_token":""}}`,
		"missing-token":  `{"code":0,"data":{}}`,
	} {
		t.Run(name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if name == "provider-error" {
					w.WriteHeader(http.StatusBadGateway)
				}
				_, _ = io.WriteString(w, body)
			}))
			defer ts.Close()
			if _, err := (HTTPTokenExchanger{Client: ts.Client()}).Exchange(context.Background(), AppConfig{AppKey: "k", AppSecret: "s", TokenURL: ts.URL}, "code"); err == nil {
				t.Fatal("accepted invalid provider response")
			}
		})
	}
}

func timeForTest() time.Duration { return time.Hour }
