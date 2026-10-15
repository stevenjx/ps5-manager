package meta

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// SteamGridDB 接口（https://www.steamgriddb.com/static/openapi.yml，version 2.10.0）
//
//	base : https://www.steamgriddb.com/api/v2
//	auth : Authorization: Bearer <API_KEY>
//	GET  /search/autocomplete/{term}   按名称搜索
//	GET  /grids/game/{gameId}          竖版封面
//	GET  /heroes/game/{gameId}         横版 Hero 背景
//	GET  /logos/game/{gameId}          无水印透明 Logo
//	GET  /icons/game/{gameId}          方形图标
const sgdbBase = "https://www.steamgriddb.com/api/v2"

// SGDB 素材尺寸
const (
	sgdbGridDimensions = "600x900"  // 2:3 竖版封面
	sgdbHeroDimensions = "1920x620" // 横版 Hero
)

// sgdbEnvelope SGDB 通用响应外壳
type sgdbEnvelope[T any] struct {
	Success bool `json:"success"`
	Page    int  `json:"page"`
	Total   int  `json:"total"`
	Data    []T  `json:"data"`
	Errors  any  `json:"errors"`
}

// sgdbGame 搜索结果条目
type sgdbGame struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	ReleaseDate int64  `json:"release_date"`
	Verified    bool   `json:"verified"`
}

// sgdbAsset 素材条目
type sgdbAsset struct {
	ID       int    `json:"id"`
	URL      string `json:"url"`
	Thumb    string `json:"thumb"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Style    string `json:"style"`
	Mime     string `json:"mime"`
	Author   any    `json:"author"`
	NSFW     bool   `json:"nsfw"`
	Humor    bool   `json:"humor"`
	Epilepsy bool   `json:"epilepsy"`
}

// SGDBClient SteamGridDB 客户端
type SGDBClient struct {
	apiKey string
	http   *http.Client
	// baseURL 默认取官方地址，测试时可注入 httptest 服务
	baseURL string
}

// NewSGDBClient 构造 SteamGridDB 客户端
func NewSGDBClient(cfg Config) *SGDBClient {
	return &SGDBClient{
		apiKey:  strings.TrimSpace(cfg.SteamGridDBKey),
		http:    newHTTPClient(cfg.Timeout),
		baseURL: sgdbBase,
	}
}

// Enabled 是否具备 API Key
func (c *SGDBClient) Enabled() bool {
	return c != nil && c.apiKey != ""
}

func (c *SGDBClient) headers() map[string]string {
	return map[string]string{"Authorization": "Bearer " + c.apiKey}
}

// Search 按名称搜索 SteamGridDB 条目
func (c *SGDBClient) Search(ctx context.Context, term string) ([]sgdbGame, error) {
	if !c.Enabled() {
		return nil, nil
	}
	term = strings.TrimSpace(term)
	if term == "" {
		return nil, nil
	}
	// 路径参数需转义，避免斜杠等字符破坏 URL
	url := c.baseURL + "/search/autocomplete/" + escapePathSegment(term)

	var env sgdbEnvelope[sgdbGame]
	if err := getJSON(ctx, c.http, url, c.headers(), &env); err != nil {
		return nil, err
	}
	return env.Data, nil
}

// Artwork 拉取某条目的高清素材（封面 / Hero / Logo / 图标）
func (c *SGDBClient) Artwork(ctx context.Context, gameID int) (*Result, error) {
	if !c.Enabled() || gameID <= 0 {
		return nil, nil
	}

	r := newResult()
	r.Source = SourceSGDB

	// 竖版封面：只取静态、非 NSFW / 非恶搞图
	if assets, err := c.assets(ctx, "grids", gameID, map[string]string{
		"dimensions": sgdbGridDimensions,
		"types":      "static",
		"nsfw":       "false",
		"humor":      "false",
	}); err == nil {
		if u := firstAssetURL(assets); u != "" {
			r.Artwork.Cover = u
			r.Sources[FieldCover] = SourceSGDB
		}
	}

	// 横版 Hero
	if assets, err := c.assets(ctx, "heroes", gameID, map[string]string{
		"dimensions": sgdbHeroDimensions,
		"types":      "static",
		"nsfw":       "false",
		"humor":      "false",
	}); err == nil {
		if u := firstAssetURL(assets); u != "" {
			r.Artwork.Hero = u
			r.Sources[FieldHero] = SourceSGDB
			r.Artwork.Banner = u
			r.Sources[FieldBanner] = SourceSGDB
		}
	}

	// 透明 Logo：优先 official 风格
	if assets, err := c.assets(ctx, "logos", gameID, map[string]string{
		"styles": "official",
		"types":  "static",
		"nsfw":   "false",
		"humor":  "false",
	}); err == nil {
		if u := firstAssetURL(assets); u != "" {
			r.Artwork.Logo = u
			r.Sources[FieldLogo] = SourceSGDB
		}
	}

	if r.Artwork.Cover == "" && r.Artwork.Hero == "" && r.Artwork.Logo == "" {
		return nil, nil
	}
	return r, nil
}

// assets 请求某一类素材
func (c *SGDBClient) assets(ctx context.Context, kind string, gameID int, params map[string]string) ([]sgdbAsset, error) {
	url := joinQuery(fmt.Sprintf("%s/%s/game/%d", c.baseURL, kind, gameID), params)
	var env sgdbEnvelope[sgdbAsset]
	if err := getJSON(ctx, c.http, url, c.headers(), &env); err != nil {
		return nil, err
	}
	return env.Data, nil
}

// FetchForName 先按名称搜索，再拉取最佳匹配条目的素材
func (c *SGDBClient) FetchForName(ctx context.Context, name string) (*Result, error) {
	if !c.Enabled() {
		return nil, nil
	}
	games, err := c.Search(ctx, name)
	if err != nil || len(games) == 0 {
		return nil, err
	}
	best := pickBestSGDB(games, name)
	if best == nil {
		return nil, nil
	}
	return c.Artwork(ctx, best.ID)
}

// pickBestSGDB 选出与名称最匹配的条目
func pickBestSGDB(games []sgdbGame, want string) *sgdbGame {
	wantNorm := normalizeName(want)
	bestIdx, bestScore := -1, -1.0
	for i := range games {
		got := normalizeName(games[i].Name)
		var score float64
		switch {
		case got == wantNorm:
			score = 100
		case strings.HasPrefix(got, wantNorm) || strings.HasPrefix(wantNorm, got):
			score = 80
		case strings.Contains(got, wantNorm) || strings.Contains(wantNorm, got):
			score = 65
		default:
			score = 40 * tokenOverlap(wantNorm, got)
		}
		if games[i].Verified {
			score += 5 // SGDB 人工核验过的条目更可靠
		}
		if score > bestScore {
			bestIdx, bestScore = i, score
		}
	}
	if bestIdx < 0 {
		return nil
	}
	return &games[bestIdx]
}

// firstAssetURL 返回第一个可用素材地址（优先宽高更合适者）
func firstAssetURL(assets []sgdbAsset) string {
	for _, a := range assets {
		if strings.TrimSpace(a.URL) != "" && !a.NSFW && !a.Humor {
			return a.URL
		}
	}
	return ""
}

// escapePathSegment 转义 URL 路径片段
func escapePathSegment(s string) string {
	// 保留字母数字与常见安全字符，其余百分号编码
	const safe = "-_.~!$&'()*+,;=:@"
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case strings.ContainsRune(safe, r):
			b.WriteRune(r)
		default:
			for _, by := range []byte(string(r)) {
				fmt.Fprintf(&b, "%%%02X", by)
			}
		}
	}
	return b.String()
}
