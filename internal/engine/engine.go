// Package engine orchestrates task runs across hosts and broadcasts live events.
package engine

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Songxwn/Auto-Deploy/internal/models"
	"github.com/Songxwn/Auto-Deploy/internal/secure"
	"github.com/Songxwn/Auto-Deploy/internal/sshx"
	"github.com/Songxwn/Auto-Deploy/internal/store"
)

// Executor runs a single script on a host. It is an interface so tests can
// substitute a fake implementation.
type Executor interface {
	Run(ctx context.Context, h models.Host, secret, passphrase, script string, onLine func(stream, text string)) (int, error)
}

// SSHExecutor is the real Executor backed by SSH.
type SSHExecutor struct {
	DialTimeout time.Duration
	KnownHosts  string
}

// Run implements Executor.
func (e SSHExecutor) Run(ctx context.Context, h models.Host, secret, passphrase, script string, onLine func(stream, text string)) (int, error) {
	return sshx.Run(ctx, h, secret, passphrase, script, e.DialTimeout, e.KnownHosts, onLine)
}

// Event is a live message pushed to SSE subscribers.
type Event struct {
	Type      string     `json:"type"` // "log" | "status"
	RunID     string     `json:"run_id"`
	RunHostID string     `json:"run_host_id,omitempty"`
	Seq       int64      `json:"seq,omitempty"`
	TS        *time.Time `json:"ts,omitempty"`
	Stream    string     `json:"stream,omitempty"`
	Text      string     `json:"text,omitempty"`
	Status    string     `json:"status,omitempty"`
	ExitCode  *int       `json:"exit_code,omitempty"`
}

// hub fans out events to per-run subscribers.
type hub struct {
	mu   sync.Mutex
	subs map[string]map[chan Event]struct{}
}

func newHub() *hub {
	return &hub{subs: map[string]map[chan Event]struct{}{}}
}

func (h *hub) subscribe(runID string) chan Event {
	ch := make(chan Event, 4096)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[runID] == nil {
		h.subs[runID] = map[chan Event]struct{}{}
	}
	h.subs[runID][ch] = struct{}{}
	return ch
}

func (h *hub) unsubscribe(runID string, ch chan Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set := h.subs[runID]; set != nil {
		delete(set, ch)
		if len(set) == 0 {
			delete(h.subs, runID)
		}
	}
}

func (h *hub) publish(runID string, ev Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[runID] {
		select {
		case ch <- ev:
		default:
			// Slow consumer: drop the live event. Logs remain persisted and
			// can be recovered by reloading the run.
		}
	}
}

// Engine starts, cancels and supervises runs.
type Engine struct {
	store       *store.Store
	box         *secure.Box
	exec        Executor
	concurrency int
	runTimeout  time.Duration
	hub         *hub

	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

// New creates an Engine.
func New(st *store.Store, box *secure.Box, exec Executor, concurrency int, runTimeout time.Duration) *Engine {
	if concurrency < 1 {
		concurrency = 1
	}
	return &Engine{
		store:       st,
		box:         box,
		exec:        exec,
		concurrency: concurrency,
		runTimeout:  runTimeout,
		hub:         newHub(),
		cancels:     map[string]context.CancelFunc{},
	}
}

// Subscribe returns a channel of live events for a run.
func (e *Engine) Subscribe(runID string) chan Event { return e.hub.subscribe(runID) }

// Unsubscribe removes a subscriber.
func (e *Engine) Unsubscribe(runID string, ch chan Event) { e.hub.unsubscribe(runID, ch) }

// StartRun persists a new run and executes it asynchronously.
func (e *Engine) StartRun(task models.Task, hosts []models.Host) (models.Run, error) {
	now := time.Now()
	run := models.Run{
		ID:        store.NewID(),
		TaskID:    task.ID,
		TaskName:  task.Name,
		Status:    models.StatusRunning,
		CreatedAt: now,
		StartedAt: now,
	}
	for _, h := range hosts {
		run.Hosts = append(run.Hosts, models.RunHost{
			ID:       store.NewID(),
			RunID:    run.ID,
			HostID:   h.ID,
			HostName: h.Name,
			Status:   models.StatusPending,
			ExitCode: -1,
		})
	}
	if err := e.store.CreateRun(&run); err != nil {
		return models.Run{}, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	if e.runTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, e.runTimeout)
	}
	e.mu.Lock()
	e.cancels[run.ID] = cancel
	e.mu.Unlock()

	go e.execute(ctx, run, task, hosts)
	return run, nil
}

// Cancel aborts a running run. It reports whether a cancel was issued.
func (e *Engine) Cancel(runID string) bool {
	e.mu.Lock()
	cancel, ok := e.cancels[runID]
	e.mu.Unlock()
	if ok {
		cancel()
	}
	return ok
}

// CancelAll aborts every in-flight run (used on shutdown).
func (e *Engine) CancelAll() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, cancel := range e.cancels {
		cancel()
	}
}

func (e *Engine) execute(ctx context.Context, run models.Run, task models.Task, hosts []models.Host) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("执行器异常", "run", run.ID, "panic", r)
			now := time.Now()
			_ = e.store.UpdateRunStatus(run.ID, models.StatusFailed, &now)
			e.hub.publish(run.ID, Event{Type: "status", RunID: run.ID, Status: models.StatusFailed})
		}
		e.mu.Lock()
		delete(e.cancels, run.ID)
		e.mu.Unlock()
	}()

	sem := make(chan struct{}, e.concurrency)
	var wg sync.WaitGroup
	var anyFailed atomic.Bool

	for i := range hosts {
		wg.Add(1)
		go func(h models.Host, rh models.RunHost) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if ctx.Err() != nil {
				e.finishCanceledHost(run.ID, rh.ID)
				return
			}
			if status := e.runHost(ctx, run.ID, task, h, rh.ID); status != models.StatusSuccess {
				anyFailed.Store(true)
			}
		}(hosts[i], run.Hosts[i])
	}
	wg.Wait()

	status := models.StatusSuccess
	switch {
	case ctx.Err() != nil:
		status = models.StatusCanceled
	case anyFailed.Load():
		status = models.StatusFailed
	}
	now := time.Now()
	if err := e.store.UpdateRunStatus(run.ID, status, &now); err != nil {
		slog.Error("更新执行状态失败", "run", run.ID, "err", err)
	}
	e.hub.publish(run.ID, Event{Type: "status", RunID: run.ID, Status: status})
}

func (e *Engine) finishCanceledHost(runID, rhID string) {
	now := time.Now()
	_ = e.store.FinishRunHost(rhID, models.StatusCanceled, -1, now)
	e.hub.publish(runID, Event{Type: "status", RunID: runID, RunHostID: rhID, Status: models.StatusCanceled})
}

// runHost executes all steps sequentially on one host and returns its status.
func (e *Engine) runHost(ctx context.Context, runID string, task models.Task, h models.Host, rhID string) string {
	var mu sync.Mutex
	var seq int64
	logf := func(stream, text string) {
		mu.Lock()
		seq++
		l := models.LogLine{RunHostID: rhID, Seq: seq, TS: time.Now(), Stream: stream, Text: text}
		mu.Unlock()
		if err := e.store.AppendLog(l); err != nil {
			slog.Error("写入日志失败", "run_host", rhID, "err", err)
		}
		ts := l.TS
		e.hub.publish(runID, Event{Type: "log", RunID: runID, RunHostID: rhID, Seq: l.Seq, TS: &ts, Stream: stream, Text: text})
	}

	_ = e.store.StartRunHost(rhID, time.Now())
	e.hub.publish(runID, Event{Type: "status", RunID: runID, RunHostID: rhID, Status: models.StatusRunning})

	secret, err := e.box.Decrypt(h.Secret)
	if err != nil {
		logf("system", "凭据解密失败: "+err.Error())
		return e.finishHost(runID, rhID, models.StatusFailed, -1)
	}
	passphrase, err := e.box.Decrypt(h.Passphrase)
	if err != nil {
		logf("system", "密钥口令解密失败: "+err.Error())
		return e.finishHost(runID, rhID, models.StatusFailed, -1)
	}

	logf("system", fmt.Sprintf("目标 %s@%s:%d (%s)", h.User, h.Addr, h.Port, h.OSType))
	status := models.StatusSuccess
	exitCode := 0
	for i, step := range task.Steps {
		if ctx.Err() != nil {
			status = models.StatusCanceled
			logf("system", "执行已取消")
			break
		}
		name := step.Name
		if name == "" {
			name = fmt.Sprintf("步骤 %d", i+1)
		}
		logf("system", fmt.Sprintf("━━ 步骤 %d/%d · %s ━━", i+1, len(task.Steps), name))
		code, err := e.exec.Run(ctx, h, secret, passphrase, step.Script, logf)
		exitCode = code
		if err != nil {
			if ctx.Err() != nil {
				status = models.StatusCanceled
				logf("system", "执行已取消")
			} else {
				status = models.StatusFailed
				logf("system", "错误: "+err.Error())
			}
			break
		}
		if code != 0 {
			status = models.StatusFailed
			logf("system", fmt.Sprintf("步骤退出码 %d，终止后续步骤", code))
			break
		}
		logf("system", "步骤完成")
	}
	return e.finishHost(runID, rhID, status, exitCode)
}

func (e *Engine) finishHost(runID, rhID, status string, exitCode int) string {
	_ = e.store.FinishRunHost(rhID, status, exitCode, time.Now())
	code := exitCode
	e.hub.publish(runID, Event{Type: "status", RunID: runID, RunHostID: rhID, Status: status, ExitCode: &code})
	return status
}
