#!/bin/bash
# ==============================================================
#  Vless-Audit 一键部署脚本
#  集成 Xray-core + VLESS 协议 + 流量审计面板
#  参考 233boy/Xray, wulabing/Xray_onekey 风格
# ==============================================================
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
set -e

# ── 颜色 ──────────────────────────────────────────────────
RED='\033[0;31m';    GREEN='\033[0;32m';   YELLOW='\033[1;33m'
BLUE='\033[0;34m';   PURPLE='\033[0;35m';  CYAN='\033[0;36m'
WHITE='\033[1;37m';  NC='\033[0m'

red()    { echo -e "${RED}$1${NC}"; }
green()  { echo -e "${GREEN}$1${NC}"; }
yellow() { echo -e "${YELLOW}$1${NC}"; }
cyan()   { echo -e "${CYAN}$1${NC}"; }

# ── 变量 ──────────────────────────────────────────────────
INSTALL_DIR="/opt/vless-audit"
XRAY_DIR="$INSTALL_DIR/xray"
AUDIT_DIR="$INSTALL_DIR/audit"
CERT_DIR="$INSTALL_DIR/cert"
LOG_DIR="$INSTALL_DIR/log"

# 可配置项
VLESS_PORT=443
API_PORT=10086
AUDIT_PORT=8080
UUID=$(cat /proc/sys/kernel/random/uuid)
REGISTER_SECRET=""
FALLBACK_PORT=80

# ── 横幅 ──────────────────────────────────────────────────
banner() {
  clear
  green "╔══════════════════════════════════════════════╗"
  green "║                                              ║"
  green "║     Vless-Audit  一键部署脚本                 ║"
  green "║     Xray VLESS + 流量审计面板                 ║"
  green "║                                              ║"
  green "╚══════════════════════════════════════════════╝"
  echo
}

# ── 检测 root ─────────────────────────────────────────────
check_root() {
  if [[ $EUID -ne 0 ]]; then
    red "请使用 root 权限运行: sudo bash $0"
    exit 1
  fi
}

# ── 检测系统 ──────────────────────────────────────────────
check_os() {
  if [[ -f /etc/os-release ]]; then
    . /etc/os-release
    OS=$ID
    OS_VER=$VERSION_ID
  else
    red "无法检测操作系统"
    exit 1
  fi

  case $OS in
    debian|ubuntu|centos|rhel|fedora|rocky|almalinux) ;;
    *) yellow "未经测试的系统: $OS，继续安装可能存在风险" ;;
  esac

  ARCH=$(uname -m)
  case $ARCH in
    x86_64)  XRAY_ARCH="linux-64"   ;;
    aarch64) XRAY_ARCH="linux-arm64-v8a" ;;
    *) red "不支持的架构: $ARCH"; exit 1 ;;
  esac

  green "系统: $OS $OS_VER  $ARCH"
}

# ── 安装依赖 ──────────────────────────────────────────────
install_deps() {
  green "安装依赖..."
  if command -v apt &>/dev/null; then
    apt update -qq
    apt install -y -qq curl wget unzip tar jq socat cron 2>/dev/null
  elif command -v yum &>/dev/null; then
    yum install -y -q curl wget unzip tar jq socat cronie 2>/dev/null
  elif command -v dnf &>/dev/null; then
    dnf install -y -q curl wget unzip tar jq socat cronie 2>/dev/null
  fi
  green "依赖安装完成"
}

# ── 开启 BBR ──────────────────────────────────────────────
enable_bbr() {
  green "配置 BBR 拥塞控制..."
  local SYSCTL="/etc/sysctl.d/99-bbr.conf"
  cat > $SYSCTL <<EOF
net.core.default_qdisc = fq
net.ipv4.tcp_congestion_control = bbr
EOF
  sysctl -p $SYSCTL >/dev/null 2>&1
  green "BBR 已开启"
}

# ── 配置防火墙 ────────────────────────────────────────────
setup_firewall() {
  green "配置防火墙..."
  if command -v ufw &>/dev/null; then
    ufw allow $VLESS_PORT/tcp 2>/dev/null
    ufw allow $VLESS_PORT/udp 2>/dev/null
    ufw allow $AUDIT_PORT/tcp 2>/dev/null
    ufw --force enable 2>/dev/null
  elif command -v firewall-cmd &>/dev/null; then
    firewall-cmd --permanent --add-port=$VLESS_PORT/tcp 2>/dev/null
    firewall-cmd --permanent --add-port=$VLESS_PORT/udp 2>/dev/null
    firewall-cmd --permanent --add-port=$AUDIT_PORT/tcp 2>/dev/null
    firewall-cmd --reload 2>/dev/null
  fi
  # iptables 兜底
  iptables -I INPUT -p tcp --dport $VLESS_PORT -j ACCEPT 2>/dev/null
  iptables -I INPUT -p udp --dport $VLESS_PORT -j ACCEPT 2>/dev/null
  iptables -I INPUT -p tcp --dport $AUDIT_PORT -j ACCEPT 2>/dev/null
  green "防火墙配置完成"
}

# ── 获取公网 IP ───────────────────────────────────────────
get_ip() {
  IP=$(curl -s4 ip.sb 2>/dev/null || curl -s4 ifconfig.me 2>/dev/null || curl -s4 api.ipify.org 2>/dev/null)
  if [[ -z "$IP" ]]; then
    IP=$(hostname -I 2>/dev/null | awk '{print $1}')
  fi
  echo "$IP"
}

# ── 下载 Xray ─────────────────────────────────────────────
install_xray() {
  green "下载 Xray-core..."
  XRAY_VER=$(curl -s https://api.github.com/repos/XTLS/Xray-core/releases/latest | jq -r .tag_name)
  XRAY_URL="https://github.com/XTLS/Xray-core/releases/download/${XRAY_VER}/Xray-${XRAY_ARCH}.zip"

  mkdir -p $XRAY_DIR
  curl -L -s "$XRAY_URL" -o /tmp/xray.zip
  unzip -oq /tmp/xray.zip -d $XRAY_DIR
  chmod +x $XRAY_DIR/xray
  rm -f /tmp/xray.zip

  $XRAY_DIR/xray version 2>&1 | head -1
  green "Xray $XRAY_VER 安装完成"
}

# ── 编译/复制 vless-audit ─────────────────────────────────
install_audit() {
  green "安装 vless-audit..."
  mkdir -p $AUDIT_DIR

  # 优先使用本地编译好的二进制
  if [[ -f "./vless-audit" ]]; then
    cp ./vless-audit $AUDIT_DIR/
    chmod +x $AUDIT_DIR/vless-audit
    green "vless-audit 已复制"
  elif [[ -f "./vless-audit-${OS}-${ARCH}" ]]; then
    cp "./vless-audit-${OS}-${ARCH}" $AUDIT_DIR/vless-audit
    chmod +x $AUDIT_DIR/vless-audit
    green "vless-audit 已复制"
  else
    yellow "未找到预编译的 vless-audit 二进制"
    yellow "请先执行: cd vless-audit && ./build.sh"
    yellow "然后将 dist/ 下对应文件放到当前目录"
    read -p "跳过 vless-audit 继续? [y/N]: " skip
    if [[ "$skip" != "y" && "$skip" != "Y" ]]; then
      exit 1
    fi
  fi
}

# ── 设置注册口令 ──────────────────────────────────────────
set_register_secret() {
  echo
  cyan "══════ 用户自助注册设置 ══════"
  echo "  用户可以访问 http://你的IP:${AUDIT_PORT}/app/register.html 自助申请 VLESS 连接"
  echo "  设置一个申请口令，只有知道口令的人才能注册"
  echo
  read -p "  请输入申请口令 (留空则关闭自助注册): " REGISTER_SECRET
  if [[ -z "$REGISTER_SECRET" ]]; then
    yellow "  已关闭自助注册"
  else
    green "  注册口令已设置: $REGISTER_SECRET"
  fi
}

# ── 生成配置 ──────────────────────────────────────────────
gen_config() {
  green "生成配置文件..."
  mkdir -p $CERT_DIR $LOG_DIR

  # Xray 配置
  cat > $XRAY_DIR/config.json <<XEOF
{
  "log": {
    "loglevel": "warning",
    "access": "${LOG_DIR}/access.log",
    "error": "${LOG_DIR}/error.log"
  },
  "api": { "tag": "api", "services": ["StatsService", "HandlerService"] },
  "stats": {},
  "policy": {
    "levels": {
      "0": { "statsUserUplink": true, "statsUserDownlink": true }
    }
  },
  "inbounds": [
    {
      "tag": "api",
      "listen": "127.0.0.1",
      "port": ${API_PORT},
      "protocol": "dokodemo-door",
      "settings": { "address": "127.0.0.1" }
    },
    {
      "tag": "vless-in",
      "listen": "0.0.0.0",
      "port": ${VLESS_PORT},
      "protocol": "vless",
      "settings": {
        "clients": [{ "id": "${UUID}", "email": "admin@user", "level": 0 }],
        "decryption": "none"
      },
      "streamSettings": { "network": "tcp" },
      "sniffing": { "enabled": true, "destOverride": ["http", "tls"] }
    }
  ],
  "outbounds": [
    { "protocol": "freedom", "tag": "direct" }
  ],
  "routing": {
    "rules": [
      { "type": "field", "inboundTag": ["api"], "outboundTag": "api" }
    ]
  }
}
XEOF

  # vless-audit 配置
  cat > $AUDIT_DIR/config.json <<AEOF
{
  "listen": ":${AUDIT_PORT}",
  "db_path": "${AUDIT_DIR}/vless-audit.db",
  "xray_api": "127.0.0.1:${API_PORT}",
  "access_log": "${LOG_DIR}/access.log",
  "poll_interval_sec": 10,
  "retention_days": 365,
  "register_secret": "${REGISTER_SECRET}",
  "xray_config_path": "${XRAY_DIR}/config.json",
  "xray_bin_path": "${XRAY_DIR}/xray"
}
AEOF

  green "配置文件生成完成"
}

# ── 注册服务 ──────────────────────────────────────────────
install_service() {
  green "注册 systemd 服务..."

  cat > /etc/systemd/system/xray.service <<EOF
[Unit]
Description=Xray-core VLESS Service
Documentation=https://xtls.github.io
After=network.target nss-lookup.target

[Service]
Type=simple
User=root
ExecStart=${XRAY_DIR}/xray run -config ${XRAY_DIR}/config.json
Restart=on-failure
RestartSec=5s
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF

  cat > /etc/systemd/system/vless-audit.service <<EOF
[Unit]
Description=Vless-Audit Monitor
Documentation=https://github.com/your/vless-audit
After=network.target xray.service
Requires=xray.service

[Service]
Type=simple
User=root
ExecStart=${AUDIT_DIR}/vless-audit -config ${AUDIT_DIR}/config.json
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  systemctl enable xray vless-audit 2>/dev/null
  green "服务注册完成"
}

# ── 日志轮转 ──────────────────────────────────────────────
setup_logrotate() {
  cat > /etc/logrotate.d/vless-audit <<EOF
${LOG_DIR}/*.log {
    daily
    rotate 7
    missingok
    notifempty
    compress
    copytruncate
}
EOF
}

# ── 启动服务 ──────────────────────────────────────────────
start_services() {
  green "启动服务..."
  systemctl restart xray
  sleep 2
  systemctl restart vless-audit 2>/dev/null || true
  sleep 2

  if systemctl is-active --quiet xray; then
    green "Xray 运行中 ✓"
  else
    red "Xray 启动失败，请检查: journalctl -u xray -n 20"
  fi

  if systemctl is-active --quiet vless-audit 2>/dev/null; then
    green "vless-audit 运行中 ✓"
  else
    yellow "vless-audit 未运行（可能缺少二进制）"
  fi
}

# ── 添加用户 ──────────────────────────────────────────────
add_user() {
  read -p "输入用户标识 (email): " USER_EMAIL
  read -p "输入用户 UUID (留空自动生成): " USER_UUID
  [[ -z "$USER_UUID" ]] && USER_UUID=$(cat /proc/sys/kernel/random/uuid)
  read -p "输入用户等级 (默认0): " USER_LEVEL
  [[ -z "$USER_LEVEL" ]] && USER_LEVEL=0

  python3 -c "
import json
with open('$XRAY_DIR/config.json') as f: cfg = json.load(f)
for ib in cfg['inbounds']:
    if ib['tag'] == 'vless-in':
        ib['settings']['clients'].append({'id':'$USER_UUID','email':'$USER_EMAIL','level':int('$USER_LEVEL')})
with open('$XRAY_DIR/config.json','w') as f: json.dump(cfg,f,indent=2)
" 2>/dev/null || {
    yellow "Python3 不可用，请手动编辑 $XRAY_DIR/config.json"
    yellow "在 vless-in → settings → clients 中添加:"
    yellow "  { \"id\": \"$USER_UUID\", \"email\": \"$USER_EMAIL\", \"level\": $USER_LEVEL }"
  }

  systemctl restart xray
  green "用户 $USER_EMAIL 已添加"
  cyan "UUID: $USER_UUID"
}

# ── 删除用户 ──────────────────────────────────────────────
del_user() {
  read -p "输入要删除的用户标识 (email): " USER_EMAIL
  python3 -c "
import json
with open('$XRAY_DIR/config.json') as f: cfg = json.load(f)
for ib in cfg['inbounds']:
    if ib['tag'] == 'vless-in':
        ib['settings']['clients'] = [c for c in ib['settings']['clients'] if c.get('email') != '$USER_EMAIL']
with open('$XRAY_DIR/config.json','w') as f: json.dump(cfg,f,indent=2)
" 2>/dev/null

  systemctl restart xray
  green "用户 $USER_EMAIL 已删除"
}

# ── 列出用户 ──────────────────────────────────────────────
list_users() {
  cyan "当前 VLESS 用户:"
  python3 -c "
import json
with open('$XRAY_DIR/config.json') as f: cfg = json.load(f)
for ib in cfg['inbounds']:
    if ib['tag'] == 'vless-in':
        for c in ib['settings']['clients']:
            print(f\"  邮箱: {c.get('email','?')}  |  UUID: {c.get('id','?')[:16]}...  |  等级: {c.get('level',0)}\")
" 2>/dev/null || {
    yellow "无法解析配置，请查看 $XRAY_DIR/config.json"
  }
}

# ── 状态面板 ──────────────────────────────────────────────
show_status() {
  local IP=$(get_ip)
  clear
  green "╔══════════════════════════════════════════════╗"
  green "║          Vless-Audit  运行状态               ║"
  green "╚══════════════════════════════════════════════╝"
  echo
  cyan "  服务器 IP:  $IP"
  echo

  if systemctl is-active --quiet xray 2>/dev/null; then
    green "  Xray:       ● 运行中"
  else
    red "  Xray:       ○ 已停止"
  fi

  if systemctl is-active --quiet vless-audit 2>/dev/null; then
    green "  审计系统:   ● 运行中"
  else
    yellow "  审计系统:   ○ 未启动"
  fi

  echo
  cyan "═══ 连接信息 ═══"
  echo
  echo "  协议:       VLESS + TCP"
  echo "  地址:       $IP"
  echo "  端口:       $VLESS_PORT"
  echo "  UUID:       $UUID"
  echo "  加密:       none"
  echo
  echo "  VLESS 链接:"
  yellow "  vless://$UUID@$IP:$VLESS_PORT?encryption=none&type=tcp&security=none#Vless-Server"
  echo
  cyan "═══ 审计面板 ═══"
  echo
  green "  http://$IP:$AUDIT_PORT/app/"
  echo
  cyan "═══ 用户自助申请 ═══"
  echo
  green "  http://$IP:$AUDIT_PORT/app/register.html"
  if [[ -n "$REGISTER_SECRET" ]]; then
    echo "  申请口令: $REGISTER_SECRET"
  else
    yellow "  (自助注册已关闭，请在 config.json 中设置 register_secret)"
  fi
  echo
  cyan "═══ 管理命令 ═══"
  echo
  echo "  systemctl status xray        查看 Xray 状态"
  echo "  systemctl restart xray       重启 Xray"
  echo "  journalctl -u xray -f        查看 Xray 日志"
  echo "  journalctl -u vless-audit -f 查看审计日志"
  echo
}

# ── 卸载 ──────────────────────────────────────────────────
uninstall() {
  red "警告: 即将完全卸载 Xray 和 vless-audit！"
  read -p "确认卸载? 输入 YES 继续: " confirm
  [[ "$confirm" != "YES" ]] && { yellow "已取消"; return; }

  systemctl stop xray vless-audit 2>/dev/null
  systemctl disable xray vless-audit 2>/dev/null
  rm -f /etc/systemd/system/xray.service
  rm -f /etc/systemd/system/vless-audit.service
  rm -f /etc/logrotate.d/vless-audit
  systemctl daemon-reload
  rm -rf $INSTALL_DIR
  green "卸载完成"
}

# ── 主菜单 ────────────────────────────────────────────────
menu() {
  while true; do
    banner
    cyan  "  1. 安装 Xray + vless-audit"
    cyan  "  2. 添加 VLESS 用户"
    cyan  "  3. 删除 VLESS 用户"
    cyan  "  4. 查看用户列表"
    cyan  "  5. 查看运行状态"
    cyan  "  6. 重启服务"
    cyan  "  7. 查看日志"
    cyan  "  8. 卸载"
    cyan  "  0. 退出"
    echo
    read -p "  请选择 [0-8]: " choice

    case $choice in
      1)
        check_root
        check_os
        install_deps
        enable_bbr
        install_xray
        install_audit
        set_register_secret
        gen_config
        setup_firewall
        install_service
        setup_logrotate
        start_services
        show_status
        ;;
      2) add_user ;;
      3) del_user ;;
      4) list_users ;;
      5) show_status ;;
      6)
        systemctl restart xray
        systemctl restart vless-audit 2>/dev/null
        green "服务已重启"
        ;;
      7)
        cyan "Xray 日志:"
        journalctl -u xray -n 10 --no-pager
        echo
        cyan "vless-audit 日志:"
        journalctl -u vless-audit -n 10 --no-pager 2>/dev/null || yellow "审计服务未运行"
        echo
        read -p "按回车返回..."
        ;;
      8) uninstall ;;
      0) green "再见!"; exit 0 ;;
      *) red "无效选择" ;;
    esac
    echo
    read -p "按回车返回菜单..."
  done
}

# ── 入口 ──────────────────────────────────────────────────
if [[ $# -eq 0 ]]; then
  menu
else
  case $1 in
    install) check_root; check_os; install_deps; enable_bbr; install_xray; install_audit; set_register_secret; gen_config; setup_firewall; install_service; setup_logrotate; start_services; show_status ;;
    add)     add_user ;;
    del)     del_user ;;
    list)    list_users ;;
    status)  show_status ;;
    restart) systemctl restart xray; systemctl restart vless-audit 2>/dev/null; green "已重启" ;;
    uninstall) uninstall ;;
    *)       menu ;;
  esac
fi
