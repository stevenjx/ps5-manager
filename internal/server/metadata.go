package server

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ps5-manager/internal/meta"
	"ps5-manager/internal/store"
)

// SetPipeline 注入元数据刮削流水线
func (h *ServerHandler) SetPipeline(p *meta.Pipeline) {
	h.meta = p
}

// ---------- 状态 ----------

// handleMetadataStatus 返回刮削能力、覆盖率与当前进度
func (h *ServerHandler) handleMetadataStatus(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"pipeline": nil,
		"stats":    nil,
		"progress": nil,
	}
	if h.meta != nil {
		resp["pipeline"] = h.meta.Status()
	}
	if stats, err := h.store.CountMeta(); err == nil {
		resp["stats"] = stats
	}
	resp["progress"] = h.scanner.Progress()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	_ = json.NewEncoder(w).Encode(resp)
}

// ---------- 设置 ----------

type metadataSettingsRequest struct {
	Enabled          *bool    `json:"enabled"`
	IGDBClientID     *string  `json:"igdb_client_id"`
	IGDBClientSecret *string  `json:"igdb_client_secret"`
	SteamGridDBKey   *string  `json:"sgdb_api_key"`
	HasheousEnabled  *bool    `json:"hasheous_enabled"`
	PlaymatchEnabled *bool    `json:"playmatch_enabled"`
	TitleDBEnabled   *bool    `json:"titledb_enabled"`
	BangumiEnabled   *bool    `json:"bangumi_enabled"`
	Language         []string `json:"language"`
	Region           []string `json:"region"`
	Concurrency      *int     `json:"concurrency"`
}

// handleMetadataSettings 读写元数据刮削配置。
//
// 安全约定：密钥只落库/读库，回显时一律脱敏；接口不会把明文密钥返回给前端。
func (h *ServerHandler) handleMetadataSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")

	switch r.Method {
	case http.MethodGet:
		cfg := meta.ConfigFromEnv().Resolve(h.store)
		if h.meta != nil {
			cfg = h.meta.Config()
		}
		// 数据库中的开关与列表（覆盖默认值）
		enabled := cfg.Enabled
		if v, err := h.store.GetSetting("meta_enabled"); err == nil && v != "" {
			enabled = v == "1" || strings.EqualFold(v, "true")
		}
		hasheous := cfg.HasheousEnabled
		if v, err := h.store.GetSetting("meta_hasheous_enabled"); err == nil && v != "" {
			hasheous = v == "1" || strings.EqualFold(v, "true")
		}
		playmatch := cfg.PlaymatchEnabled
		if v, err := h.store.GetSetting("meta_playmatch_enabled"); err == nil && v != "" {
			playmatch = v == "1" || strings.EqualFold(v, "true")
		}
		titledb := cfg.TitleDBEnabled
		if v, err := h.store.GetSetting("meta_titledb_enabled"); err == nil && v != "" {
			titledb = v == "1" || strings.EqualFold(v, "true")
		}
		bangumi := cfg.BangumiEnabled
		if v, err := h.store.GetSetting("meta_bangumi_enabled"); err == nil && v != "" {
			bangumi = v == "1" || strings.EqualFold(v, "true")
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"enabled":           enabled,
			"hasheous_enabled":  hasheous,
			"playmatch_enabled": playmatch,
			"titledb_enabled":   titledb,
			"bangumi_enabled":   bangumi,
			"igdb_client_id":    cfg.IGDBClientID,
			"igdb_secret_mask":  meta.Mask(cfg.IGDBClientSecret),
			"sgdb_key_mask":     meta.Mask(cfg.SteamGridDBKey),
			"igdb_configured":   cfg.HasIGDB(),
			"sgdb_configured":   cfg.HasSGDB(),
			"chinese_sources":   []string{"gamelist", "titledb", "bangumi"},
			"language":          cfg.Language,
			"region":            cfg.Region,
			"concurrency":       cfg.Concurrency,
			"hash_platforms":    []string{"ps3"},
		})
		return

	case http.MethodPost:
		var req metadataSettingsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid json", http.StatusBadRequest)
			return
		}

		if req.Enabled != nil {
			_ = h.store.SetSetting("meta_enabled", boolSetting(*req.Enabled))
		}
		if req.HasheousEnabled != nil {
			_ = h.store.SetSetting("meta_hasheous_enabled", boolSetting(*req.HasheousEnabled))
		}
		if req.PlaymatchEnabled != nil {
			_ = h.store.SetSetting("meta_playmatch_enabled", boolSetting(*req.PlaymatchEnabled))
		}
		if req.TitleDBEnabled != nil {
			_ = h.store.SetSetting("meta_titledb_enabled", boolSetting(*req.TitleDBEnabled))
		}
		if req.BangumiEnabled != nil {
			_ = h.store.SetSetting("meta_bangumi_enabled", boolSetting(*req.BangumiEnabled))
		}
		// 密钥：留空表示不修改，避免前端回显脱敏值把密钥写坏
		if v := strings.TrimSpace(derefString(req.IGDBClientID)); v != "" {
			_ = h.store.SetSetting(meta.SettingIGDBClientID, v)
		}
		if v := strings.TrimSpace(derefString(req.IGDBClientSecret)); v != "" && !isMasked(v) {
			_ = h.store.SetSetting(meta.SettingIGDBClientSecret, v)
		}
		if v := strings.TrimSpace(derefString(req.SteamGridDBKey)); v != "" && !isMasked(v) {
			_ = h.store.SetSetting(meta.SettingSGDBAPIKey, v)
		}
		if len(req.Language) > 0 {
			_ = h.store.SetSetting("meta_language", strings.Join(req.Language, ","))
		}
		if len(req.Region) > 0 {
			_ = h.store.SetSetting("meta_region", strings.Join(req.Region, ","))
		}
		if req.Concurrency != nil && *req.Concurrency > 0 {
			_ = h.store.SetSetting("meta_concurrency", strconv.Itoa(*req.Concurrency))
		}

		h.rebuildPipeline()

		_, _ = w.Write([]byte(`{"status":"success"}`))
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// BuildPipeline 依据环境变量与数据库设置组装元数据流水线。
//
// 优先级：环境变量（部署期）> settings 表（界面配置）> 内置默认值。
// configDir 用于解析 TitleDB 等大文件的落盘缓存目录。
func BuildPipeline(st *store.Store, configDir string) *meta.Pipeline {
	cfg := meta.ConfigFromEnv().Resolve(st)

	readBool := func(key string, dst *bool) {
		if v, err := st.GetSetting(key); err == nil && v != "" {
			*dst = v == "1" || strings.EqualFold(v, "true")
		}
	}
	readBool("meta_enabled", &cfg.Enabled)
	readBool("meta_hasheous_enabled", &cfg.HasheousEnabled)
	readBool("meta_playmatch_enabled", &cfg.PlaymatchEnabled)
	readBool("meta_titledb_enabled", &cfg.TitleDBEnabled)
	readBool("meta_bangumi_enabled", &cfg.BangumiEnabled)

	if v, err := st.GetSetting("meta_language"); err == nil && v != "" {
		if list := splitCSV(v); len(list) > 0 {
			cfg.Language = list
		}
	}
	if v, err := st.GetSetting("meta_region"); err == nil && v != "" {
		if list := splitCSV(v); len(list) > 0 {
			cfg.Region = list
		}
	}
	if v, err := st.GetSetting("meta_concurrency"); err == nil && v != "" {
		if n, cerr := strconv.Atoi(v); cerr == nil && n > 0 {
			cfg.Concurrency = n
		}
	}

	return meta.NewPipeline(cfg, cfg.CacheDirFor(configDir))
}

// rebuildPipeline 依据最新设置重建流水线（使界面修改立即生效，无需重启）
func (h *ServerHandler) rebuildPipeline() {
	p := BuildPipeline(h.store, h.cfg.ConfigDir)
	h.meta = p
	h.scanner.SetPipeline(p)
}

// ---------- 单条记录的元数据 ----------

// handleGameMetadata 返回某个游戏的元数据
func (h *ServerHandler) handleGameMetadata(w http.ResponseWriter, r *http.Request, gameID int64) {
	m, err := h.store.GetMeta(gameID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": m})
}

// handleScrapeGame 手动（重新）刮削单个游戏
func (h *ServerHandler) handleScrapeGame(w http.ResponseWriter, r *http.Request, gameID int64) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.meta == nil {
		http.Error(w, "元数据刮削未启用", http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	res, err := h.scanner.EnrichOne(ctx, gameID)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		log.Printf("[Meta] 手动刮削失败 id=%d: %v", gameID, err)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	matched := res != nil && !res.IsEmpty()
	src := ""
	if res != nil {
		src = string(res.Source)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  map[bool]string{true: "ok", false: "not_found"}[matched],
		"source":  src,
		"summary": summaryOrEmpty(res),
	})
}

// handleScrapeAll 批量刮削（后台执行，前端轮询 /api/v1/metadata/status）
func (h *ServerHandler) handleScrapeAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.meta == nil {
		http.Error(w, "元数据刮削未启用", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Platforms []string `json:"platforms"`
		Force     bool     `json:"force"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	started := h.scanner.EnrichAll(context.Background(), req.Platforms, req.Force)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  map[bool]string{true: "started", false: "already_running"}[started],
		"started": started,
	})
}

// ---------- 素材代理（把远端高清素材缓存到本地后再提供） ----------

// handleGameArt 代理远端素材，避免前端直连第三方 CDN 带来的跨域与防盗链问题。
//
//	GET /api/v1/games/{id}/art?kind=hero|logo|banner|cover|screenshot&i=0
func (h *ServerHandler) handleGameArt(w http.ResponseWriter, r *http.Request, gameID int64) {
	m, err := h.store.GetMeta(gameID)
	if err != nil || m == nil {
		http.NotFound(w, r)
		return
	}

	kind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
	if kind == "" {
		kind = "hero"
	}

	var rawURL string
	switch kind {
	case "cover":
		rawURL = m.Cover
	case "hero":
		rawURL = m.Hero
	case "logo":
		rawURL = m.Logo
	case "banner":
		rawURL = m.Banner
	case "screenshot":
		idx, _ := strconv.Atoi(r.URL.Query().Get("i"))
		if idx >= 0 && idx < len(m.Screenshots) {
			rawURL = m.Screenshots[idx]
		}
	default:
		http.NotFound(w, r)
		return
	}

	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		http.NotFound(w, r)
		return
	}

	// 本地 file:// 素材（来自 gamelist.xml）直接落盘服务
	if strings.HasPrefix(rawURL, "file://") {
		serveLocalFile(w, r, strings.TrimPrefix(rawURL, "file://"))
		return
	}

	// 内容寻址缓存：同一 URL 只下载一次
	sum := sha1.Sum([]byte(rawURL))
	cachePath := filepath.Join(h.cfg.CoverDir, "art", hex.EncodeToString(sum[:])[:20]+".img")
	if fi, statErr := os.Stat(cachePath); statErr == nil && fi.Size() > 0 {
		serveLocalFile(w, r, cachePath)
		return
	}

	if h.meta == nil {
		http.NotFound(w, r)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	data, ferr := meta.FetchImage(ctx, h.meta.Config(), rawURL)
	if ferr != nil || len(data) == 0 {
		http.NotFound(w, r)
		return
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err == nil {
		if werr := os.WriteFile(cachePath, data, 0o644); werr != nil {
			log.Printf("[Meta] 素材缓存写入失败: %v", werr)
		}
	}
	writeImage(w, data, http.DetectContentType(data))
}

// handleGameArtDownload 直接把远端素材原样下载（供「导出素材」使用）
func (h *ServerHandler) handleGameArtDownload(w http.ResponseWriter, r *http.Request, gameID int64) {
	m, err := h.store.GetMeta(gameID)
	if err != nil || m == nil {
		http.NotFound(w, r)
		return
	}
	kind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
	rawURL := map[string]string{
		"cover": m.Cover, "hero": m.Hero, "logo": m.Logo, "banner": m.Banner,
	}[kind]
	if strings.TrimSpace(rawURL) == "" || h.meta == nil {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	data, ferr := meta.FetchImage(ctx, h.meta.Config(), rawURL)
	if ferr != nil {
		http.NotFound(w, r)
		return
	}
	name := kind + filepath.Ext(rawURL)
	if name == kind {
		name = kind + ".jpg"
	}
	w.Header().Set("Content-Disposition", "attachment; filename=\""+name+"\"")
	writeImage(w, data, http.DetectContentType(data))
}

// ---------- 小工具 ----------

func boolSetting(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// isMasked 判断前端是否把脱敏值原样回传（形如 ********abcd）
func isMasked(v string) bool {
	return strings.HasPrefix(v, "********")
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func summaryOrEmpty(res *meta.Result) string {
	if res == nil {
		return ""
	}
	if res.Summary != "" {
		return res.Summary
	}
	return res.NameCN
}
