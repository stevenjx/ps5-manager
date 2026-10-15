package store

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

// Meta 一条记录的刮削元数据。
// 字段与 meta.Result 对应，来源信息保留在 Sources 里便于界面标注出处。
type Meta struct {
	IGDBID      int64             `json:"igdb_id"`
	NameCN      string            `json:"name_cn"`
	Summary     string            `json:"summary"`
	ReleaseDate string            `json:"release_date"`
	Developer   string            `json:"developer"`
	Publisher   string            `json:"publisher"`
	Genres      []string          `json:"genres"`
	Rating      float64           `json:"rating"`
	Source      string            `json:"source"`
	Sources     map[string]string `json:"sources"`
	Cover       string            `json:"art_cover"`
	Hero        string            `json:"art_hero"`
	Logo        string            `json:"art_logo"`
	Banner      string            `json:"art_banner"`
	Screenshots []string          `json:"art_screenshots"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// IsEmpty 是否没有任何有效元数据
func (m *Meta) IsEmpty() bool {
	if m == nil {
		return true
	}
	return m.Summary == "" && m.NameCN == "" && m.IGDBID == 0 &&
		m.Cover == "" && m.ReleaseDate == ""
}

// ReleaseYear 从 release_date（YYYY-MM-DD）提取年份；无则返回 0
func (m *Meta) ReleaseYear() int {
	if m == nil || len(m.ReleaseDate) < 4 {
		return 0
	}
	y := 0
	for i := 0; i < 4; i++ {
		c := m.ReleaseDate[i]
		if c < '0' || c > '9' {
			return 0
		}
		y = y*10 + int(c-'0')
	}
	return y
}

// MetaTarget 刮削流水线需要的记录字段
type MetaTarget struct {
	ID          int64
	TitleID     string
	ContentID   string
	Title       string
	DisplayName string
	Platform    string
	Category    string
	FilePath    string
	FileSize    int64
}

// UpdateMetadata 写入刮削结果
func (s *Store) UpdateMetadata(id int64, m *Meta) error {
	if m == nil {
		return nil
	}
	genres, _ := json.Marshal(m.Genres)
	sources, _ := json.Marshal(m.Sources)
	shots, _ := json.Marshal(m.Screenshots)

	_, err := s.db.Exec(`
		UPDATE packages SET
			igdb_id = ?, name_cn = ?, summary = ?, release_date = ?,
			developer = ?, publisher = ?, genres = ?, rating = ?,
			meta_source = ?, meta_sources = ?,
			art_cover = ?, art_hero = ?, art_logo = ?, art_banner = ?, art_screenshots = ?,
			meta_updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		m.IGDBID, m.NameCN, m.Summary, m.ReleaseDate,
		m.Developer, m.Publisher, string(genres), m.Rating,
		m.Source, string(sources),
		m.Cover, m.Hero, m.Logo, m.Banner, string(shots),
		id,
	)
	return err
}

// ClearMetadata 清空某条记录的元数据（用于重新刮削）
func (s *Store) ClearMetadata(id int64) error {
	_, err := s.db.Exec(`
		UPDATE packages SET
			igdb_id = 0, name_cn = '', summary = '', release_date = '',
			developer = '', publisher = '', genres = '', rating = 0,
			meta_source = '', meta_sources = '',
			art_cover = '', art_hero = '', art_logo = '', art_banner = '', art_screenshots = '',
			meta_updated_at = NULL
		WHERE id = ?`, id)
	return err
}

const metaSelectColumns = `coalesce(igdb_id,0), coalesce(name_cn,''), coalesce(summary,''),
	coalesce(release_date,''), coalesce(developer,''), coalesce(publisher,''),
	coalesce(genres,''), coalesce(rating,0), coalesce(meta_source,''),
	coalesce(meta_sources,''), coalesce(art_cover,''), coalesce(art_hero,''),
	coalesce(art_logo,''), coalesce(art_banner,''), coalesce(art_screenshots,''),
	meta_updated_at`

func scanMeta(scan func(...any) error) (*Meta, error) {
	var m Meta
	var genres, sources, shots string
	var updated sql.NullTime
	if err := scan(
		&m.IGDBID, &m.NameCN, &m.Summary, &m.ReleaseDate, &m.Developer, &m.Publisher,
		&genres, &m.Rating, &m.Source, &sources,
		&m.Cover, &m.Hero, &m.Logo, &m.Banner, &shots, &updated,
	); err != nil {
		return nil, err
	}
	m.Genres = decodeStringList(genres)
	m.Screenshots = decodeStringList(shots)
	if sources != "" {
		_ = json.Unmarshal([]byte(sources), &m.Sources)
	}
	if m.Sources == nil {
		m.Sources = map[string]string{}
	}
	if updated.Valid {
		m.UpdatedAt = updated.Time
	}
	return &m, nil
}

func decodeStringList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// GetMetaForIDs 批量读取元数据（仅返回存在的记录）
func (s *Store) GetMetaForIDs(ids []int64) (map[int64]*Meta, error) {
	out := map[int64]*Meta{}
	if len(ids) == 0 {
		return out, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := `SELECT id, ` + metaSelectColumns + ` FROM packages WHERE id IN (` +
		strings.Join(placeholders, ",") + `)`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		m, err := scanMeta(func(dest ...any) error {
			all := append([]any{&id}, dest...)
			return rows.Scan(all...)
		})
		if err != nil {
			return nil, err
		}
		out[id] = m
	}
	return out, rows.Err()
}

// GetMeta 读取单条记录的元数据
func (s *Store) GetMeta(id int64) (*Meta, error) {
	query := `SELECT ` + metaSelectColumns + ` FROM packages WHERE id = ?`
	row := s.db.QueryRow(query, id)
	m, err := scanMeta(row.Scan)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// ListMetaTargets 返回可用于刮削的记录（仅本体），供后台任务与手动刮削使用。
//
// platforms 为空表示不限平台。onlyMissing=true 时只返回尚未刮削过的记录。
func (s *Store) ListMetaTargets(platforms []string, onlyMissing bool, limit int) ([]MetaTarget, error) {
	query := `SELECT id, coalesce(title_id,''), coalesce(content_id,''), coalesce(title,''),
	                 coalesce(display_name,''), coalesce(platform,''), coalesce(category,'game'),
	                 file_path, coalesce(file_size,0)
	          FROM packages
	          WHERE coalesce(category,'game') = 'game'`
	var args []any

	if len(platforms) > 0 {
		ph := make([]string, len(platforms))
		for i, p := range platforms {
			ph[i] = "?"
			args = append(args, strings.ToUpper(strings.TrimSpace(p)))
		}
		query += ` AND UPPER(coalesce(platform,'')) IN (` + strings.Join(ph, ",") + `)`
	}
	if onlyMissing {
		query += ` AND (coalesce(meta_source,'') = '' OR meta_updated_at IS NULL)`
	}
	query += ` ORDER BY id ASC`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []MetaTarget
	for rows.Next() {
		var t MetaTarget
		if err := rows.Scan(&t.ID, &t.TitleID, &t.ContentID, &t.Title, &t.DisplayName,
			&t.Platform, &t.Category, &t.FilePath, &t.FileSize); err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, rows.Err()
}

// GetMetaTarget 读取单条记录的刮削目标信息；不存在返回 (nil, nil)
func (s *Store) GetMetaTarget(id int64) (*MetaTarget, error) {
	row := s.db.QueryRow(`
		SELECT id, coalesce(title_id,''), coalesce(content_id,''), coalesce(title,''),
		       coalesce(display_name,''), coalesce(platform,''), coalesce(category,'game'),
		       file_path, coalesce(file_size,0)
		FROM packages WHERE id = ?`, id)

	var t MetaTarget
	if err := row.Scan(&t.ID, &t.TitleID, &t.ContentID, &t.Title, &t.DisplayName,
		&t.Platform, &t.Category, &t.FilePath, &t.FileSize); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

// MetaStats 元数据覆盖率统计
type MetaStats struct {
	Total       int `json:"total"`
	Scraped     int `json:"scraped"`
	WithSummary int `json:"with_summary"`
	WithIGDBID  int `json:"with_igdb_id"`
	WithArtwork int `json:"with_artwork"`
}

// CountMeta 统计刮削进度
func (s *Store) CountMeta() (MetaStats, error) {
	var st MetaStats
	err := s.db.QueryRow(`
		SELECT
			COUNT(1),
			SUM(CASE WHEN coalesce(meta_source,'') <> '' THEN 1 ELSE 0 END),
			SUM(CASE WHEN coalesce(summary,'') <> '' THEN 1 ELSE 0 END),
			SUM(CASE WHEN coalesce(igdb_id,0) > 0 THEN 1 ELSE 0 END),
			SUM(CASE WHEN coalesce(art_cover,'') <> '' OR coalesce(art_hero,'') <> '' THEN 1 ELSE 0 END)
		FROM packages
		WHERE coalesce(category,'game') = 'game'
	`).Scan(&st.Total, &st.Scraped, &st.WithSummary, &st.WithIGDBID, &st.WithArtwork)
	return st, err
}
