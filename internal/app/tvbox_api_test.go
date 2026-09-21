package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func tvboxFixtureApp(t *testing.T) *UIApp {
	t.Helper()
	return &UIApp{cfg: Config{dataDir: t.TempDir()}, libraryAttempted: true, dramas: []Drama{
		{ID: "hongguo:100", Source: sourceHongguo, Title: "红果测试剧", Desc: "一部短剧", CategoryName: "短剧", TotalEpisode: 12, CoverURL: "https://img.example/100.jpg"},
		{ID: "huangdou:200", Source: sourceHuangdou, Title: "黄豆另一部", CategoryName: "都市"},
	}}
}

func TestTVBoxListAndTokenGuard(t *testing.T) {
	t.Setenv("JUKU_TVBOX_ENABLED", "1")
	t.Setenv("JUKU_TVBOX_TOKEN", "secret-token")
	app := tvboxFixtureApp(t)
	request := httptest.NewRequest(http.MethodGet, "/api.php/provide/vod/?ac=list&wd=%E7%BA%A2%E6%9E%9C&token=secret-token", nil)
	writer := httptest.NewRecorder()
	app.routes().ServeHTTP(writer, request)
	if writer.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", writer.Code, writer.Body.String())
	}
	var response tvboxResponse
	if err := json.Unmarshal(writer.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code != 1 || response.Total != 1 || len(response.List) != 1 || response.List[0].VodID != "hongguo:100" || response.List[0].VodPic == "" {
		t.Fatalf("unexpected response: %+v", response)
	}
	unauthorized := httptest.NewRecorder()
	app.routes().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api.php/provide/vod/?ac=list", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d", unauthorized.Code)
	}
}

func TestTVBoxHugePageDoesNotOverflow(t *testing.T) {
	t.Setenv("JUKU_TVBOX_ENABLED", "1")
	t.Setenv("JUKU_TVBOX_TOKEN", "secret-token")
	app := tvboxFixtureApp(t)
	writer := httptest.NewRecorder()
	app.routes().ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/api.php/provide/vod/?ac=list&pg=9223372036854775807&token=secret-token", nil))
	if writer.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", writer.Code, writer.Body.String())
	}
	var response tvboxResponse
	if err := json.Unmarshal(writer.Body.Bytes(), &response); err != nil || len(response.List) != 0 || response.Total != 2 {
		t.Fatalf("unexpected huge-page response: err=%v response=%+v", err, response)
	}
}

func TestTVBoxConfigIncludesTokenizedAPI(t *testing.T) {
	t.Setenv("JUKU_TVBOX_ENABLED", "true")
	t.Setenv("JUKU_TVBOX_TOKEN", "secret-token")
	app := tvboxFixtureApp(t)
	request := httptest.NewRequest(http.MethodGet, "https://example.test/api/tvbox/config?token=secret-token", nil)
	writer := httptest.NewRecorder()
	app.routes().ServeHTTP(writer, request)
	if writer.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", writer.Code, writer.Body.String())
	}
	var response struct {
		Sites []struct {
			API string `json:"api"`
		} `json:"sites"`
	}
	if err := json.Unmarshal(writer.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Sites) != 1 {
		t.Fatalf("sites=%d", len(response.Sites))
	}
	parsed, err := url.Parse(response.Sites[0].API)
	if err != nil || parsed.Host != "example.test" || parsed.Query().Get("token") != "secret-token" || !strings.HasSuffix(parsed.Path, "/api.php/provide/vod/") {
		t.Fatalf("unexpected api url=%q err=%v", response.Sites[0].API, err)
	}
}

func TestTVBoxDisabledIsNotFound(t *testing.T) {
	t.Setenv("JUKU_TVBOX_ENABLED", "0")
	t.Setenv("JUKU_TVBOX_TOKEN", "")
	app := tvboxFixtureApp(t)
	writer := httptest.NewRecorder()
	app.routes().ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/api.php/provide/vod/?ac=list", nil))
	if writer.Code != http.StatusNotFound {
		t.Fatalf("status=%d", writer.Code)
	}
}

func TestTVBoxDetailUsesAutomaticPlayGateway(t *testing.T) {
	t.Setenv("JUKU_TVBOX_ENABLED", "1")
	t.Setenv("JUKU_TVBOX_TOKEN", "catalog-token")
	id := "hongguo:7000000000000000001"
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		t.Fatal("cached TVBox detail must not access upstream", request.URL)
		return nil, nil
	})
	d.hongguoClient().details["7000000000000000001"] = hongguoDetailEntry{Drama: Drama{ID: id, Title: "TVBox 详情"}, Chapters: []Chapter{{ID: id + ":8000000000000000001", Source: sourceHongguo, Title: "第一集", VideoURL: "hongguo-cenc://8000000000000000001", CurrentEpisode: rawEpisode(1)}}, ExpiresAt: time.Now().Add(time.Minute)}
	app := &UIApp{cfg: d.cfg, downloader: d, libraryAttempted: true, dramas: []Drama{{ID: id, Source: sourceHongguo, Title: "TVBox 详情"}}}
	writer := httptest.NewRecorder()
	app.routes().ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "https://library.test/api.php/provide/vod/?ac=detail&ids="+url.QueryEscape(id)+"&token=catalog-token", nil))
	if writer.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", writer.Code, writer.Body.String())
	}
	var response tvboxResponse
	if err := json.Unmarshal(writer.Body.Bytes(), &response); err != nil || len(response.List) != 1 {
		t.Fatalf("invalid detail: %v %+v", err, response)
	}
	play := response.List[0].VodPlayURL
	if !strings.Contains(play, "/api/tvbox/play?") || strings.Contains(play, "/api/emby/stream.m3u8") || strings.Contains(play, "catalog-token") {
		t.Fatalf("detail did not use the signed automatic gateway: %s", play)
	}
}

func TestTVBoxDirectHLSWithoutBrowserCORSOrReferer(t *testing.T) {
	var playlistCalls, segmentCalls atomic.Int32
	client := &http.Client{Transport: rankingTransport(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Referer") != "" || request.Header.Get("Cookie") != "" || request.Header.Get("Authorization") != "" {
			t.Fatalf("direct probe leaked credentials: %+v", request.Header)
		}
		response := &http.Response{Header: http.Header{}, Request: request}
		switch request.URL.Path {
		case "/index.m3u8":
			playlistCalls.Add(1)
			response.StatusCode = http.StatusOK
			response.Header.Set("Content-Type", "application/vnd.apple.mpegurl")
			response.Body = io.NopCloser(strings.NewReader("#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXTINF:6,\nsegment.ts\n#EXT-X-ENDLIST\n"))
		case "/segment.ts":
			segmentCalls.Add(1)
			response.StatusCode = http.StatusPartialContent
			response.Header.Set("Content-Range", "bytes 0-0/188")
			response.Body = io.NopCloser(strings.NewReader("G"))
		default:
			response.StatusCode = http.StatusNotFound
			response.Body = http.NoBody
		}
		return response, nil
	})}
	address := "https://media.example.com/index.m3u8"
	if !probeTVBoxHLSWithClient(context.Background(), client, address) || playlistCalls.Load() == 0 || segmentCalls.Load() == 0 {
		t.Fatal("public single-variant HLS was not accepted for TVBox direct playback")
	}
	cfg := defaultConfig()
	cfg.dataDir = t.TempDir()
	app := &UIApp{cfg: cfg, downloader: NewDownloader(cfg)}
	media := &playbackMediaSession{ctx: context.Background(), cancel: func() {}, media: providerMedia{URL: address, HLSKey: make([]byte, 16)}, plan: playbackMediaPlan{Player: "hls"}}
	if app.allowTVBoxDirect(context.Background(), media) {
		t.Fatal("server-held HLS key was exposed as direct playback")
	}
	media.media.HLSKey = nil
	media.media.Variants = []providerMedia{{Quality: 1080}}
	if app.allowTVBoxDirect(context.Background(), media) {
		t.Fatal("multi-variant HLS was exposed without a stable selected source URL")
	}
}

func TestTVBoxDirectProbeRejectsRefererProtectedPrivateAndCyclicHLS(t *testing.T) {
	protected := &http.Client{Transport: rankingTransport(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{}, Body: http.NoBody, Request: request}, nil
	})}
	if probeTVBoxHLSWithClient(context.Background(), protected, "https://media.example.com/index.m3u8") {
		t.Fatal("Referer-protected HLS must fall back to the server proxy")
	}
	if validTVBoxDirectURL(mustURL(t, "http://127.0.0.1/private")) || validTVBoxDirectURL(mustURL(t, "http://169.254.169.254/latest/meta-data")) || validTVBoxDirectURL(mustURL(t, "https://service.internal/media")) {
		t.Fatal("private or internal direct URL accepted")
	}
	cyclic := &http.Client{Transport: rankingTransport(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1\nindex.m3u8\n")), Request: request}, nil
	})}
	if probeTVBoxHLSWithClient(context.Background(), cyclic, "https://media.example.com/index.m3u8") {
		t.Fatal("cyclic HLS playlist accepted as directly playable")
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestTVBoxPlayCanForceProxyAndRejectTampering(t *testing.T) {
	t.Setenv("JUKU_TVBOX_DIRECT", "0")
	app := &UIApp{cfg: Config{dataDir: t.TempDir()}}
	key, err := app.embySigningKey(true)
	if err != nil {
		t.Fatal(err)
	}
	id, chapter := "hongguo:1", "hongguo:1:1"
	query := url.Values{"id": {id}, "chapter": {chapter}, "key": {embyToken(key, id, chapter)}}
	writer := httptest.NewRecorder()
	app.routes().ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "http://localhost"+tvboxPlayPath+"?"+query.Encode(), nil))
	if writer.Code != http.StatusFound || writer.Header().Get("X-Juku-TVBox-Delivery") != "proxy" || !strings.HasPrefix(writer.Header().Get("Location"), "/api/emby/stream.m3u8?") || !strings.Contains(writer.Header().Get("Location"), "tvbox_proxy=1") {
		t.Fatalf("proxy fallback failed: %d %q %q", writer.Code, writer.Header().Get("Location"), writer.Header().Get("X-Juku-TVBox-Delivery"))
	}
	query.Set("chapter", "hongguo:1:2")
	forged := httptest.NewRecorder()
	app.routes().ServeHTTP(forged, httptest.NewRequest(http.MethodGet, "http://localhost"+tvboxPlayPath+"?"+query.Encode(), nil))
	if forged.Code != http.StatusForbidden {
		t.Fatalf("tampered signed episode accepted: %d", forged.Code)
	}
}
