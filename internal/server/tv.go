package server

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"ps5-manager/internal/installer"
	"ps5-manager/internal/store"
)

// TV 端 API：面向 1080P / 4K 大屏与手柄操作的只读目录 + 安装入口。
//
// 字段命名严格对齐 Overdrive 的 catalog 契约（id / title / category / rating /
// size_bytes / cover_url / backdrop_url / version / package_count），
// 便于前端直接复用 Overdrive 的卡片与 Spotlight 布局。
//
// 设计约定：
//   - 全部复用既有 store / meta / installer，不复制业务逻辑；
//   - 列表一次性批量取元数据，避免 N+1 查询；
//   - 图片一律走既有的 /api/v1/games/{id}/... 代理端点（带本地缓存与回退）。

// tvGame 目录中的一款游戏（对应 Overdrive catalog 的一项）
type tvGame struct {
	ID           string   `json:"id"`            // TitleID，Overdrive 用它做主键
	Title        string   `json:"title"`         // 展示名（中文优先）
	Category     string   `json:"category"`      // 主类型（genres[0]，缺省用平台）
	Genres       []string `json:"genres"`        //
	Rating       int      `json:"rating"`        // 0-100，缺省 0
	SizeBytes    int64    `json:"size_bytes"`    // 本体 + 补丁 + DLC 总容量
	CoverURL     string   `json:"cover_url"`     // 2:3 竖版封面
	BackdropURL  string   `json:"backdrop_url"`  // 16:9 横版背景
	LogoURL      string   `json:"logo_url"`      // 透明 Logo（可空）
	Version      string   `json:"version"`       //
	PackageCount int      `json:"package_count"` // 本体 + 补丁 + DLC 的包数量
	Platform     string   `json:"platform"`      // PS5 / PS4 / PS3 / SWITCH
	Year         int      `json:"year"`          //
	Developer    string   `json:"developer"`     //
	HasMeta      bool     `json:"has_meta"`      // 是否刮削到元数据
	HasBackdrop  bool     `json:"has_backdrop"`  // 是否有真正的 16:9 横版背景（否则 backdrop_url 为竖版封面）
}

// tvPackage 详情页里的一个可安装包
type tvPackage struct {
	ID          int64  `json:"id"`
	Kind        string `json:"kind"` // base | patch | dlc
	Title       string `json:"title"`
	Version     string `json:"version"`
	SizeBytes   int64  `json:"size_bytes"`
	DownloadURL string `json:"download_url"`
}

// tvPage 详情页数据
type tvPage struct {
	Game        tvGame      `json:"game"`
	Summary     string      `json:"summary"`
	Publisher   string      `json:"publisher"`
	Developer   string      `json:"developer"`
	ReleaseDate string      `json:"release_date"`
	Rating      float64     `json:"rating"`
	Screenshots []string    `json:"screenshots"`
	Packages    []tvPackage `json:"packages"`
	DLCCount    int         `json:"dlc_count"`
	PatchCount  int         `json:"patch_count"`
}

// ---------- 目录构建 ----------

// buildTVGames 读取全部本体并组装成 TV 目录项。
func (h *ServerHandler) buildTVGames() ([]tvGame, error) {
	games, err := h.store.GetGames("")
	if err != nil {
		return nil, err
	}

	// 只保留本地文件仍存在的记录，并统计各 TitleID 的包数量与总容量。
	all, lerr := h.store.ListFilePaths()
	existIDs := make(map[int64]bool, len(all))
	pkgCount := make(map[string]int)
	totalSize := make(map[string]int64)
	if lerr == nil {
		for _, p := range filterExistingPackages(all) {
			existIDs[p.ID] = true
			pkgCount[p.TitleID]++
			totalSize[p.TitleID] += p.FileSize
		}
	}

	// 批量取元数据
	ids := make([]int64, 0, len(games))
	for _, g := range games {
		ids = append(ids, g.ID)
	}
	metaMap := map[int64]*store.Meta{}
	if len(ids) > 0 {
		if m, merr := h.store.GetMetaForIDs(ids); merr == nil {
			metaMap = m
		}
	}

	out := make([]tvGame, 0, len(games))
	for _, g := range games {
		if g.Category != "" && g.Category != "game" {
			continue
		}
		if lerr == nil && !existIDs[g.ID] {
			continue
		}

		m := metaMap[g.ID]
		title := firstNonEmpty(metaNameCN(m), g.DisplayName, g.Title, g.TitleID)
		genres := []string{}
		rating := 0
		developer := ""
		coverURL := "/api/v1/games/" + itoa(g.ID) + "/icon?mode=cover"
		backdropURL := ""
		logoURL := ""
		if m != nil {
			if len(m.Genres) > 0 {
				genres = m.Genres
			}
			rating = int(math.Round(m.Rating))
			developer = m.Developer
			backdropURL = "/api/v1/games/" + itoa(g.ID) + "/art?kind=hero"
			if m.Hero == "" && m.Banner == "" {
				backdropURL = coverURL
			}
			if m.Logo != "" {
				logoURL = "/api/v1/games/" + itoa(g.ID) + "/art?kind=logo"
			}
		}
		if backdropURL == "" {
			backdropURL = coverURL
		}

		count := pkgCount[g.TitleID]
		if count == 0 {
			count = 1
		}
		size := totalSize[g.TitleID]
		if size == 0 {
			size = g.FileSize
		}

		cat := g.Platform
		if len(genres) > 0 {
			cat = genres[0]
		}

		hasBackdrop := m != nil && (m.Hero != "" || m.Banner != "")

		out = append(out, tvGame{
			ID:           g.TitleID,
			Title:        title,
			Category:     cat,
			Genres:       genres,
			Rating:       rating,
			SizeBytes:    size,
			CoverURL:     coverURL,
			BackdropURL:  backdropURL,
			LogoURL:      logoURL,
			Version:      g.Version,
			PackageCount: count,
			Platform:     g.Platform,
			Year:         m.ReleaseYear(),
			Developer:    developer,
			HasMeta:      m != nil && !m.IsEmpty(),
			HasBackdrop:  hasBackdrop,
		})
	}
	return out, nil
}

// ---------- 处理器 ----------

// handleTVCatalog GET /api/tv/catalog
func (h *ServerHandler) handleTVCatalog(w http.ResponseWriter, r *http.Request) {
	games, err := h.buildTVGames()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeTVJSON(w, map[string]any{
		"games": games,
		"total": len(games),
	})
}

// handleTVSpotlight GET /api/tv/spotlight
//
// 返回 5-8 款用于 Discover 顶部 16:9 Banner 轮播的游戏：
// 优先高评分，其次近期入库，最后用有背景图的游戏补足。
func (h *ServerHandler) handleTVSpotlight(w http.ResponseWriter, r *http.Request) {
	games, err := h.buildTVGames()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	ranked := make([]tvGame, len(games))
	copy(ranked, games)
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Rating != ranked[j].Rating {
			return ranked[i].Rating > ranked[j].Rating
		}
		return ranked[i].Year > ranked[j].Year
	})

	const want = 8
	out := make([]tvGame, 0, want)
	seen := map[string]bool{}
	for _, g := range ranked {
		if len(out) >= want {
			break
		}
		if seen[g.ID] || !g.HasMeta {
			continue
		}
		seen[g.ID] = true
		out = append(out, g)
	}
	// 元数据不足时用其余游戏补足，保证 Banner 不空
	for _, g := range ranked {
		if len(out) >= want {
			break
		}
		if seen[g.ID] {
			continue
		}
		seen[g.ID] = true
		out = append(out, g)
	}

	writeTVJSON(w, map[string]any{"games": out})
}

// handleTVPage GET /api/tv/pages/{title_id}
func (h *ServerHandler) handleTVPage(w http.ResponseWriter, r *http.Request) {
	titleID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/tv/pages/"))
	titleID = strings.Trim(titleID, "/")
	if titleID == "" {
		http.Error(w, "missing title_id", http.StatusBadRequest)
		return
	}

	pkgs, err := h.store.GetPackagesByTitleID(titleID)
	if err != nil || len(pkgs) == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	var base *store.Package
	for i := range pkgs {
		if pkgs[i].Category == "" || pkgs[i].Category == "game" {
			base = &pkgs[i]
			break
		}
	}
	if base == nil {
		base = &pkgs[0]
	}

	m, _ := h.store.GetMeta(base.ID)
	games, _ := h.buildTVGames()
	var g tvGame
	for _, item := range games {
		if item.ID == titleID {
			g = item
			break
		}
	}
	if g.ID == "" {
		// 目录里没找到（例如刚扫描进来），用基础字段兜底
		g = tvGame{
			ID:           base.TitleID,
			Title:        firstNonEmpty(base.DisplayName, base.Title),
			Platform:     base.Platform,
			Version:      base.Version,
			CoverURL:     "/api/v1/games/" + itoa(base.ID) + "/icon?mode=cover",
			SizeBytes:    base.FileSize,
			PackageCount: len(pkgs),
		}
		g.BackdropURL = g.CoverURL
	}

	page := tvPage{Game: g, Packages: make([]tvPackage, 0, len(pkgs))}
	if m != nil {
		page.Summary = m.Summary
		page.Publisher = m.Publisher
		page.Developer = m.Developer
		page.ReleaseDate = m.ReleaseDate
		page.Rating = m.Rating
		page.Screenshots = m.Screenshots
	}

	for _, p := range pkgs {
		kind := p.Category
		switch kind {
		case "", "game":
			kind = "base"
		case "patch":
			page.PatchCount++
		case "dlc":
			page.DLCCount++
		}
		page.Packages = append(page.Packages, tvPackage{
			ID:          p.ID,
			Kind:        kind,
			Title:       firstNonEmpty(p.DisplayName, p.Title, p.ContentID),
			Version:     p.Version,
			SizeBytes:   p.FileSize,
			DownloadURL: "/download/" + itoa(p.ID),
		})
	}

	writeTVJSON(w, page)
}

// tvInstallRequest POST /api/tv/install
type tvInstallRequest struct {
	TitleID     string `json:"title_id"`
	PackageType string `json:"package_type"` // all | base | patch
	PS5IP       string `json:"ps5_ip"`
}

// handleTVInstall POST /api/tv/install
//
// 手柄按下「确定」后触发：把选中包生成局域网直链，交给 installer 异步下发。
// ps5_ip 缺省时依次尝试：请求体 → settings 表 tv_ps5_ip。
func (h *ServerHandler) handleTVInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req tvInstallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}
	req.TitleID = strings.TrimSpace(req.TitleID)
	if req.TitleID == "" {
		http.Error(w, "missing title_id", http.StatusBadRequest)
		return
	}

	ps5IP := strings.TrimSpace(req.PS5IP)
	if ps5IP == "" {
		if v, err := h.store.GetSetting("tv_ps5_ip"); err == nil {
			ps5IP = strings.TrimSpace(v)
		}
	}
	if ps5IP == "" {
		writeTVJSONStatus(w, http.StatusBadRequest, map[string]any{
			"error":       "ps5_not_configured",
			"message":     "尚未设置 PS5 地址，请先在 TV 端设置里填写",
			"need_device": true,
		})
		return
	}

	pkgs, err := h.store.GetPackagesByTitleID(req.TitleID)
	if err != nil || len(pkgs) == 0 {
		http.Error(w, "game not found", http.StatusNotFound)
		return
	}

	// 按 package_type 组装要安装的包（顺序：本体 → 补丁 → DLC）
	var base *store.Package
	var patches, dlcs []store.Package
	for i := range pkgs {
		switch pkgs[i].Category {
		case "", "game":
			if base == nil {
				base = &pkgs[i]
			}
		case "patch":
			patches = append(patches, pkgs[i])
		case "dlc":
			dlcs = append(dlcs, pkgs[i])
		}
	}
	if base == nil {
		http.Error(w, "base package not found", http.StatusNotFound)
		return
	}

	kind := strings.ToLower(strings.TrimSpace(req.PackageType))
	if kind == "" {
		kind = "all"
	}
	target := []store.Package{*base}
	switch kind {
	case "base":
		// 只装本体
	case "patch":
		target = patches
		if len(target) == 0 {
			target = []store.Package{*base}
		}
	default: // all
		target = append(target, patches...)
		target = append(target, dlcs...)
	}
	if len(target) == 0 {
		http.Error(w, "nothing to install", http.StatusBadRequest)
		return
	}

	// 任务模型是「本体 + DLC 列表」，这里把补丁并入 DLC 队列按顺序下发
	game := target[0]
	rest := target[1:]

	urlBuilder := func(pkgID int64) string {
		ip := h.cfg.ExternalIP
		if ip == "" {
			ip = h.detectHostIP()
		}
		return "http://" + ip + ":" + itoa(int64(h.cfg.Port)) + "/download/" + itoa(pkgID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	task := h.installer.Start(ctx, ps5IP, 9090, &game, rest, urlBuilder)

	// 记住本次成功的地址，下次无需再填
	_ = h.store.SetSetting("tv_ps5_ip", ps5IP)

	writeTVJSON(w, map[string]any{
		"status":  "started",
		"task_id": task.ID,
		"total":   task.Total,
		"queued":  len(target),
	})
}

// handleTVTasks GET /api/tv/tasks
//
// 返回内存中的安装任务，供卡片进度条轮询（按 game_id 匹配）。
func (h *ServerHandler) handleTVTasks(w http.ResponseWriter, r *http.Request) {
	writeTVJSON(w, map[string]any{"tasks": installer.DefaultTasks.List()})
}

// handleTVIndex 提供 TV 端单页应用（内嵌 assets/tv/index.html）。
// 所有 /tv 与 /tv/* 路径都返回同一个 HTML，前端用 History API 做路由。
func (h *ServerHandler) handleTVIndex(w http.ResponseWriter, r *http.Request) {
	raw, err := assetsFS.ReadFile("assets/tv/index.html")
	if err != nil {
		http.Error(w, "tv ui not built", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(raw)
}

// ---------- 小工具 ----------

func writeTVJSON(w http.ResponseWriter, v any) {
	writeTVJSONStatus(w, http.StatusOK, v)
}

func writeTVJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// metaNameCN 安全取中文名（m 可能为 nil）
func metaNameCN(m *store.Meta) string {
	if m == nil {
		return ""
	}
	return m.NameCN
}

// itoa 整数转字符串（避免每处都 import strconv 的冗长写法）
func itoa(v int64) string { return strconv.FormatInt(v, 10) }
