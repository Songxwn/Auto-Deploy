package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Songxwn/Auto-Deploy/internal/engine"
	"github.com/Songxwn/Auto-Deploy/internal/models"
)

func (s *Server) runTask(w http.ResponseWriter, r *http.Request) {
	task, err := s.store.GetTask(r.PathValue("id"))
	if storeErr(w, err) {
		return
	}
	var req struct {
		HostIDs []string `json:"host_ids"`
	}
	if err := decodeBody(w, r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.HostIDs) == 0 {
		writeErr(w, http.StatusBadRequest, "请至少选择一台主机")
		return
	}
	hosts := make([]models.Host, 0, len(req.HostIDs))
	seen := map[string]bool{}
	for _, id := range req.HostIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		h, err := s.store.GetHost(id)
		if storeErr(w, err) {
			return
		}
		if h.OSType != task.OSType {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("主机 %s 的系统为 %s，与任务要求的 %s 不匹配", h.Name, h.OSType, task.OSType))
			return
		}
		hosts = append(hosts, h)
	}
	run, err := s.engine.StartRun(task, hosts)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, run)
}

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := s.store.ListRuns(limit)
	if storeErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.store.GetRun(r.PathValue("id"))
	if storeErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) cancelRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, err := s.store.GetRun(id)
	if storeErr(w, err) {
		return
	}
	if models.Terminal(run.Status) {
		writeErr(w, http.StatusConflict, "该执行已结束，无法取消")
		return
	}
	s.engine.Cancel(id)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// runLogs streams persisted + live events over SSE, or exports plain text
// when ?format=text is given.
func (s *Server) runLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ch := s.engine.Subscribe(id)
	defer s.engine.Unsubscribe(id, ch)

	run, err := s.store.GetRun(id)
	if storeErr(w, err) {
		return
	}
	if r.URL.Query().Get("format") == "text" {
		s.exportLogs(w, run)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "当前环境不支持流式响应")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	send := func(ev engine.Event) bool {
		b, err := json.Marshal(ev)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	// Replay everything already persisted.
	lastSeq := map[string]int64{}
	lines, err := s.store.LogsForRun(id)
	if err != nil {
		return
	}
	for _, l := range lines {
		ts := l.TS
		if !send(engine.Event{Type: "log", RunID: id, RunHostID: l.RunHostID, Seq: l.Seq, TS: &ts, Stream: l.Stream, Text: l.Text}) {
			return
		}
		lastSeq[l.RunHostID] = l.Seq
	}
	// Snapshot of current statuses.
	if !send(engine.Event{Type: "status", RunID: id, Status: run.Status}) {
		return
	}
	for _, rh := range run.Hosts {
		ev := engine.Event{Type: "status", RunID: id, RunHostID: rh.ID, Status: rh.Status}
		if rh.Status != models.StatusPending {
			code := rh.ExitCode
			ev.ExitCode = &code
		}
		if !send(ev) {
			return
		}
	}
	if models.Terminal(run.Status) {
		return
	}

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case ev := <-ch:
			if ev.Type == "log" && ev.Seq <= lastSeq[ev.RunHostID] {
				continue // already replayed
			}
			if !send(ev) {
				return
			}
			if ev.Type == "status" && ev.RunHostID == "" && models.Terminal(ev.Status) {
				return
			}
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) exportLogs(w http.ResponseWriter, run models.Run) {
	lines, err := s.store.LogsForRun(run.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	byHost := map[string][]models.LogLine{}
	for _, l := range lines {
		byHost[l.RunHostID] = append(byHost[l.RunHostID], l)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Auto-Deploy 执行日志\n任务: %s\n执行 ID: %s\n状态: %s\n\n", run.TaskName, run.ID, run.Status)
	for _, rh := range run.Hosts {
		fmt.Fprintf(&b, "════════ 主机: %s (%s, 退出码 %d) ════════\n", rh.HostName, rh.Status, rh.ExitCode)
		for _, l := range byHost[rh.ID] {
			fmt.Fprintf(&b, "[%s][%s] %s\n", l.TS.Format("15:04:05"), l.Stream, l.Text)
		}
		b.WriteString("\n")
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.txt", run.ID))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b.String()))
}
