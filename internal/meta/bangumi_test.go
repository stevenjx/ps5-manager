package meta

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// bangumiStubServer 返回一个模拟 Bangumi 搜索接口的服务端。
// 命中 keywords 中任一关键词时返回 payload，否则返回空 data。
func bangumiStubServer(t *testing.T, payload map[string][]byte, hits *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			atomic.AddInt32(hits, 1)
		}
		if r.URL.Path != "/v0/search/subjects" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("请求必须携带 User-Agent")
		}
		var req struct {
			Keyword string `json:"keyword"`
			Filter  struct {
				Type []int `json:"type"`
			} `json:"filter"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if len(req.Filter.Type) != 1 || req.Filter.Type[0] != bangumiSubjectTypeGame {
			t.Errorf("filter.type 应为 [4]（游戏），得到 %v", req.Filter.Type)
		}
		w.Header().Set("Content-Type", "application/json")
		if body, ok := payload[req.Keyword]; ok {
			_, _ = w.Write(body)
			return
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
}

func newBangumiTestClient(baseURL string) *BangumiClient {
	cfg := DefaultConfig()
	cfg.BangumiEnabled = true
	c := NewBangumiClient(cfg)
	c.baseURL = baseURL
	return c
}

// godOfWarPayload 模拟 "God of War" 的检索结果：2005 版在前、2018 版在后。
// 两者名称完全相同，因此必须靠「收藏数」把 2018 版挑出来。
const godOfWarPayload = `{
  "data": [
    {
      "id": 6309, "name": "God of War", "name_cn": "战神",
      "date": "2005-03-22", "platform": "游戏",
      "summary": "初代战神。",
      "rating": {"score": 7.6, "total": 500},
      "collection": {"collect": 653, "wish": 112, "doing": 16, "on_hold": 40, "dropped": 19},
      "images": {"large": "https://lain.bgm.tv/pic/cover/l/old.jpg"},
      "infobox": [{"key": "开发", "value": "SCE Santa Monica Studio"}]
    },
    {
      "id": 185189, "name": "God of War", "name_cn": "战神",
      "date": "2018-04-20", "platform": "游戏",
      "summary": "由 Santa Monica Studio 打造，克雷多斯在北欧神话中的全新旅程。",
      "rating": {"score": 8.0, "total": 3000},
      "collection": {"collect": 2341, "wish": 389, "doing": 154, "on_hold": 152, "dropped": 70},
      "images": {"large": "https://lain.bgm.tv/pic/cover/l/new.jpg"},
      "infobox": [
        {"key": "中文名", "value": "战神"},
        {"key": "平台", "value": [{"v": "PS4"}, {"v": "PC"}]},
        {"key": "游戏类型", "value": "ACT"},
        {"key": "开发", "value": "Santa Monica Studio"},
        {"key": "发行", "value": "索尼互动娱乐"}
      ]
    }
  ]
}`

func TestBangumi_PrefersMostCollectedOnTie(t *testing.T) {
	srv := bangumiStubServer(t, map[string][]byte{
		"God of War": []byte(godOfWarPayload),
	}, nil)
	defer srv.Close()

	c := newBangumiTestClient(srv.URL)
	res, err := c.Lookup(context.Background(), []string{"God of War"})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("应命中 2018 版")
	}
	if res.ReleaseDate != "2018-04-20" {
		t.Errorf("ReleaseDate = %q, 期望 2018-04-20（收藏数更高的 2018 版）", res.ReleaseDate)
	}
	if res.NameCN != "战神" {
		t.Errorf("NameCN = %q", res.NameCN)
	}
	if res.Developer != "Santa Monica Studio" {
		t.Errorf("Developer = %q", res.Developer)
	}
	if res.Publisher != "索尼互动娱乐" {
		t.Errorf("Publisher = %q", res.Publisher)
	}
	if len(res.Genres) != 1 || res.Genres[0] != "ACT" {
		t.Errorf("Genres = %v", res.Genres)
	}
	if res.Rating != 80 {
		t.Errorf("Rating = %v, 期望 80（8.0 × 10）", res.Rating)
	}
	if res.Source != SourceBangumi {
		t.Errorf("Source = %q", res.Source)
	}
	if res.Title != "" {
		t.Errorf("Title 应留空交给 IGDB/文件解析，得到 %q", res.Title)
	}
	if res.Artwork.Cover != "https://lain.bgm.tv/pic/cover/l/new.jpg" {
		t.Errorf("Cover = %q", res.Artwork.Cover)
	}
}

func TestBangumi_SimplifiedChineseKeyword(t *testing.T) {
	// 简体关键词应能匹配 Bangumi 的 name_cn（可能带空格）
	payload := `{"data":[{
	  "id": 1, "name": "The Last of Us", "name_cn": "最后生还者",
	  "date": "2013-06-14", "summary": "末日题材动作冒险。",
	  "collection": {"collect": 1200},
	  "infobox": [{"key": "开发", "value": "Naughty Dog"}]
	}]}`
	srv := bangumiStubServer(t, map[string][]byte{"最后生还者": []byte(payload)}, nil)
	defer srv.Close()

	c := newBangumiTestClient(srv.URL)
	res, err := c.Lookup(context.Background(), []string{"最后生还者"})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.NameCN != "最后生还者" {
		t.Fatalf("应命中「最后生还者」，得到 %+v", res)
	}
	if res.Developer != "Naughty Dog" {
		t.Errorf("Developer = %q", res.Developer)
	}
}

func TestBangumi_FallsBackToNextKeyword(t *testing.T) {
	// 繁体关键词搜不到 → 自动尝试下一个（简体）关键词
	var hits int32
	srv := bangumiStubServer(t, map[string][]byte{
		"战神4": []byte(`{"data":[{
		  "id": 185189, "name": "God of War", "name_cn": "战神",
		  "date": "2018-04-20", "summary": "北欧神话新篇章。",
		  "collection": {"collect": 2341}
		}]}`),
	}, &hits)
	defer srv.Close()

	c := newBangumiTestClient(srv.URL)
	res, err := c.Lookup(context.Background(), []string{"戰神四", "战神4"})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("第二个关键词应命中")
	}
	if n := atomic.LoadInt32(&hits); n != 2 {
		t.Errorf("应发起 2 次检索（首个落空后回退），实际 %d 次", n)
	}
}

func TestBangumi_RejectsWeakMatch(t *testing.T) {
	// 名称完全不相干的候选必须被阈值挡掉（宁缺毋滥）
	payload := `{"data":[{
	  "id": 9, "name": "Some Unrelated Game", "name_cn": "无关游戏",
	  "date": "2001-01-01", "summary": "无关。",
	  "collection": {"collect": 10}
	}]}`
	srv := bangumiStubServer(t, map[string][]byte{"Control Resonant": []byte(payload)}, nil)
	defer srv.Close()

	c := newBangumiTestClient(srv.URL)
	res, err := c.Lookup(context.Background(), []string{"Control Resonant"})
	if err != nil {
		t.Fatal(err)
	}
	if res != nil {
		t.Errorf("弱匹配应被丢弃，得到 %+v", res)
	}
}

func TestBangumi_DisabledMakesNoRequest(t *testing.T) {
	var hits int32
	srv := bangumiStubServer(t, nil, &hits)
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.BangumiEnabled = false
	c := NewBangumiClient(cfg)
	c.baseURL = srv.URL

	res, err := c.Lookup(context.Background(), []string{"God of War"})
	if err != nil || res != nil {
		t.Fatalf("关闭后应返回 (nil, nil)，得到 (%v, %v)", res, err)
	}
	if n := atomic.LoadInt32(&hits); n != 0 {
		t.Errorf("关闭后不应发起请求，实际 %d 次", n)
	}
}

func TestNormalizeBangumiDate(t *testing.T) {
	cases := map[string]string{
		"2018-04-20": "2018-04-20",
		"2020年1月11日": "2020-01-11",
		"2019/10/31": "2019-10-31",
		"2019.10.31": "2019-10-31",
		"2019-1-5":   "2019-01-05",
		"2019":       "",
		"":           "",
		"即将发售":       "",
	}
	for in, want := range cases {
		if got := normalizeBangumiDate(in); got != want {
			t.Errorf("normalizeBangumiDate(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

func TestToSimplified(t *testing.T) {
	cases := map[string]string{
		"戰神":         "战神",
		"最後生還者":      "最后生还者",
		"女神異聞錄5皇家版":  "女神异闻录5皇家版",
		"God of War": "God of War", // 无繁体字时原样返回
		"":           "",
	}
	for in, want := range cases {
		if got := ToSimplified(in); got != want {
			t.Errorf("ToSimplified(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

func TestNormalizeCJKName_KeepsChinese(t *testing.T) {
	// 关键回归：中文不能被剔成空串（igdb.go 的 normalizeName 会这么做）
	if got := normalizeCJKName("最后生还者"); got != "最后生还者" {
		t.Errorf("中文名被破坏: %q", got)
	}
	if got := normalizeCJKName("Persona 5 Royal"); got != "persona5royal" {
		t.Errorf("normalizeCJKName(\"Persona 5 Royal\") = %q", got)
	}
	if got := normalizeCJKName("God of War: Ragnarök"); !strings.Contains(got, "godofwar") {
		t.Errorf("normalizeCJKName 结果异常: %q", got)
	}
}

func TestPipeline_ChineseSourceOrder(t *testing.T) {
	// 中文源（gamelist / titledb / bangumi）必须排在 IGDB 之前：
	// 有中文简介时不得被 IGDB 的英文简介顶替。
	p := NewPipeline(DefaultConfig(), t.TempDir())

	prio, ok := p.Status()["priority"].([]string)
	if !ok {
		t.Fatal("Status().priority 类型异常")
	}
	idx := make(map[string]int, len(prio))
	for i, name := range prio {
		idx[name] = i
	}
	for _, name := range []string{"gamelist", "titledb", "bangumi", "igdb", "sgdb"} {
		if _, ok := idx[name]; !ok {
			t.Fatalf("priority 缺少 %q，实际为 %v", name, prio)
		}
	}
	for _, name := range []string{"gamelist", "bangumi", "titledb"} {
		if idx[name] > idx["igdb"] {
			t.Errorf("中文源 %s 应排在 igdb 之前，实际顺序 %v", name, prio)
		}
	}
	if idx["gamelist"] > idx["bangumi"] {
		t.Errorf("gamelist 应优先于 bangumi，实际顺序 %v", prio)
	}
	// Bangumi 必须在 TitleDB 之前，否则英文简介会挡住中文简介
	if idx["bangumi"] > idx["titledb"] {
		t.Errorf("bangumi 应优先于 titledb（中文优先），实际顺序 %v", prio)
	}
}

func TestBangumi_RejectsLongTailContainment(t *testing.T) {
	// 回归：搜索 "The Legend of Zelda" 时，不能命中
	// 「Cadence of Hyrule … Featuring The Legend of Zelda」这类长尾条目，
	// 而应命中覆盖率更高的《旷野之息》。
	payload := `{"data":[
	  {
	    "id": 278458, "name": "Cadence of Hyrule — Crypt of the NecroDancer Featuring The Legend of Zelda",
	    "name_cn": "凯登丝勇闯海拉鲁：死灵舞师地牢 塞尔达传说 合作巨献",
	    "date": "2019-06-13", "summary": "节奏地牢联动作品。",
	    "collection": {"collect": 300}
	  },
	  {
	    "id": 12345, "name": "The Legend of Zelda: Breath of the Wild",
	    "name_cn": "塞尔达传说：旷野之息",
	    "date": "2017-03-03", "summary": "在海拉鲁大陆展开的全新冒险。",
	    "collection": {"collect": 3000}
	  }
	]}`
	srv := bangumiStubServer(t, map[string][]byte{"The Legend of Zelda": []byte(payload)}, nil)
	defer srv.Close()

	c := newBangumiTestClient(srv.URL)
	res, err := c.Lookup(context.Background(), []string{"The Legend of Zelda"})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("应命中覆盖率更高的条目")
	}
	if res.NameCN != "塞尔达传说：旷野之息" {
		t.Errorf("NameCN = %q，期望命中《旷野之息》而非长尾联动作品", res.NameCN)
	}
}

func TestBangumiScore_Coverage(t *testing.T) {
	kw := normalizeCJKName("The Legend of Zelda")
	longTail := bangumiSubject{Name: "Cadence of Hyrule — Crypt of the NecroDancer Featuring The Legend of Zelda"}
	bw := bangumiSubject{Name: "The Legend of Zelda: Breath of the Wild"}

	if sc := bangumiScore(longTail, kw); sc >= bangumiMatchThreshold {
		t.Errorf("长尾条目得分 %.1f 应低于阈值 %d", sc, bangumiMatchThreshold)
	}
	if sc := bangumiScore(bw, kw); sc < bangumiMatchThreshold {
		t.Errorf("《旷野之息》得分 %.1f 应不低于阈值 %d", sc, bangumiMatchThreshold)
	}
	// 精确匹配必须满分
	exact := bangumiSubject{Name: "The Legend of Zelda"}
	if sc := bangumiScore(exact, kw); sc != 100 {
		t.Errorf("精确匹配得分 = %.1f, 期望 100", sc)
	}
}
