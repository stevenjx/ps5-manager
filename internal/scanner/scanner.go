package scanner

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ps5-manager/internal/cover"
	"ps5-manager/internal/meta"
	"ps5-manager/internal/naming"
	"ps5-manager/internal/pkg"
	"ps5-manager/internal/store"
	"ps5-manager/internal/sw"
)

// errNoPipeline 未启用元数据刮削
var errNoPipeline = errors.New("元数据刮削未启用")

type Scanner struct {
	store *store.Store
	cover *cover.Store

	// ---- 元数据刮削（见 enrich.go）----
	enrichMu      sync.Mutex
	meta          *meta.Pipeline
	rootsProvider func() []string
	progress      EnrichProgress
}

func NewScanner(s *store.Store, cs *cover.Store) *Scanner {
	return &Scanner{store: s, cover: cs}
}

// ScanDirectories 扫描给定的多个目录列表（支持 NAS 挂载路径与本地相对/绝对路径）。
// 流程：逐个目录扫描 → 清理失效记录 → 为缺失封面的游戏补齐封面。
func (s *Scanner) ScanDirectories(ctx context.Context, dirPaths []string) {
	var scannedRoots []string
	for _, dir := range dirPaths {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			abs = dir
		}
		if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
			log.Printf("[Scanner] 目录不存在或无访问权限，跳过: %s", abs)
			continue
		}
		if err := s.ScanDirectory(ctx, abs); err != nil {
			log.Printf("[Scanner] 目录扫描结束或跳过 %s: %v", abs, err)
			continue
		}
		scannedRoots = append(scannedRoots, abs)
	}

	// 清理「本次成功扫描到的根目录下、但文件已不存在」的失效记录
	s.PruneMissing(scannedRoots)

	// 为缺失封面的游戏补齐竖版封面
	s.FetchMissingCovers(ctx)
}

func (s *Scanner) ScanDirectory(ctx context.Context, dirPath string) error {
	absPath, err := filepath.Abs(dirPath)
	if err != nil {
		absPath = dirPath
	}

	info, err := os.Stat(absPath)
	if err != nil {
		log.Printf("[Scanner] 目标目录不存在或无访问权限，跳过: %s", absPath)
		return nil
	}
	if !info.IsDir() {
		return nil
	}

	log.Printf("[Scanner] 开始扫描目录: %s", absPath)
	return filepath.Walk(absPath, func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if fileInfo.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".pkg":
			s.handlePKG(path)
		case ".iso":
			s.handlePS3ISO(path)
		case ".xci", ".nsp", ".nsz":
			s.handleSwitch(path)
		}
		return nil
	})
}

// ScanFile 解析并入库单个游戏文件（供实时监测增量调用）
func (s *Scanner) ScanFile(path string) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pkg":
		s.handlePKG(path)
	case ".iso":
		s.handlePS3ISO(path)
	case ".xci", ".nsp", ".nsz":
		s.handleSwitch(path)
	}
}

// ScanFiles 批量解析并入库指定文件（增量扫描）
func (s *Scanner) ScanFiles(ctx context.Context, paths []string) {
	for _, p := range paths {
		select {
		case <-ctx.Done():
			return
		default:
		}
		s.ScanFile(p)
	}
}

// RemoveFiles 移除指定文件对应的记录（文件被删除时同步清理）
func (s *Scanner) RemoveFiles(paths []string) {
	for _, p := range paths {
		if err := s.store.DeleteByFilePath(p); err != nil {
			log.Printf("[Scanner] 移除记录失败 %s: %v", p, err)
			continue
		}
		log.Printf("[Scanner] 已移除记录（文件不存在）: %s", p)
	}
}

// handlePKG 解析并入库 PlayStation PKG
func (s *Scanner) handlePKG(path string) {
	parsed, err := pkg.Parse(path)
	if err != nil {
		log.Printf("[Scanner] 解析 PKG 失败跳过: %s: %v", path, err)
		return
	}

	// 展示名：优先 PKG 内 param.json 真实名称；DLC 无真实名时才做标签美化
	displayName := pkg.DisplayNameFor(parsed.Title, parsed.ContentID, parsed.Category, parsed.HasRealTitle)

	rec := &store.Package{
		TitleID:               parsed.TitleID,
		ContentID:             parsed.ContentID,
		Title:                 parsed.Title,
		DisplayName:           displayName,
		IsPS5:                 parsed.IsPS5,
		Platform:              parsed.Platform,
		Version:               parsed.Version,
		Region:                parsed.Region,
		Category:              parsed.Category,
		FilePath:              path,
		FileSize:              parsed.FileSize,
		CategoryAuthoritative: parsed.CategoryFromParam,
	}

	if err := s.store.SavePackage(rec, parsed.CoverImage); err != nil {
		log.Printf("[Scanner] 保存入库失败: %s: %v", path, err)
		return
	}
	log.Printf("[Scanner] 已收录 %s [%s] (%s): %s", parsed.Platform, parsed.Category, parsed.TitleID, displayName)
}

// handlePS3ISO 解析并入库 PlayStation 3 标准镜像（Redump .iso）。
//
// PS3 镜像不做容器内解析（UDF/ISO9660 解析代价高且收益有限），
// 改为「规范化文件名 + 序列号」入库；后续由元数据流水线用
// 哈希匹配（Hasheous/Playmatch，仅 PS3 允许）精确关联 IGDB ID。
func (s *Scanner) handlePS3ISO(path string) {
	fi, err := os.Stat(path)
	if err != nil {
		log.Printf("[Scanner] 读取 PS3 镜像失败跳过: %s: %v", path, err)
		return
	}

	parsed := naming.ParsePath(path)

	// 序列号（如 BLES00932）作为 TitleID；缺失时退化为文件名
	titleID := parsed.PSSerial
	title := parsed.Name
	if title == "" {
		title = naming.CleanFileName(filepath.Base(path))
	}
	if titleID == "" {
		titleID = naming.CleanFileName(filepath.Base(path))
	}

	region := ""
	if len(parsed.Regions) > 0 {
		region = strings.ToUpper(parsed.Regions[0])
	}

	rec := &store.Package{
		TitleID:               titleID,
		ContentID:             titleID,
		Title:                 title,
		DisplayName:           title,
		IsPS5:                 false,
		Platform:              "PS3",
		Version:               parsed.Version,
		Region:                region,
		Category:              naming.CategoryGame,
		FilePath:              path,
		FileSize:              fi.Size(),
		CategoryAuthoritative: true,
	}

	if err := s.store.SavePackage(rec, nil); err != nil {
		log.Printf("[Scanner] 保存入库失败: %s: %v", path, err)
		return
	}
	log.Printf("[Scanner] 已收录 PS3 [game] (%s): %s", titleID, title)
}

// handleSwitch 解析并入库 Nintendo Switch 游戏文件（XCI / NSP / NSZ）
func (s *Scanner) handleSwitch(path string) {
	parsed, err := sw.Parse(path)
	if err != nil {
		log.Printf("[Scanner] 解析 Switch 文件失败跳过: %s: %v", path, err)
		return
	}

	displayName := parsed.Title
	if parsed.Category == "dlc" && pkg.NameLooksRaw(parsed.Title, parsed.ContentID) {
		displayName = pkg.BeautifyDLCName(parsed.Title, parsed.ContentID)
	}

	rec := &store.Package{
		TitleID:     parsed.TitleID,
		ContentID:   parsed.ContentID,
		Title:       parsed.Title,
		DisplayName: displayName,
		IsPS5:       false,
		Platform:    "SWITCH",
		Version:     parsed.Version,
		Region:      parsed.Region,
		Category:    parsed.Category,
		FilePath:    path,
		FileSize:    parsed.FileSize,
		// Switch 分类由 TitleID 索引位确定性推导，视为权威
		CategoryAuthoritative: true,
	}

	if err := s.store.SavePackage(rec, nil); err != nil {
		log.Printf("[Scanner] 保存入库失败: %s: %v", path, err)
		return
	}
	log.Printf("[Scanner] 已收录 SWITCH [%s] (%s/%s): %s", parsed.Category, parsed.TitleID, parsed.Container, displayName)
}

// PruneMissing 删除「文件已不存在」的记录。
//
// 安全约束：只处理 file_path 位于 roots（本次成功扫描到的目录）之下的记录，
// 避免外接硬盘临时离线时把整库误删。
func (s *Scanner) PruneMissing(roots []string) {
	if len(roots) == 0 {
		return
	}
	list, err := s.store.ListFilePaths()
	if err != nil {
		log.Printf("[Scanner] 失效清理：读取记录失败: %v", err)
		return
	}

	removed := 0
	for _, p := range list {
		if p.FilePath == "" || !pathUnderAny(p.FilePath, roots) {
			continue
		}
		if _, err := os.Stat(p.FilePath); err == nil {
			continue // 文件仍在
		}
		if err := s.store.DeletePackage(p.ID); err != nil {
			log.Printf("[Scanner] 失效清理：删除失败 id=%d: %v", p.ID, err)
			continue
		}
		removed++
	}
	if removed > 0 {
		log.Printf("[Scanner] 失效清理完成：移除 %d 条本地已不存在的记录", removed)
	}
}

// pathUnderAny 判断文件路径是否位于任一给定根目录之下
func pathUnderAny(filePath string, roots []string) bool {
	ap, err := filepath.Abs(filePath)
	if err != nil {
		ap = filePath
	}
	for _, r := range roots {
		rel, err := filepath.Rel(r, ap)
		if err != nil {
			continue
		}
		if rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		return true
	}
	return false
}

// FetchMissingCovers 为所有「尚无本地封面」的游戏自动抓取封面：
//  1. 优先使用元数据刮削已拿到的高清素材地址（IGDB / SteamGridDB / Switch TitleDB / 本地 gamelist）
//  2. PlayStation：回退 PS Store 官方竖版封面（2:3）
//  3. Nintendo Switch：回退 tinfoil.media 官方方形图标
func (s *Scanner) FetchMissingCovers(ctx context.Context) {
	if s.cover == nil {
		return
	}
	games, err := s.store.GetGames("")
	if err != nil {
		log.Printf("[Scanner] 自动封面：读取游戏列表失败: %v", err)
		return
	}

	missing := func(g store.Package) bool {
		if s.cover.Exists(g.TitleID, g.ID) {
			return false
		}
		if strings.EqualFold(g.Platform, "SWITCH") {
			return g.TitleID != ""
		}
		return g.TitleID != "" || g.ContentID != ""
	}

	// 一次性取回待处理记录的刮削元数据，避免逐条查询
	var pendingIDs []int64
	for i := range games {
		if missing(games[i]) {
			pendingIDs = append(pendingIDs, games[i].ID)
		}
	}
	if len(pendingIDs) == 0 {
		return
	}

	metaByID, err := s.store.GetMetaForIDs(pendingIDs)
	if err != nil {
		log.Printf("[Scanner] 自动封面：读取元数据失败: %v", err)
		metaByID = nil
	}

	log.Printf("[Scanner] 检测到 %d 个游戏缺少封面，开始自动抓取...", len(pendingIDs))

	pipe := s.pipeline()
	fetched := 0
	for i := range games {
		select {
		case <-ctx.Done():
			return
		default:
		}
		g := games[i]
		if !missing(g) {
			continue
		}

		var imgData []byte

		// 1) 先用刮削到的高清素材（在线 URL 直接下载；本地 file:// 直接读）
		if m := metaByID[g.ID]; m != nil && pipe != nil {
			for _, candidate := range []string{m.Cover, m.Hero, m.Banner} {
				if strings.TrimSpace(candidate) == "" {
					continue
				}
				data, ferr := meta.FetchImage(ctx, pipe.Config(), candidate)
				if ferr == nil && len(data) > 1024 {
					imgData = data
					break
				}
			}
		}

		// 2) 回退到平台官方源
		if len(imgData) <= 1024 {
			if strings.EqualFold(g.Platform, "SWITCH") {
				imgData = sw.FetchOnlineIcon(g.TitleID)
			} else if strings.EqualFold(g.Platform, "PS3") {
				imgData = nil // PS3 无公开封面接口，交由刮削源处理
			} else {
				imgData = pkg.FetchOnlineCoverByGameID(g.TitleID, g.ContentID)
			}
		}

		if len(imgData) <= 1024 {
			log.Printf("[Scanner] 自动封面未命中: %s (%s)", g.Title, g.TitleID)
			continue
		}
		if err := s.cover.Save(g.TitleID, g.ID, imgData); err != nil {
			log.Printf("[Scanner] 自动封面保存失败: %s: %v", g.Title, err)
			continue
		}
		_ = s.store.SetCoverStatus(g.ID, true)
		fetched++
		log.Printf("[Scanner] 自动封面已获取: %s (%s) %d bytes", g.Title, g.TitleID, len(imgData))
		time.Sleep(300 * time.Millisecond)
	}

	if fetched > 0 {
		log.Printf("[Scanner] 自动封面抓取完成：成功 %d/%d", fetched, len(pendingIDs))
	}
}

// ReclassifyCategories 对数据库中已有记录重新计算内容类型（game / dlc）与展示名。
//
// 注意：category 若已由 PKG 内 param.json 权威判定（CategoryAuthoritative），
// 则不再被启发式覆盖，避免外接盘离线时把 DLC 误改回本体。
func (s *Scanner) ReclassifyCategories() {
	list, err := s.store.ListPackagesForRefine()
	if err != nil {
		log.Printf("[Scanner] 分类校正读取失败: %v", err)
		return
	}

	fixed := 0
	for i := range list {
		p := list[i]

		want := p.Category
		if !p.CategoryAuthoritative {
			want = pkg.DetectCategory(p.FilePath, p.Title, p.ContentID)
		}

		wantDisplay := p.DisplayName
		if wantDisplay == "" || pkg.NameLooksRaw(wantDisplay, p.ContentID) {
			wantDisplay = pkg.DisplayNameFor(p.Title, p.ContentID, want, false)
		}
		if wantDisplay == "" {
			wantDisplay = p.Title
		}

		if want != p.Category || wantDisplay != p.DisplayName {
			if err := s.store.UpdateMeta(p.ID, want, wantDisplay); err != nil {
				log.Printf("[Scanner] 分类校正写入失败 id=%d: %v", p.ID, err)
				continue
			}
			fixed++
		}
	}
	if fixed > 0 {
		log.Printf("[Scanner] 分类/名称校正完成：共修正 %d 条记录", fixed)
	}
}
