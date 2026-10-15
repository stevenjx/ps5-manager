package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ps5-manager/internal/cover"
	"ps5-manager/internal/discovery"
	"ps5-manager/internal/installer"
	"ps5-manager/internal/meta"
	"ps5-manager/internal/pkg"
	"ps5-manager/internal/scanner"
	"ps5-manager/internal/store"
	"ps5-manager/internal/sw"
)

type Config struct {
	Port       int
	ExternalIP string
	CoverDir   string
	GamesDir   string
	ConfigDir  string
	SubnetCIDR string
}

type ServerHandler struct {
	cfg        Config
	store      *store.Store
	scanner    *scanner.Scanner
	discoverer *discovery.Discoverer
	installer  *installer.Client
	covers     *cover.Store
	meta       *meta.Pipeline
}

func NewServerHandler(cfg Config, st *store.Store, sc *scanner.Scanner, disc *discovery.Discoverer, inst *installer.Client) *ServerHandler {
	return &ServerHandler{
		cfg:        cfg,
		store:      st,
		scanner:    sc,
		discoverer: disc,
		installer:  inst,
		covers:     cover.NewStore(cfg.CoverDir),
	}
}

func (h *ServerHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/games", h.handleGetGames)
	mux.HandleFunc("/api/v1/games/dlcs", h.handleGetGameDLCs)
	mux.HandleFunc("/api/v1/games/", h.handleGameResource)
	mux.HandleFunc("/api/v1/devices", h.handleGetDevices)
	mux.HandleFunc("/api/v1/scan", h.handleTriggerScan)
	mux.HandleFunc("/api/v1/install", h.handleInstall)
	mux.HandleFunc("/api/v1/install/status", h.handleInstallStatus)
	mux.HandleFunc("/api/v1/settings", h.handleSettings)
	mux.HandleFunc("/api/v1/browse", h.handleBrowse)
	mux.HandleFunc("/api/v1/progress", h.handleProgress)

	// 元数据刮削
	mux.HandleFunc("/api/v1/metadata/status", h.handleMetadataStatus)
	mux.HandleFunc("/api/v1/metadata/settings", h.handleMetadataSettings)
	mux.HandleFunc("/api/v1/metadata/scrape", h.handleScrapeAll)

	// 静态资源（CSS / JS 等 embed 资源）
	mux.Handle("/static/", h.assetsHandler())

	// PKG 下载
	mux.HandleFunc("/download/", h.handleDownload)

	// 根路由
	mux.HandleFunc("/", h.handleWebIndex)
}

func (h *ServerHandler) handleGetGames(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("search")
	if search == "" {
		search = r.URL.Query().Get("q")
	}

	games, err := h.store.GetGames(search)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 实时可用性过滤：本地文件已不存在（例如外接硬盘已退出）的游戏不显示，
	// 并按「实际存在的 DLC 文件」重算 DLC 数量。
	if all, lerr := h.store.ListFilePaths(); lerr == nil {
		existing := filterExistingPackages(all)
		existIDs := make(map[int64]bool, len(existing))
		counts := make(map[string]int)
		for _, p := range existing {
			existIDs[p.ID] = true
			if p.Category == "dlc" {
				counts[p.TitleID]++
			}
		}
		kept := make([]store.Package, 0, len(games))
		for _, g := range games {
			if !existIDs[g.ID] {
				continue
			}
			g.DLCCount = counts[g.TitleID]
			kept = append(kept, g)
		}
		games = kept
	}

	// 附加刮削到的元数据（一次性批量查询，避免 N+1）
	if len(games) > 0 {
		ids := make([]int64, 0, len(games))
		for i := range games {
			ids = append(ids, games[i].ID)
		}
		if metaMap, merr := h.store.GetMetaForIDs(ids); merr == nil {
			for i := range games {
				games[i].Meta = metaMap[games[i].ID]
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data": games,
	})
}

// filterExistingPackages 并行探测文件是否存在，只保留本地文件仍存在的记录。
// 这样界面完全跟随本地文件状态实时变化。
func filterExistingPackages(list []store.Package) []store.Package {
	if len(list) == 0 {
		return list
	}
	keep := make([]bool, len(list))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 24)
	for i := range list {
		if list[i].FilePath == "" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			if _, err := os.Stat(list[i].FilePath); err == nil {
				keep[i] = true
			}
		}(i)
	}
	wg.Wait()

	out := make([]store.Package, 0, len(list))
	for i := range list {
		if keep[i] {
			out = append(out, list[i])
		}
	}
	return out
}

func (h *ServerHandler) handleGetGameDLCs(w http.ResponseWriter, r *http.Request) {
	titleID := r.URL.Query().Get("title_id")
	if titleID == "" {
		http.Error(w, "missing title_id", http.StatusBadRequest)
		return
	}

	dlcs, err := h.store.GetDLCsByTitleID(titleID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 实时过滤：只返回本地仍存在的 DLC 文件
	dlcs = filterExistingPackages(dlcs)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data": dlcs,
	})
}

func (h *ServerHandler) handleGetDevices(w http.ResponseWriter, r *http.Request) {
	cidr := h.cfg.SubnetCIDR
	if cidr == "" {
		cidr = h.detectSubnetCIDR()
	}

	devices, err := h.discoverer.ScanSubnet(r.Context(), cidr)
	if err != nil {
		devices = []discovery.Device{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data": devices,
	})
}

func (h *ServerHandler) handleTriggerScan(w http.ResponseWriter, r *http.Request) {
	scanDirs := h.store.GetScanDirs(h.cfg.GamesDir)
	go func() {
		h.scanner.ScanDirectories(context.Background(), scanDirs)
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "scan_started",
	})
}

func (h *ServerHandler) handleInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		GameID int64  `json:"game_id"`
		PS5IP  string `json:"ps5_ip"`
		Port   int    `json:"port"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	if req.Port == 0 {
		req.Port = 9090
	}
	if strings.TrimSpace(req.PS5IP) == "" {
		http.Error(w, "缺少 PS5 IP", http.StatusBadRequest)
		return
	}

	game, err := h.store.GetPackageByID(req.GameID)
	if err != nil {
		http.Error(w, "Game not found", http.StatusNotFound)
		return
	}

	dlcs, _ := h.store.GetDLCsByTitleID(game.TitleID)

	urlBuilder := func(pkgID int64) string {
		serverIP := h.cfg.ExternalIP
		if serverIP == "" {
			serverIP = h.detectHostIP()
		}
		return fmt.Sprintf("http://%s:%d/download/%d", serverIP, h.cfg.Port, pkgID)
	}

	// 创建「本体 → DLC 顺序安装」任务，立即返回任务 ID，前端轮询进度
	task := h.installer.Start(r.Context(), req.PS5IP, req.Port, game, dlcs, urlBuilder)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "started",
		"task_id":   task.ID,
		"dlc_count": len(dlcs),
		"total":     task.Total,
	})
}

// handleInstallStatus 返回某个安装任务的实时进度与逐项结果
func (h *ServerHandler) handleInstallStatus(w http.ResponseWriter, r *http.Request) {
	taskID := r.URL.Query().Get("id")
	if taskID == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	task, ok := installer.DefaultTasks.Get(taskID)
	if !ok {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(map[string]interface{}{"data": task})
}

func (h *ServerHandler) handleGameResource(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/games/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	game, err := h.store.GetPackageByID(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if len(parts) == 1 && r.Method == http.MethodDelete {
		_ = h.store.DeletePackage(id)
		w.WriteHeader(http.StatusOK)
		return
	}

	if len(parts) >= 2 {
		action := parts[1]
		switch action {
		case "icon":
			h.serveGameImage(w, r, game)
			return
		case "metadata":
			h.handleGameMetadata(w, r, id)
			return
		case "scrape":
			h.handleScrapeGame(w, r, id)
			return
		case "art":
			if len(parts) >= 3 && parts[2] == "download" {
				h.handleGameArtDownload(w, r, id)
				return
			}
			h.handleGameArt(w, r, id)
			return
		case "cover":
			if len(parts) == 3 && parts[2] == "fetch" {
				h.handleOnlineCoverFetch(w, r, game)
				return
			}
			h.handleCoverUploadOrDelete(w, r, game, id)
			return
		}
	}

	http.NotFound(w, r)
}

// serveGameImage 根据 ?mode= 返回不同形态的游戏图像：
//   - mode=cover（默认）：竖版完整封面（优先本地/在线官方 2:3 封面）
//   - mode=icon：方形游戏图标（优先 PKG 内置 icon0.png）
//
// 回退链：本地封面 → PKG 内置图标 → 在线抓取。命中在线结果后落地缓存。
func (h *ServerHandler) serveGameImage(w http.ResponseWriter, r *http.Request, game *store.Package) {
	mode := strings.ToLower(r.URL.Query().Get("mode"))
	if mode != "icon" && mode != "cover" {
		mode = "cover"
	}

	// ---- Nintendo Switch ----
	// 优先级：本地封面（用户上传 / 已缓存图标）→ 容器内明文图标 → 在线官方图标 → 占位
	if strings.EqualFold(game.Platform, "SWITCH") {
		if localCover := h.covers.Find(game.TitleID, game.ID); localCover != "" {
			serveLocalFile(w, r, localCover)
			return
		}
		if game.FilePath != "" {
			if imgData := sw.ExtractIcon(game.FilePath); len(imgData) > 0 {
				if err := h.covers.Save(game.TitleID, game.ID, imgData); err == nil {
					_ = h.store.SetCoverStatus(game.ID, true)
				}
				writeImage(w, imgData, "image/jpeg")
				return
			}
		}
		// 在线官方图标（tinfoil.media，按本体 TitleID）
		if imgData := sw.FetchOnlineIcon(game.TitleID); len(imgData) > 1024 {
			if err := h.covers.Save(game.TitleID, game.ID, imgData); err == nil {
				_ = h.store.SetCoverStatus(game.ID, true)
			}
			writeImage(w, imgData, "image/jpeg")
			return
		}
		http.NotFound(w, r)
		return
	}

	// ---- PlayStation ----
	if mode == "icon" {
		// 图标模式：优先 PKG 内置方形图标
		if game.FilePath != "" {
			if imgData := pkg.ExtractIconFromPKG(game.FilePath); len(imgData) > 0 {
				writeImage(w, imgData, "image/png")
				return
			}
		}
		// 回退到本地封面（可能是竖版，前端会按方形裁剪）
		if localCover := h.covers.Find(game.TitleID, game.ID); localCover != "" {
			serveLocalFile(w, r, localCover)
			return
		}
		// 再回退在线
		h.serveOnlineThenCache(w, r, game)
		return
	}

	// 封面模式：优先本地竖版封面
	if localCover := h.covers.Find(game.TitleID, game.ID); localCover != "" {
		serveLocalFile(w, r, localCover)
		return
	}
	// 无本地封面时：尝试在线抓取 2:3 官方竖版封面
	if imgData := pkg.FetchOnlineCoverByGameID(game.TitleID, game.ContentID); len(imgData) > 1024 {
		if err := h.covers.Save(game.TitleID, game.ID, imgData); err == nil {
			_ = h.store.SetCoverStatus(game.ID, true)
		}
		writeImage(w, imgData, "image/jpeg")
		return
	}
	// 最后回退 PKG 内置图标（方形，前端按 2:3 裁剪展示，聊胜于无）
	if game.FilePath != "" {
		if imgData := pkg.ExtractIconFromPKG(game.FilePath); len(imgData) > 0 {
			writeImage(w, imgData, "image/png")
			return
		}
	}

	http.NotFound(w, r)
}

// serveOnlineThenCache 在线抓取封面（用于 icon 模式的末端回退）
func (h *ServerHandler) serveOnlineThenCache(w http.ResponseWriter, r *http.Request, game *store.Package) {
	if imgData := pkg.FetchOnlineCoverByGameID(game.TitleID, game.ContentID); len(imgData) > 1024 {
		if err := h.covers.Save(game.TitleID, game.ID, imgData); err == nil {
			_ = h.store.SetCoverStatus(game.ID, true)
		}
		writeImage(w, imgData, "image/jpeg")
		return
	}
	http.NotFound(w, r)
}

func writeImage(w http.ResponseWriter, data []byte, contentType string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Write(data)
}

func serveLocalFile(w http.ResponseWriter, r *http.Request, path string) {
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, path)
}

func (h *ServerHandler) handleCoverUploadOrDelete(w http.ResponseWriter, r *http.Request, game *store.Package, id int64) {
	switch r.Method {
	case http.MethodPost:
		// 上传限制 8MB，避免 OOM
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			http.Error(w, "Bad file", http.StatusBadRequest)
			return
		}
		file, _, err := r.FormFile("cover")
		if err != nil {
			http.Error(w, "Bad file", http.StatusBadRequest)
			return
		}
		defer file.Close()

		data, err := io.ReadAll(io.LimitReader(file, 8<<20))
		if err != nil || len(data) == 0 {
			http.Error(w, "Read failed", http.StatusBadRequest)
			return
		}

		if err := h.covers.Save(game.TitleID, game.ID, data); err != nil {
			http.Error(w, "Save failed", http.StatusInternalServerError)
			return
		}
		_ = h.store.SetCoverStatus(id, true)
		w.WriteHeader(http.StatusOK)
		return

	case http.MethodDelete:
		h.covers.Remove(game.TitleID, game.ID)
		_ = h.store.SetCoverStatus(id, false)
		w.WriteHeader(http.StatusOK)
		return
	}

	http.NotFound(w, r)
}

// handleOnlineCoverFetch 主动从 PS Store 官方源 + 镜像源抓取封面（异步调用，不阻塞 UI）
func (h *ServerHandler) handleOnlineCoverFetch(w http.ResponseWriter, r *http.Request, game *store.Package) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if game.TitleID == "" && game.ContentID == "" {
		http.Error(w, "missing title_id / content_id", http.StatusBadRequest)
		return
	}

	// 给抓取 20s 超时（页面抓取 + 多候选图逐个校验），避免卡住浏览器
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	imgDataCh := make(chan []byte, 1)
	go func() {
		imgDataCh <- pkg.FetchOnlineCoverByGameID(game.TitleID, game.ContentID)
	}()

	var imgData []byte
	select {
	case <-ctx.Done():
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status": "error",
			"error":  "fetch timeout (>12s)",
		})
		return
	case d := <-imgDataCh:
		imgData = d
	}

	if len(imgData) <= 1024 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status": "error",
			"error":  "no cover found in any source",
		})
		return
	}

	if err := h.covers.Save(game.TitleID, game.ID, imgData); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status": "error",
			"error":  "save failed: " + err.Error(),
		})
		return
	}
	_ = h.store.SetCoverStatus(game.ID, true)

	log.Printf("[Cover] 在线抓取封面成功: id=%d titleID=%s size=%d bytes", game.ID, game.TitleID, len(imgData))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"size":   strconv.Itoa(len(imgData)),
	})
}

func (h *ServerHandler) handleSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")

	if r.Method == http.MethodGet {
		ps5Dir, _ := h.store.GetSetting("ps5_dir")
		if ps5Dir == "" {
			ps5Dir = h.cfg.GamesDir
		}
		ps4Dir, _ := h.store.GetSetting("ps4_dir")
		switchDir, _ := h.store.GetSetting("switch_dir")
		customJSON, _ := h.store.GetSetting("custom_dirs")

		var customDirs []string
		if customJSON != "" {
			_ = json.Unmarshal([]byte(customJSON), &customDirs)
		}
		if customDirs == nil {
			customDirs = []string{}
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"ps5_dir":     ps5Dir,
			"ps4_dir":     ps4Dir,
			"switch_dir":  switchDir,
			"custom_dirs": customDirs,
		})
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Ps5Dir     string   `json:"ps5_dir"`
			Ps4Dir     string   `json:"ps4_dir"`
			SwitchDir  string   `json:"switch_dir"`
			CustomDirs []string `json:"custom_dirs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid json", http.StatusBadRequest)
			return
		}

		_ = h.store.SetSetting("ps5_dir", req.Ps5Dir)
		_ = h.store.SetSetting("ps4_dir", req.Ps4Dir)
		_ = h.store.SetSetting("switch_dir", req.SwitchDir)
		customBytes, _ := json.Marshal(req.CustomDirs)
		_ = h.store.SetSetting("custom_dirs", string(customBytes))

		scanDirs := h.store.GetScanDirs(h.cfg.GamesDir)
		go func() {
			log.Printf("[Settings] 目录更新，后台触发重新扫描: %v", scanDirs)
			h.scanner.ScanDirectories(context.Background(), scanDirs)
		}()

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success"}`))
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (h *ServerHandler) handleBrowse(w http.ResponseWriter, r *http.Request) {
	rawPath := strings.TrimSpace(r.URL.Query().Get("path"))

	targetPath := rawPath
	if targetPath == "" {
		targetPath = "/"
	}

	if !filepath.IsAbs(targetPath) {
		if abs, err := filepath.Abs(targetPath); err == nil {
			targetPath = abs
		}
	}
	targetPath = filepath.Clean(targetPath)

	validDir := targetPath
	for {
		fi, err := os.Stat(validDir)
		if err == nil {
			if fi.IsDir() {
				break
			}
			validDir = filepath.Dir(validDir)
			continue
		}

		parent := filepath.Dir(validDir)
		if parent == validDir || parent == "" || parent == "." {
			if pkgFi, pkgErr := os.Stat("/packages"); pkgErr == nil && pkgFi.IsDir() {
				validDir = "/packages"
			} else if cwd, cwdErr := os.Getwd(); cwdErr == nil {
				validDir = cwd
			} else {
				validDir = "/"
			}
			break
		}
		validDir = parent
	}

	type dirItem struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}

	var dirs []dirItem
	entries, err := os.ReadDir(validDir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				dirs = append(dirs, dirItem{
					Name: e.Name(),
					Path: filepath.Join(validDir, e.Name()),
				})
			}
		}
	}

	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name)
	})

	parent := filepath.Dir(validDir)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"current": validDir,
		"parent":  parent,
		"dirs":    dirs,
	})
}

func (h *ServerHandler) handleProgress(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write([]byte(`{"data":[]}`))
}

func (h *ServerHandler) handleDownload(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/download/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	pkgItem, err := h.store.GetPackageByID(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	file, err := os.Open(pkgItem.FilePath)
	if err != nil {
		http.Error(w, "File unreadable", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		http.Error(w, "Stat error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(w, r, filepath.Base(pkgItem.FilePath), stat.ModTime(), file)
}

func (h *ServerHandler) detectSubnetCIDR() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "192.168.1.0/24"
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			ipStr := ipnet.IP.String()
			if !strings.HasPrefix(ipStr, "172.") && !strings.HasPrefix(ipStr, "127.") {
				mask := ipnet.Mask
				return fmt.Sprintf("%s/%d", ipnet.IP.Mask(mask).String(), 24)
			}
		}
	}
	return "192.168.1.0/24"
}

func (h *ServerHandler) detectHostIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			ipStr := ipnet.IP.String()
			if !strings.HasPrefix(ipStr, "172.") && !strings.HasPrefix(ipStr, "127.") {
				return ipStr
			}
		}
	}
	return "127.0.0.1"
}
