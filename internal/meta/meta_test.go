package meta

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---------- 哈希策略 ----------

func TestHashingAllowed(t *testing.T) {
	// 严格约束：只有 PS3 允许全文件哈希；PS4/PS5/Switch 严禁
	cases := map[string]bool{
		"ps3": true, "PS3": true,
		"ps4": false, "ps5": false, "switch": false, "SWITCH": false,
		"": false, "xbox": false,
	}
	for platform, want := range cases {
		if got := HashingAllowed(platform); got != want {
			t.Errorf("HashingAllowed(%q) = %v, 期望 %v", platform, got, want)
		}
	}
}

func TestComputeFileHashes(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.iso")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := ComputeFileHashes(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if h.MD5 != "5d41402abc4b2a76b9719d911017c592" {
		t.Errorf("MD5 = %s", h.MD5)
	}
	if h.SHA1 != "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d" {
		t.Errorf("SHA1 = %s", h.SHA1)
	}
	// CRC32 为 8 位小写十六进制（对齐 RomM crc32_to_hex）
	if h.CRC != "3610a686" {
		t.Errorf("CRC = %s", h.CRC)
	}
	if len(h.CRC) != 8 {
		t.Errorf("CRC 应为 8 位，得到 %d", len(h.CRC))
	}
}

func TestFileHashes_PlaymatchQuery(t *testing.T) {
	h := FileHashes{MD5: "m", SHA1: "s", CRC: "c"}
	q := h.PlaymatchQuery()
	if q["md5"] != "m" || q["sha1"] != "s" || q["crc"] != "c" {
		t.Errorf("Playmatch 参数名应为小写 md5/sha1/crc，得到 %v", q)
	}
}

// ---------- IGDB ----------

func TestIGDBImageURL(t *testing.T) {
	cases := []struct{ in, size, want string }{
		{"//images.igdb.com/igdb/image/upload/t_thumb/abc.jpg", "t_cover_big_2x",
			"https://images.igdb.com/igdb/image/upload/t_cover_big_2x/abc.jpg"},
		{"//images.igdb.com/igdb/image/upload/t_screenshot_med/x.png", "t_1080p",
			"https://images.igdb.com/igdb/image/upload/t_1080p/x.png"},
		{"", "t_cover_big_2x", ""},
	}
	for _, c := range cases {
		if got := igdbImageURL(c.in, c.size); got != c.want {
			t.Errorf("igdbImageURL(%q,%q) = %q, 期望 %q", c.in, c.size, got, c.want)
		}
	}
}

func TestPickBestIGDB_PrefersBaseGameOverDLC(t *testing.T) {
	games := []igdbGame{
		{ID: 1, Name: "God of War", Category: igdbCategoryDLCAddon,
			Platforms: []igdbPlatform{{ID: 167}}},
		{ID: 2, Name: "God of War", Category: igdbCategoryMainGame,
			Platforms: []igdbPlatform{{ID: 167}}},
	}
	best := pickBestIGDB(games, "God of War", "ps5")
	if best == nil || best.ID != 2 {
		t.Errorf("应选中本体（id=2），得到 %+v", best)
	}
}

func TestPickBestIGDB_PlatformBonus(t *testing.T) {
	// 名称完全相同，PS5 平台条目应胜出
	games := []igdbGame{
		{ID: 1, Name: "Astro Bot", Category: igdbCategoryMainGame,
			Platforms: []igdbPlatform{{ID: 48}}},
		{ID: 2, Name: "Astro Bot", Category: igdbCategoryMainGame,
			Platforms: []igdbPlatform{{ID: 167}}},
	}
	best := pickBestIGDB(games, "Astro Bot", "ps5")
	if best == nil || best.ID != 2 {
		t.Errorf("应选中 PS5 条目（id=2），得到 %+v", best)
	}
}

func TestIGDB_PlatformIDs(t *testing.T) {
	// 对照 RomM backend/adapters/services/igdb.py -> IGDB_PLATFORM_LIST
	want := map[string]int{"ps3": 9, "ps4": 48, "ps5": 167, "switch": 130}
	for slug, id := range want {
		if igdbPlatformIDs[slug] != id {
			t.Errorf("igdbPlatformIDs[%s] = %d, 期望 %d", slug, igdbPlatformIDs[slug], id)
		}
	}
}

// ---------- 本地 gamelist ----------

func TestGamelistLookup_ChineseDescriptionWins(t *testing.T) {
	dir := t.TempDir()
	gameDir := filepath.Join(dir, "God of War (2018) (USA)")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(gameDir, "CUSA07408 - God of War.pkg")
	if err := os.WriteFile(pkg, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	xml := `<?xml version="1.0" encoding="UTF-8"?>
<gameList>
  <game>
    <path>./CUSA07408 - God of War.pkg</path>
    <name>战神</name>
    <desc>奎托斯再度踏上北欧神话之旅。</desc>
    <developer>Santa Monica Studio</developer>
    <publisher>索尼互动娱乐</publisher>
    <genre>动作/冒险</genre>
    <releasedate>20180420T000000</releasedate>
    <rating>0.92</rating>
  </game>
</gameList>`
	if err := os.WriteFile(filepath.Join(gameDir, GamelistFileName), []byte(xml), 0o644); err != nil {
		t.Fatal(err)
	}

	p := NewGamelistProvider()
	target := Target{FilePath: pkg, GameName: "God of War", Platform: "ps5"}
	r, err := p.Lookup(context.Background(), target, dir)
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Fatal("gamelist 未命中")
	}
	if r.NameCN != "战神" {
		t.Errorf("NameCN = %q, 期望 战神", r.NameCN)
	}
	if !strings.Contains(r.Summary, "北欧神话") {
		t.Errorf("Summary = %q", r.Summary)
	}
	if r.ReleaseDate != "2018-04-20" {
		t.Errorf("ReleaseDate = %q, 期望 2018-04-20", r.ReleaseDate)
	}
	if r.Rating != 92 {
		t.Errorf("Rating = %v, 期望 92（0..1 应换算为 0..100）", r.Rating)
	}
	if len(r.Genres) != 2 || r.Genres[0] != "动作" || r.Genres[1] != "冒险" {
		t.Errorf("Genres = %v, 期望 [动作 冒险]", r.Genres)
	}
	if r.Source != SourceGamelist {
		t.Errorf("Source = %q", r.Source)
	}
}

// ---------- TitleDB ----------

func TestTitleDBLookup_Streaming(t *testing.T) {
	body := `{
  "0100F2C0115B6000": {
    "id": "0100F2C0115B6000",
    "name": "The Legend of Zelda: Tears of the Kingdom",
    "description": "在广阔的海拉鲁大陆上展开冒险。",
    "publisher": "Nintendo",
    "developer": "Nintendo EPD",
    "releaseDate": 20230512,
    "category": ["Adventure", "Action"],
    "languages": ["zh", "en"],
    "bannerUrl": "https://img.example.com/banner.jpg",
    "frontBoxArt": "https://img.example.com/box.jpg",
    "screenshots": ["https://img.example.com/s1.jpg"],
    "size": 17179869184,
    "isDemo": false
  },
  "0100000000000001": {
    "name": "Unwanted Entry",
    "description": "不应被解析"
  }
}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.SwitchTitleDBURLs = []string{srv.URL + "/US.en.json"}
	cfg.TitleDBTTL = time.Hour

	db := NewTitleDB(cfg, t.TempDir())
	got, err := db.Lookup(context.Background(), []string{"0100F2C0115B6000"})
	if err != nil {
		t.Fatal(err)
	}
	e, ok := got["0100F2C0115B6000"]
	if !ok {
		t.Fatalf("未命中，得到 %v", got)
	}
	if e.Name != "The Legend of Zelda: Tears of the Kingdom" {
		t.Errorf("Name = %q", e.Name)
	}
	if e.ReleaseDate != "2023-05-12" {
		t.Errorf("ReleaseDate = %q, 期望 2023-05-12", e.ReleaseDate)
	}
	if e.Publisher != "Nintendo" {
		t.Errorf("Publisher = %q", e.Publisher)
	}
	if e.Size != 17179869184 {
		t.Errorf("Size = %d", e.Size)
	}

	r := e.ToResult()
	if r.Summary == "" || r.Artwork.Cover != "https://img.example.com/box.jpg" {
		t.Errorf("ToResult 结果不完整: %+v", r)
	}
}

func TestFormatTitleDBDate(t *testing.T) {
	if got := formatTitleDBDate(20230512); got != "2023-05-12" {
		t.Errorf("got %q", got)
	}
	if got := formatTitleDBDate(0); got != "" {
		t.Errorf("0 应返回空串，得到 %q", got)
	}
}

// ---------- Hasheous ----------

func TestHasheousLookup(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.URL.Path != "/Lookup/ByHash" {
			t.Errorf("路径 = %s", r.URL.Path)
		}
		if r.URL.Query().Get("returnFields") != "Signatures, Metadata, Attributes" {
			t.Errorf("returnFields = %q", r.URL.Query().Get("returnFields"))
		}
		// 校验请求体是哈希对象数组
		var body []map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("请求体解析失败: %v", err)
		}
		if len(body) != 1 || body[0]["mD5"] == "" {
			t.Errorf("请求体 = %v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "id": 42,
  "name": "Demon's Souls",
  "metadata": [{"source": "IGDB", "immutableId": "1064"}],
  "attributes": [{"attributeName": "Logo", "link": "/images/logo.png"}],
  "signatures": {"Redump": {}}
}`))
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.HasheousURL = srv.URL
	c := NewHasheousClient(cfg)

	r, err := c.Lookup(context.Background(), FileHashes{MD5: "abc", SHA1: "def", CRC: "12345678"})
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Fatal("未命中")
	}
	if r.IGDBID != 1064 {
		t.Errorf("IGDBID = %d, 期望 1064", r.IGDBID)
	}
	if r.Artwork.Cover != srv.URL+"/images/logo.png" {
		t.Errorf("Cover = %q", r.Artwork.Cover)
	}
	if r.Title != "Demon's Souls" {
		t.Errorf("Title = %q", r.Title)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("请求次数 = %d", calls)
	}
}

// ---------- Playmatch ----------

func TestPlaymatchLookup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/identify/ids" {
			t.Errorf("路径 = %s", r.URL.Path)
		}
		// Playmatch 参数名为小写 md5 / sha1 / crc
		if r.URL.Query().Get("md5") != "abc" {
			t.Errorf("md5 参数 = %q", r.URL.Query().Get("md5"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "gameMatchType": "FullMatch",
  "externalMetadata": [
    {"providerName": "IGDB", "providerId": 7346},
    {"providerName": "MOBYGAMES", "providerId": 99}
  ]
}`))
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.PlaymatchURL = srv.URL
	c := NewPlaymatchClient(cfg)

	r, err := c.Lookup(context.Background(), FileHashes{MD5: "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if r == nil || r.IGDBID != 7346 {
		t.Fatalf("IGDBID 应为 7346，得到 %+v", r)
	}
}

// ---------- SteamGridDB ----------

func TestSGDBArtwork(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/search/autocomplete/"):
			_, _ = w.Write([]byte(`{"success":true,"data":[{"id":77,"name":"God of War","verified":true}]}`))
		case strings.HasPrefix(r.URL.Path, "/grids/game/"):
			_, _ = w.Write([]byte(`{"success":true,"data":[{"id":1,"url":"https://cdn.example.com/grid.png","nsfw":false,"humor":false}]}`))
		case strings.HasPrefix(r.URL.Path, "/heroes/game/"):
			_, _ = w.Write([]byte(`{"success":true,"data":[{"id":2,"url":"https://cdn.example.com/hero.png","nsfw":false,"humor":false}]}`))
		case strings.HasPrefix(r.URL.Path, "/logos/game/"):
			_, _ = w.Write([]byte(`{"success":true,"data":[{"id":3,"url":"https://cdn.example.com/logo.png","nsfw":false,"humor":false}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.SteamGridDBKey = "test-key"
	c := NewSGDBClient(cfg)
	c.baseURL = srv.URL

	r, err := c.FetchForName(context.Background(), "God of War")
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Fatal("未取到素材")
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if r.Artwork.Cover != "https://cdn.example.com/grid.png" {
		t.Errorf("Cover = %q", r.Artwork.Cover)
	}
	if r.Artwork.Hero != "https://cdn.example.com/hero.png" {
		t.Errorf("Hero = %q", r.Artwork.Hero)
	}
	if r.Artwork.Logo != "https://cdn.example.com/logo.png" {
		t.Errorf("Logo = %q", r.Artwork.Logo)
	}
}

func TestSGDBEscapePathSegment(t *testing.T) {
	// 斜杠与空格必须转义（否则会破坏 URL 路径）；
	// 冒号属于 RFC 3986 的 pchar，可保留。
	got := escapePathSegment("Half-Life: Alyx/2")
	if strings.ContainsAny(got, "/ ") {
		t.Errorf("转义后不应含 / 或空格，得到 %q", got)
	}
	if !strings.Contains(got, "%2F") {
		t.Errorf("斜杠应被百分号编码，得到 %q", got)
	}
}

// ---------- 流水线 ----------

// fakeIGDBServer 伪造 IGDB 令牌与查询接口
func fakeIGDBServer(t *testing.T, gamesJSON string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok","expires_in":3600}`))
	})
	mux.HandleFunc("/v4/games", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Client-ID") == "" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Errorf("缺少 IGDB 鉴权头: Client-ID=%q Authorization=%q",
				r.Header.Get("Client-ID"), r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(gamesJSON))
	})
	return httptest.NewServer(mux)
}

func newTestPipeline(t *testing.T, cfg Config, igdbSrv *httptest.Server) *Pipeline {
	t.Helper()
	p := NewPipeline(cfg, t.TempDir())
	// 单元测试必须完全离线：这些用例只验证 IGDB / TitleDB / 哈希分支。
	// Bangumi 没有可注入的 baseURL，若保持启用就会真的请求 api.bgm.tv，
	// 在 CI 上表现为 20s 超时后整个 test job 失败。
	p.bangumi = &BangumiClient{enabled: false}
	if igdbSrv != nil {
		p.igdb = &IGDBClient{
			clientID: "cid", clientSecret: "sec",
			http:     newHTTPClient(5 * time.Second),
			tokenURL: igdbSrv.URL + "/token",
			apiBase:  igdbSrv.URL + "/v4",
		}
	}
	return p
}

func TestPipeline_GamelistBeatsIGDB(t *testing.T) {
	// 本地中文简介应胜出；IGDB 只补空缺字段（发售日期/开发商）
	dir := t.TempDir()
	gameDir := filepath.Join(dir, "God of War")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(gameDir, "CUSA07408.pkg")
	if err := os.WriteFile(pkg, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	xml := `<gameList><game><path>./CUSA07408.pkg</path><name>战神</name><desc>本地中文简介</desc></game></gameList>`
	if err := os.WriteFile(filepath.Join(gameDir, GamelistFileName), []byte(xml), 0o644); err != nil {
		t.Fatal(err)
	}

	igdbSrv := fakeIGDBServer(t, `[{
		"id": 1064, "name": "God of War", "summary": "IGDB English summary",
		"first_release_date": 1524182400, "category": 0,
		"cover": {"url": "//images.igdb.com/igdb/image/upload/t_thumb/c.jpg"},
		"involved_companies": [{"developer": true, "company": {"name": "Santa Monica Studio"}}],
		"platforms": [{"id": 167, "name": "PlayStation 5"}]
	}]`)
	defer igdbSrv.Close()

	cfg := DefaultConfig()
	p := newTestPipeline(t, cfg, igdbSrv)

	target := Target{
		GameID: 1, Platform: "ps5", FilePath: pkg,
		GameName: "God of War", TitleID: "CUSA07408",
	}
	r, err := p.Scrape(context.Background(), target, dir)
	if err != nil {
		t.Fatalf("Scrape 报错: %v", err)
	}

	if r.Summary != "本地中文简介" {
		t.Errorf("Summary 应为本地中文，得到 %q", r.Summary)
	}
	if r.Sources[FieldSummary] != SourceGamelist {
		t.Errorf("Summary 来源应为 gamelist，得到 %q", r.Sources[FieldSummary])
	}
	if r.NameCN != "战神" {
		t.Errorf("NameCN = %q", r.NameCN)
	}
	// IGDB 补齐本地没有的字段
	if r.Developer != "Santa Monica Studio" {
		t.Errorf("Developer = %q（应由 IGDB 补齐）", r.Developer)
	}
	if r.ReleaseDate != "2018-04-20" {
		t.Errorf("ReleaseDate = %q（应由 IGDB 补齐）", r.ReleaseDate)
	}
	if r.Sources[FieldDeveloper] != SourceIGDB {
		t.Errorf("Developer 来源应为 igdb，得到 %q", r.Sources[FieldDeveloper])
	}
	if r.IGDBID != 1064 {
		t.Errorf("IGDBID = %d", r.IGDBID)
	}
	if !strings.Contains(r.Artwork.Cover, "t_cover_big_2x") {
		t.Errorf("封面应被改写为高清尺寸，得到 %q", r.Artwork.Cover)
	}
}

func TestPipeline_InlineProviderTagSkipsSearch(t *testing.T) {
	// (igdb-1234) 内联标签应直接按 ID 取，不做名称搜索
	var searchCalled int32
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"tok","expires_in":3600}`))
	})
	mux.HandleFunc("/v4/games", func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		body := string(buf[:n])
		if strings.Contains(body, "search") {
			atomic.AddInt32(&searchCalled, 1)
		}
		if !strings.Contains(body, "where id = 1234") {
			t.Errorf("应按 ID 查询，实际 body = %q", body)
		}
		_, _ = w.Write([]byte(`[{"id":1234,"name":"Bound Game","summary":"s","category":0}]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := DefaultConfig()
	p := newTestPipeline(t, cfg, srv)

	target := Target{
		GameID: 2, Platform: "ps5", GameName: "Some Localized Name",
		ProviderID: 1234, TitleID: "CUSA00000",
	}
	r, err := p.Scrape(context.Background(), target, "")
	if err != nil {
		t.Fatalf("Scrape 报错: %v", err)
	}
	if atomic.LoadInt32(&searchCalled) != 0 {
		t.Error("有内联 ID 时不应执行名称搜索")
	}
	if r.Title != "Bound Game" {
		t.Errorf("Title = %q", r.Title)
	}
}

func TestPipeline_PS5NeverHashes(t *testing.T) {
	// PS5 严禁哈希：Hasheous / Playmatch 都不应被调用
	var hashCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hashCalls, 1)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	pkg := filepath.Join(dir, "game.pkg")
	if err := os.WriteFile(pkg, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.HasheousURL = srv.URL
	cfg.PlaymatchURL = srv.URL
	p := newTestPipeline(t, cfg, nil)

	target := Target{GameID: 3, Platform: "ps5", FilePath: pkg, GameName: "Some Game"}
	if _, err := p.Scrape(context.Background(), target, dir); err != nil {
		t.Fatalf("Scrape 报错: %v", err)
	}
	if n := atomic.LoadInt32(&hashCalls); n != 0 {
		t.Errorf("PS5 不应发起哈希匹配请求，实际 %d 次", n)
	}
}

func TestPipeline_PS3UsesHashMatch(t *testing.T) {
	// PS3 允许哈希：应调用 Hasheous 并回填 IGDB ID
	var hashCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hashCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"name":"Demon's Souls","metadata":[{"source":"IGDB","immutableId":"1064"}]}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	iso := filepath.Join(dir, "Demon's Souls (Europe) [BLES00932].iso")
	if err := os.WriteFile(iso, []byte("dummy iso content"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.HasheousURL = srv.URL
	cfg.PlaymatchEnabled = false
	cfg.SwitchTitleDBURLs = nil
	p := newTestPipeline(t, cfg, nil)

	target := Target{GameID: 4, Platform: "ps3", FilePath: iso, GameName: "Demon's Souls"}
	r, err := p.Scrape(context.Background(), target, dir)
	if err != nil {
		t.Fatalf("Scrape 报错: %v", err)
	}
	if n := atomic.LoadInt32(&hashCalls); n != 1 {
		t.Errorf("PS3 应发起 1 次哈希匹配，实际 %d 次", n)
	}
	if r.IGDBID != 1064 {
		t.Errorf("IGDBID = %d, 期望 1064", r.IGDBID)
	}
}

func TestPipeline_SwitchUsesTitleDB(t *testing.T) {
	body := `{"0100F2C0115B6000":{"name":"塞尔达传说 王国之泪","description":"海拉鲁冒险","publisher":"任天堂","releaseDate":20230512}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	dir := t.TempDir()
	nsp := filepath.Join(dir, "zelda.nsp")
	if err := os.WriteFile(nsp, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.SwitchTitleDBURLs = []string{srv.URL + "/US.en.json"}
	cfg.Enabled = true
	p := newTestPipeline(t, cfg, nil)

	target := Target{
		GameID: 5, Platform: "switch", FilePath: nsp,
		GameName: "The Legend of Zelda", TitleID: "0100F2C0115B6000",
	}
	r, err := p.Scrape(context.Background(), target, dir)
	if err != nil {
		t.Fatalf("Scrape 报错: %v", err)
	}
	if r.Title != "塞尔达传说 王国之泪" {
		t.Errorf("Title = %q", r.Title)
	}
	if r.Sources[FieldTitle] != SourceTitleDB {
		t.Errorf("Title 来源 = %q", r.Sources[FieldTitle])
	}
	if r.ReleaseDate != "2023-05-12" {
		t.Errorf("ReleaseDate = %q", r.ReleaseDate)
	}
}

func TestPipeline_DisabledStillReadsGamelist(t *testing.T) {
	// 关闭在线刮削后，本地 gamelist 仍应可用
	dir := t.TempDir()
	gameDir := filepath.Join(dir, "Game")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(gameDir, "a.pkg")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	xml := `<gameList><game><path>./a.pkg</path><name>中文名</name><desc>中文简介</desc></game></gameList>`
	if err := os.WriteFile(filepath.Join(gameDir, GamelistFileName), []byte(xml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.Enabled = false
	p := newTestPipeline(t, cfg, nil)

	r, err := p.Scrape(context.Background(), Target{GameID: 6, Platform: "ps5", FilePath: f}, dir)
	if err != nil {
		t.Fatalf("Scrape 报错: %v", err)
	}
	if r.Summary != "中文简介" {
		t.Errorf("关闭在线刮削后本地简介仍应可用，得到 %q", r.Summary)
	}
}

// ---------- 配置 ----------

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("IGDB_CLIENT_ID", "cid")
	t.Setenv("IGDB_CLIENT_SECRET", "sec")
	t.Setenv("STEAMGRIDDB_API_KEY", "sgdb")
	t.Setenv("HASHEOUS_API_ENABLED", "false")
	t.Setenv("METADATA_LANGUAGE", "zh,en,ja")
	t.Setenv("METADATA_CONCURRENCY", "8")
	t.Setenv("SWITCH_TITLEDB_URLS", "https://a/CN.zh.json, https://b/US.en.json")

	c := ConfigFromEnv()
	if !c.HasIGDB() {
		t.Error("HasIGDB 应为 true")
	}
	if !c.HasSGDB() {
		t.Error("HasSGDB 应为 true")
	}
	if c.HasheousEnabled {
		t.Error("HASHEOUS_API_ENABLED=false 应关闭")
	}
	if len(c.Language) != 3 || c.Language[0] != "zh" {
		t.Errorf("Language = %v", c.Language)
	}
	if c.Concurrency != 8 {
		t.Errorf("Concurrency = %d", c.Concurrency)
	}
	if len(c.SwitchTitleDBURLs) != 2 || !strings.HasSuffix(c.SwitchTitleDBURLs[0], "CN.zh.json") {
		t.Errorf("SwitchTitleDBURLs = %v", c.SwitchTitleDBURLs)
	}
}

func TestConfigResolve_SettingsFallback(t *testing.T) {
	// 环境变量优先；环境变量为空时回退到 settings 表
	c := Config{IGDBClientID: "", IGDBClientSecret: "from-env"}
	got := c.Resolve(stubSettings{"igdb_client_id": "from-settings", "igdb_client_secret": "ignored"})
	if got.IGDBClientID != "from-settings" {
		t.Errorf("IGDBClientID = %q，应从 settings 补齐", got.IGDBClientID)
	}
	if got.IGDBClientSecret != "from-env" {
		t.Errorf("IGDBClientSecret = %q，环境变量应优先", got.IGDBClientSecret)
	}
}

type stubSettings map[string]string

func (s stubSettings) GetSetting(key string) (string, error) { return s[key], nil }

func TestMask(t *testing.T) {
	if got := Mask("abcdef123456"); !strings.HasSuffix(got, "3456") || strings.Contains(got, "abcdef") {
		t.Errorf("Mask 结果不应暴露明文: %q", got)
	}
	if Mask("") != "" {
		t.Error("空串应返回空串")
	}
}

// ---------- Result 合并语义 ----------

func TestResultMerge_FirstMatchWins(t *testing.T) {
	out := newResult()
	first := newResult()
	first.Source = SourceGamelist
	first.Summary = "中文简介"
	first.Publisher = "本地发行商"
	out.Merge(first)

	second := newResult()
	second.Source = SourceIGDB
	second.Summary = "IGDB 简介"
	second.Publisher = "IGDB 发行商"
	second.Developer = "IGDB 开发商"
	out.Merge(second)

	if out.Summary != "中文简介" {
		t.Errorf("Summary 应保留首个命中值，得到 %q", out.Summary)
	}
	if out.Publisher != "本地发行商" {
		t.Errorf("Publisher 应保留首个命中值，得到 %q", out.Publisher)
	}
	if out.Developer != "IGDB 开发商" {
		t.Errorf("Developer 应被后续源补齐，得到 %q", out.Developer)
	}
	if out.Sources[FieldSummary] != SourceGamelist || out.Sources[FieldDeveloper] != SourceIGDB {
		t.Errorf("来源记录不正确: %v", out.Sources)
	}
}

func TestTitleDB_CanBeDisabled(t *testing.T) {
	// 关闭后不应发起任何下载请求（官方库约 90MB，必须可关闭）
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.SwitchTitleDBURLs = []string{srv.URL + "/US.en.json"}
	cfg.TitleDBEnabled = false

	db := NewTitleDB(cfg, t.TempDir())
	got, err := db.Lookup(context.Background(), []string{"0100F2C0115B6000"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("关闭后不应有结果，得到 %v", got)
	}
	if n := atomic.LoadInt32(&hits); n != 0 {
		t.Errorf("关闭后不应发起请求，实际 %d 次", n)
	}
}

func TestConfigFromEnv_TitleDBToggle(t *testing.T) {
	t.Setenv("SWITCH_TITLEDB_ENABLED", "false")
	if c := ConfigFromEnv(); c.TitleDBEnabled {
		t.Error("SWITCH_TITLEDB_ENABLED=false 应关闭 TitleDB")
	}
	t.Setenv("SWITCH_TITLEDB_ENABLED", "true")
	if c := ConfigFromEnv(); !c.TitleDBEnabled {
		t.Error("SWITCH_TITLEDB_ENABLED=true 应开启 TitleDB")
	}
}
