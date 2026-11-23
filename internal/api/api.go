// Package api exposes the REST API, SSE log stream and embedded web UI.
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Songxwn/Auto-Deploy/internal/config"
	"github.com/Songxwn/Auto-Deploy/internal/engine"
	"github.com/Songxwn/Auto-Deploy/internal/secure"
	"github.com/Songxwn/Auto-Deploy/internal/store"
	"github.com/Songxwn/Auto-Deploy/web"
)

// Server holds the dependencies of all HTTP handlers.
type Server struct {
	cfg     config.Config
	store   *store.Store
	box     *secure.Box
	engine  *engine.Engine
	version string
}

// New creates a Server.
func New(cfg config.Config, st *store.Store, box *secure.Box, eng *engine.Engine, version string) *Server {
	return &Server{cfg: cfg, store: st, box: box, engine: eng, version: version}
}

// Handler builds the root HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.handleLogout)

	authed := http.NewServeMux()
	authed.HandleFunc("GET /api/me", s.handleMe)

	authed.HandleFunc("GET /api/hosts", s.listHosts)
	authed.HandleFunc("POST /api/hosts", s.createHost)
	authed.HandleFunc("GET /api/hosts/{id}", s.getHost)
	authed.HandleFunc("PUT /api/hosts/{id}", s.updateHost)
	authed.HandleFunc("DELETE /api/hosts/{id}", s.deleteHost)
	authed.HandleFunc("POST /api/hosts/{id}/test", s.testHost)

	authed.HandleFunc("GET /api/tasks", s.listTasks)
	authed.HandleFunc("POST /api/tasks", s.createTask)
	authed.HandleFunc("GET /api/tasks/{id}", s.getTask)
	authed.HandleFunc("PUT /api/tasks/{id}", s.updateTask)
	authed.HandleFunc("DELETE /api/tasks/{id}", s.deleteTask)
	authed.HandleFunc("POST /api/tasks/{id}/run", s.runTask)

	authed.HandleFunc("GET /api/runs", s.listRuns)
	authed.HandleFunc("GET /api/runs/{id}", s.getRun)
	authed.HandleFunc("POST /api/runs/{id}/cancel", s.cancelRun)
	authed.HandleFunc("GET /api/runs/{id}/logs", s.runLogs)
	authed.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusNotFound, "接口不存在")
	})

	mux.Handle("/api/", s.requireAuth(authed))
	mux.Handle("/", http.FileServerFS(web.FS))
	return mux
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.version})
}

// ---------------------------------------------------------------- helpers

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	if err := dec.Decode(v); err != nil {
		return errors.New("请求体不是有效的 JSON")
	}
	return nil
}

func storeErr(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "记录不存在")
	} else {
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
	return true
}
