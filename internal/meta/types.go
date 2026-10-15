// Package meta 实现多平台元数据刮削：按「首个匹配成功即采用」的回退流水线，
// 依次尝试 本地 gamelist.xml（中文）→ Switch TitleDB → 哈希匹配（Hasheous/Playmatch）
// → IGDB（官方英文兜底）→ SteamGridDB（高清素材补齐）。
//
// 所有外部接口的 URL、路径、参数与字段名均来自各服务的官方规范，未做猜测：
//   - IGDB      : api-docs.igdb.com（games 字段见 igdb.go 注释）
//   - SteamGridDB: https://www.steamgriddb.com/static/openapi.yml
//   - Hasheous  : RomM backend/handler/metadata/hasheous_handler.py -> /Lookup/ByHash
//   - Playmatch : RomM backend/handler/metadata/playmatch_handler.py -> /identify/ids
//   - TitleDB   : RomM backend/tasks/scheduled/update_switch_titledb.py
//     （数据源 https://github.com/blawar/titledb）
package meta

import (
	"strings"
)

// Source 元数据来源标识
type Source string

const (
	SourceGamelist  Source = "gamelist"  // 本地 gamelist.xml（中文优先）
	SourceTitleDB   Source = "titledb"   // Switch TitleDB（官方 eShop 数据）
	SourceBangumi   Source = "bangumi"   // Bangumi 番组计划（免 Key 中文元数据）
	SourceHasheous  Source = "hasheous"  // Hasheous 哈希匹配（免 Key）
	SourcePlaymatch Source = "playmatch" // Playmatch 哈希匹配（免 Key）
	SourceIGDB      Source = "igdb"      // IGDB 官方元数据
	SourceSGDB      Source = "sgdb"      // SteamGridDB 高清素材
)

// 可被流水线填充的字段名（用于记录「每个字段来自哪个源」）
const (
	FieldTitle       = "title"
	FieldNameCN      = "name_cn"
	FieldSummary     = "summary"
	FieldReleaseDate = "release_date"
	FieldDeveloper   = "developer"
	FieldPublisher   = "publisher"
	FieldGenres      = "genres"
	FieldRating      = "rating"
	FieldCover       = "cover"
	FieldHero        = "hero"
	FieldLogo        = "logo"
	FieldBanner      = "banner"
	FieldScreenshots = "screenshots"
)

// Artwork 高清素材（均为可直接访问的图片 URL）
type Artwork struct {
	Cover       string   `json:"cover"`       // 高清竖版封面
	Hero        string   `json:"hero"`        // 横版 Hero 背景
	Logo        string   `json:"logo"`        // 无水印透明 Logo
	Banner      string   `json:"banner"`      // 横版 Banner
	Screenshots []string `json:"screenshots"` // 官方截图
}

// Result 一次刮削的归一化结果。
// 所有字段都是「尽力而为」，缺失即留空，由流水线逐源补齐。
type Result struct {
	Source      Source   `json:"source"`       // 最终主要来源（首个命中源）
	Title       string   `json:"title"`        // 官方名称
	NameCN      string   `json:"name_cn"`      // 中文译名（本地 gamelist / 中文 TitleDB）
	Summary     string   `json:"summary"`      // 游戏简介
	ReleaseDate string   `json:"release_date"` // YYYY-MM-DD
	Developer   string   `json:"developer"`    // 开发商
	Publisher   string   `json:"publisher"`    // 发行商
	Genres      []string `json:"genres"`       // 类型
	Rating      float64  `json:"rating"`       // 评分（IGDB rating，0-100）
	IGDBID      int64    `json:"igdb_id"`      // IGDB 游戏 ID
	Artwork     Artwork  `json:"artwork"`

	// Sources 记录每个字段最终由哪个源提供，便于在 UI 上说明数据出处
	Sources map[string]Source `json:"sources"`
}

func newResult() *Result {
	return &Result{Sources: map[string]Source{}}
}

// setStr 仅在目标字段为空时写入，并记录来源（First match wins）
func (r *Result) setStr(field string, src Source, dst *string, val string) {
	if dst == nil || *dst != "" {
		return
	}
	val = strings.TrimSpace(val)
	if val == "" {
		return
	}
	*dst = val
	r.Sources[field] = src
}

// setList 仅在目标切片为空时写入
func (r *Result) setList(field string, src Source, dst *[]string, val []string) {
	if dst == nil || len(*dst) > 0 || len(val) == 0 {
		return
	}
	clean := make([]string, 0, len(val))
	for _, v := range val {
		if v = strings.TrimSpace(v); v != "" {
			clean = append(clean, v)
		}
	}
	if len(clean) == 0 {
		return
	}
	*dst = clean
	r.Sources[field] = src
}

// Merge 把 other 中非空、且当前为空字段的值合并进来。
func (r *Result) Merge(other *Result) {
	if other == nil {
		return
	}
	r.setStr(FieldTitle, other.Source, &r.Title, other.Title)
	r.setStr(FieldNameCN, other.Source, &r.NameCN, other.NameCN)
	r.setStr(FieldSummary, other.Source, &r.Summary, other.Summary)
	r.setStr(FieldReleaseDate, other.Source, &r.ReleaseDate, other.ReleaseDate)
	r.setStr(FieldDeveloper, other.Source, &r.Developer, other.Developer)
	r.setStr(FieldPublisher, other.Source, &r.Publisher, other.Publisher)
	r.setList(FieldGenres, other.Source, &r.Genres, other.Genres)
	r.setStr(FieldCover, other.Source, &r.Artwork.Cover, other.Artwork.Cover)
	r.setStr(FieldHero, other.Source, &r.Artwork.Hero, other.Artwork.Hero)
	r.setStr(FieldLogo, other.Source, &r.Artwork.Logo, other.Artwork.Logo)
	r.setStr(FieldBanner, other.Source, &r.Artwork.Banner, other.Artwork.Banner)
	r.setList(FieldScreenshots, other.Source, &r.Artwork.Screenshots, other.Artwork.Screenshots)

	if r.Rating == 0 && other.Rating > 0 {
		r.Rating = other.Rating
		r.Sources[FieldRating] = other.Source
	}
	if r.IGDBID == 0 && other.IGDBID > 0 {
		r.IGDBID = other.IGDBID
	}
	if r.Source == "" {
		r.Source = other.Source
	}
}

// IsEmpty 是否什么都没刮到
func (r *Result) IsEmpty() bool {
	return r.Title == "" && r.NameCN == "" && r.Summary == "" &&
		r.Artwork.Cover == "" && r.IGDBID == 0 && r.ReleaseDate == ""
}

// ---------- 目标平台与哈希策略 ----------

// Target 一个待刮削目标（由 scanner 组装）
type Target struct {
	// GameID 数据库记录 ID
	GameID int64
	// Platform 平台 slug：ps3 / ps4 / ps5 / switch
	Platform string
	// TitleID PS 平台的 TitleID / ContentID 前缀，或 Switch 的 16 位 TitleID
	TitleID string
	// ContentID PS 平台完整 ContentID
	ContentID string
	// FilePath 本地文件路径
	FilePath string
	// GameName 由命名规范解析出的游戏名（用于在线搜索）
	GameName string
	// Regions 由文件名解析出的地区码（小写）
	Regions []string
	// Languages 由文件名解析出的语言码
	Languages []string
	// ProviderID 内联 (igdb-xxxx) 标签提供的 ID；0 表示无
	ProviderID int64
	// FileSize 文件字节数（供哈希匹配源判断）
	FileSize int64
}

// hashAllowedPlatforms 允许执行全文件哈希的平台白名单。
//
// 严格约束：PS4 / PS5 的 PKG 体积巨大，**严禁**做全文件哈希；
// Switch 的 NSP/XCI 同理（RomM 5.4.0 的 NON_HASHABLE_PLATFORMS 也包含它们）。
// 因此这里只允许 PS3（标准 Redump 镜像），其余平台一律走
// 「规范化文件名 + (igdb-xxxx) 语义化标签」纯文件名匹配。
var hashAllowedPlatforms = map[string]bool{
	"ps3": true,
}

// HashingAllowed 判断该平台是否允许计算文件哈希用于匹配
func HashingAllowed(platform string) bool {
	return hashAllowedPlatforms[strings.ToLower(strings.TrimSpace(platform))]
}
