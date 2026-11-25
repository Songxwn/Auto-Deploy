package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Songxwn/Auto-Deploy/internal/api"
	"github.com/Songxwn/Auto-Deploy/internal/config"
	"github.com/Songxwn/Auto-Deploy/internal/engine"
	"github.com/Songxwn/Auto-Deploy/internal/secure"
	"github.com/Songxwn/Auto-Deploy/internal/store"
	"github.com/Songxwn/Auto-Deploy/internal/templates"
)

var version = "dev"

func main() {
	configPath := flag.String("config", "config.yaml", "配置文件路径（不存在时使用默认值与环境变量）")
	showVersion := flag.Bool("version", false, "显示版本号")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		os.Exit(1)
	}
	st, err := store.Open(filepath.Join(cfg.DataDir, "auto-deploy.db"))
	if err != nil {
		slog.Error("打开数据库失败", "err", err)
		os.Exit(1)
	}
	defer st.Close()
	if err := st.SeedTemplates(templates.BuiltIn()); err != nil {
		slog.Error("初始化内置模板失败", "err", err)
		os.Exit(1)
	}

	box := secure.NewBox(cfg.Secret)
	eng := engine.New(st, box, engine.SSHExecutor{
		DialTimeout: time.Duration(cfg.SSHTimeout) * time.Second,
		KnownHosts:  cfg.KnownHosts,
	}, cfg.Concurrency, time.Duration(cfg.RunTimeout)*time.Second)

	srv := api.New(cfg, st, box, eng, version)
	hs := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		slog.Info("Auto-Deploy 已启动", "version", version, "listen", cfg.Listen, "data_dir", cfg.DataDir)
		if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP 服务失败", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("正在关闭，取消进行中的执行…")
	eng.CancelAll()
	shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := hs.Shutdown(shCtx); err != nil {
		slog.Error("关闭 HTTP 服务失败", "err", err)
	}
}
