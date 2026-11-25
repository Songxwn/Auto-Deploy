package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Songxwn/Auto-Deploy/internal/models"
	"github.com/Songxwn/Auto-Deploy/internal/store"
)

type templateInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	OSType      string `json:"os_type"`
	Category    string `json:"category"`
	Script      string `json:"script"`
}

func (in *templateInput) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	in.Category = strings.TrimSpace(in.Category)
	if in.Name == "" {
		return fmt.Errorf("模板名称不能为空")
	}
	if in.OSType != models.OSLinux && in.OSType != models.OSWindows {
		return fmt.Errorf("系统类型必须为 linux 或 windows")
	}
	if strings.TrimSpace(in.Script) == "" {
		return fmt.Errorf("脚本内容不能为空")
	}
	switch in.Category {
	case models.CatWeb, models.CatDatabase, models.CatRuntime, models.CatSystem, models.CatNetwork, models.CatCustom:
	case "":
		in.Category = models.CatCustom
	default:
		return fmt.Errorf("未知的模板分类: %s", in.Category)
	}
	return nil
}

func (s *Server) listTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := s.store.ListTemplates()
	if storeErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, templates)
}

func (s *Server) getTemplate(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.GetTemplate(r.PathValue("id"))
	if storeErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) createTemplate(w http.ResponseWriter, r *http.Request) {
	var in templateInput
	if err := decodeBody(w, r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := in.validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now()
	t := models.Template{
		ID: store.NewID(), Name: in.Name, Description: in.Description,
		OSType: in.OSType, Category: in.Category, Script: in.Script,
		BuiltIn: false, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.CreateTemplate(&t); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) updateTemplate(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.GetTemplate(r.PathValue("id"))
	if storeErr(w, err) {
		return
	}
	if t.BuiltIn {
		writeErr(w, http.StatusConflict, "内置模板不可修改，可复制为自定义模板后编辑")
		return
	}
	var in templateInput
	if err := decodeBody(w, r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := in.validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	t.Name, t.Description, t.OSType, t.Category, t.Script = in.Name, in.Description, in.OSType, in.Category, in.Script
	t.UpdatedAt = time.Now()
	if err := s.store.UpdateTemplate(&t); storeErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) deleteTemplate(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.GetTemplate(r.PathValue("id"))
	if storeErr(w, err) {
		return
	}
	if t.BuiltIn {
		writeErr(w, http.StatusConflict, "内置模板不可删除")
		return
	}
	if err := s.store.DeleteTemplate(t.ID); storeErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
