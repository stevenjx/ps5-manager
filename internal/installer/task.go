package installer

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"ps5-manager/internal/store"
)

// ItemState 单个安装项的状态
type ItemState string

const (
	StatePending ItemState = "pending" // 排队中
	StateRunning ItemState = "running" // 传输/安装中
	StateSuccess ItemState = "success" // 成功
	StateFailed  ItemState = "failed"  // 失败
)

// InstallItem 安装任务中的一个包（本体或某个 DLC）
type InstallItem struct {
	PackageID int64     `json:"package_id"`
	Kind      string    `json:"kind"` // "base" | "dlc"
	Title     string    `json:"title"`
	TitleID   string    `json:"title_id"`
	State     ItemState `json:"state"`
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// InstallTask 一次「传送到 PS5」的完整任务：本体 + 若干 DLC，顺序执行
type InstallTask struct {
	ID        string        `json:"id"`
	PS5IP     string        `json:"ps5_ip"`
	GameID    int64         `json:"game_id"`
	GameTitle string        `json:"game_title"`
	State     ItemState     `json:"state"` // 整体状态：pending/running/success/failed
	Items     []InstallItem `json:"items"`
	Total     int           `json:"total"`
	Done      int           `json:"done"`
	Failed    int           `json:"failed"`
	Message   string        `json:"message"`
	StartedAt time.Time     `json:"started_at"`
	EndedAt   time.Time     `json:"ended_at"`
}

// TaskManager 内存中的安装任务表
type TaskManager struct {
	mu    sync.RWMutex
	tasks map[string]*InstallTask
	seq   int64
}

var DefaultTasks = &TaskManager{tasks: make(map[string]*InstallTask)}

func (tm *TaskManager) newID() string {
	tm.seq++
	return fmt.Sprintf("t%d-%d", time.Now().UnixNano(), tm.seq)
}

func (tm *TaskManager) put(t *InstallTask) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.tasks[t.ID] = t
}

// Get 返回任务快照（深拷贝，避免并发读写）
func (tm *TaskManager) Get(id string) (*InstallTask, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	t, ok := tm.tasks[id]
	if !ok {
		return nil, false
	}
	cp := *t
	cp.Items = append([]InstallItem(nil), t.Items...)
	return &cp, true
}

// List 返回当前所有安装任务快照（按开始时间倒序），供 TV 端卡片进度条轮询。
func (tm *TaskManager) List() []*InstallTask {
	tm.mu.RLock()
	out := make([]*InstallTask, 0, len(tm.tasks))
	for _, t := range tm.tasks {
		cp := *t
		cp.Items = append([]InstallItem(nil), t.Items...)
		out = append(out, &cp)
	}
	tm.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}

// updateItem 安全更新某项状态，并重算汇总字段
func (tm *TaskManager) updateItem(taskID string, idx int, state ItemState, errMsg string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	t, ok := tm.tasks[taskID]
	if !ok {
		return
	}
	if idx < 0 || idx >= len(t.Items) {
		return
	}
	t.Items[idx].State = state
	t.Items[idx].Error = errMsg
	t.Items[idx].UpdatedAt = time.Now()

	done, failed := 0, 0
	for _, it := range t.Items {
		switch it.State {
		case StateSuccess:
			done++
		case StateFailed:
			failed++
		}
	}
	t.Done = done
	t.Failed = failed
	if done+failed == len(t.Items) {
		if failed == 0 {
			t.State = StateSuccess
		} else if done == 0 {
			t.State = StateFailed
		} else {
			// 部分成功仍视为 failed（前端会展示明细）
			t.State = StateFailed
		}
		t.EndedAt = time.Now()
	}
}

func (tm *TaskManager) setRunning(taskID string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if t, ok := tm.tasks[taskID]; ok {
		t.State = StateRunning
	}
}

// Start 创建并后台执行一个顺序安装任务，立即返回任务 ID 与初始快照
func (c *Client) Start(ctx context.Context, ps5IP string, port int, game *store.Package, dlcs []store.Package, urlBuilder func(int64) string) *InstallTask {
	task := &InstallTask{
		ID:        DefaultTasks.newID(),
		PS5IP:     ps5IP,
		GameID:    game.ID,
		GameTitle: displayName(game),
		State:     StatePending,
		StartedAt: time.Now(),
	}

	// 本体永远排第一，随后按 ID 顺序排列 DLC
	task.Items = append(task.Items, InstallItem{
		PackageID: game.ID,
		Kind:      "base",
		Title:     displayName(game),
		TitleID:   game.TitleID,
		State:     StatePending,
	})
	for _, d := range dlcs {
		task.Items = append(task.Items, InstallItem{
			PackageID: d.ID,
			Kind:      "dlc",
			Title:     displayName(&d),
			TitleID:   d.TitleID,
			State:     StatePending,
		})
	}
	task.Total = len(task.Items)
	DefaultTasks.put(task)

	go c.runTask(ctx, task.ID, ps5IP, port, urlBuilder)
	return task
}

// runTask 严格顺序执行：先本体，再逐个 DLC，实时记录每一项成败
func (c *Client) runTask(ctx context.Context, taskID, ps5IP string, port int, urlBuilder func(int64) string) {
	DefaultTasks.setRunning(taskID)
	snap, ok := DefaultTasks.Get(taskID)
	if !ok {
		return
	}

	for idx := range snap.Items {
		item := snap.Items[idx]
		DefaultTasks.updateItem(taskID, idx, StateRunning, "")

		pkgURL := urlBuilder(item.PackageID)
		log.Printf("[Installer] 任务 %s 安装 (%d/%d) %s [%s] -> %s",
			taskID, idx+1, len(snap.Items), item.Title, item.Kind, pkgURL)

		// 每个包给 30 分钟超时（大体积本体可能耗时较久）
		itemCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		err := c.SendInstall(itemCtx, ps5IP, port, pkgURL)
		cancel()

		if err != nil {
			log.Printf("[Installer] 任务 %s 第 %d 项失败: %v", taskID, idx+1, err)
			DefaultTasks.updateItem(taskID, idx, StateFailed, err.Error())
			// 本体失败则终止后续（DLC 依附本体）；DLC 失败则继续尝试其余 DLC
			if item.Kind == "base" {
				for j := idx + 1; j < len(snap.Items); j++ {
					DefaultTasks.updateItem(taskID, j, StateFailed, "本体安装失败，已跳过")
				}
				break
			}
			continue
		}

		DefaultTasks.updateItem(taskID, idx, StateSuccess, "")
		// 顺序安装时给 PS5 守护进程一点缓冲，避免并发写冲突
		if idx < len(snap.Items)-1 {
			time.Sleep(2 * time.Second)
		}
	}

	// 汇总整体信息
	if t, ok := DefaultTasks.Get(taskID); ok {
		switch {
		case t.Failed == 0 && t.Done == t.Total:
			t.Message = fmt.Sprintf("全部安装成功：本体 + %d 个 DLC", t.Total-1)
		case t.Done == 0:
			t.Message = fmt.Sprintf("安装失败：%d 项全部未成功", t.Failed)
		default:
			t.Message = fmt.Sprintf("部分成功：%d 项成功，%d 项失败", t.Done, t.Failed)
		}
		t.EndedAt = time.Now()
		DefaultTasks.mu.Lock()
		if stored, ok := DefaultTasks.tasks[taskID]; ok {
			stored.Message = t.Message
			stored.EndedAt = t.EndedAt
		}
		DefaultTasks.mu.Unlock()
	}
}

// displayName 优先返回友好展示名，回退到 Title
func displayName(p *store.Package) string {
	if p == nil {
		return ""
	}
	if p.DisplayName != "" {
		return p.DisplayName
	}
	return p.Title
}
