package meta

import (
	"context"
	"encoding/xml"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GamelistFileName 本地元数据文件名（ES-DE / Batocera / RetroBAT 通用）
const GamelistFileName = "gamelist.xml"

// gamelistXML gamelist.xml 根节点
type gamelistXML struct {
	XMLName xml.Name         `xml:"gameList"`
	Games   []gamelistGame   `xml:"game"`
	Folders []gamelistFolder `xml:"folder"`
}

type gamelistGame struct {
	Path        string `xml:"path"`
	Name        string `xml:"name"`
	Desc        string `xml:"desc"`
	Image       string `xml:"image"`
	Thumbnail   string `xml:"thumbnail"`
	ReleaseDate string `xml:"releasedate"`
	Developer   string `xml:"developer"`
	Publisher   string `xml:"publisher"`
	Genre       string `xml:"genre"`
	Rating      string `xml:"rating"`
}

type gamelistFolder struct {
	Path string `xml:"path"`
	Name string `xml:"name"`
	Desc string `xml:"desc"`
}

// gamelistIndex 一个 gamelist.xml 的索引结果
type gamelistIndex struct {
	dir   string
	byKey map[string]*Result
}

// GamelistProvider 读取本地 gamelist.xml 注入中文官方简介与中文译名。
//
// 匹配顺序（与 RomM 的 gamelist 导入器一致）：先按 <path> 相对路径，
// 再按文件名（去扩展名），最后按 <name>。缓存按「目录 + mtime」失效。
type GamelistProvider struct {
	mu    sync.Mutex
	cache map[string]cachedIndex
}

type cachedIndex struct {
	mtime time.Time
	index *gamelistIndex
}

// NewGamelistProvider 构造本地 gamelist 提供者
func NewGamelistProvider() *GamelistProvider {
	return &GamelistProvider{cache: map[string]cachedIndex{}}
}

// Lookup 在文件所在目录链与扫描根目录中查找 gamelist.xml 并匹配当前游戏。
func (p *GamelistProvider) Lookup(_ context.Context, t Target, scanRoot string) (*Result, error) {
	if p == nil || strings.TrimSpace(t.FilePath) == "" {
		return nil, nil
	}

	for _, dir := range gamelistSearchDirs(t.FilePath, scanRoot) {
		idx := p.load(dir)
		if idx == nil {
			continue
		}
		if r := idx.match(t.FilePath, t.GameName); r != nil {
			return r, nil
		}
	}
	return nil, nil
}

// gamelistSearchDirs 返回需要检查 gamelist.xml 的目录（就近优先）
func gamelistSearchDirs(filePath, scanRoot string) []string {
	var dirs []string
	seen := map[string]bool{}
	add := func(d string) {
		if d == "" || seen[d] {
			return
		}
		seen[d] = true
		dirs = append(dirs, d)
	}

	cur := filepath.Dir(filePath)
	for i := 0; i < 3; i++ {
		add(cur)
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	add(strings.TrimSpace(scanRoot))
	return dirs
}

// load 读取并缓存某个目录下的 gamelist.xml
func (p *GamelistProvider) load(dir string) *gamelistIndex {
	path := filepath.Join(dir, GamelistFileName)
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return nil
	}

	p.mu.Lock()
	if c, ok := p.cache[path]; ok && c.mtime.Equal(fi.ModTime()) {
		p.mu.Unlock()
		return c.index
	}
	p.mu.Unlock()

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc gamelistXML
	if err := xml.Unmarshal(raw, &doc); err != nil {
		return nil
	}

	idx := &gamelistIndex{dir: dir, byKey: map[string]*Result{}}
	for i := range doc.Games {
		g := doc.Games[i]
		r := convertGamelistGame(g, dir)
		if r == nil {
			continue
		}
		for _, key := range gamelistKeys(g, dir) {
			if _, exists := idx.byKey[key]; !exists {
				idx.byKey[key] = r
			}
		}
	}

	p.mu.Lock()
	p.cache[path] = cachedIndex{mtime: fi.ModTime(), index: idx}
	p.mu.Unlock()
	return idx
}

// gamelistKeys 生成可用于匹配的键
func gamelistKeys(g gamelistGame, dir string) []string {
	var keys []string
	if p := strings.TrimSpace(g.Path); p != "" {
		// <path> 通常是 ./xxx 或相对平台目录的路径
		clean := filepath.Clean(strings.TrimPrefix(strings.TrimPrefix(p, "./"), ".\\"))
		if abs := filepath.Join(dir, clean); abs != "" {
			keys = append(keys, normKey(abs))
		}
		keys = append(keys, normKey(filepath.Base(clean)))
		keys = append(keys, normKey(trimExt(filepath.Base(clean))))
	}
	if n := strings.TrimSpace(g.Name); n != "" {
		keys = append(keys, normKey(n))
	}
	return keys
}

// match 按「相对路径 → 文件名 → 名称」顺序匹配
func (idx *gamelistIndex) match(filePath, gameName string) *Result {
	if idx == nil {
		return nil
	}
	candidates := []string{
		normKey(filePath),
		normKey(filepath.Base(filePath)),
		normKey(trimExt(filepath.Base(filePath))),
	}
	if gameName != "" {
		candidates = append(candidates, normKey(gameName))
	}
	for _, c := range candidates {
		if r, ok := idx.byKey[c]; ok {
			return r
		}
	}
	return nil
}

// convertGamelistGame 把一条 gamelist 记录转换为 Result
func convertGamelistGame(g gamelistGame, dir string) *Result {
	name := strings.TrimSpace(g.Name)
	desc := strings.TrimSpace(g.Desc)
	if name == "" && desc == "" {
		return nil
	}

	r := newResult()
	r.Source = SourceGamelist

	// <name> 通常是中文译名；作为中文名写入，同时充当官方名的兜底
	if name != "" {
		r.NameCN = name
		r.Sources[FieldNameCN] = SourceGamelist
	}
	if desc != "" {
		r.Summary = desc
		r.Sources[FieldSummary] = SourceGamelist
	}
	if v := strings.TrimSpace(g.Developer); v != "" {
		r.Developer = v
		r.Sources[FieldDeveloper] = SourceGamelist
	}
	if v := strings.TrimSpace(g.Publisher); v != "" {
		r.Publisher = v
		r.Sources[FieldPublisher] = SourceGamelist
	}
	if v := strings.TrimSpace(g.Genre); v != "" {
		r.Genres = splitGenres(v)
		r.Sources[FieldGenres] = SourceGamelist
	}
	if v := parseGamelistDate(g.ReleaseDate); v != "" {
		r.ReleaseDate = v
		r.Sources[FieldReleaseDate] = SourceGamelist
	}
	if v := parseGamelistRating(g.Rating); v > 0 {
		r.Rating = v
		r.Sources[FieldRating] = SourceGamelist
	}
	// 本地封面（相对 gamelist.xml 所在目录）
	if img := strings.TrimSpace(g.Image); img != "" {
		if p := resolveGamelistPath(dir, img); p != "" {
			r.Artwork.Cover = "file://" + p
			r.Sources[FieldCover] = SourceGamelist
		}
	}
	return r
}

// parseGamelistDate 解析 ES-DE 的 releasedate（YYYYMMDDTHHMMSS）与常见日期写法
func parseGamelistDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, layout := range []string{
		"20060102T150405",
		"20060102T1504",
		"20060102",
		"2006-01-02T15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return ""
}

// parseGamelistRating ES-DE 的 rating 为 0..1，统一换算为 0..100
func parseGamelistRating(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	if f <= 1 {
		f *= 100
	}
	if f < 0 || f > 100 {
		return 0
	}
	return f
}

func splitGenres(s string) []string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '/' || r == ',' || r == ';' || r == '|'
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// resolveGamelistPath 把 <image> 解析为绝对路径（仅接受本地已存在的文件）
func resolveGamelistPath(dir, img string) string {
	if strings.HasPrefix(img, "http://") || strings.HasPrefix(img, "https://") {
		return "" // 远程图片不走本地路径
	}
	p := filepath.Clean(strings.TrimPrefix(strings.TrimPrefix(img, "./"), ".\\"))
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
		return p
	}
	return ""
}

func normKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func trimExt(name string) string {
	return strings.TrimSuffix(name, filepath.Ext(name))
}
