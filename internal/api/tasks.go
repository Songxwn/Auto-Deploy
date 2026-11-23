package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Songxwn/Auto-Deploy/internal/models"
	"github.com/Songxwn/Auto-Deploy/internal/store"
)

type taskInput struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	OSType      string        `json:"os_type"`
	Steps       []models.Step `json:"steps"`
}

func (in *taskInput) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if in.Name == "" {
		return fmt.Errorf("任务名称不能为空")
	}
	if in.OSType != models.OSLinux && in.OSType != models.OSWindows {
		return fmt.Errorf("系统类型必须为 linux 或 windows")
	}
	if len(in.Steps) == 0 {
		return fmt.Errorf("至少需要一个步骤")
	}
	for i := range in.Steps {
		in.Steps[i].Name = strings.TrimSpace(in.Steps[i].Name)
		if strings.TrimSpace(in.Steps[i].Script) == "" {
			return fmt.Errorf("步骤 %d 的脚本不能为空", i+1)
		}
		if in.Steps[i].Name == "" {
			in.Steps[i].Name = fmt.Sprintf("步骤 %d", i+1)
		}
	}
	return nil
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.store.ListTasks()
	if storeErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.GetTask(r.PathValue("id"))
	if storeErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var in taskInput
	if err := decodeBody(w, r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := in.validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now()
	t := models.Task{
		ID: store.NewID(), Name: in.Name, Description: in.Description,
		OSType: in.OSType, Steps: in.Steps, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.CreateTask(&t); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) updateTask(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.GetTask(r.PathValue("id"))
	if storeErr(w, err) {
		return
	}
	var in taskInput
	if err := decodeBody(w, r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := in.validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	t.Name, t.Description, t.OSType, t.Steps = in.Name, in.Description, in.OSType, in.Steps
	t.UpdatedAt = time.Now()
	if err := s.store.UpdateTask(&t); storeErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteTask(r.PathValue("id")); storeErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
