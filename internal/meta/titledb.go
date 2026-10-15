package meta

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// maxTitleDBBytes TitleDB 单文件体积上限（官方 US.en.json 约 90MB）
const maxTitleDBBytes = 320 << 20

// TitleDBEntry TitleDB 中一条记录（字段名取自 https://github.com/blawar/titledb 实际数据）
type TitleDBEntry struct {
	TitleID     string   `json:"title_id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Intro       string   `json:"intro"`
	Publisher   string   `json:"publisher"`
	Developer   string   `json:"developer"`
	ReleaseDate string   `json:"release_date"` // YYYY-MM-DD
	Category    []string `json:"category"`
	Languages   []string `json:"languages"`
	BannerURL   string   `json:"banner_url"`
	IconURL     string   `json:"icon_url"`
	BoxArtURL   string   `json:"box_art_url"`
	Screenshots []string `json:"screenshots"`
	Size        int64    `json:"size"`
	IsDemo      bool     `json:"is_demo"`
}

// TitleDB Switch TitleDB 查询器。
//
// 官方数据源（RomM backend/tasks/scheduled/update_switch_titledb.py 使用同一地址）：
//
//	https://raw.githubusercontent.com/blawar/titledb/master/US.en.json
//
// 该文件约 90MB，因此采用「落盘缓存 + 流式解析」，只保留库里实际存在的 TitleID。
// 需要中文名称与简介时，可把 CN.zh.json 放在列表前面。
type TitleDB struct {
	enabled  bool
	urls     []string
	cacheDir string
	ttl      time.Duration
	http     *http.Client

	mu sync.Mutex
}

// NewTitleDB 构造 TitleDB 查询器
func NewTitleDB(cfg Config, cacheDir string) *TitleDB {
	urls := cfg.SwitchTitleDBURLs
	if len(urls) == 0 {
		urls = []string{DefaultSwitchTitleDBURL}
	}
	ttl := cfg.TitleDBTTL
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	return &TitleDB{
		enabled:  cfg.TitleDBEnabled,
		urls:     urls,
		cacheDir: cacheDir,
		ttl:      ttl,
		http:     newHTTPClient(5 * time.Minute), // 大文件下载需要更长超时
	}
}

// Lookup 查询若干 TitleID，返回命中的条目（未命中的键不出现在结果里）。
// 按 urls 顺序尝试，先命中者优先。
func (t *TitleDB) Lookup(ctx context.Context, titleIDs []string) (map[string]TitleDBEntry, error) {
	if t == nil || !t.enabled || len(titleIDs) == 0 {
		return nil, nil
	}
	want := make(map[string]bool, len(titleIDs))
	for _, id := range titleIDs {
		if id = normalizeTitleID(id); id != "" {
			want[id] = true
		}
	}
	if len(want) == 0 {
		return nil, nil
	}

	out := make(map[string]TitleDBEntry, len(want))
	var lastErr error
	for _, u := range t.urls {
		if len(out) >= len(want) {
			break
		}
		missing := make(map[string]bool, len(want))
		for id := range want {
			if _, ok := out[id]; !ok {
				missing[id] = true
			}
		}
		found, err := t.lookupInSource(ctx, u, missing)
		if err != nil {
			lastErr = err
			continue
		}
		for k, v := range found {
			if _, exists := out[k]; !exists {
				out[k] = v
			}
		}
	}
	if len(out) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return out, nil
}

// lookupInSource 在单个数据源中流式查找
func (t *TitleDB) lookupInSource(ctx context.Context, rawURL string, want map[string]bool) (map[string]TitleDBEntry, error) {
	f, err := t.ensureCached(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := make(map[string]TitleDBEntry)
	dec := json.NewDecoder(f)
	dec.UseNumber()

	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("TitleDB 解析失败: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("TitleDB 顶层不是 JSON 对象")
	}

	for dec.More() {
		select {
		case <-ctx.Done():
			return out, ctx.Err()
		default:
		}

		keyTok, err := dec.Token()
		if err != nil {
			return out, fmt.Errorf("TitleDB 读取键失败: %w", err)
		}
		key, _ := keyTok.(string)
		normKey := normalizeTitleID(key)

		if !want[normKey] {
			// 不需要的条目直接跳过（RawMessage 仅持有字节，不建 map）
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return out, fmt.Errorf("TitleDB 跳过节失败: %w", err)
			}
			continue
		}

		// 命中的条目解析为 map，容错性最好
		var raw map[string]any
		if err := dec.Decode(&raw); err != nil {
			return out, fmt.Errorf("TitleDB 解析条目失败: %w", err)
		}
		out[normKey] = titleDBEntryFromMap(normKey, raw)
	}
	return out, nil
}

// ensureCached 返回可读的本地文件句柄，必要时先下载并缓存
func (t *TitleDB) ensureCached(ctx context.Context, rawURL string) (*os.File, error) {
	path := t.cachePath(rawURL)

	if fi, err := os.Stat(path); err == nil && fi.Size() > 0 {
		if time.Since(fi.ModTime()) < t.ttl {
			return os.Open(path)
		}
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// 双检：等锁期间可能已被其它 goroutine 刷新
	if fi, err := os.Stat(path); err == nil && fi.Size() > 0 {
		if time.Since(fi.ModTime()) < t.ttl {
			return os.Open(path)
		}
	}

	if err := t.download(ctx, rawURL, path); err != nil {
		// 下载失败但本地有旧缓存时，降级使用旧缓存而不是报错
		if fi, statErr := os.Stat(path); statErr == nil && fi.Size() > 0 {
			return os.Open(path)
		}
		return nil, err
	}
	return os.Open(path)
}

// download 下载到 .tmp 后原子重命名
func (t *TitleDB) download(ctx context.Context, rawURL, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)

	resp, err := t.http.Do(req)
	if err != nil {
		return fmt.Errorf("TitleDB 下载失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{StatusCode: resp.StatusCode, URL: rawURL}
	}

	tmp := dst + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(f, io.LimitReader(resp.Body, maxTitleDBBytes))
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if n == 0 {
		_ = os.Remove(tmp)
		return fmt.Errorf("TitleDB 下载内容为空: %s", rawURL)
	}
	return os.Rename(tmp, dst)
}

// cachePath 缓存文件路径：<sha1(前12位)>-<basename>
func (t *TitleDB) cachePath(rawURL string) string {
	sum := sha1.Sum([]byte(rawURL))
	base := filepath.Base(strings.SplitN(rawURL, "?", 2)[0])
	if base == "" || base == "." || base == "/" {
		base = "titledb.json"
	}
	return filepath.Join(t.cacheDir, hex.EncodeToString(sum[:])[:12]+"-"+base)
}

// normalizeTitleID 归一化为大写 16 位十六进制
func normalizeTitleID(id string) string {
	id = strings.ToUpper(strings.TrimSpace(id))
	id = strings.Trim(id, "[]")
	if len(id) != 16 {
		return ""
	}
	for _, r := range id {
		if !strings.ContainsRune("0123456789ABCDEF", r) {
			return ""
		}
	}
	return id
}

// titleDBEntryFromMap 从 map 中安全提取字段
func titleDBEntryFromMap(titleID string, m map[string]any) TitleDBEntry {
	e := TitleDBEntry{
		TitleID:     titleID,
		Name:        mapStr(m, "name"),
		Description: mapStr(m, "description"),
		Intro:       mapStr(m, "intro"),
		Publisher:   mapStr(m, "publisher"),
		Developer:   mapStr(m, "developer"),
		BannerURL:   mapStr(m, "bannerUrl"),
		IconURL:     mapStr(m, "iconUrl"),
		BoxArtURL:   mapStr(m, "frontBoxArt"),
		Category:    mapStrSlice(m, "category"),
		Languages:   mapStrSlice(m, "languages"),
		Screenshots: mapStrSlice(m, "screenshots"),
		Size:        mapInt(m, "size"),
		IsDemo:      mapBool(m, "isDemo"),
	}
	if d := mapInt(m, "releaseDate"); d > 0 {
		e.ReleaseDate = formatTitleDBDate(d)
	}
	return e
}

func mapStr(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func mapInt(m map[string]any, key string) int64 {
	switch v := m[key].(type) {
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n
		}
	case float64:
		return int64(v)
	case string:
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			return n
		}
	}
	return 0
}

func mapBool(m map[string]any, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

func mapStrSlice(m map[string]any, key string) []string {
	arr, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// formatTitleDBDate 把 YYYYMMDD 整数格式化为 YYYY-MM-DD
func formatTitleDBDate(v int64) string {
	s := strconv.FormatInt(v, 10)
	if len(s) != 8 {
		return ""
	}
	t, err := time.Parse("20060102", s)
	if err != nil {
		return ""
	}
	return t.Format("2006-01-02")
}

// ToResult 把 TitleDB 条目转换为统一 Result（作为 Switch 平台的官方元数据源）
func (e TitleDBEntry) ToResult() *Result {
	r := newResult()
	r.Source = SourceTitleDB

	// TitleDB 的 name 是官方 eShop 名称；中文数据源（如 CN.zh.json）直接给出中文名
	r.Title = e.Name
	if e.Name != "" {
		r.Sources[FieldTitle] = SourceTitleDB
	}
	desc := e.Description
	if strings.TrimSpace(desc) == "" {
		desc = e.Intro
	}
	if strings.TrimSpace(desc) != "" {
		r.Summary = desc
		r.Sources[FieldSummary] = SourceTitleDB
	}
	if e.Publisher != "" {
		r.Publisher = e.Publisher
		r.Sources[FieldPublisher] = SourceTitleDB
	}
	if e.Developer != "" {
		r.Developer = e.Developer
		r.Sources[FieldDeveloper] = SourceTitleDB
	}
	if e.ReleaseDate != "" {
		r.ReleaseDate = e.ReleaseDate
		r.Sources[FieldReleaseDate] = SourceTitleDB
	}
	if len(e.Category) > 0 {
		r.Genres = e.Category
		r.Sources[FieldGenres] = SourceTitleDB
	}
	// 竖版封面优先 frontBoxArt，其次横版 banner
	if e.BoxArtURL != "" {
		r.Artwork.Cover = e.BoxArtURL
		r.Sources[FieldCover] = SourceTitleDB
	}
	if e.BannerURL != "" {
		r.Artwork.Banner = e.BannerURL
		r.Sources[FieldBanner] = SourceTitleDB
	}
	if len(e.Screenshots) > 0 {
		r.Artwork.Screenshots = e.Screenshots
		r.Sources[FieldScreenshots] = SourceTitleDB
	}
	return r
}
