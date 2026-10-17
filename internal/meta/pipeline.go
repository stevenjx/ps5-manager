package meta

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
)

// Pipeline 元数据刮削流水线。
//
// 回退顺序（First match wins：每个字段首个填成功的源即被采用）：
//  1. gamelist  —— 本地 gamelist.xml，注入中文官方简介与中文译名（最高优先级）
//  2. bangumi   —— 中文在线源（免 Key）：简体中文名 / 中文简介 / 开发商 / 发行商 / 评分
//  3. titledb   —— Switch 专用：按 TitleID 精确匹配官方 eShop 名称 / 简介 / Banner / BoxArt
//  4. hasheous / playmatch —— 哈希匹配（仅 PS3 白名单平台），命中后回填 IGDB ID
//  5. igdb      —— 通用兜底：官方简介、发售日期、开发商/发行商、竖版封面、截图
//  6. sgdb      —— SteamGridDB：补齐高清封面 / 透明 Logo / Hero 背景（不覆盖已有）
//
// 中文优先：gamelist 与 bangumi 都排在 TitleDB / IGDB 之前，因此有中文简介时
// 不会被英文数据顶替；只有中文源全部落空时才回退到英文源。
//
// 严格约束：PS4 / PS5 / Switch **不参与哈希计算**（见 HashingAllowed），
// 它们只走「规范化文件名 + (igdb-xxxx) 语义化标签」纯文件名匹配。
type Pipeline struct {
	cfg       Config
	gamelist  *GamelistProvider
	titledb   *TitleDB
	bangumi   *BangumiClient
	hasheous  *HasheousClient
	playmatch *PlaymatchClient
	igdb      *IGDBClient
	sgdb      *SGDBClient
}

// NewPipeline 构造流水线。cacheDir 用于 TitleDB 等大文件落盘缓存。
func NewPipeline(cfg Config, cacheDir string) *Pipeline {
	return &Pipeline{
		cfg:       cfg,
		gamelist:  NewGamelistProvider(),
		titledb:   NewTitleDB(cfg, cacheDir),
		bangumi:   NewBangumiClient(cfg),
		hasheous:  NewHasheousClient(cfg),
		playmatch: NewPlaymatchClient(cfg),
		igdb:      NewIGDBClient(cfg),
		sgdb:      NewSGDBClient(cfg),
	}
}

// Config 返回当前配置
func (p *Pipeline) Config() Config { return p.cfg }

// OnlineEnabled 是否有任何在线源可用
func (p *Pipeline) OnlineEnabled() bool {
	if p == nil || !p.cfg.Enabled {
		return false
	}
	return p.igdb.Enabled() || p.sgdb.Enabled() || p.bangumi.Enabled() ||
		p.hasheous.Enabled() || p.playmatch.Enabled() || len(p.cfg.SwitchTitleDBURLs) > 0
}

// Scrape 对单个目标执行完整回退流水线。
//
// scanRoot 为该文件所属扫描根目录（用于就近查找 gamelist.xml），可为空。
// 返回的 Result 可能为空（各源均未命中），但 err 只表示「出现了非致命错误」，
// 调用方应把 Result 落库、把 err 记日志即可，绝不能因此中断扫描。
func (p *Pipeline) Scrape(ctx context.Context, t Target, scanRoot string) (*Result, error) {
	if p == nil {
		return nil, nil
	}
	out := newResult()
	var errs []error

	// ---- 1) 本地 gamelist.xml（中文优先，离线可用）----
	if p.gamelist != nil {
		if gl, err := p.gamelist.Lookup(ctx, t, scanRoot); err != nil {
			errs = append(errs, err)
		} else if gl != nil {
			out.Merge(gl)
		}
	}

	if !p.cfg.Enabled {
		return out, errors.Join(errs...)
	}

	// ---- 2) Bangumi 中文元数据（免 Key；中文简介优先于英文源）----
	// 排在 TitleDB / IGDB 之前，确保中文名与中文简介不会被英文数据顶替。
	// 仅在「尚无简介」时检索，避免对已有中文本地简介的记录做无谓请求。
	// 封面此处先不合并，留给 IGDB / SteamGridDB 优先，最后再兜底（见函数末尾）。
	var bg *Result
	if p.bangumi.Enabled() && out.Summary == "" {
		if kws := bangumiKeywords(t, out); len(kws) > 0 {
			res, err := p.bangumi.Lookup(ctx, kws)
			if err != nil {
				errs = append(errs, err)
			} else if res != nil {
				bg = res
				cover := res.Artwork.Cover
				res.Artwork.Cover = ""
				out.Merge(res)
				res.Artwork.Cover = cover
			}
		}
	}

	// ---- 3) Switch TitleDB（仅 Switch，按 TitleID 精确匹配官方数据）----
	if strings.EqualFold(t.Platform, "switch") && t.TitleID != "" && p.titledb != nil {
		if m, err := p.titledb.Lookup(ctx, []string{t.TitleID}); err != nil {
			errs = append(errs, err)
		} else if e, ok := m[normalizeTitleID(t.TitleID)]; ok {
			out.Merge(e.ToResult())
		}
	}

	// ---- 4) 哈希匹配（仅白名单平台；PS4/PS5/Switch 严禁哈希）----
	igdbID := t.ProviderID
	if igdbID == 0 && HashingAllowed(t.Platform) && strings.TrimSpace(t.FilePath) != "" {
		igdbID = p.hashMatch(ctx, t, out, &errs)
	}

	// ---- 5) IGDB 官方元数据（英文兜底）----
	if p.igdb.Enabled() {
		var ig *Result
		var err error
		if igdbID > 0 {
			ig, err = p.igdb.FetchByID(ctx, igdbID)
			if err != nil {
				errs = append(errs, err)
			}
		}
		if ig == nil {
			searchName := strings.TrimSpace(out.Title)
			if searchName == "" {
				searchName = strings.TrimSpace(t.GameName)
			}
			ig, err = p.igdb.Search(ctx, searchName, t.Platform)
			if err != nil {
				errs = append(errs, err)
			}
		}
		if ig != nil {
			out.Merge(ig)
		}
	}

	// ---- 6) SteamGridDB 素材补齐（只填空缺字段）----
	if p.sgdb.Enabled() {
		name := strings.TrimSpace(out.Title)
		if name == "" {
			name = strings.TrimSpace(t.GameName)
		}
		if name != "" {
			sg, err := p.sgdb.FetchForName(ctx, name)
			if err != nil {
				errs = append(errs, err)
			} else if sg != nil {
				out.Merge(sg)
			}
		}
	}

	// 封面兜底：IGDB / SteamGridDB 均未提供封面时，采用 Bangumi 的竖版封面
	if out.Artwork.Cover == "" && bg != nil && bg.Artwork.Cover != "" {
		out.Artwork.Cover = bg.Artwork.Cover
		out.Sources[FieldCover] = SourceBangumi
	}

	return out, errors.Join(errs...)
}

// bangumiKeywords 组装用于 Bangumi 检索的关键词（按优先级，并追加繁转简变体）。
//
// 港版 / 台版游戏名多为繁体（如「戰神」「最後生還者」），而 Bangumi 以简体为主，
// 因此先试原始名，再试简体名，任一命中即返回。
func bangumiKeywords(t Target, out *Result) []string {
	base := []string{t.GameName, out.Title, out.NameCN}

	var kws []string
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		kws = append(kws, s)
	}

	for _, s := range base {
		add(s)
	}
	for _, s := range base {
		if simp := ToSimplified(s); simp != s {
			add(simp)
		}
	}
	return kws
}

// hashMatch 计算文件哈希并依次尝试 Hasheous、Playmatch
func (p *Pipeline) hashMatch(ctx context.Context, t Target, out *Result, errs *[]error) int64 {
	hashes, err := ComputeFileHashes(ctx, t.FilePath)
	if err != nil {
		*errs = append(*errs, err)
		return 0
	}
	if hashes.Empty() {
		return 0
	}
	log.Printf("[Meta] 哈希匹配 %s (%s) md5=%s", t.GameName, t.Platform, truncate(hashes.MD5, 12))

	var igdbID int64
	if p.hasheous.Enabled() {
		if res, err := p.hasheous.Lookup(ctx, hashes); err != nil {
			*errs = append(*errs, err)
		} else if res != nil {
			out.Merge(res)
			igdbID = res.IGDBID
		}
	}
	if igdbID == 0 && p.playmatch.Enabled() {
		if res, err := p.playmatch.Lookup(ctx, hashes); err != nil {
			*errs = append(*errs, err)
		} else if res != nil {
			out.Merge(res)
			igdbID = res.IGDBID
		}
	}
	return igdbID
}

// SGDBEnabled 是否配置了 SteamGridDB（批量补 Hero 图需要它）
func (p *Pipeline) SGDBEnabled() bool {
	return p != nil && p.sgdb.Enabled()
}

// FetchHero 按游戏名从 SteamGridDB 取一张 16:9 横版 Hero 背景图。
//
// 仅用于「批量补 Hero 图」功能：SteamGridDB 只提供图片，不返回任何文字，
// 因此这里只取 Artwork.Hero，不触碰其他字段。
func (p *Pipeline) FetchHero(ctx context.Context, name string) (string, error) {
	if p == nil || !p.sgdb.Enabled() {
		return "", errors.New("未配置 SteamGridDB API Key")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	res, err := p.sgdb.FetchForName(ctx, name)
	if err != nil {
		return "", err
	}
	if res == nil {
		return "", nil
	}
	return strings.TrimSpace(res.Artwork.Hero), nil
}

// FetchCoverBytes 按名称从 SteamGridDB 取一张 2:3 竖版封面并下载为字节流。
//
// 与 FetchHero 的区别：这里返回图片本身，供上层直接落盘缓存。
// 未配置 Key、未命中或下载失败时返回 (nil, nil)，调用方继续走降级链路。
func (p *Pipeline) FetchCoverBytes(ctx context.Context, name string) ([]byte, error) {
	if p == nil || !p.sgdb.Enabled() {
		return nil, errors.New("未配置 SteamGridDB API Key")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	res, err := p.sgdb.FetchForName(ctx, name)
	if err != nil || res == nil {
		return nil, err
	}
	u := strings.TrimSpace(res.Artwork.Cover)
	if u == "" {
		return nil, nil
	}
	data, err := FetchImage(ctx, p.cfg, u)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// FetchImage 下载一张图片（供上层把刮削到的封面缓存到本地封面目录复用）。
// 返回的数据已校验确为图片格式。
func FetchImage(ctx context.Context, cfg Config, rawURL string) ([]byte, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, errors.New("空图片地址")
	}
	if strings.HasPrefix(rawURL, "file://") {
		return os.ReadFile(strings.TrimPrefix(rawURL, "file://"))
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return nil, errors.New("不支持的图片地址协议")
	}
	return downloadImage(ctx, newHTTPClient(cfg.Timeout), rawURL)
}

// Status 返回刮削能力状态（供设置界面回显，凭证一律脱敏）
func (p *Pipeline) Status() map[string]any {
	if p == nil {
		return map[string]any{"enabled": false}
	}
	sources := []map[string]any{
		{"id": string(SourceGamelist), "name": "本地 gamelist.xml（中文优先）", "enabled": true, "free": true},
		{"id": string(SourceBangumi), "name": "Bangumi 番组计划（免 Key，中文简介）",
			"enabled": p.bangumi.Enabled(), "free": true},
		{"id": string(SourceTitleDB), "name": "Switch TitleDB（免 Key，首次下载约 90MB）",
			"enabled": p.cfg.TitleDBEnabled, "free": true},
		{"id": string(SourceHasheous), "name": "Hasheous（免 Key，仅 PS3 哈希）",
			"enabled": p.hasheous.Enabled(), "free": true},
		{"id": string(SourcePlaymatch), "name": "Playmatch（免 Key，仅 PS3 哈希）",
			"enabled": p.playmatch.Enabled(), "free": true},
		{"id": string(SourceIGDB), "name": "IGDB（官方元数据兜底）",
			"enabled": p.igdb.Enabled(), "free": false},
		{"id": string(SourceSGDB), "name": "SteamGridDB（高清素材）",
			"enabled": p.sgdb.Enabled(), "free": false},
	}

	return map[string]any{
		"enabled":          p.cfg.Enabled,
		"online_enabled":   p.OnlineEnabled(),
		"igdb_configured":  p.igdb.Enabled(),
		"sgdb_configured":  p.sgdb.Enabled(),
		"bangumi_enabled":  p.bangumi.Enabled(),
		"igdb_client_id":   p.cfg.IGDBClientID,
		"igdb_secret":      Mask(p.cfg.IGDBClientSecret),
		"sgdb_key":         Mask(p.cfg.SteamGridDBKey),
		"language":         p.cfg.Language,
		"region":           p.cfg.Region,
		"concurrency":      p.cfg.Concurrency,
		"priority":         []string{"gamelist", "bangumi", "titledb", "hasheous", "playmatch", "igdb", "sgdb"},
		"hash_platforms":   []string{"ps3"},
		"chinese_sources":  []string{"gamelist", "bangumi", "titledb"},
		"no_key_available": p.bangumi.Enabled() || p.hasheous.Enabled() || p.playmatch.Enabled() || p.cfg.TitleDBEnabled,
		"sources":          sources,
	}
}
