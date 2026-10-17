package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Package struct {
	ID          int64     `json:"id"`
	TitleID     string    `json:"title_id"`
	ContentID   string    `json:"content_id"`
	Title       string    `json:"title"`
	DisplayName string    `json:"display_name"` // 友好展示名（param.json 真实名 / 美化名）
	IsPS5       bool      `json:"is_ps5"`
	Platform    string    `json:"platform"`
	Version     string    `json:"version"`
	Region      string    `json:"region"`
	FilePath    string    `json:"file_path"`
	FileSize    int64     `json:"file_size"`
	HasCover    bool      `json:"has_cover"`
	Category    string    `json:"category"` // "game", "dlc", "patch"
	DLCCount    int       `json:"dlc_count"`
	UpdatedAt   time.Time `json:"updated_at"`

	// CategoryAuthoritative 表示 category 来自 PKG 内 param.json 的权威字段，
	// 不允许被启发式重分类覆盖（防止外接盘离线时被错误改回）。
	CategoryAuthoritative bool `json:"-"`

	// Meta 刮削到的元数据（仅列表/详情接口按需填充，见 store.Meta）
	Meta *Meta `json:"meta,omitempty"`
}

type Store struct {
	db       *sql.DB
	coverDir string
}

func NewStore(dbPath, coverDir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(coverDir, 0755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}

	s := &Store{
		db:       db,
		coverDir: coverDir,
	}

	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

func (s *Store) initSchema() error {
	createPackagesSQL := `
	CREATE TABLE IF NOT EXISTS packages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title_id TEXT NOT NULL,
		content_id TEXT NOT NULL,
		title TEXT NOT NULL,
		is_ps5 INTEGER NOT NULL DEFAULT 1,
		platform TEXT DEFAULT '',
		version TEXT DEFAULT '',
		region TEXT DEFAULT '',
		file_path TEXT NOT NULL UNIQUE,
		file_size INTEGER NOT NULL,
		has_cover INTEGER NOT NULL DEFAULT 0,
		category TEXT DEFAULT 'game',
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_packages_title_id ON packages(title_id);
	`
	if _, err := s.db.Exec(createPackagesSQL); err != nil {
		return err
	}

	createSettingsSQL := `
	CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);
	`
	if _, err := s.db.Exec(createSettingsSQL); err != nil {
		return err
	}

	// category 字段兼容
	var colExists int
	checkColSQL := `SELECT COUNT(1) FROM pragma_table_info('packages') WHERE name='category'`
	if err := s.db.QueryRow(checkColSQL).Scan(&colExists); err == nil && colExists == 0 {
		if _, err := s.db.Exec(`ALTER TABLE packages ADD COLUMN category TEXT DEFAULT 'game'`); err != nil {
			return err
		}
	}

	// display_name 字段兼容（友好展示名）
	var dnExists int
	checkDnSQL := `SELECT COUNT(1) FROM pragma_table_info('packages') WHERE name='display_name'`
	if err := s.db.QueryRow(checkDnSQL).Scan(&dnExists); err == nil && dnExists == 0 {
		if _, err := s.db.Exec(`ALTER TABLE packages ADD COLUMN display_name TEXT DEFAULT ''`); err != nil {
			return err
		}
	}

	// category_authoritative 字段兼容（分类是否来自 param.json 权威判定）
	var caExists int
	checkCaSQL := `SELECT COUNT(1) FROM pragma_table_info('packages') WHERE name='category_authoritative'`
	if err := s.db.QueryRow(checkCaSQL).Scan(&caExists); err == nil && caExists == 0 {
		if _, err := s.db.Exec(`ALTER TABLE packages ADD COLUMN category_authoritative INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}

	// category 索引，加速按类型过滤
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_packages_category ON packages(category);`); err != nil {
		return err
	}

	// 修复历史库遗留的 content_id UNIQUE 约束（必须在补列之后执行）
	if err := s.migrateContentIDUnique(); err != nil {
		return err
	}

	// 元数据刮削相关列（必须放在表重建之后，否则重建会丢掉这些列）
	if err := s.migrateMetaColumns(); err != nil {
		return err
	}

	return nil
}

// metaColumns 元数据刮削所需的列（列名 -> 建表片段）。
// 全部带默认值，保证旧库升级后既有记录仍可正常读写。
var metaColumns = []struct{ name, ddl string }{
	{"igdb_id", "INTEGER NOT NULL DEFAULT 0"},
	{"name_cn", "TEXT DEFAULT ''"},
	{"summary", "TEXT DEFAULT ''"},
	{"release_date", "TEXT DEFAULT ''"},
	{"developer", "TEXT DEFAULT ''"},
	{"publisher", "TEXT DEFAULT ''"},
	{"genres", "TEXT DEFAULT ''"},
	{"rating", "REAL NOT NULL DEFAULT 0"},
	{"meta_source", "TEXT DEFAULT ''"},
	{"meta_sources", "TEXT DEFAULT ''"},
	{"art_cover", "TEXT DEFAULT ''"},
	{"art_hero", "TEXT DEFAULT ''"},
	{"art_logo", "TEXT DEFAULT ''"},
	{"art_banner", "TEXT DEFAULT ''"},
	{"art_screenshots", "TEXT DEFAULT ''"},
	{"meta_updated_at", "DATETIME"},
}

// migrateMetaColumns 幂等地补齐元数据列
func (s *Store) migrateMetaColumns() error {
	for _, col := range metaColumns {
		var exists int
		err := s.db.QueryRow(
			`SELECT COUNT(1) FROM pragma_table_info('packages') WHERE name = ?`, col.name,
		).Scan(&exists)
		if err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		if _, err := s.db.Exec(
			`ALTER TABLE packages ADD COLUMN ` + col.name + ` ` + col.ddl,
		); err != nil {
			return fmt.Errorf("新增元数据列 %s 失败: %w", col.name, err)
		}
		log.Printf("[Store] packages 表新增元数据列: %s", col.name)
	}

	// 便于按「是否已刮削」筛选
	if _, err := s.db.Exec(
		`CREATE INDEX IF NOT EXISTS idx_packages_igdb_id ON packages(igdb_id);`,
	); err != nil {
		return err
	}
	if _, err := s.db.Exec(
		`CREATE INDEX IF NOT EXISTS idx_packages_meta_source ON packages(meta_source);`,
	); err != nil {
		return err
	}
	return nil
}

// migrateContentIDUnique 修复早期版本遗留的 `content_id UNIQUE` 约束。
//
// 早期建表语句把 content_id 设为 UNIQUE，导致同一游戏更换路径后（例如外接盘
// 盘符变化、文件搬迁）再次入库时报 “UNIQUE constraint failed: packages.content_id”，
// 扫描整体失败。SQLite 无法直接删除列约束，这里通过「重建表 + 迁移数据」修复，
// 只保留 file_path 一个唯一约束。
func (s *Store) migrateContentIDUnique() error {
	var cnt int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master
		 WHERE type='index' AND tbl_name='packages' AND name LIKE 'sqlite_autoindex_packages_%'`,
	).Scan(&cnt)
	if err != nil {
		return nil // 表不存在等情况，跳过
	}
	if cnt < 2 {
		return nil // 仅有 file_path 一个唯一约束，无需迁移
	}

	log.Printf("[Store] 检测到历史库的 content_id UNIQUE 约束，正在重建 packages 表以修复...")

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmts := []string{
		`CREATE TABLE packages_migrated (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title_id TEXT NOT NULL,
			content_id TEXT NOT NULL,
			title TEXT NOT NULL,
			display_name TEXT DEFAULT '',
			is_ps5 INTEGER NOT NULL DEFAULT 1,
			platform TEXT DEFAULT '',
			version TEXT DEFAULT '',
			region TEXT DEFAULT '',
			file_path TEXT NOT NULL UNIQUE,
			file_size INTEGER NOT NULL,
			has_cover INTEGER NOT NULL DEFAULT 0,
			category TEXT DEFAULT 'game',
			category_authoritative INTEGER NOT NULL DEFAULT 0,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`INSERT INTO packages_migrated
			(id, title_id, content_id, title, display_name, is_ps5, platform, version, region,
			 file_path, file_size, has_cover, category, category_authoritative, updated_at)
		 SELECT id, title_id, content_id, title, COALESCE(display_name,''), is_ps5,
		        COALESCE(platform,''), COALESCE(version,''), COALESCE(region,''),
		        file_path, file_size, has_cover, COALESCE(category,'game'),
		        COALESCE(category_authoritative,0), updated_at
		 FROM packages`,
		`DROP TABLE packages`,
		`ALTER TABLE packages_migrated RENAME TO packages`,
		`CREATE INDEX IF NOT EXISTS idx_packages_title_id ON packages(title_id)`,
		`CREATE INDEX IF NOT EXISTS idx_packages_category ON packages(category)`,
	}
	for _, q := range stmts {
		if _, err := tx.Exec(q); err != nil {
			return fmt.Errorf("重建 packages 表失败: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	log.Printf("[Store] packages 表重建完成：content_id 唯一约束已移除")
	return nil
}

// DeleteByFilePath 按文件路径删除记录（用于实时监测到文件被删除时同步清理）
func (s *Store) DeleteByFilePath(filePath string) error {
	_, err := s.db.Exec(`DELETE FROM packages WHERE file_path = ?`, filePath)
	return err
}

// GetSetting 获取配置
func (s *Store) GetSetting(key string) (string, error) {
	var val string
	err := s.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&val)
	if err != nil {
		return "", err
	}
	return val, nil
}

// SetSetting 保存配置
func (s *Store) SetSetting(key, val string) error {
	_, err := s.db.Exec(`
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value;
	`, key, val)
	return err
}

// GetScanDirs 获取所有配置的扫描目录（去重）
func (s *Store) GetScanDirs(defaultDir string) []string {
	dirSet := make(map[string]bool)
	keys := []string{"ps5_dir", "ps4_dir", "switch_dir"}
	for _, k := range keys {
		val, err := s.GetSetting(k)
		if err == nil && strings.TrimSpace(val) != "" {
			dirSet[strings.TrimSpace(val)] = true
		}
	}

	// 读取自定义目录列表
	customJSON, err := s.GetSetting("custom_dirs")
	if err == nil && customJSON != "" {
		var customs []string
		if err := json.Unmarshal([]byte(customJSON), &customs); err == nil {
			for _, c := range customs {
				if strings.TrimSpace(c) != "" {
					dirSet[strings.TrimSpace(c)] = true
				}
			}
		}
	}

	var result []string
	for d := range dirSet {
		result = append(result, d)
	}

	if len(result) == 0 && defaultDir != "" {
		result = append(result, defaultDir)
	}

	return result
}

func (s *Store) SavePackage(p *Package, coverData []byte) error {
	// 若传入了有效的封面二进制（来自 PKG 内置 icon0），则 hasCover=1
	hasCover := 0
	if len(coverData) > 0 {
		hasCover = 1
	}

	p.Category = strings.ToLower(strings.TrimSpace(p.Category))
	if p.Category == "" {
		p.Category = "game"
	}

	query := `
	INSERT INTO packages (
		title_id, content_id, title, display_name, is_ps5, platform, version, region,
		file_path, file_size, has_cover, category, category_authoritative, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(file_path) DO UPDATE SET
		title_id = excluded.title_id,
		content_id = excluded.content_id,
		title = excluded.title,
		display_name = CASE WHEN excluded.display_name <> '' THEN excluded.display_name ELSE packages.display_name END,
		is_ps5 = excluded.is_ps5,
		platform = excluded.platform,
		version = excluded.version,
		region = excluded.region,
		file_size = excluded.file_size,
		has_cover = CASE WHEN excluded.has_cover = 1 THEN 1 ELSE packages.has_cover END,
		category = excluded.category,
		category_authoritative = excluded.category_authoritative,
		updated_at = CURRENT_TIMESTAMP;
	`

	isPS5Int := 0
	if p.IsPS5 {
		isPS5Int = 1
	}
	authInt := 0
	if p.CategoryAuthoritative {
		authInt = 1
	}

	_, err := s.db.Exec(query,
		p.TitleID, p.ContentID, p.Title, p.DisplayName, isPS5Int, p.Platform, p.Version, p.Region,
		p.FilePath, p.FileSize, hasCover, p.Category, authInt,
	)
	return err
}

func (s *Store) GetGames(search string) ([]Package, error) {
	query := `
	SELECT p.id, p.title_id, p.content_id, p.title, coalesce(p.display_name,''), p.is_ps5,
	       coalesce(p.platform, ''),
	       COALESCE((SELECT p2.version FROM packages p2
	                 WHERE p2.title_id = p.title_id AND p2.category = 'patch' AND p2.version <> ''
	                 LIMIT 1), coalesce(p.version, '')),
	       coalesce(p.region, ''),
	       p.file_path, p.file_size, p.has_cover, coalesce(p.category, 'game'), p.updated_at,
	       (SELECT COUNT(1) FROM packages d WHERE d.title_id = p.title_id AND d.category = 'dlc') as dlc_count
	FROM packages p
	WHERE p.category = 'game'
	`
	var args []interface{}
	if search != "" {
		query += ` AND (p.title LIKE ? OR p.display_name LIKE ? OR p.title_id LIKE ? OR p.region LIKE ?)`
		term := "%" + search + "%"
		args = append(args, term, term, term, term)
	}
	query += ` ORDER BY p.id DESC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Package
	for rows.Next() {
		var p Package
		var isPS5, hasCover int
		if err := rows.Scan(
			&p.ID, &p.TitleID, &p.ContentID, &p.Title, &p.DisplayName, &isPS5,
			&p.Platform, &p.Version, &p.Region,
			&p.FilePath, &p.FileSize, &hasCover, &p.Category, &p.UpdatedAt,
			&p.DLCCount,
		); err != nil {
			return nil, err
		}
		p.IsPS5 = (isPS5 == 1)
		p.HasCover = (hasCover == 1)
		if (p.Region == "" || p.Region == "全球") && len(p.ContentID) >= 2 {
			switch strings.ToUpper(p.ContentID[:2]) {
			case "HP":
				p.Region = "港版"
			case "JP":
				p.Region = "日版"
			case "UP":
				p.Region = "美版"
			case "EP":
				p.Region = "欧版"
			case "KP":
				p.Region = "韩版"
			}
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

func (s *Store) GetDLCsByTitleID(titleID string) ([]Package, error) {
	query := `
	SELECT id, title_id, content_id, title, coalesce(display_name,''), is_ps5,
	       coalesce(platform, ''), coalesce(version, ''), coalesce(region, ''),
	       file_path, file_size, has_cover, coalesce(category, 'dlc'), updated_at, 0 as dlc_count
	FROM packages
	WHERE title_id = ? AND category = 'dlc'
	ORDER BY id ASC
	`
	rows, err := s.db.Query(query, titleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Package
	for rows.Next() {
		var p Package
		var isPS5, hasCover int
		if err := rows.Scan(
			&p.ID, &p.TitleID, &p.ContentID, &p.Title, &p.DisplayName, &isPS5,
			&p.Platform, &p.Version, &p.Region,
			&p.FilePath, &p.FileSize, &hasCover, &p.Category, &p.UpdatedAt,
			&p.DLCCount,
		); err != nil {
			return nil, err
		}
		p.IsPS5 = (isPS5 == 1)
		p.HasCover = (hasCover == 1)
		list = append(list, p)
	}
	return list, rows.Err()
}

// GetPackagesByTitleID 返回某个 TitleID 下的全部包（本体 / 补丁 / DLC），
// 供 TV 端详情页列出可安装项。
func (s *Store) GetPackagesByTitleID(titleID string) ([]Package, error) {
	query := `
	SELECT id, title_id, content_id, title, coalesce(display_name,''), is_ps5,
	       coalesce(platform, ''), coalesce(version, ''), coalesce(region, ''),
	       file_path, file_size, has_cover, coalesce(category, 'game'), updated_at, 0 as dlc_count
	FROM packages
	WHERE title_id = ?
	ORDER BY
	  CASE coalesce(category, 'game') WHEN 'game' THEN 0 WHEN 'patch' THEN 1 ELSE 2 END,
	  id ASC
	`
	rows, err := s.db.Query(query, titleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Package
	for rows.Next() {
		var p Package
		var isPS5, hasCover int
		if err := rows.Scan(
			&p.ID, &p.TitleID, &p.ContentID, &p.Title, &p.DisplayName, &isPS5,
			&p.Platform, &p.Version, &p.Region,
			&p.FilePath, &p.FileSize, &hasCover, &p.Category, &p.UpdatedAt,
			&p.DLCCount,
		); err != nil {
			return nil, err
		}
		p.IsPS5 = (isPS5 == 1)
		p.HasCover = (hasCover == 1)
		list = append(list, p)
	}
	return list, rows.Err()
}

func (s *Store) GetPackageByID(id int64) (*Package, error) {
	query := `
	SELECT id, title_id, content_id, title, coalesce(display_name,''), is_ps5,
	       coalesce(platform, ''), coalesce(version, ''), coalesce(region, ''),
	       file_path, file_size, has_cover, coalesce(category, 'game'), updated_at,
	       (SELECT COUNT(1) FROM packages d WHERE d.title_id = packages.title_id AND d.category = 'dlc') as dlc_count
	FROM packages
	WHERE id = ?
	`
	var p Package
	var isPS5, hasCover int
	err := s.db.QueryRow(query, id).Scan(
		&p.ID, &p.TitleID, &p.ContentID, &p.Title, &p.DisplayName, &isPS5,
		&p.Platform, &p.Version, &p.Region,
		&p.FilePath, &p.FileSize, &hasCover, &p.Category, &p.UpdatedAt,
		&p.DLCCount,
	)
	if err != nil {
		return nil, err
	}
	p.IsPS5 = (isPS5 == 1)
	p.HasCover = (hasCover == 1)
	return &p, nil
}

func (s *Store) DeletePackage(id int64) error {
	_, err := s.db.Exec("DELETE FROM packages WHERE id = ?", id)
	return err
}

// ListPackagesForRefine 返回用于「重新分类 + 名称美化」的最小字段集
func (s *Store) ListPackagesForRefine() ([]Package, error) {
	rows, err := s.db.Query(`
		SELECT id, title_id, content_id, title, coalesce(display_name,''),
		       coalesce(category,'game'), file_path, file_size, coalesce(platform,''),
		       coalesce(category_authoritative, 0)
		FROM packages
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Package
	for rows.Next() {
		var p Package
		var auth int
		if err := rows.Scan(&p.ID, &p.TitleID, &p.ContentID, &p.Title, &p.DisplayName,
			&p.Category, &p.FilePath, &p.FileSize, &p.Platform, &auth); err != nil {
			return nil, err
		}
		p.CategoryAuthoritative = (auth == 1)
		list = append(list, p)
	}
	return list, rows.Err()
}

// UpdateMeta 更新分类与展示名（不改变 authoritative 标记）
func (s *Store) UpdateMeta(id int64, category, displayName string) error {
	_, err := s.db.Exec(`UPDATE packages SET category = ?, display_name = ? WHERE id = ?`,
		category, displayName, id)
	return err
}

// ListFilePaths 返回所有记录的 id / title_id / category / 文件路径，
// 供「失效清理」与「实时可用性过滤」使用
func (s *Store) ListFilePaths() ([]Package, error) {
	rows, err := s.db.Query(`SELECT id, coalesce(title_id,''), coalesce(category,'game'), file_path FROM packages`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Package
	for rows.Next() {
		var p Package
		if err := rows.Scan(&p.ID, &p.TitleID, &p.Category, &p.FilePath); err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

func (s *Store) SetCoverStatus(id int64, hasCover bool) error {
	val := 0
	if hasCover {
		val = 1
	}
	_, err := s.db.Exec("UPDATE packages SET has_cover = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", val, id)
	return err
}

func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
