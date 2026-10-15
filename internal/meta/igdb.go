package meta

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// IGDB 接口常量（api-docs.igdb.com）
const (
	igdbTokenURL = "https://id.twitch.tv/oauth2/token"
	igdbAPIBase  = "https://api.igdb.com/v4"
)

// igdbPlatformIDs IGDB 平台 ID。
// 取自 RomM backend/adapters/services/igdb.py -> IGDB_PLATFORM_LIST（与 IGDB 官方数据一致）：
//
//	ps3 -> 9, ps4 -> 48, ps5 -> 167, switch -> 130
var igdbPlatformIDs = map[string]int{
	"ps3": 9, "ps4": 48, "ps5": 167, "switch": 130,
}

// IGDB game.category 枚举（Henry-Sarabia/igdb game.go，注释标注
// "Expected GameCategory enums from the IGDB"）：
//
//	0 MainGame, 1 DLCAddon, 2 Expansion, 3 Bundle,
//	4 StandaloneExpansion, 5 Mod, 6 Episode, 7 Season
const (
	igdbCategoryMainGame  = 0
	igdbCategoryDLCAddon  = 1
	igdbCategoryExpansion = 2
	igdbCategoryBundle    = 3
	igdbCategoryMod       = 5
	igdbCategoryEpisode   = 6
	igdbCategorySeason    = 7
)

// igdbNonBaseCategories 检索「本体」时应降权的分类。
// 未列出的取值（含未来新增分类）一律视为中性，不做臆测。
var igdbNonBaseCategories = map[int]bool{
	igdbCategoryDLCAddon:  true,
	igdbCategoryExpansion: true,
	igdbCategoryBundle:    true,
	igdbCategoryMod:       true,
	igdbCategoryEpisode:   true,
	igdbCategorySeason:    true,
}

// IGDB 图片尺寸令牌
const (
	igdbSizeCover      = "t_cover_big_2x"    // 528x748（2:3 竖版封面）
	igdbSizeScreenshot = "t_screenshot_huge" // 1280x720
	igdbSizeArtwork    = "t_1080p"           // 1920x1080 横版
)

// igdbQueryFields 请求字段，均为 IGDB game 实体标准字段
const igdbQueryFields = "fields id,name,summary,storyline,first_release_date,rating,total_rating," +
	"url,category,genres.name,cover.url,artworks.url,screenshots.url," +
	"involved_companies.company.name,involved_companies.developer,involved_companies.publisher," +
	"platforms.id,platforms.name;"

// ---------- 响应结构 ----------

type igdbNamed struct {
	Name string `json:"name"`
}

type igdbImage struct {
	URL string `json:"url"`
}

type igdbInvolvedCompany struct {
	Developer bool `json:"developer"`
	Publisher bool `json:"publisher"`
	Company   struct {
		Name string `json:"name"`
	} `json:"company"`
}

type igdbPlatform struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type igdbGame struct {
	ID                int64                 `json:"id"`
	Name              string                `json:"name"`
	Summary           string                `json:"summary"`
	Storyline         string                `json:"storyline"`
	FirstReleaseDate  int64                 `json:"first_release_date"`
	Rating            float64               `json:"rating"`
	TotalRating       float64               `json:"total_rating"`
	URL               string                `json:"url"`
	Category          int                   `json:"category"`
	Genres            []igdbNamed           `json:"genres"`
	Cover             *igdbImage            `json:"cover"`
	Artworks          []igdbImage           `json:"artworks"`
	Screenshots       []igdbImage           `json:"screenshots"`
	InvolvedCompanies []igdbInvolvedCompany `json:"involved_companies"`
	Platforms         []igdbPlatform        `json:"platforms"`
}

// ---------- 客户端 ----------

// IGDBClient IGDB 客户端，内部缓存 Twitch access_token
type IGDBClient struct {
	clientID     string
	clientSecret string
	http         *http.Client

	// tokenURL / apiBase 默认取官方地址，测试时可注入 httptest 服务
	tokenURL string
	apiBase  string

	mu          sync.Mutex
	token       string
	tokenExpiry time.Time
}

// NewIGDBClient 构造 IGDB 客户端
func NewIGDBClient(cfg Config) *IGDBClient {
	return &IGDBClient{
		clientID:     strings.TrimSpace(cfg.IGDBClientID),
		clientSecret: strings.TrimSpace(cfg.IGDBClientSecret),
		http:         newHTTPClient(cfg.Timeout),
		tokenURL:     igdbTokenURL,
		apiBase:      igdbAPIBase,
	}
}

// Enabled 是否具备凭证
func (c *IGDBClient) Enabled() bool {
	return c != nil && c.clientID != "" && c.clientSecret != ""
}

// accessToken 获取（或复用）access_token
func (c *IGDBClient) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.token != "" && time.Now().Before(c.tokenExpiry) {
		tok := c.token
		c.mu.Unlock()
		return tok, nil
	}
	c.mu.Unlock()

	endpoint := joinQuery(c.tokenURL, map[string]string{
		"client_id":     c.clientID,
		"client_secret": c.clientSecret,
		"grant_type":    "client_credentials",
	})
	var resp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := postJSON(ctx, c.http, endpoint, nil, nil, &resp); err != nil {
		return "", fmt.Errorf("IGDB 获取 access_token 失败: %w", err)
	}
	if resp.AccessToken == "" {
		return "", fmt.Errorf("IGDB 返回空 access_token")
	}

	c.mu.Lock()
	c.token = resp.AccessToken
	ttl := time.Duration(resp.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = time.Hour
	}
	if ttl > time.Minute {
		ttl -= time.Minute // 提前 60 秒失效，避免边界问题
	}
	c.tokenExpiry = time.Now().Add(ttl)
	c.mu.Unlock()

	return resp.AccessToken, nil
}

// query 执行一次 Apicalypse 查询
func (c *IGDBClient) query(ctx context.Context, endpoint, body string, out any) error {
	tok, err := c.accessToken(ctx)
	if err != nil {
		return err
	}
	headers := map[string]string{
		"Client-ID":     c.clientID,
		"Authorization": "Bearer " + tok,
	}
	return postRaw(ctx, c.http, c.apiBase+endpoint, "text/plain", body, headers, out)
}

// FetchByID 按 IGDB ID 精确获取（用于 (igdb-xxxx) 内联标签）
func (c *IGDBClient) FetchByID(ctx context.Context, id int64) (*Result, error) {
	if !c.Enabled() || id <= 0 {
		return nil, nil
	}
	body := igdbQueryFields + fmt.Sprintf(" where id = %d; limit 1;", id)
	var games []igdbGame
	if err := c.query(ctx, "/games", body, &games); err != nil {
		return nil, err
	}
	if len(games) == 0 {
		return nil, nil
	}
	return convertIGDB(games[0], SourceIGDB), nil
}

// Search 按名称检索并挑选最匹配的一条
func (c *IGDBClient) Search(ctx context.Context, name, platform string) (*Result, error) {
	if !c.Enabled() {
		return nil, nil
	}
	name = strings.TrimSpace(name)
	if len([]rune(name)) < 2 {
		return nil, nil
	}

	// Apicalypse 字符串转义：去掉双引号与反斜杠，避免破坏查询语法
	safe := strings.NewReplacer(`"`, " ", `\`, " ").Replace(name)
	body := igdbQueryFields + fmt.Sprintf(` search "%s"; limit 20;`, safe)

	var games []igdbGame
	if err := c.query(ctx, "/games", body, &games); err != nil {
		return nil, err
	}
	if len(games) == 0 {
		return nil, nil
	}

	best := pickBestIGDB(games, name, platform)
	if best == nil {
		return nil, nil
	}
	return convertIGDB(*best, SourceIGDB), nil
}

// ---------- 匹配打分 ----------

var nonAlnumRe = regexp.MustCompile(`[^a-z0-9]+`)

// normalizeName 归一化名称用于比较（小写、去标点、压空白）
func normalizeName(s string) string {
	s = strings.ToLower(s)
	s = nonAlnumRe.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(s), " ")
}

// pickBestIGDB 在候选集中选出最匹配的条目
func pickBestIGDB(games []igdbGame, want, platform string) *igdbGame {
	wantNorm := normalizeName(want)
	platID, hasPlat := igdbPlatformIDs[strings.ToLower(strings.TrimSpace(platform))]

	bestIdx, bestScore := -1, math.Inf(-1)
	for i := range games {
		score := igdbScore(&games[i], wantNorm, platID, hasPlat)
		if score > bestScore {
			bestIdx, bestScore = i, score
		}
	}
	if bestIdx < 0 {
		return nil
	}
	return &games[bestIdx]
}

// igdbScore 综合名称相似度、内容分类与平台命中打分
func igdbScore(g *igdbGame, wantNorm string, platID int, hasPlat bool) float64 {
	gotNorm := normalizeName(g.Name)

	var score float64
	switch {
	case gotNorm == "" || wantNorm == "":
		score = 0
	case gotNorm == wantNorm:
		score = 100
	case strings.HasPrefix(gotNorm, wantNorm) || strings.HasPrefix(wantNorm, gotNorm):
		score = 80
	case strings.Contains(gotNorm, wantNorm) || strings.Contains(wantNorm, gotNorm):
		score = 65
	default:
		score = 40 * tokenOverlap(wantNorm, gotNorm) // 0..40
	}

	// 内容分类：DLC / 资料片 / 合集等非本体降权
	if igdbNonBaseCategories[g.Category] {
		score -= 45
	} else if g.Category == igdbCategoryMainGame {
		score += 6
	}

	// 平台命中加成（软性，不做硬过滤，避免漏匹配）
	if hasPlat {
		for _, p := range g.Platforms {
			if p.ID == platID {
				score += 12
				break
			}
		}
	}

	if g.Cover != nil && g.Cover.URL != "" {
		score += 3
	}
	if strings.TrimSpace(g.Summary) != "" {
		score += 2
	}
	if g.FirstReleaseDate > 0 {
		score += 0.5
	}
	return score
}

// tokenOverlap 词元 Jaccard 相似度（0..1）
func tokenOverlap(a, b string) float64 {
	as := strings.Fields(a)
	bs := strings.Fields(b)
	if len(as) == 0 || len(bs) == 0 {
		return 0
	}
	set := make(map[string]bool, len(as))
	for _, t := range as {
		set[t] = true
	}
	hit := 0
	for _, t := range bs {
		if set[t] {
			hit++
		}
	}
	union := len(as) + len(bs) - hit
	if union <= 0 {
		return 0
	}
	return float64(hit) / float64(union)
}

// convertIGDB 把 IGDB 响应转换为统一 Result
func convertIGDB(g igdbGame, src Source) *Result {
	r := newResult()
	r.Source = src
	r.IGDBID = g.ID
	r.Title = g.Name
	if g.Name != "" {
		r.Sources[FieldTitle] = src
	}

	summary := strings.TrimSpace(g.Summary)
	if summary == "" {
		summary = strings.TrimSpace(g.Storyline)
	}
	if summary != "" {
		r.Summary = summary
		r.Sources[FieldSummary] = src
	}

	if g.FirstReleaseDate > 0 {
		r.ReleaseDate = time.Unix(g.FirstReleaseDate, 0).UTC().Format("2006-01-02")
		r.Sources[FieldReleaseDate] = src
	}

	for _, ic := range g.InvolvedCompanies {
		name := strings.TrimSpace(ic.Company.Name)
		if name == "" {
			continue
		}
		if ic.Developer && r.Developer == "" {
			r.Developer = name
			r.Sources[FieldDeveloper] = src
		}
		if ic.Publisher && r.Publisher == "" {
			r.Publisher = name
			r.Sources[FieldPublisher] = src
		}
	}

	genres := make([]string, 0, len(g.Genres))
	for _, gn := range g.Genres {
		if gn.Name != "" {
			genres = append(genres, gn.Name)
		}
	}
	if len(genres) > 0 {
		r.Genres = genres
		r.Sources[FieldGenres] = src
	}

	// 评分：优先用户评分 rating，其次综合分 total_rating（均为 0-100）
	if g.Rating > 0 {
		r.Rating = math.Round(g.Rating*10) / 10
	} else if g.TotalRating > 0 {
		r.Rating = math.Round(g.TotalRating*10) / 10
	}
	if r.Rating > 0 {
		r.Sources[FieldRating] = src
	}

	if g.Cover != nil && g.Cover.URL != "" {
		r.Artwork.Cover = igdbImageURL(g.Cover.URL, igdbSizeCover)
		r.Sources[FieldCover] = src
	}
	for _, a := range g.Artworks {
		if a.URL != "" {
			r.Artwork.Hero = igdbImageURL(a.URL, igdbSizeArtwork)
			r.Sources[FieldHero] = src
			break
		}
	}
	shots := make([]string, 0, len(g.Screenshots))
	for _, s := range g.Screenshots {
		if s.URL != "" {
			shots = append(shots, igdbImageURL(s.URL, igdbSizeScreenshot))
		}
	}
	if len(shots) > 0 {
		r.Artwork.Screenshots = shots
		r.Sources[FieldScreenshots] = src
	}
	return r
}

// igdbImageURL 把 IGDB 图片地址改写为指定尺寸，并补全 https 前缀。
//
// IGDB 返回形如 `//images.igdb.com/igdb/image/upload/t_thumb/abc123.jpg`
func igdbImageURL(raw, size string) string {
	u := strings.TrimSpace(raw)
	if u == "" {
		return ""
	}
	if strings.HasPrefix(u, "//") {
		u = "https:" + u
	} else if !strings.HasPrefix(u, "http") {
		u = "https://images.igdb.com/igdb/image/upload/" + strings.TrimPrefix(u, "/")
	}
	if size != "" {
		if idx := strings.Index(u, "/upload/"); idx >= 0 {
			head := u[:idx+len("/upload/")]
			rest := u[idx+len("/upload/"):]
			if slash := strings.Index(rest, "/"); slash >= 0 {
				u = head + size + rest[slash:]
			}
		}
	}
	return u
}
