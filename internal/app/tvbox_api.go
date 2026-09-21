package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// TVBox compatibility uses the widely supported MacCMS/Apple CMS JSON shape.
// It is intentionally opt-in because it exposes catalog metadata and signed
// playback URLs to a third-party client.
const (
	tvboxAPIPath        = "/api.php/provide/vod/"
	tvboxAPIPathNoSlash = "/api.php/provide/vod"
	tvboxConfigPath     = "/api/tvbox/config"
	tvboxCoverPath      = "/api/tvbox/cover"
)

type tvboxResponse struct {
	Code      int          `json:"code"`
	Msg       string       `json:"msg"`
	Page      int          `json:"page"`
	PageCount int          `json:"pagecount"`
	Limit     string       `json:"limit"`
	Total     int          `json:"total"`
	List      []tvboxVod   `json:"list"`
	Class     []tvboxClass `json:"class,omitempty"`
}

type tvboxClass struct {
	TypeID   string `json:"type_id"`
	TypeName string `json:"type_name"`
}

type tvboxVod struct {
	VodID       string `json:"vod_id"`
	VodName     string `json:"vod_name"`
	TypeID      string `json:"type_id"`
	TypeName    string `json:"type_name"`
	VodPic      string `json:"vod_pic,omitempty"`
	VodRemarks  string `json:"vod_remarks,omitempty"`
	VodYear     string `json:"vod_year,omitempty"`
	VodArea     string `json:"vod_area,omitempty"`
	VodLang     string `json:"vod_lang,omitempty"`
	VodActor    string `json:"vod_actor,omitempty"`
	VodDirector string `json:"vod_director,omitempty"`
	VodContent  string `json:"vod_content,omitempty"`
	VodTime     string `json:"vod_time,omitempty"`
	VodPlayFrom string `json:"vod_play_from,omitempty"`
	VodPlayURL  string `json:"vod_play_url,omitempty"`
}

func tvboxEnabled() bool {
	value := strings.TrimSpace(os.Getenv("JUKU_TVBOX_ENABLED"))
	return value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes") || strings.EqualFold(value, "on")
}

func tvboxConfiguredToken() string {
	return strings.TrimSpace(os.Getenv("JUKU_TVBOX_TOKEN"))
}

func tvboxAuthorized(request *http.Request) bool {
	configured := tvboxConfiguredToken()
	if configured == "" {
		return false
	}
	provided := strings.TrimSpace(request.URL.Query().Get("token"))
	if provided == "" {
		provided = strings.TrimSpace(request.URL.Query().Get("key"))
	}
	if provided == "" {
		auth := strings.TrimSpace(request.Header.Get("Authorization"))
		if len(auth) >= 7 && strings.EqualFold(auth[:7], "Bearer ") {
			provided = strings.TrimSpace(auth[7:])
		}
	}
	return provided != "" && subtleStringEqual(provided, configured)
}

func subtleStringEqual(a, b string) bool {
	// Keep token comparison independent of token length and avoid leaking the
	// configured value through a straightforward early-exit comparison.
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

func (app *UIApp) tvboxGuard(writer http.ResponseWriter, request *http.Request) bool {
	if !tvboxEnabled() {
		http.NotFound(writer, request)
		return false
	}
	if tvboxConfiguredToken() == "" {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]any{"code": 0, "msg": "TVBox 已开启但未设置 JUKU_TVBOX_TOKEN"})
		return false
	}
	if !tvboxAuthorized(request) {
		writer.Header().Set("WWW-Authenticate", `Bearer realm="juku-tvbox"`)
		writeJSON(writer, http.StatusUnauthorized, map[string]any{"code": 0, "msg": "tvbox token required"})
		return false
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Access-Control-Allow-Origin", "*")
	return true
}

func (app *UIApp) handleTVBoxConfig(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writer.Header().Set("Allow", "GET, HEAD")
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]any{"code": 0, "msg": "method not allowed"})
		return
	}
	if !app.tvboxGuard(writer, request) {
		return
	}
	apiURL := tvboxAbsoluteURL(request, tvboxAPIPath)
	if token := tvboxConfiguredToken(); token != "" {
		parsed, _ := url.Parse(apiURL)
		query := parsed.Query()
		query.Set("token", token)
		parsed.RawQuery = query.Encode()
		apiURL = parsed.String()
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"sites": []map[string]any{{
			"key":         "juku",
			"name":        "短剧库",
			"type":        1,
			"api":         apiURL,
			"searchable":  1,
			"quickSearch": 1,
			"filterable":  1,
			"playerType":  1,
		}},
	})
}

func (app *UIApp) handleTVBoxAPI(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writer.Header().Set("Allow", "GET, HEAD")
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]any{"code": 0, "msg": "method not allowed"})
		return
	}
	if !app.tvboxGuard(writer, request) {
		return
	}
	if request.Method == http.MethodHead {
		writer.WriteHeader(http.StatusOK)
		return
	}
	query := request.URL.Query()
	ac := strings.ToLower(strings.TrimSpace(query.Get("ac")))
	if ac == "" {
		ac = "list"
	}
	if ac != "list" && ac != "detail" {
		writeJSON(writer, http.StatusBadRequest, tvboxResponse{Code: 0, Msg: "仅支持 ac=list 或 ac=detail"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 90*time.Second)
	defer cancel()
	dramas := app.tvboxDramas(ctx)
	filtered := tvboxFilterDramas(dramas, firstNonEmpty(query.Get("wd"), query.Get("q"), query.Get("keyword")), tvboxTypeQuery(query))
	if ids := strings.TrimSpace(query.Get("ids")); ids != "" {
		filtered = tvboxFilterIDs(filtered, ids)
	}
	if ac == "detail" {
		ids := strings.TrimSpace(query.Get("ids"))
		if ids == "" {
			writeJSON(writer, http.StatusBadRequest, tvboxResponse{Code: 0, Msg: "ac=detail 需要 ids"})
			return
		}
		if len(ids) > 8192 || len(strings.Split(ids, ",")) > 20 {
			writeJSON(writer, http.StatusBadRequest, tvboxResponse{Code: 0, Msg: "一次最多查询 20 部剧"})
			return
		}
		app.writeTVBoxDetails(writer, request, ctx, filtered)
		return
	}
	page := tvboxPositiveInt(query.Get("pg"), 1)
	limit := tvboxPositiveInt(query.Get("pagesize"), 20)
	if raw := query.Get("limit"); raw != "" {
		limit = tvboxPositiveInt(raw, limit)
	}
	if limit > 100 {
		limit = 100
	}
	start := len(filtered)
	pageCount := 0
	if len(filtered) > 0 {
		pageCount = (len(filtered) + limit - 1) / limit
		if page <= pageCount {
			start = (page - 1) * limit
		}
	}
	end := start + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	list := make([]tvboxVod, 0, end-start)
	for _, drama := range filtered[start:end] {
		list = append(list, app.tvboxVodFromDrama(request, drama))
	}
	writeJSON(writer, http.StatusOK, tvboxResponse{Code: 1, Msg: "数据列表", Page: page, PageCount: pageCount, Limit: strconv.Itoa(limit), Total: len(filtered), List: list, Class: tvboxClasses()})
}

func (app *UIApp) tvboxDramas(ctx context.Context) []Drama {
	app.mu.Lock()
	if len(app.dramas) == 0 && app.libraryLoading == nil && !app.libraryAttempted && app.downloader != nil {
		app.startLibraryLoadLocked("", libraryLoadRefresh, nil)
	}
	loading := app.libraryLoading
	dramas := append([]Drama(nil), app.dramas...)
	app.mu.Unlock()
	if loading != nil {
		select {
		case <-loading:
		case <-ctx.Done():
		}
		app.mu.Lock()
		dramas = append([]Drama(nil), app.dramas...)
		app.mu.Unlock()
	}
	return dramas
}

func tvboxTypeQuery(query url.Values) string {
	return firstNonEmpty(query.Get("t"), query.Get("type"), query.Get("typeid"), query.Get("type_id"))
}

func tvboxFilterDramas(dramas []Drama, keyword, typeID string) []Drama {
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	typeID = strings.TrimSpace(typeID)
	if typeID != "1" && typeID != "2" && typeID != "3" && typeID != "4" && typeID != "5" {
		typeID = ""
	}
	result := make([]Drama, 0, len(dramas))
	for _, drama := range dramas {
		if typeID != "" && tvboxTypeID(drama) != typeID {
			continue
		}
		if keyword != "" && !tvboxDramaContains(drama, keyword) {
			continue
		}
		result = append(result, drama)
	}
	return result
}

func tvboxFilterIDs(dramas []Drama, raw string) []Drama {
	wanted := make(map[string]bool)
	for _, id := range strings.Split(raw, ",") {
		if id = strings.TrimSpace(id); id != "" {
			wanted[id] = true
		}
	}
	result := make([]Drama, 0, len(wanted))
	for _, drama := range dramas {
		if wanted[drama.ID] {
			result = append(result, drama)
		}
	}
	return result
}

func tvboxDramaContains(drama Drama, keyword string) bool {
	fields := []string{drama.ID, drama.DisplayTitle(), drama.Name, drama.Desc, drama.Intro, drama.ChannelName, drama.CategoryName, drama.Category, drama.TypeName, strings.Join(drama.Tags, " ")}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), keyword) {
			return true
		}
	}
	return false
}

func tvboxTypeID(drama Drama) string {
	source := canonicalProviderSource(firstNonEmpty(drama.Source, sourceFromDramaID(drama.ID)))
	switch source {
	case sourceHuangdou:
		return "2"
	case sourceHongguo:
		return "3"
	case sourceHuangguoAI:
		return "4"
	case sourceHuangguoVideo:
		return "5"
	default:
		return "1"
	}
}

func tvboxTypeName(drama Drama) string {
	switch tvboxTypeID(drama) {
	case "2":
		return "黄豆"
	case "3":
		return "红果"
	case "4":
		return "黄果 AI"
	case "5":
		return "黄果视频"
	default:
		return "黄果"
	}
}

func tvboxClasses() []tvboxClass {
	return []tvboxClass{{"1", "黄果"}, {"2", "黄豆"}, {"3", "红果"}, {"4", "黄果 AI"}, {"5", "黄果视频"}}
}

func tvboxPositiveInt(raw string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

func (app *UIApp) tvboxVodFromDrama(request *http.Request, drama Drama) tvboxVod {
	copy := drama
	normalizeDramaCover(&copy)
	count := firstNonEmpty(stringValue(copy.TotalEpisode), stringValue(copy.TotalEpisodeSnake), stringValue(copy.ChapterCount), stringValue(copy.ChapterCountSnake), stringValue(copy.EpisodeCount), stringValue(copy.EpisodeCountSnake))
	remarks := strings.TrimSpace(copy.Remark)
	if remarks == "" && count != "" {
		remarks = "共 " + count + " 集"
	}
	return tvboxVod{VodID: copy.ID, VodName: copy.DisplayTitle(), TypeID: tvboxTypeID(copy), TypeName: firstNonEmpty(copy.CategoryName, copy.TypeName, tvboxTypeName(copy)), VodPic: tvboxCoverURL(request, copy), VodRemarks: remarks, VodYear: tvboxYear(copy.OnlineDate), VodContent: firstNonEmpty(copy.Desc, copy.Intro), VodTime: copy.OnlineDate}
}

func (app *UIApp) writeTVBoxDetails(writer http.ResponseWriter, request *http.Request, ctx context.Context, dramas []Drama) {
	key, err := app.embySigningKey(true)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, tvboxResponse{Code: 0, Msg: "无法初始化播放签名"})
		return
	}
	list := make([]tvboxVod, 0, len(dramas))
	for _, drama := range dramas {
		vod := app.tvboxVodFromDrama(request, drama)
		title, chapters, chapterErr := app.tvboxChapters(ctx, drama)
		if chapterErr != nil {
			vod.VodContent = strings.TrimSpace(firstNonEmpty(vod.VodContent, ""))
			list = append(list, vod)
			continue
		}
		vod.VodName = firstNonEmpty(title, vod.VodName)
		vod.VodPlayFrom = "短剧库"
		plays := make([]string, 0, len(chapters))
		for index, chapter := range chapters {
			if !validEmbyIdentity(drama.ID, chapter.ID) {
				continue
			}
			label := firstNonEmpty(chapter.Title, "第 "+chapter.EpisodeString(index+1)+" 集")
			query := url.Values{"id": {drama.ID}, "chapter": {chapter.ID}, "key": {embyToken(key, drama.ID, chapter.ID)}}
			plays = append(plays, label+"$"+tvboxAbsoluteURL(request, tvboxPlayPath+"?"+query.Encode()))
		}
		vod.VodPlayURL = strings.Join(plays, "#")
		list = append(list, vod)
	}
	writeJSON(writer, http.StatusOK, tvboxResponse{Code: 1, Msg: "数据列表", Page: 1, PageCount: 1, Limit: strconv.Itoa(len(list)), Total: len(list), List: list})
}

func (app *UIApp) tvboxChapters(ctx context.Context, drama Drama) (string, []Chapter, error) {
	if isHuangguoProviderSource(drama.Source) {
		sourceID := strings.TrimSpace(drama.SourceID)
		if sourceID == "" {
			if _, parsedID, ok := splitProviderDramaID(drama.ID); ok {
				sourceID = parsedID
			} else {
				sourceID = strings.TrimSpace(drama.ID)
			}
		}
		return app.downloader.GetHuangguoChapters(ctx, drama.Source, sourceID)
	}
	return app.downloader.GetDramaChapters(ctx, drama.ID)
}

func tvboxCoverURL(request *http.Request, drama Drama) string {
	if bestDramaCover(drama) == "" {
		return ""
	}
	query := url.Values{"id": {drama.ID}}
	if token := tvboxConfiguredToken(); token != "" {
		query.Set("token", token)
	}
	return tvboxAbsoluteURL(request, tvboxCoverPath+"?"+query.Encode())
}

func (app *UIApp) handleTVBoxCover(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writer.Header().Set("Allow", "GET, HEAD")
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !app.tvboxGuard(writer, request) {
		return
	}
	id := strings.TrimSpace(request.URL.Query().Get("id"))
	app.mu.Lock()
	var drama Drama
	for _, candidate := range app.dramas {
		if candidate.ID == id {
			drama = candidate
			break
		}
	}
	app.mu.Unlock()
	remote := bestDramaCover(drama)
	if drama.ID == "" || remote == "" {
		http.NotFound(writer, request)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithValue(request.Context(), coverSourceKey{}, dramaProvider(drama)), 45*time.Second)
	defer cancel()
	data, err := app.loadCoverImage(ctx, remote, decodeImageBytes)
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "封面暂不可用"})
		return
	}
	writer.Header().Set("Content-Type", imageContentType(data))
	writer.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(writer, request, "cover", time.Time{}, bytes.NewReader(data))
}

func tvboxYear(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 4 {
		return value[:4]
	}
	return value
}

func stringValue(value any) string {
	switch raw := value.(type) {
	case string:
		return strings.TrimSpace(raw)
	case json.Number:
		return raw.String()
	case float64:
		return strconv.Itoa(int(raw))
	case float32:
		return strconv.Itoa(int(raw))
	case int:
		return strconv.Itoa(raw)
	case int64:
		return strconv.FormatInt(raw, 10)
	default:
		return ""
	}
}

func tvboxAbsoluteURL(request *http.Request, raw string) string {
	if parsed, err := url.Parse(strings.TrimSpace(raw)); err == nil && parsed.IsAbs() {
		return parsed.String()
	}
	scheme := "http"
	if forwarded := strings.TrimSpace(strings.Split(request.Header.Get("X-Forwarded-Proto"), ",")[0]); forwarded == "http" || forwarded == "https" {
		scheme = forwarded
	} else if request.TLS != nil {
		scheme = "https"
	}
	base := scheme + "://" + request.Host
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(raw, "/")
}
