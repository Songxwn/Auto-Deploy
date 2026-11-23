# 🚀 Auto-Deploy

基于 **Go + SSH** 的软件全自动化部署配置平台。通过 Web 控制台管理 Linux / Windows 主机，
编排多步骤 bash / PowerShell 脚本，一键并行执行「开局」部署，并实时查看每台主机的执行日志。

- **单二进制交付**：前端内嵌、SQLite 存储（纯 Go 驱动，无 CGO），下载即用
- **双平台目标**：Linux 走 `bash -s`，Windows 走 OpenSSH + PowerShell（`-EncodedCommand`，无引号转义问题）
- **多步骤任务**：按顺序执行步骤，任一步骤失败立即中止该主机；多主机并行、可取消
- **实时日志**：SSE 推送 stdout / stderr / 系统事件，支持日志回放与导出
- **凭据安全**：密码 / 私钥使用 AES-256-GCM 加密落库，API 永不回显
- **CI/CD**：GitHub Actions 自动构建 6 平台二进制并发布到 Releases，多架构镜像推送到 GHCR

## 快速开始

### 方式一：二进制

从 [Releases](../../releases) 下载对应平台的压缩包，解压后运行：

```bash
# Linux / macOS
AUTO_DEPLOY_PASSWORD='你的密码' ./autodeploy

# Windows (PowerShell)
$env:AUTO_DEPLOY_PASSWORD='你的密码'; .\autodeploy.exe
```

浏览器打开 <http://localhost:8080>，默认用户名 `admin`。
**未设置密码时使用默认密码 `admin123`，请尽快修改。**

### 方式二：Docker

```bash
docker run -d --name auto-deploy \
  -p 8080:8080 \
  -v auto-deploy-data:/data \
  -e AUTO_DEPLOY_PASSWORD='你的密码' \
  ghcr.io/songxwn/auto-deploy:latest
```

或使用 docker-compose：

```bash
AUTO_DEPLOY_PASSWORD='你的密码' docker compose up -d
```

### 方式三：源码构建

```bash
make build        # 产物 bin/autodeploy
make run          # 直接运行
make test         # 单元测试
```

需要 Go 1.27+。

## 目标主机要求

### Linux

- 开启 SSH 服务，安装 `bash`
- 部署账号具备相应权限（root 或免密 sudo）

### Windows

安装并启动 OpenSSH Server（Windows 10/11、Server 2019+ 自带可选功能）：

```powershell
Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0
Set-Service -Name sshd -StartupType Automatic
Start-Service sshd
New-NetFirewallRule -Name sshd -DisplayName 'OpenSSH Server (sshd)' `
  -Enabled True -Direction Inbound -Protocol TCP -Action Allow -LocalPort 22
```

Auto-Deploy 通过 `powershell -EncodedCommand` 执行脚本，**无需**修改 SSH 默认 Shell。
脚本自动附加 `$ErrorActionPreference = 'Stop'` 与原生程序退出码传播，保证失败可感知。

> 注意：若目标机 SSH 默认 Shell 为 `cmd.exe`，受命令行长度限制，单个步骤脚本建议小于 2KB；
> 默认 Shell 为 PowerShell 时无此限制。可将默认 Shell 设为 PowerShell：
> `New-ItemProperty -Path "HKLM:\SOFTWARE\OpenSSH" -Name DefaultShell -Value "C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe" -PropertyType String -Force`

## 使用流程

1. **主机管理**：添加目标主机（地址 / 端口 / 用户 / 密码或私钥 / 系统类型），点击「测试」验证连通性
2. **部署任务**：创建任务，选择目标系统，编排多个脚本步骤（支持排序）
3. **执行**：点击「▶ 执行」，勾选目标主机（自动按系统过滤），多主机并行执行
4. **执行记录**：实时查看每台主机的日志与状态，可取消执行、下载完整日志

### 任务示例（Linux · Nginx 开局）

| 步骤 | 脚本 |
|---|---|
| 安装 | `apt-get update && apt-get install -y nginx` |
| 配置 | `cat > /etc/nginx/conf.d/app.conf <<'EOF'`<br>`server { listen 80; root /var/www/app; }`<br>`EOF` |
| 启动 | `systemctl enable --now nginx && systemctl reload nginx` |

### 任务示例（Windows · IIS 开局）

| 步骤 | 脚本 |
|---|---|
| 安装 | `Install-WindowsFeature -Name Web-Server -IncludeManagementTools` |
| 站点 | `New-Website -Name app -Port 80 -PhysicalPath C:\inetpub\app -Force` |
| 防火墙 | `New-NetFirewallRule -DisplayName 'HTTP' -Direction Inbound -Protocol TCP -LocalPort 80 -Action Allow` |

## 配置

配置优先级：**环境变量 > 配置文件（`-config` 指定，默认 `config.yaml`）> 默认值**。
参考 [`config.example.yaml`](config.example.yaml)。

| 配置项 | 环境变量 | 默认值 | 说明 |
|---|---|---|---|
| `listen` | `AUTO_DEPLOY_LISTEN` | `:8080` | HTTP 监听地址 |
| `data_dir` | `AUTO_DEPLOY_DATA_DIR` | `./data` | 数据库与密钥目录 |
| `username` | `AUTO_DEPLOY_USERNAME` | `admin` | 登录用户名 |
| `password` | `AUTO_DEPLOY_PASSWORD` | `admin123` | 登录密码（务必修改） |
| `secret` | `AUTO_DEPLOY_SECRET` | 自动生成 | 凭据加密密钥 |
| `known_hosts` | `AUTO_DEPLOY_KNOWN_HOSTS` | 空 | SSH 主机密钥校验文件 |
| `concurrency` | `AUTO_DEPLOY_CONCURRENCY` | `8` | 并行主机数上限 |
| `ssh_timeout` | `AUTO_DEPLOY_SSH_TIMEOUT` | `15` | SSH 连接超时（秒） |
| `run_timeout` | `AUTO_DEPLOY_RUN_TIMEOUT` | `3600` | 单次执行超时（秒），0 不限 |

## API

除 `/healthz`、`/api/login` 外均需登录（Cookie 或 `Authorization: Bearer <token>`）。

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/login` | 登录，返回会话 Cookie |
| GET | `/api/hosts` / POST | 主机列表 / 新增 |
| GET/PUT/DELETE | `/api/hosts/{id}` | 主机详情 / 更新 / 删除 |
| POST | `/api/hosts/{id}/test` | 连接测试 |
| GET | `/api/tasks` / POST | 任务列表 / 新增 |
| GET/PUT/DELETE | `/api/tasks/{id}` | 任务详情 / 更新 / 删除 |
| POST | `/api/tasks/{id}/run` | 执行任务，body：`{"host_ids":[...]}` |
| GET | `/api/runs` | 执行记录 |
| GET | `/api/runs/{id}` | 执行详情 |
| POST | `/api/runs/{id}/cancel` | 取消执行 |
| GET | `/api/runs/{id}/logs` | SSE 实时日志（`?format=text` 导出文本） |

## 安全说明

- 主机密码 / 私钥以 AES-256-GCM 加密存储，密钥默认自动生成于 `data/secret.key`（权限 0600）
- 会话令牌为 HMAC 签名，有效期 7 天；登录比较使用常量时间算法
- 默认跳过 SSH 主机密钥校验（便于内网批量开局）；公网环境请配置 `known_hosts`
- 建议将本服务部署在内网，或通过反向代理启用 HTTPS

## CI/CD

| 工作流 | 触发 | 产物 |
|---|---|---|
| `ci.yml` | push / PR | gofmt 检查、vet、`go test -race`、构建 |
| `release.yml` | tag `v*` | 6 平台二进制（linux/darwin/windows × amd64/arm64）+ SHA256 校验 → GitHub Releases |
| `docker.yml` | main / tag `v*` | `linux/amd64` + `linux/arm64` 镜像 → `ghcr.io/<owner>/auto-deploy` |

发布新版本：

```bash
git tag v1.0.0
git push origin v1.0.0
```

## 项目结构

```
cmd/autodeploy/        入口
internal/config/       配置加载（YAML + 环境变量）
internal/secure/       AES-GCM 凭据加密
internal/store/        SQLite 存储
internal/sshx/         SSH 执行器（bash / PowerShell）
internal/engine/       执行调度、日志广播
internal/api/          REST API + SSE + 鉴权
web/                   内嵌前端（零依赖 SPA）
```

## License

[MIT](LICENSE)
