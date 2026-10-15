package meta

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// titledbStub 返回一个提供给定 JSON 内容的 TitleDB 服务端
func titledbStub(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

// 模拟 blawar/titledb 的真实结构：
// 顶层键是 nsuId（14 位），16 位 TitleID 在条目的 id 字段里。
const titledbFixture = `{
  "70010000000025": {
    "id": "01007EF00011E000",
    "nsuId": 70010000000025,
    "name": "The Legend of Zelda: Breath of the Wild",
    "publisher": "Nintendo",
    "developer": "Nintendo EPD",
    "description": "在海拉鲁大陆展开的全新冒险，解开古代遗迹的谜题。",
    "releaseDate": 20170303,
    "iconUrl": "https://example.com/icon.jpg",
    "frontBoxArt": "https://example.com/box.jpg",
    "bannerUrl": "https://example.com/banner.jpg",
    "category": ["Adventure", "Action"],
    "screenshots": ["https://example.com/s1.jpg"],
    "rightsId": "01007EF00011E0000000000000000000"
  },
  "70010000000041": {
    "id": "0100000000010000",
    "name": "Some Other Game",
    "description": "无关游戏。"
  }
}`

func TestTitleDB_MatchesByInnerTitleID(t *testing.T) {
	// 回归：必须按条目的 id 字段（16 位 TitleID）匹配，
	// 而不是按顶层键（nsuId）匹配——后者永远匹配不上。
	srv := titledbStub(t, titledbFixture)
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.SwitchTitleDBURLs = []string{srv.URL + "/US.en.json"}
	cfg.TitleDBEnabled = true

	db := NewTitleDB(cfg, t.TempDir())
	got, err := db.Lookup(context.Background(), []string{"01007EF00011E000"})
	if err != nil {
		t.Fatal(err)
	}
	e, ok := got["01007EF00011E000"]
	if !ok {
		t.Fatalf("应按条目 id 命中，实际结果: %v", got)
	}
	if e.Name != "The Legend of Zelda: Breath of the Wild" {
		t.Errorf("Name = %q", e.Name)
	}
	if e.Publisher != "Nintendo" {
		t.Errorf("Publisher = %q", e.Publisher)
	}
	if e.ReleaseDate != "2017-03-03" {
		t.Errorf("ReleaseDate = %q（releaseDate=20170303 应格式化为 YYYY-MM-DD）", e.ReleaseDate)
	}
	if e.BoxArtURL != "https://example.com/box.jpg" {
		t.Errorf("BoxArtURL = %q", e.BoxArtURL)
	}
	if e.BannerURL != "https://example.com/banner.jpg" {
		t.Errorf("BannerURL = %q", e.BannerURL)
	}
	if len(e.Screenshots) != 1 {
		t.Errorf("Screenshots = %v", e.Screenshots)
	}

	// 转换为 Result 后应带上封面与简介
	r := e.ToResult()
	if r.Source != SourceTitleDB {
		t.Errorf("Source = %q", r.Source)
	}
	if r.Artwork.Cover != "https://example.com/box.jpg" {
		t.Errorf("Cover = %q", r.Artwork.Cover)
	}
	if r.Summary == "" {
		t.Error("Summary 不应为空")
	}
}

func TestTitleDB_NsuIdIsNotTitleID(t *testing.T) {
	// 用 nsuId 去查不应命中（它不是 TitleID）
	srv := titledbStub(t, titledbFixture)
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.SwitchTitleDBURLs = []string{srv.URL + "/US.en.json"}
	db := NewTitleDB(cfg, t.TempDir())

	got, err := db.Lookup(context.Background(), []string{"70010000000025"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("nsuId 不是合法 TitleID，不应命中，得到 %v", got)
	}
}

func TestTitleDB_MultipleTitleIDs(t *testing.T) {
	srv := titledbStub(t, titledbFixture)
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.SwitchTitleDBURLs = []string{srv.URL + "/US.en.json"}
	db := NewTitleDB(cfg, t.TempDir())

	got, err := db.Lookup(context.Background(), []string{"01007EF00011E000", "0100000000010000"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("应命中 2 条，实际 %d: %v", len(got), got)
	}
}
