// Package watcher 提供游戏目录的实时监测能力。
//
// 采用「轮询 + 快照差异」而非 inotify：用户的 PKG 目录常挂载自 NAS
// （SMB/NFS），inotify 在多数网络挂载上不可靠；轮询方式在任何存储上都能工作，
// 且实现简单、无额外依赖。
//
// 检测到新增/修改 → 增量解析入库；检测到删除 → 同步移除记录。
package watcher

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ps5-manager/internal/scanner"
)

var mediaExts = map[string]bool{
	".pkg": true,
	".xci": true,
	".nsp": true,
	".nsz": true,
}

type fileStamp struct {
	size    int64
	modTime time.Time
}

// Watcher 实时监测游戏目录变化
type Watcher struct {
	scanner  *scanner.Scanner
	dirs     func() []string
	interval time.Duration
	snapshot map[string]fileStamp
}

// New 创建监测器。dirs 为动态获取当前扫描目录的回调（支持设置变更后自动生效）。
func New(sc *scanner.Scanner, dirs func() []string, interval time.Duration) *Watcher {
	return &Watcher{
		scanner:  sc,
		dirs:     dirs,
		interval: interval,
		snapshot: make(map[string]fileStamp),
	}
}

// Run 阻塞运行监测循环，直到 ctx 取消。
func (w *Watcher) Run(ctx context.Context) {
	if w.interval <= 0 {
		log.Printf("[Watcher] 实时目录监测已禁用（interval=0）")
		return
	}

	// 建立基线快照（只记录，不触发扫描）
	w.snapshot, _ = w.scan()
	log.Printf("[Watcher] 实时目录监测已启动：间隔 %s，当前追踪 %d 个游戏文件",
		w.interval, len(w.snapshot))

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Printf("[Watcher] 实时目录监测已停止")
			return
		case <-ticker.C:
			w.poll(ctx)
		}
	}
}

// scan 遍历所有可用目录，返回文件快照与「本次成功访问的根目录」列表。
// 根目录不可用（例如外接盘离线）时会被跳过，避免其下文件被误判为删除。
func (w *Watcher) scan() (map[string]fileStamp, []string) {
	out := make(map[string]fileStamp)
	var roots []string

	for _, dir := range w.dirs() {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			abs = dir
		}
		if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
			continue // 目录不可用，跳过（不视为删除）
		}
		roots = append(roots, abs)

		_ = filepath.Walk(abs, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil || info.IsDir() {
				return nil
			}
			if !mediaExts[strings.ToLower(filepath.Ext(path))] {
				return nil
			}
			out[path] = fileStamp{size: info.Size(), modTime: info.ModTime()}
			return nil
		})
	}
	return out, roots
}

func (w *Watcher) poll(ctx context.Context) {
	current, roots := w.scan()

	var added, removed []string
	for p, st := range current {
		if old, ok := w.snapshot[p]; !ok || old != st {
			added = append(added, p)
		}
	}
	for p := range w.snapshot {
		if _, ok := current[p]; ok {
			continue
		}
		// 仅当该文件位于本次成功访问的根目录下，才认定为「已删除」
		if pathUnderAny(p, roots) {
			removed = append(removed, p)
		}
	}
	w.snapshot = current

	if len(added) == 0 && len(removed) == 0 {
		return
	}

	log.Printf("[Watcher] 检测到目录变化：新增/修改 %d 个，删除 %d 个", len(added), len(removed))
	if len(added) > 0 {
		w.scanner.ScanFiles(ctx, added)
		w.scanner.FetchMissingCovers(ctx)
	}
	if len(removed) > 0 {
		w.scanner.RemoveFiles(removed)
	}
}

func pathUnderAny(filePath string, roots []string) bool {
	ap, err := filepath.Abs(filePath)
	if err != nil {
		ap = filePath
	}
	for _, r := range roots {
		rel, err := filepath.Rel(r, ap)
		if err != nil {
			continue
		}
		if rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		return true
	}
	return false
}
