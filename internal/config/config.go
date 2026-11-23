// Package config loads application configuration from file, environment and defaults.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Config holds all runtime settings.
type Config struct {
	Listen      string `yaml:"listen"`      // HTTP listen address
	DataDir     string `yaml:"data_dir"`    // database / secret key directory
	Username    string `yaml:"username"`    // web login user
	Password    string `yaml:"password"`    // web login password
	Secret      string `yaml:"secret"`      // encryption secret; auto-generated when empty
	KnownHosts  string `yaml:"known_hosts"` // optional known_hosts file for SSH host key verification
	Concurrency int    `yaml:"concurrency"` // max hosts executed in parallel per run
	SSHTimeout  int    `yaml:"ssh_timeout"` // SSH dial timeout in seconds
	RunTimeout  int    `yaml:"run_timeout"` // per-run timeout in seconds, 0 = unlimited
}

// Default returns the built-in defaults.
func Default() Config {
	return Config{
		Listen:      ":8080",
		DataDir:     "./data",
		Username:    "admin",
		Concurrency: 8,
		SSHTimeout:  15,
		RunTimeout:  3600,
	}
}

// Load reads the optional YAML file, applies environment overrides and
// prepares the data directory (including an auto-generated encryption key).
func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		data, err := os.ReadFile(path)
		switch {
		case err == nil:
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				return cfg, fmt.Errorf("解析配置文件 %s 失败: %w", path, err)
			}
		case !os.IsNotExist(err):
			return cfg, err
		}
	}

	envStr("AUTO_DEPLOY_LISTEN", &cfg.Listen)
	envStr("AUTO_DEPLOY_DATA_DIR", &cfg.DataDir)
	envStr("AUTO_DEPLOY_USERNAME", &cfg.Username)
	envStr("AUTO_DEPLOY_PASSWORD", &cfg.Password)
	envStr("AUTO_DEPLOY_SECRET", &cfg.Secret)
	envStr("AUTO_DEPLOY_KNOWN_HOSTS", &cfg.KnownHosts)
	envInt("AUTO_DEPLOY_CONCURRENCY", &cfg.Concurrency)
	envInt("AUTO_DEPLOY_SSH_TIMEOUT", &cfg.SSHTimeout)
	envInt("AUTO_DEPLOY_RUN_TIMEOUT", &cfg.RunTimeout)

	if cfg.Concurrency < 1 {
		cfg.Concurrency = 1
	}
	if cfg.SSHTimeout < 1 {
		cfg.SSHTimeout = 15
	}
	if cfg.Username == "" {
		cfg.Username = "admin"
	}
	if cfg.Password == "" {
		cfg.Password = "admin123"
		slog.Warn("未设置登录密码，使用默认密码 admin123，请尽快通过配置文件或 AUTO_DEPLOY_PASSWORD 修改")
	}

	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return cfg, fmt.Errorf("创建数据目录失败: %w", err)
	}
	if cfg.Secret == "" {
		key, err := loadOrCreateKey(filepath.Join(cfg.DataDir, "secret.key"))
		if err != nil {
			return cfg, err
		}
		cfg.Secret = key
	}
	return cfg, nil
}

func loadOrCreateKey(path string) (string, error) {
	if data, err := os.ReadFile(path); err == nil && len(data) >= 32 {
		return string(data), nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	key := hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(key), 0o600); err != nil {
		return "", fmt.Errorf("写入密钥文件失败: %w", err)
	}
	slog.Info("已生成数据加密密钥", "path", path)
	return key, nil
}

func envStr(name string, dst *string) {
	if v := os.Getenv(name); v != "" {
		*dst = v
	}
}

func envInt(name string, dst *int) {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			*dst = n
		}
	}
}
