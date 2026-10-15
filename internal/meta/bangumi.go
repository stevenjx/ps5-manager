package meta

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Bangumi（番组计划 bgm.tv）中文游戏元数据客户端。
//
// 定位：**免 API Key 的中文元数据源**，用来补上「没有 IGDB 凭证时中文简介缺失」的短板。
// 官方 API 文档：https://github.com/bangumi/api
//   - 检索：POST /v0/search/subjects  body {"keyword": "...", "filter": {"type": [4]}}
//     （type=4 为「游戏」条目类型）
//   - 检索结果已包含 name / name_cn / summary / date / images / infobox / rating / collection，
//     因此无需再请求 /v0/subjects/{id} 详情。
//
// 官方要求所有请求携带可识别来源的 User-Agent，并对单 IP 有频率限制，
// 因此这里做了最小请求间隔（bangumiMinInterval）。
type BangumiClient struct {
	enabled bool
	baseURL string
	http    *http.Client

	mu       sync.Mutex
	lastCall time.Time
}

// bangumiMinInterval 两次请求之间的最小间隔（官方建议单 IP 不超过 1 次/秒）
const bangumiMinInterval = 300 * time.Millisecond

// bangumiSubjectTypeGame Bangumi 条目类型：游戏
const bangumiSubjectTypeGame = 4

// NewBangumiClient 构造 Bangumi 客户端
func NewBangumiClient(cfg Config) *BangumiClient {
	return &BangumiClient{
		enabled: cfg.BangumiEnabled,
		baseURL: DefaultBangumiURL,
		http:    newHTTPClient(cfg.Timeout),
	}
}

// Enabled 是否启用
func (c *BangumiClient) Enabled() bool {
	return c != nil && c.enabled
}

// ---------- 请求 / 响应结构 ----------

type bangumiSearchReq struct {
	Keyword string            `json:"keyword"`
	Filter  bangumiSearchFltr `json:"filter"`
}

type bangumiSearchFltr struct {
	Type []int `json:"type"`
}

type bangumiSearchResp struct {
	Data []bangumiSubject `json:"data"`
}

type bangumiSubject struct {
	ID         int64             `json:"id"`
	Name       string            `json:"name"`
	NameCN     string            `json:"name_cn"`
	Summary    string            `json:"summary"`
	Date       string            `json:"date"`
	Platform   string            `json:"platform"`
	Images     map[string]string `json:"images"`
	Rating     bangumiRating     `json:"rating"`
	Collection bangumiCollection `json:"collection"`
	Infobox    []bangumiInfoItem `json:"infobox"`
}

type bangumiRating struct {
	Score float64 `json:"score"`
	Total int     `json:"total"`
}

type bangumiCollection struct {
	Collect int `json:"collect"`
	Wish    int `json:"wish"`
	Doing   int `json:"doing"`
	OnHold  int `json:"on_hold"`
	Dropped int `json:"dropped"`
}

// bangumiInfoItem infobox 的值可能是字符串，也可能是 [{"v": "..."}] 数组
type bangumiInfoItem struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

// ---------- 查询 ----------

// Lookup 依次用 keywords 检索，返回首个匹配成功的条目结果。
//
// keywords 由调用方按优先级排列（通常是：文件名解析名 → 已有标题 → 繁转简变体）。
// 全部关键词都无匹配时返回 (nil, nil)；返回 err 仅表示发生了网络/解析错误。
func (c *BangumiClient) Lookup(ctx context.Context, keywords []string) (*Result, error) {
	if c == nil || !c.enabled {
		return nil, nil
	}

	var lastErr error
	for _, kw := range dedupeStrings(keywords) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		subs, err := c.search(ctx, kw)
		if err != nil {
			lastErr = err
			continue
		}
		if best := pickBestBangumi(subs, kw); best != nil {
			return best.toResult(), nil
		}
	}
	return nil, lastErr
}

// search 执行一次条目检索
func (c *BangumiClient) search(ctx context.Context, keyword string) ([]bangumiSubject, error) {
	c.throttle()
	var resp bangumiSearchResp
	body := bangumiSearchReq{
		Keyword: keyword,
		Filter:  bangumiSearchFltr{Type: []int{bangumiSubjectTypeGame}},
	}
	if err := postJSON(ctx, c.http, c.baseURL+"/v0/search/subjects", nil, body, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// throttle 限制请求频率，避免触发 Bangumi 的速率限制
func (c *BangumiClient) throttle() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if wait := bangumiMinInterval - time.Since(c.lastCall); wait > 0 {
		time.Sleep(wait)
	}
	c.lastCall = time.Now()
}

// ---------- 匹配打分 ----------

// pickBestBangumi 从候选条目中挑选与关键词最匹配的一条。
//
// 打分规则：
//   - 名称完全一致           → 100
//   - 一方包含另一方         → 60 ~ 90（按长度比例，越接近越高）
//   - 低于阈值视为不匹配     → 丢弃
//
// 分数相同时优先「收藏数（collection.collect）更多」的条目，
// 这样 "God of War" 会命中 2018 版而不是 2005 版。
func pickBestBangumi(subs []bangumiSubject, keyword string) *bangumiSubject {
	kw := normalizeCJKName(keyword)
	if kw == "" {
		return nil
	}

	type scored struct {
		sub   bangumiSubject
		score float64
	}
	var list []scored
	for _, s := range subs {
		if sc := bangumiScore(s, kw); sc >= bangumiMatchThreshold {
			list = append(list, scored{sub: s, score: sc})
		}
	}
	if len(list) == 0 {
		return nil
	}

	sort.SliceStable(list, func(i, j int) bool {
		if list[i].score != list[j].score {
			return list[i].score > list[j].score
		}
		return list[i].sub.Collection.Collect > list[j].sub.Collection.Collect
	})
	best := list[0].sub
	return &best
}

// bangumiMatchThreshold 低于该分数视为误配（宁缺毋滥）。
//
// 取值说明：精确匹配为 100；包含匹配按覆盖率给分。
// 50 分意味着候选名与关键词至少有约一半重合，足以挡住
// 「Cadence of Hyrule … Featuring The Legend of Zelda」这类长尾误配（约 27%），
// 同时允许「The Legend of Zelda: Breath of the Wild」命中（约 52%）。
const bangumiMatchThreshold = 50

// bangumiScore 计算候选条目与关键词的匹配分（0-100）。
//
// 关键：包含匹配按「覆盖率」给分，而不是固定给高分。
// 例如关键词 "thelegendofzelda" 与候选
// "cadenceofhyrulecryptofthenecrodancerfeaturingthelegendofzelda" 虽然存在包含关系，
// 但覆盖率仅约 27%，必须被判为误配丢弃；
// 而 "persona5royal" 对 "persona5" 的覆盖率约 64%，属于合理命中。
func bangumiScore(s bangumiSubject, kw string) float64 {
	best := 0.0
	for _, raw := range []string{s.Name, s.NameCN} {
		n := normalizeCJKName(raw)
		if n == "" {
			continue
		}
		var sc float64
		switch {
		case n == kw:
			sc = 100
		case strings.Contains(n, kw) || strings.Contains(kw, n):
			short, long := len(n), len(kw)
			if short > long {
				short, long = long, short
			}
			sc = 100 * float64(short) / float64(long)
		}
		if sc > best {
			best = sc
		}
	}
	return best
}

// ---------- 转换为 Result ----------

// toResult 把 Bangumi 条目转换为统一结果。
//
// 注意：这里**不写 Title**（官方原名交给 IGDB / 文件解析），只填中文相关字段；
// 封面也仅作为兜底，由流水线在 IGDB / SteamGridDB 均无封面时才采用。
func (s *bangumiSubject) toResult() *Result {
	r := newResult()
	r.Source = SourceBangumi

	// 中文名：优先 name_cn（简体）；若 name 本身已是中文则兜底使用
	cn := strings.TrimSpace(s.NameCN)
	if cn == "" && containsCJK(s.Name) {
		cn = strings.TrimSpace(s.Name)
	}
	if cn != "" {
		r.NameCN = cn
		r.Sources[FieldNameCN] = SourceBangumi
	}

	if v := strings.TrimSpace(s.Summary); v != "" {
		r.Summary = v
		r.Sources[FieldSummary] = SourceBangumi
	}
	if d := normalizeBangumiDate(s.Date); d != "" {
		r.ReleaseDate = d
		r.Sources[FieldReleaseDate] = SourceBangumi
	}

	dev, pub, genres := bangumiInfobox(s.Infobox)
	if dev != "" {
		r.Developer = dev
		r.Sources[FieldDeveloper] = SourceBangumi
	}
	if pub != "" {
		r.Publisher = pub
		r.Sources[FieldPublisher] = SourceBangumi
	}
	if len(genres) > 0 {
		r.Genres = genres
		r.Sources[FieldGenres] = SourceBangumi
	}

	// Bangumi 评分是 0-10，统一换算为 0-100
	if s.Rating.Score > 0 {
		rating := s.Rating.Score * 10
		if rating > 100 {
			rating = 100
		}
		r.Rating = rating
		r.Sources[FieldRating] = SourceBangumi
	}

	if img := strings.TrimSpace(s.Images["large"]); img != "" {
		r.Artwork.Cover = img
		r.Sources[FieldCover] = SourceBangumi
	}
	return r
}

// bangumiInfobox 从 infobox 中提取开发商 / 发行商 / 游戏类型
func bangumiInfobox(items []bangumiInfoItem) (developer, publisher string, genres []string) {
	for _, it := range items {
		val := bangumiInfoValue(it.Value)
		if val == "" {
			continue
		}
		switch strings.TrimSpace(it.Key) {
		case "开发", "开发商", "制作":
			if developer == "" {
				developer = val
			}
		case "发行", "发行商", "出版社":
			if publisher == "" {
				publisher = val
			}
		case "游戏类型", "类型":
			if len(genres) == 0 {
				genres = splitGenres(val)
			}
		}
	}
	return developer, publisher, genres
}

// bangumiInfoValue 解析 infobox 的值（字符串 或 [{"v":"..."}]）
func bangumiInfoValue(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return strings.TrimSpace(str)
	}
	var arr []struct {
		V string `json:"v"`
	}
	if err := json.Unmarshal(raw, &arr); err == nil {
		var parts []string
		for _, a := range arr {
			if v := strings.TrimSpace(a.V); v != "" {
				parts = append(parts, v)
			}
		}
		return strings.Join(parts, "、")
	}
	return ""
}

// ---------- 小工具 ----------

// normalizeCJKName 归一化名称：小写、去掉空格与常见标点，便于比较。
//
// 与 igdb.go 的 normalizeName 不同：这里**保留中日韩字符**，
// 因为中文游戏名（如「最后生还者」）一旦被 [^a-z0-9] 规则剔除就什么都不剩了。
func normalizeCJKName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r',
			'-', '_', ':', '：', '·', '・', '•',
			'.', ',', '，', '、', ';', '；',
			'(', ')', '（', '）', '[', ']', '【', '】',
			'\'', '"', '’', '‘', '“', '”', '「', '」', '『', '』',
			'!', '！', '?', '？', '/', '\\', '&', '+', '~', '～', '#', '@', '*':
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// normalizeBangumiDate 把 Bangumi 的各种日期写法统一为 YYYY-MM-DD
func normalizeBangumiDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// 统一分隔符，便于按 4-2-2 切分
	repl := strings.NewReplacer("年", "-", "月", "-", "日", "", "/", "-", ".", "-")
	s = repl.Replace(s)
	parts := strings.Split(s, "-")
	if len(parts) < 2 {
		return ""
	}
	year := strings.TrimSpace(parts[0])
	month := strings.TrimSpace(parts[1])
	if len(year) != 4 {
		return ""
	}
	month = leftPad2(month)
	if month == "" {
		return year
	}
	if len(parts) < 3 {
		return year + "-" + month
	}
	day := leftPad2(strings.TrimSpace(parts[2]))
	if day == "" {
		return year + "-" + month
	}
	return year + "-" + month + "-" + day
}

// leftPad2 把 "1" 补成 "01"；非 1-2 位数字返回空
func leftPad2(s string) string {
	if len(s) == 1 && s[0] >= '0' && s[0] <= '9' {
		return "0" + s
	}
	if len(s) == 2 && s[0] >= '0' && s[0] <= '9' && s[1] >= '0' && s[1] <= '9' {
		return s
	}
	return ""
}

// containsCJK 是否含有中日韩统一表意文字
func containsCJK(s string) bool {
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

// dedupeStrings 去重并剔除空白项（保持原顺序）
func dedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// ToSimplified 把繁体字符串转换为简体（仅做字符级映射）。
//
// 用于把港版 / 台版游戏的繁体名（如「戰神」「最後生還者」）转为简体，
// 以便在中文元数据源中检索。映射表见 tradsimp.go（OpenCC TSCharacters 子集）。
func ToSimplified(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	changed := false
	for _, r := range s {
		if simp, ok := tradToSimp[r]; ok {
			b.WriteRune(simp)
			changed = true
			continue
		}
		b.WriteRune(r)
	}
	if !changed {
		return s
	}
	return b.String()
}
