package scanner

import (
	"context"
	"log"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"ps5-manager/internal/meta"
	"ps5-manager/internal/naming"
	"ps5-manager/internal/store"
)

// EnrichProgress 元数据刮削进度（供前端轮询）
type EnrichProgress struct {
	Running   bool      `json:"running"`
	Total     int       `json:"total"`
	Done      int       `json:"done"`
	Failed    int       `json:"failed"`
	Matched   int       `json:"matched"`
	Current   string    `json:"current"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
}

// SetPipeline 注入元数据刮削流水线（未注入时刮削功能自动关闭）
func (s *Scanner) SetPipeline(p *meta.Pipeline) {
	s.enrichMu.Lock()
	s.meta = p
	s.enrichMu.Unlock()
}

// SetRootsProvider 注册扫描根目录提供者（用于就近查找 gamelist.xml）
func (s *Scanner) SetRootsProvider(fn func() []string) {
	s.enrichMu.Lock()
	s.rootsProvider = fn
	s.enrichMu.Unlock()
}

// Progress 返回当前刮削进度
func (s *Scanner) Progress() EnrichProgress {
	s.enrichMu.Lock()
	defer s.enrichMu.Unlock()
	return s.progress
}

func (s *Scanner) roots() []string {
	s.enrichMu.Lock()
	fn := s.rootsProvider
	s.enrichMu.Unlock()
	if fn == nil {
		return nil
	}
	return fn()
}

// pipeline 返回已注入的流水线（可能为 nil）
func (s *Scanner) pipeline() *meta.Pipeline {
	s.enrichMu.Lock()
	defer s.enrichMu.Unlock()
	return s.meta
}

// EnrichAll 批量刮削元数据。
//
// platforms 为空表示全部平台；force=true 时重新刮削已有记录。
// 返回 false 表示已有任务在跑（本次不重复启动）。
func (s *Scanner) EnrichAll(ctx context.Context, platforms []string, force bool) bool {
	p := s.pipeline()
	if p == nil {
		log.Printf("[Scanner] 未启用元数据刮削，跳过")
		return false
	}

	s.enrichMu.Lock()
	if s.progress.Running {
		s.enrichMu.Unlock()
		return false
	}
	s.progress = EnrichProgress{Running: true, StartedAt: time.Now()}
	s.enrichMu.Unlock()

	go func() {
		defer func() {
			s.enrichMu.Lock()
			s.progress.Running = false
			s.progress.EndedAt = time.Now()
			s.progress.Current = ""
			final := s.progress
			s.enrichMu.Unlock()
			log.Printf("[Scanner] 元数据刮削结束：成功 %d / 失败 %d / 命中 %d，共 %d 条",
				final.Done, final.Failed, final.Matched, final.Total)
		}()

		targets, err := s.store.ListMetaTargets(platforms, !force, 0)
		if err != nil {
			log.Printf("[Scanner] 元数据刮削：读取目标失败: %v", err)
			return
		}

		s.enrichMu.Lock()
		s.progress.Total = len(targets)
		s.enrichMu.Unlock()

		if len(targets) == 0 {
			log.Printf("[Scanner] 元数据刮削：没有待处理记录")
			return
		}
		log.Printf("[Scanner] 元数据刮削开始，共 %d 条（并发 %d）", len(targets), p.Config().Concurrency)

		roots := s.roots()
		concurrency := p.Config().Concurrency
		if concurrency <= 0 {
			concurrency = 4
		}

		sem := make(chan struct{}, concurrency)
		var wg sync.WaitGroup
		for i := range targets {
			select {
			case <-ctx.Done():
				wg.Wait()
				return
			case sem <- struct{}{}:
			}

			wg.Add(1)
			go func(t store.MetaTarget) {
				defer wg.Done()
				defer func() { <-sem }()

				s.enrichMu.Lock()
				s.progress.Current = t.DisplayName
				if s.progress.Current == "" {
					s.progress.Current = t.Title
				}
				s.enrichMu.Unlock()

				res, err := s.scrapeTarget(ctx, p, t, roots)
				if err != nil {
					log.Printf("[Scanner] 刮削失败 %s: %v", t.Title, err)
				}

				matched := res != nil && !res.IsEmpty()
				if matched {
					if perr := s.persistMeta(t.ID, res); perr != nil {
						log.Printf("[Scanner] 元数据入库失败 id=%d: %v", t.ID, perr)
						matched = false
					}
				}

				s.enrichMu.Lock()
				if err != nil {
					s.progress.Failed++
				} else {
					s.progress.Done++
					if matched {
						s.progress.Matched++
					}
				}
				s.enrichMu.Unlock()
			}(targets[i])
		}
		wg.Wait()

		// 刮削完成后把新拿到的高清封面缓存到本地封面目录，
		// 这样即使素材源临时不可用，界面也仍有封面可用。
		refreshCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		s.FetchMissingCovers(refreshCtx)
		cancel()
	}()

	return true
}

// EnrichOne 同步刮削单条记录（用于界面上「重新刮削此游戏」）。
func (s *Scanner) EnrichOne(ctx context.Context, gameID int64) (*meta.Result, error) {
	p := s.pipeline()
	if p == nil {
		return nil, errNoPipeline
	}

	t, err := s.store.GetMetaTarget(gameID)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, nil
	}

	// 手动刮削视为强制刷新：先清空旧值，避免「首个命中即采用」把旧数据留在原位
	if err := s.store.ClearMetadata(gameID); err != nil {
		return nil, err
	}

	res, err := s.scrapeTarget(ctx, p, *t, s.roots())
	if err != nil {
		return res, err
	}
	if res != nil && !res.IsEmpty() {
		if perr := s.persistMeta(gameID, res); perr != nil {
			return res, perr
		}
	}
	return res, nil
}

// scrapeTarget 组装 Target 并执行流水线
func (*Scanner) scrapeTarget(ctx context.Context, p *meta.Pipeline, t store.MetaTarget, roots []string) (*meta.Result, error) {
	target := buildTarget(t)
	root := scanRootFor(t.FilePath, roots)
	return p.Scrape(ctx, target, root)
}

// buildTarget 用命名规范解析出的信息组装刮削目标
func buildTarget(t store.MetaTarget) meta.Target {
	parsed := naming.ParsePath(t.FilePath)

	name := parsed.Name
	if name == "" {
		name = strings.TrimSpace(t.DisplayName)
	}
	if name == "" {
		name = strings.TrimSpace(t.Title)
	}

	// (igdb-xxxx) 内联标签：命中后直接按 ID 精确获取，跳过名称搜索
	var providerID int64
	if strings.EqualFold(parsed.ProviderTag, "igdb") && parsed.ProviderID != "" {
		if id, err := strconv.ParseInt(parsed.ProviderID, 10, 64); err == nil {
			providerID = id
		}
	}

	platform := strings.ToLower(strings.TrimSpace(t.Platform))
	titleID := t.TitleID
	// PS3 的 TitleID 落在文件名的序列号上（如 [BLES00932]）
	if titleID == "" && parsed.PSSerial != "" {
		titleID = parsed.PSSerial
	}

	return meta.Target{
		GameID:     t.ID,
		Platform:   platform,
		TitleID:    titleID,
		ContentID:  t.ContentID,
		FilePath:   t.FilePath,
		GameName:   name,
		Regions:    parsed.Regions,
		Languages:  parsed.Languages,
		ProviderID: providerID,
		FileSize:   t.FileSize,
	}
}

// scanRootFor 返回文件所属的扫描根目录（最长匹配前缀）
func scanRootFor(filePath string, roots []string) string {
	ap, err := filepath.Abs(filePath)
	if err != nil {
		ap = filePath
	}
	best := ""
	for _, r := range roots {
		ar, err := filepath.Abs(r)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(ar, ap)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		if len(ar) > len(best) {
			best = ar
		}
	}
	return best
}

// persistMeta 把刮削结果写入数据库
func (s *Scanner) persistMeta(gameID int64, r *meta.Result) error {
	if r == nil {
		return nil
	}
	m := &store.Meta{
		IGDBID:      r.IGDBID,
		NameCN:      r.NameCN,
		Summary:     r.Summary,
		ReleaseDate: r.ReleaseDate,
		Developer:   r.Developer,
		Publisher:   r.Publisher,
		Genres:      r.Genres,
		Rating:      r.Rating,
		Source:      string(r.Source),
		Cover:       r.Artwork.Cover,
		Hero:        r.Artwork.Hero,
		Logo:        r.Artwork.Logo,
		Banner:      r.Artwork.Banner,
		Screenshots: r.Artwork.Screenshots,
	}
	if r.Sources != nil {
		m.Sources = make(map[string]string, len(r.Sources))
		for k, v := range r.Sources {
			m.Sources[k] = string(v)
		}
	}
	return s.store.UpdateMetadata(gameID, m)
}
