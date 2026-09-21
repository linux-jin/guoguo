package app

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"
)

const tvboxPlayPath = "/api/tvbox/play"

func tvboxDirectEnabled() bool {
	value := strings.TrimSpace(os.Getenv("JUKU_TVBOX_DIRECT"))
	return value != "0" && !strings.EqualFold(value, "false") && !strings.EqualFold(value, "no") && !strings.EqualFold(value, "off")
}

func (app *UIApp) handleTVBoxPlay(writer http.ResponseWriter, request *http.Request) {
	id, chapter, ok := app.authorizeEmby(writer, request)
	if !ok {
		return
	}
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	if request.Method == http.MethodHead {
		writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		writer.Header().Set("X-Juku-TVBox-Delivery", "deferred")
		return
	}
	fallback := tvboxPlaybackFallback(request, id, chapter)
	if !tvboxDirectEnabled() {
		tvboxRedirect(writer, fallback, "proxy")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 45*time.Second)
	defer cancel()
	task, downloadID, err := app.embyTask(ctx, id, chapter)
	if err != nil {
		tvboxRedirect(writer, fallback, "proxy")
		return
	}
	media, err := app.resolveMediaSession(request.Context(), ctx, task, downloadID, 0)
	if err != nil {
		tvboxRedirect(writer, fallback, "proxy")
		return
	}
	defer media.Close()
	if app.allowTVBoxDirect(ctx, media) {
		writer.Header().Set("Referrer-Policy", "no-referrer")
		tvboxRedirect(writer, media.media.URL, "direct")
		return
	}
	tvboxRedirect(writer, fallback, "proxy")
}

func tvboxRedirect(writer http.ResponseWriter, location, delivery string) {
	writer.Header().Set("Location", location)
	writer.Header().Set("X-Juku-TVBox-Delivery", delivery)
	writer.WriteHeader(http.StatusFound)
}

func tvboxPlaybackFallback(request *http.Request, id, chapter string) string {
	query := url.Values{"id": {id}, "chapter": {chapter}, "key": {request.URL.Query().Get("key")}, "tvbox_proxy": {"1"}}
	if account := request.URL.Query().Get("account"); account != "" {
		query.Set("account", account)
	}
	return "/api/emby/stream.m3u8?" + query.Encode()
}

func (app *UIApp) allowTVBoxDirect(ctx context.Context, media *playbackMediaSession) bool {
	if media == nil || media.local != "" || media.media.URL == "" || len(media.key) != 0 || len(media.media.HLSKey) != 0 || len(media.media.CENCKey) != 0 {
		return false
	}
	parsed, err := url.Parse(media.media.URL)
	if err != nil || parsed.User != nil || !isProviderHTTPMediaURL(parsed.String()) {
		return false
	}
	if media.plan.Player == "mp4" {
		app.inspectPlaybackMedia(ctx, media)
		if !media.info.MP4 || !media.info.Range {
			return false
		}
		return app.probeTVBoxAsset(ctx, media.media.URL, true)
	}
	if media.plan.Player != "hls" || len(media.media.Variants) > 0 {
		return false
	}
	return app.probeTVBoxHLS(ctx, media.media.URL)
}

func validTVBoxDirectURL(remote *url.URL) bool {
	if remote == nil || remote.Opaque != "" || remote.User != nil || remote.Hostname() == "" || remote.Scheme != "http" && remote.Scheme != "https" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(remote.Hostname(), "."))
	if address, err := netip.ParseAddr(host); err == nil {
		return publicImageAddress(address.String())
	}
	if len(host) > 253 || !strings.Contains(host, ".") || strings.ContainsAny(host, ":%\\") {
		return false
	}
	for _, suffix := range []string{"localhost", "local", "internal", "lan", "home", "invalid", "test", "example"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return false
		}
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if char != '-' && (char < 'a' || char > 'z') && (char < '0' || char > '9') {
				return false
			}
		}
	}
	return true
}

func (app *UIApp) tvboxProbeClient() (*http.Client, func()) {
	dialer := &net.Dialer{Timeout: 8 * time.Second, KeepAlive: 15 * time.Second}
	transport := &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   8 * time.Second,
		ResponseHeaderTimeout: 8 * time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("TVBox 直连地址无效")
		}
		var addresses []netip.Addr
		if literal, parseErr := netip.ParseAddr(strings.TrimSuffix(host, ".")); parseErr == nil {
			addresses = []netip.Addr{literal.Unmap()}
		} else {
			addresses, err = net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil || len(addresses) == 0 {
				return nil, errors.New("TVBox 直连域名解析失败")
			}
		}
		for _, candidate := range addresses {
			if !publicImageAddress(candidate.String()) {
				return nil, errors.New("TVBox 直连拒绝本机、内网或保留网络")
			}
		}
		var lastErr error
		for index, candidate := range addresses {
			if index >= 4 {
				break
			}
			connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.String(), port))
			if dialErr == nil {
				return connection, nil
			}
			lastErr = dialErr
		}
		if lastErr == nil {
			lastErr = errors.New("TVBox 直连没有可用公网地址")
		}
		return nil, lastErr
	}
	client := &http.Client{Transport: transport, CheckRedirect: func(request *http.Request, previous []*http.Request) error {
		if len(previous) >= 5 || !validTVBoxDirectURL(request.URL) {
			return http.ErrUseLastResponse
		}
		if len(previous) > 0 && previous[len(previous)-1].URL.Scheme == "https" && request.URL.Scheme != "https" {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	return client, transport.CloseIdleConnections
}

func (app *UIApp) probeTVBoxAsset(ctx context.Context, address string, mp4 bool) bool {
	client, closeClient := app.tvboxProbeClient()
	defer closeClient()
	return probeTVBoxAssetWithClient(ctx, client, address, mp4)
}

func probeTVBoxAssetWithClient(ctx context.Context, client *http.Client, address string, mp4 bool) bool {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	parsed, err := url.Parse(address)
	if err != nil || !validTVBoxDirectURL(parsed) {
		return false
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return false
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("Range", "bytes=0-0")
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	response.Body.Close()
	if !validTVBoxDirectURL(response.Request.URL) {
		return false
	}
	if mp4 {
		return response.StatusCode == http.StatusPartialContent && strings.HasPrefix(response.Header.Get("Content-Range"), "bytes 0-0/")
	}
	return response.StatusCode == http.StatusOK || response.StatusCode == http.StatusPartialContent
}

func (app *UIApp) probeTVBoxHLS(ctx context.Context, address string) bool {
	client, closeClient := app.tvboxProbeClient()
	defer closeClient()
	return probeTVBoxHLSWithClient(ctx, client, address)
}

func probeTVBoxHLSWithClient(ctx context.Context, client *http.Client, address string) bool {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	states := make(map[string]uint8)
	manifests := 0
	var check func(string, bool) bool
	check = func(current string, playlist bool) (success bool) {
		switch states[current] {
		case 1:
			return false
		case 2:
			return true
		}
		if ctx.Err() != nil || len(states) >= 256 {
			return false
		}
		parsed, err := url.Parse(current)
		if err != nil || !validTVBoxDirectURL(parsed) {
			return false
		}
		states[current] = 1
		defer func() {
			if success {
				states[current] = 2
			} else {
				delete(states, current)
			}
		}()
		if !playlist {
			return probeTVBoxAssetWithClient(ctx, client, current, false)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, current, nil)
		if err != nil {
			return false
		}
		request.Header.Set("Accept-Encoding", "identity")
		response, err := client.Do(request)
		if err != nil {
			return false
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 || !validTVBoxDirectURL(response.Request.URL) {
			return false
		}
		manifests++
		body, err := io.ReadAll(io.LimitReader(response.Body, 256*1024+1))
		if err != nil || len(body) > 256*1024 || manifests > 8 || !strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(string(body), "\ufeff")), "#EXTM3U") {
			return false
		}
		base := response.Request.URL
		resolve := func(reference string, childPlaylist bool) bool {
			child, err := url.Parse(reference)
			if err != nil {
				return false
			}
			resolved := base.ResolveReference(child)
			if base.Scheme == "https" && resolved.Scheme != "https" || !validTVBoxDirectURL(resolved) {
				return false
			}
			return check(resolved.String(), childPlaylist)
		}
		nextPlaylist, assets := false, 0
		for _, raw := range strings.Split(string(body), "\n") {
			line := strings.TrimSpace(raw)
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "#EXT-X-PART:") || strings.HasPrefix(line, "#EXT-X-PRELOAD-HINT:") || strings.HasPrefix(line, "#EXT-X-RENDITION-REPORT:") {
				return false
			}
			if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
				nextPlaylist = true
				continue
			}
			if !strings.HasPrefix(line, "#") {
				assets++
				if !resolve(line, nextPlaylist) {
					return false
				}
				nextPlaylist = false
				continue
			}
			for _, tag := range []string{"#EXT-X-MEDIA:", "#EXT-X-I-FRAME-STREAM-INF:", "#EXT-X-KEY:", "#EXT-X-SESSION-KEY:", "#EXT-X-MAP:"} {
				if !strings.HasPrefix(line, tag) {
					continue
				}
				for _, match := range hlsURIAttribute.FindAllStringSubmatch(line, -1) {
					if !resolve(match[1], tag == "#EXT-X-MEDIA:" || tag == "#EXT-X-I-FRAME-STREAM-INF:") {
						return false
					}
				}
			}
		}
		return assets > 0
	}
	return check(address, true)
}
