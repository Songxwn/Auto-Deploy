package api

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Songxwn/Auto-Deploy/internal/models"
	"github.com/Songxwn/Auto-Deploy/internal/sshx"
	"github.com/Songxwn/Auto-Deploy/internal/store"
)

// hostView is the public representation of a host (never exposes secrets).
type hostView struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Addr      string    `json:"addr"`
	Port      int       `json:"port"`
	User      string    `json:"user"`
	AuthType  string    `json:"auth_type"`
	OSType    string    `json:"os_type"`
	Tags      string    `json:"tags"`
	HasSecret bool      `json:"has_secret"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toHostView(h models.Host) hostView {
	return hostView{
		ID: h.ID, Name: h.Name, Addr: h.Addr, Port: h.Port, User: h.User,
		AuthType: h.AuthType, OSType: h.OSType, Tags: h.Tags,
		HasSecret: h.Secret != "", CreatedAt: h.CreatedAt, UpdatedAt: h.UpdatedAt,
	}
}

type hostInput struct {
	Name       string `json:"name"`
	Addr       string `json:"addr"`
	Port       int    `json:"port"`
	User       string `json:"user"`
	AuthType   string `json:"auth_type"`
	Secret     string `json:"secret"`
	Passphrase string `json:"passphrase"`
	OSType     string `json:"os_type"`
	Tags       string `json:"tags"`
}

func (in *hostInput) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Addr = strings.TrimSpace(in.Addr)
	in.User = strings.TrimSpace(in.User)
	in.Tags = strings.TrimSpace(in.Tags)
	switch {
	case in.Name == "":
		return fmt.Errorf("主机名称不能为空")
	case in.Addr == "":
		return fmt.Errorf("主机地址不能为空")
	case in.User == "":
		return fmt.Errorf("SSH 用户不能为空")
	case in.OSType != models.OSLinux && in.OSType != models.OSWindows:
		return fmt.Errorf("系统类型必须为 linux 或 windows")
	case in.AuthType != models.AuthPassword && in.AuthType != models.AuthKey:
		return fmt.Errorf("认证方式必须为 password 或 key")
	}
	if in.Port == 0 {
		in.Port = 22
	}
	if in.Port < 1 || in.Port > 65535 {
		return fmt.Errorf("端口范围必须为 1-65535")
	}
	return nil
}

func (s *Server) listHosts(w http.ResponseWriter, r *http.Request) {
	hosts, err := s.store.ListHosts()
	if storeErr(w, err) {
		return
	}
	views := make([]hostView, 0, len(hosts))
	for _, h := range hosts {
		views = append(views, toHostView(h))
	}
	writeJSON(w, http.StatusOK, views)
}

func (s *Server) getHost(w http.ResponseWriter, r *http.Request) {
	h, err := s.store.GetHost(r.PathValue("id"))
	if storeErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, toHostView(h))
}

func (s *Server) createHost(w http.ResponseWriter, r *http.Request) {
	var in hostInput
	if err := decodeBody(w, r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := in.validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Secret == "" {
		writeErr(w, http.StatusBadRequest, "密码或私钥不能为空")
		return
	}
	secret, err := s.box.Encrypt(in.Secret)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	passphrase, err := s.box.Encrypt(in.Passphrase)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now()
	h := models.Host{
		ID: store.NewID(), Name: in.Name, Addr: in.Addr, Port: in.Port, User: in.User,
		AuthType: in.AuthType, Secret: secret, Passphrase: passphrase,
		OSType: in.OSType, Tags: in.Tags, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.CreateHost(&h); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toHostView(h))
}

func (s *Server) updateHost(w http.ResponseWriter, r *http.Request) {
	h, err := s.store.GetHost(r.PathValue("id"))
	if storeErr(w, err) {
		return
	}
	var in hostInput
	if err := decodeBody(w, r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := in.validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.Name, h.Addr, h.Port, h.User = in.Name, in.Addr, in.Port, in.User
	h.AuthType, h.OSType, h.Tags = in.AuthType, in.OSType, in.Tags
	// Empty secret fields keep the previously stored credentials.
	if in.Secret != "" {
		secret, err := s.box.Encrypt(in.Secret)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		h.Secret = secret
	}
	if in.Passphrase != "" {
		passphrase, err := s.box.Encrypt(in.Passphrase)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		h.Passphrase = passphrase
	}
	h.UpdatedAt = time.Now()
	if err := s.store.UpdateHost(&h); storeErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, toHostView(h))
}

func (s *Server) deleteHost(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteHost(r.PathValue("id")); storeErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) testHost(w http.ResponseWriter, r *http.Request) {
	h, err := s.store.GetHost(r.PathValue("id"))
	if storeErr(w, err) {
		return
	}
	secret, err := s.box.Decrypt(h.Secret)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "凭据解密失败: "+err.Error())
		return
	}
	passphrase, err := s.box.Decrypt(h.Passphrase)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "凭据解密失败: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var out bytes.Buffer
	code, runErr := sshx.Run(ctx, h, secret, passphrase, sshx.TestScript(h.OSType),
		time.Duration(s.cfg.SSHTimeout)*time.Second, s.cfg.KnownHosts,
		func(stream, text string) {
			if out.Len() < 64*1024 {
				fmt.Fprintln(&out, text)
			}
		})
	resp := map[string]any{
		"ok":        runErr == nil && code == 0,
		"exit_code": code,
		"output":    out.String(),
	}
	if runErr != nil {
		resp["error"] = runErr.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}
