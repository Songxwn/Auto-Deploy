// Package templates provides the built-in reusable deployment template library.
package templates

import (
	"time"

	"github.com/Songxwn/Auto-Deploy/internal/models"
)

// BuiltIn returns the curated built-in template library. IDs are stable slugs so
// seeding is idempotent and built-ins are distinguishable from user templates.
func BuiltIn() []models.Template {
	now := time.Now()
	mk := func(id, name, desc, os, cat, script string) models.Template {
		return models.Template{
			ID: id, Name: name, Description: desc, OSType: os, Category: cat,
			Script: script, BuiltIn: true, CreatedAt: now, UpdatedAt: now,
		}
	}
	return []models.Template{
		// ------------------------------------------------ Linux
		mk("nginx-linux", "Nginx 安装与启动", "Debian/Ubuntu 安装 Nginx 并设置开机自启", models.OSLinux, models.CatWeb,
			`# 安装并启动 Nginx（Debian/Ubuntu）
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y nginx
systemctl enable --now nginx
nginx -v
`),
		mk("docker-linux", "Docker 安装", "使用官方脚本安装 Docker 并启动服务", models.OSLinux, models.CatRuntime,
			`# 使用官方脚本安装 Docker（适用于主流发行版）
curl -fsSL https://get.docker.com -o /tmp/get-docker.sh
sh /tmp/get-docker.sh
systemctl enable --now docker
docker --version
`),
		mk("mysql-linux", "MySQL 安装", "Debian/Ubuntu 安装 MySQL Server", models.OSLinux, models.CatDatabase,
			`export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y mysql-server
systemctl enable --now mysql
mysql --version
`),
		mk("redis-linux", "Redis 安装", "Debian/Ubuntu 安装 Redis 并启动", models.OSLinux, models.CatDatabase,
			`export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y redis-server
systemctl enable --now redis-server
redis-server --version
`),
		mk("nodejs-linux", "Node.js 20 安装", "通过 NodeSource 安装 Node.js 20.x", models.OSLinux, models.CatRuntime,
			`# 通过 NodeSource 安装 Node.js 20.x（Debian/Ubuntu）
curl -fsSL https://deb.nodesource.com/setup_20.x | bash -
apt-get install -y nodejs
node --version
npm --version
`),
		mk("python-linux", "Python3 与 pip", "安装 Python3、pip 与 venv", models.OSLinux, models.CatRuntime,
			`export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y python3 python3-pip python3-venv
python3 --version
pip3 --version
`),
		mk("ufw-firewall-linux", "UFW 防火墙基础配置", "放行 SSH/HTTP/HTTPS 并启用 UFW", models.OSLinux, models.CatNetwork,
			`# 放行 SSH 与 HTTP/HTTPS，启用 UFW
apt-get update && apt-get install -y ufw
ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw --force enable
ufw status verbose
`),
		mk("deploy-user-linux", "创建部署用户", "创建 deploy 用户并加入 sudo 组", models.OSLinux, models.CatSystem,
			`# 创建 deploy 用户并加入 sudo（按需修改 USERNAME）
USERNAME=deploy
id "$USERNAME" >/dev/null 2>&1 || useradd -m -s /bin/bash "$USERNAME"
usermod -aG sudo "$USERNAME"
echo "用户 $USERNAME 已就绪"
`),
		mk("sysupdate-linux", "系统更新", "更新软件包索引并升级系统", models.OSLinux, models.CatSystem,
			`export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get upgrade -y
apt-get autoremove -y
`),
		mk("timezone-linux", "时区与时间同步", "设置 Asia/Shanghai 时区并启用 chrony", models.OSLinux, models.CatSystem,
			`timedatectl set-timezone Asia/Shanghai
apt-get update && apt-get install -y chrony
systemctl enable --now chrony
timedatectl
`),

		// ------------------------------------------------ Windows
		mk("iis-windows", "IIS 安装", "安装 IIS 与管理工具并放行 80 端口", models.OSWindows, models.CatWeb,
			`# 安装 IIS 及管理工具
Install-WindowsFeature -Name Web-Server -IncludeManagementTools
New-NetFirewallRule -DisplayName 'HTTP' -Direction Inbound -Protocol TCP -LocalPort 80 -Action Allow
Get-Service W3SVC | Select-Object Status, Name
`),
		mk("static-site-windows", "IIS 静态站点", "创建站点目录、示例页面并启动站点", models.OSWindows, models.CatWeb,
			`# 创建 IIS 站点与示例页面（按需修改 $site / $path / $port）
$site = 'app'
$path = 'C:\inetpub\app'
$port = 80
New-Item -ItemType Directory -Force -Path $path | Out-Null
Set-Content -Path "$path\index.html" -Value '<h1>Auto-Deploy OK</h1>' -Encoding UTF8
Import-Module WebAdministration
if (-not (Get-Website -Name $site -ErrorAction SilentlyContinue)) {
  New-Website -Name $site -Port $port -PhysicalPath $path -Force
}
Start-Website -Name $site
`),
		mk("openssh-windows", "OpenSSH Server 安装", "安装并启动 OpenSSH Server，放行 22 端口", models.OSWindows, models.CatSystem,
			`Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0
Set-Service -Name sshd -StartupType Automatic
Start-Service sshd
New-NetFirewallRule -Name sshd -DisplayName 'OpenSSH Server' -Enabled True -Direction Inbound -Protocol TCP -Action Allow -LocalPort 22
`),
		mk("choco-windows", "Chocolatey 安装", "安装 Chocolatey 包管理器", models.OSWindows, models.CatRuntime,
			`Set-ExecutionPolicy Bypass -Scope Process -Force
[System.Net.ServicePointManager]::SecurityProtocol = [System.Net.SecurityProtocolType]::Tls12
Invoke-Expression ((New-Object System.Net.WebClient).DownloadString('https://community.chocolatey.org/install.ps1'))
choco --version
`),
		mk("firewall-port-windows", "放行指定端口", "新增入站 TCP 防火墙规则", models.OSWindows, models.CatNetwork,
			`# 修改 $port 与 $name 后使用
$port = 8080
$name = 'App-HTTP'
New-NetFirewallRule -DisplayName $name -Direction Inbound -Protocol TCP -LocalPort $port -Action Allow
Get-NetFirewallRule -DisplayName $name | Select-Object DisplayName, Enabled
`),
		mk("deploy-user-windows", "创建本地用户", "创建本地部署用户并加入管理员组", models.OSWindows, models.CatSystem,
			`# 创建本地部署用户（务必修改用户名与密码）
$username = 'deploy'
$password = ConvertTo-SecureString 'ChangeMe-123!' -AsPlainText -Force
if (-not (Get-LocalUser -Name $username -ErrorAction SilentlyContinue)) {
  New-LocalUser -Name $username -Password $password -PasswordNeverExpires
}
Add-LocalGroupMember -Group 'Administrators' -Member $username
`),
	}
}
