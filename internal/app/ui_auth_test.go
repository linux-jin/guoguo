package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWrapBasicAuth(t *testing.T) {
	t.Setenv("JUKU_BASIC_AUTH_USER", "alice")
	t.Setenv("JUKU_BASIC_AUTH_PASS", "secret")
	handler := wrapBasicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok-body"))
	}))

	unauth := httptest.NewRecorder()
	handler.ServeHTTP(unauth, httptest.NewRequest(http.MethodGet, "/", nil))
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status=%d", unauth.Code)
	}

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("healthz status=%d", health.Code)
	}

	authReq := httptest.NewRequest(http.MethodGet, "/", nil)
	authReq.SetBasicAuth("alice", "secret")
	auth := httptest.NewRecorder()
	handler.ServeHTTP(auth, authReq)
	body, _ := io.ReadAll(auth.Result().Body)
	if auth.Code != http.StatusOK || string(body) != "ok-body" {
		t.Fatalf("auth status=%d body=%q", auth.Code, body)
	}
}

func TestRequireHostedAuth(t *testing.T) {
	t.Setenv("RENDER", "true")
	t.Setenv("JUKU_BASIC_AUTH_USER", "")
	t.Setenv("JUKU_BASIC_AUTH_PASS", "")
	if err := requireHostedAuth(); err == nil {
		t.Fatal("expected auth error on Render")
	}
	t.Setenv("JUKU_BASIC_AUTH_USER", "alice")
	t.Setenv("JUKU_BASIC_AUTH_PASS", "secret")
	if err := requireHostedAuth(); err != nil {
		t.Fatal(err)
	}
}

func TestWatchOnlyMode(t *testing.T) {
	t.Setenv("RENDER", "")
	t.Setenv("RENDER_SERVICE_ID", "")
	t.Setenv("JUKU_WATCH_ONLY", "1")
	if !watchOnlyMode() {
		t.Fatal("expected watch-only")
	}
	rec := httptest.NewRecorder()
	if !rejectWatchOnly(rec) || rec.Code != http.StatusForbidden {
		t.Fatalf("reject status=%d", rec.Code)
	}
}

func TestWatchOnlyDefaultOnRender(t *testing.T) {
	t.Setenv("RENDER", "true")
	t.Setenv("JUKU_WATCH_ONLY", "")
	if !watchOnlyMode() {
		t.Fatal("Render should default to watch-only")
	}
}
