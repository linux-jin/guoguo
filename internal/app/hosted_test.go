package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHostedBasicAuthAndExemptSignedRoutes(t *testing.T) {
	t.Setenv("JUKU_BASIC_AUTH_USER", "alice")
	t.Setenv("JUKU_BASIC_AUTH_PASS", "secret")
	handler := wrapHostedBasicAuth(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("ok-body"))
	}))

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d", unauthorized.Code)
	}

	authenticatedRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	authenticatedRequest.SetBasicAuth("alice", "secret")
	authenticated := httptest.NewRecorder()
	handler.ServeHTTP(authenticated, authenticatedRequest)
	body, _ := io.ReadAll(authenticated.Result().Body)
	if authenticated.Code != http.StatusOK || string(body) != "ok-body" {
		t.Fatalf("authenticated status=%d body=%q", authenticated.Code, body)
	}

	for _, path := range []string{"/healthz", tvboxAPIPath, tvboxConfigPath, tvboxPlayPath, tvboxPlayM3U8Path, "/api/tvbox/secret-token/vod", "/api/tvbox/play/hongguo:1/hongguo:1:1/abc/index.m3u8", "/api/emby/stream.m3u8", "/api/emby/media/session/file.mp4"} {
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, httptest.NewRequest(http.MethodGet, path, nil))
		if result.Code != http.StatusOK {
			t.Fatalf("exempt path %s status=%d", path, result.Code)
		}
	}
}

func TestRequireHostedAuth(t *testing.T) {
	t.Setenv("RENDER", "true")
	t.Setenv("JUKU_BASIC_AUTH_USER", "")
	t.Setenv("JUKU_BASIC_AUTH_PASS", "")
	if err := requireHostedAuth(); err == nil {
		t.Fatal("expected missing Render auth error")
	}
	t.Setenv("JUKU_BASIC_AUTH_USER", "alice")
	t.Setenv("JUKU_BASIC_AUTH_PASS", "secret")
	if err := requireHostedAuth(); err != nil {
		t.Fatal(err)
	}
}

func TestHealthz(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		writer := httptest.NewRecorder()
		handleHealthz(writer, httptest.NewRequest(method, "/healthz", nil))
		if writer.Code != http.StatusOK || method == http.MethodHead && writer.Body.Len() != 0 {
			t.Fatalf("method=%s status=%d body=%q", method, writer.Code, writer.Body.String())
		}
	}
}
