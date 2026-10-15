package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "data.db")
	st, err := NewStore(dbPath, filepath.Join(dir, "covers"))
	if err != nil {
		t.Fatalf("NewStore 失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st, dbPath
}

func TestSchema_HasMetaColumns(t *testing.T) {
	st, _ := newTestStore(t)
	for _, col := range metaColumns {
		var n int
		if err := st.db.QueryRow(
			`SELECT COUNT(1) FROM pragma_table_info('packages') WHERE name = ?`, col.name,
		).Scan(&n); err != nil {
			t.Fatalf("查询列 %s 失败: %v", col.name, err)
		}
		if n != 1 {
			t.Errorf("缺少元数据列: %s", col.name)
		}
	}
}

func TestSchema_MigrationIsIdempotent(t *testing.T) {
	_, dbPath := newTestStore(t)
	// 再次打开同一个库，迁移应可重复执行且不报错
	st2, err := NewStore(dbPath, filepath.Join(filepath.Dir(dbPath), "covers"))
	if err != nil {
		t.Fatalf("重复迁移失败: %v", err)
	}
	defer st2.Close()
}

func TestSavePackageAndMetadata(t *testing.T) {
	st, _ := newTestStore(t)

	p := &Package{
		TitleID:   "CUSA07408",
		ContentID: "UP9000-CUSA07408_00-0000000000000000",
		Title:     "God of War",
		IsPS5:     true,
		Platform:  "PS5",
		Category:  "game",
		FilePath:  "/tmp/game.pkg",
		FileSize:  1024,
	}
	if err := st.SavePackage(p, nil); err != nil {
		t.Fatalf("SavePackage 失败: %v", err)
	}

	games, err := st.GetGames("")
	if err != nil {
		t.Fatalf("GetGames 失败: %v", err)
	}
	if len(games) != 1 {
		t.Fatalf("应有 1 条记录，得到 %d", len(games))
	}
	id := games[0].ID

	m := &Meta{
		IGDBID:      1064,
		NameCN:      "战神",
		Summary:     "奎托斯再度踏上北欧神话之旅。",
		ReleaseDate: "2018-04-20",
		Developer:   "Santa Monica Studio",
		Publisher:   "Sony Interactive Entertainment",
		Genres:      []string{"Action", "Adventure"},
		Rating:      92.5,
		Source:      "gamelist",
		Sources:     map[string]string{"summary": "gamelist", "developer": "igdb"},
		Cover:       "https://images.example.com/cover.jpg",
		Hero:        "https://images.example.com/hero.jpg",
		Screenshots: []string{"https://images.example.com/s1.jpg", "https://images.example.com/s2.jpg"},
	}
	if err := st.UpdateMetadata(id, m); err != nil {
		t.Fatalf("UpdateMetadata 失败: %v", err)
	}

	got, err := st.GetMeta(id)
	if err != nil {
		t.Fatalf("GetMeta 失败: %v", err)
	}
	if got.NameCN != "战神" || got.IGDBID != 1064 {
		t.Errorf("GetMeta 结果不正确: %+v", got)
	}
	if len(got.Genres) != 2 || got.Genres[1] != "Adventure" {
		t.Errorf("Genres 反序列化失败: %v", got.Genres)
	}
	if len(got.Screenshots) != 2 {
		t.Errorf("Screenshots 反序列化失败: %v", got.Screenshots)
	}
	if got.Sources["developer"] != "igdb" {
		t.Errorf("Sources 反序列化失败: %v", got.Sources)
	}
	if got.Rating != 92.5 {
		t.Errorf("Rating = %v", got.Rating)
	}
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt 应被设置")
	}
	if got.ReleaseYear() != 2018 {
		t.Errorf("ReleaseYear = %d", got.ReleaseYear())
	}

	// 批量读取
	batch, err := st.GetMetaForIDs([]int64{id, 99999})
	if err != nil {
		t.Fatalf("GetMetaForIDs 失败: %v", err)
	}
	if len(batch) != 1 {
		t.Errorf("应只返回存在的记录，得到 %d", len(batch))
	}
	if batch[id].Summary == "" {
		t.Error("批量结果缺少 summary")
	}
}

func TestClearMetadata(t *testing.T) {
	st, _ := newTestStore(t)
	if err := st.SavePackage(&Package{
		TitleID: "X", ContentID: "X", Title: "T", Platform: "PS5",
		Category: "game", FilePath: "/tmp/x.pkg", FileSize: 1,
	}, nil); err != nil {
		t.Fatal(err)
	}
	games, _ := st.GetGames("")
	id := games[0].ID

	if err := st.UpdateMetadata(id, &Meta{IGDBID: 1, Summary: "s", Cover: "c"}); err != nil {
		t.Fatal(err)
	}
	if err := st.ClearMetadata(id); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetMeta(id)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsEmpty() {
		t.Errorf("清空后应为空: %+v", got)
	}
}

func TestListMetaTargets(t *testing.T) {
	st, _ := newTestStore(t)
	seed := []struct {
		platform string
		category string
		title    string
		scraped  bool
	}{
		{"PS5", "game", "A", false},
		{"PS5", "game", "B", true},
		{"SWITCH", "game", "C", false},
		{"PS5", "dlc", "D", false}, // DLC 不参与刮削
	}
	for i, s := range seed {
		p := &Package{
			TitleID: "T", ContentID: "T", Title: s.title,
			Platform: s.platform, Category: s.category,
			FilePath: filepath.Join("/tmp", s.title+".pkg"), FileSize: 1,
		}
		if err := st.SavePackage(p, nil); err != nil {
			t.Fatal(err)
		}
		if s.scraped {
			games, _ := st.GetGames("")
			for _, g := range games {
				if g.Title == s.title {
					if err := st.UpdateMetadata(g.ID, &Meta{Source: "igdb", Summary: "x"}); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
		_ = i
	}

	all, err := st.ListMetaTargets(nil, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Errorf("应返回 3 个本体目标（排除 DLC），得到 %d", len(all))
	}

	ps5, err := st.ListMetaTargets([]string{"PS5"}, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps5) != 2 {
		t.Errorf("PS5 本体应有 2 条，得到 %d", len(ps5))
	}

	missing, err := st.ListMetaTargets(nil, true, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 2 {
		t.Errorf("未刮削应有 2 条，得到 %d", len(missing))
	}

	limited, err := st.ListMetaTargets(nil, false, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 1 {
		t.Errorf("limit=1 应返回 1 条，得到 %d", len(limited))
	}
}

func TestCountMeta(t *testing.T) {
	st, _ := newTestStore(t)
	if err := st.SavePackage(&Package{
		TitleID: "T", ContentID: "T", Title: "A", Platform: "PS5",
		Category: "game", FilePath: "/tmp/a.pkg", FileSize: 1,
	}, nil); err != nil {
		t.Fatal(err)
	}
	games, _ := st.GetGames("")
	if err := st.UpdateMetadata(games[0].ID, &Meta{
		Source: "igdb", Summary: "s", IGDBID: 5, Cover: "c",
	}); err != nil {
		t.Fatal(err)
	}

	stats, err := st.CountMeta()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 1 || stats.Scraped != 1 || stats.WithSummary != 1 ||
		stats.WithIGDBID != 1 || stats.WithArtwork != 1 {
		t.Errorf("统计不正确: %+v", stats)
	}
}

// TestMigrateFromLegacySchema 验证早期版本（content_id UNIQUE）的库能平滑升级：
// 数据不丢、唯一约束被移除、元数据列被补齐。
func TestMigrateFromLegacySchema(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "legacy.db")

	raw, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	// 复刻早期 schema：content_id 带 UNIQUE，且没有元数据列
	if _, err := raw.Exec(`
		CREATE TABLE packages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title_id TEXT NOT NULL,
			content_id TEXT NOT NULL UNIQUE,
			title TEXT NOT NULL,
			is_ps5 INTEGER NOT NULL DEFAULT 1,
			platform TEXT DEFAULT '',
			version TEXT DEFAULT '',
			region TEXT DEFAULT '',
			file_path TEXT NOT NULL UNIQUE,
			file_size INTEGER NOT NULL,
			has_cover INTEGER NOT NULL DEFAULT 0,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO packages (title_id, content_id, title, platform, file_path, file_size)
		VALUES ('CUSA07408', 'UP9000-CUSA07408', 'God of War', 'PS5', '/tmp/a.pkg', 100),
		       ('PPSA01325', 'PPSA01325', 'Astro Bot', 'PS5', '/tmp/b.pkg', 200);
	`); err != nil {
		t.Fatalf("构造旧库失败: %v", err)
	}
	_ = raw.Close()

	st, err := NewStore(dbPath, filepath.Join(dir, "covers"))
	if err != nil {
		t.Fatalf("升级旧库失败: %v", err)
	}
	defer st.Close()

	// 数据保留
	games, err := st.GetGames("")
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 2 {
		t.Fatalf("升级后应保留 2 条记录，得到 %d", len(games))
	}

	// 元数据列已补齐且可写
	if err := st.UpdateMetadata(games[0].ID, &Meta{Summary: "ok", Source: "igdb"}); err != nil {
		t.Fatalf("升级后写入元数据失败: %v", err)
	}
	got, err := st.GetMeta(games[0].ID)
	if err != nil || got.Summary != "ok" {
		t.Fatalf("升级后读取元数据失败: %v %+v", err, got)
	}

	// content_id 唯一约束已移除：同一 content_id 换路径应能再入库
	if err := st.SavePackage(&Package{
		TitleID: "CUSA07408", ContentID: "UP9000-CUSA07408", Title: "God of War",
		Platform: "PS5", Category: "game", FilePath: "/mnt/usb/a.pkg", FileSize: 100,
	}, nil); err != nil {
		t.Fatalf("content_id 唯一约束应已移除，但写入失败: %v", err)
	}

	var autoindexes int
	if err := st.db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master
		 WHERE type='index' AND tbl_name='packages' AND name LIKE 'sqlite_autoindex_packages_%'`,
	).Scan(&autoindexes); err != nil {
		t.Fatal(err)
	}
	if autoindexes != 1 {
		t.Errorf("应只剩 file_path 一个唯一约束，实际 autoindex 数 = %d", autoindexes)
	}
}

func TestMeta_Helpers(t *testing.T) {
	var m *Meta
	if !m.IsEmpty() {
		t.Error("nil Meta 应视为空")
	}
	if m.ReleaseYear() != 0 {
		t.Error("nil Meta 年份应为 0")
	}
	m = &Meta{ReleaseDate: "2018-04-20"}
	if m.ReleaseYear() != 2018 {
		t.Errorf("ReleaseYear = %d", m.ReleaseYear())
	}
	m = &Meta{ReleaseDate: "bad"}
	if m.ReleaseYear() != 0 {
		t.Error("非法日期应返回 0")
	}
	m = &Meta{UpdatedAt: time.Now()}
	if !m.IsEmpty() {
		t.Error("IsEmpty 按内容判定：只有时间戳、无任何内容时仍应视为空")
	}
}
