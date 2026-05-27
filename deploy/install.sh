#!/bin/bash
# ==============================================================
#  Vless-Audit 一键部署脚本
#  Xray-core + VLESS 多协议 + 流量审计面板
#  github: your/vless-audit
# ==============================================================
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

# ── 颜色 ──
red='\e[31m'; green='\e[92m'; yellow='\e[33m'; cyan='\e[96m'; magenta='\e[95m'; none='\e[0m'
_red()   { echo -e "${red}$@${none}"; }
_green() { echo -e "${green}$@${none}"; }
_cyan()  { echo -e "${cyan}$@${none}"; }
_yellow(){ echo -e "${yellow}$@${none}"; }
_is_err="$(_red '✘')"
_is_ok="$(_green '✔')"

err() { echo -e "\n${_is_err} $@\n"; exit 1; }
ok()  { echo -e "${_is_ok} $@"; }

# ── 权限检查 ──
[[ $EUID != 0 ]] && err "请使用 root 运行: ${yellow}sudo bash $0${none}"

# ── 检测系统 ──
cmd=$(type -P apt || type -P yum || type -P dnf)
[[ ! $cmd ]] && err "仅支持 Debian/Ubuntu/CentOS/Rocky"

# ── 检测架构 ──
case $(uname -m) in
  x86_64|amd64)    CORE_ARCH="linux-64"   ; BIN_ARCH="vless-audit-amd64"   ;;
  aarch64|arm64)   CORE_ARCH="linux-arm64-v8a"; BIN_ARCH="vless-audit-arm64" ;;
  *) err "仅支持 64 位系统" ;;
esac

# ── 参数 ──
while [[ $# -gt 0 ]]; do
  case $1 in
    -p|--proxy)   PROXY=$2; shift 2 ;;
    -v|--version) CORE_VER=v${2#v}; shift 2 ;;
    -l|--local)   LOCAL_INSTALL=1; shift ;;
    -h|--help)    _cyan "用法: $0 [-p proxy] [-v version] [-l]"; exit ;;
    *) shift ;;
  esac
done

# ── 变量 ──
VLESS_PORT=443; API_PORT=10086; AUDIT_PORT=8080
UUID=$(cat /proc/sys/kernel/random/uuid 2>/dev/null || head -c16 /dev/urandom | xxd -p)
INSTALL_DIR="/opt/vless-audit"
XRAY_DIR="$INSTALL_DIR/xray"; AUDIT_DIR="$INSTALL_DIR/audit"
LOG_DIR="$INSTALL_DIR/log"; CERT_DIR="$INSTALL_DIR/cert"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROTO="tcp"; TLS_ENABLED="false"; WS_PATH="/ws"
USE_NGINX="false"; NGINX_LISTEN_PORT=443; XRAY_LOCAL_PORT=10001

# 临时目录
tmpdir=$(mktemp -d /tmp/vless-audit.XXXXXX)
trap "rm -rf $tmpdir" EXIT
tmpcore="$tmpdir/xray.zip"; tmpxray="$tmpdir/xray"

_wget() {
  [[ $PROXY ]] && export https_proxy=$PROXY
  wget --no-check-certificate -q --show-progress --timeout=30 -O "$1" "$2"
}

# ── 自修复 ──
auto_fix() {
  # Fix broken systemd files from old versions (semicolons instead of newlines).
  for svc in xray vless-audit; do
    local f="/etc/systemd/system/${svc}.service"
    if [[ -f "$f" ]] && grep -q ';' "$f" 2>/dev/null; then
      _yellow "检测到旧版损坏的 ${svc}.service，自动修复..."
      rm -f "$f"
    fi
  done
  # Ensure systemd is clean.
  [[ -f /etc/systemd/system/xray.service ]] || systemctl stop xray 2>/dev/null || true
  [[ -f /etc/systemd/system/vless-audit.service ]] || systemctl stop vless-audit 2>/dev/null || true
  systemctl daemon-reload 2>/dev/null || true
}

# ── 安装依赖 ──
_cyan "安装依赖..."
# Ensure unzip is available (some minimal images miss it).
type -P unzip &>/dev/null || { apt update -qq && apt install -y -qq unzip 2>/dev/null; } || { yum install -y -q unzip 2>/dev/null; } || true
install_pkg() {
  local miss=""
  for p in curl wget unzip jq openssl socat; do
    type -P $p &>/dev/null || miss="$miss $p"
  done
  [[ -z $miss ]] && return
  if [[ $cmd =~ apt ]]; then apt update -qq && apt install -y -qq $miss
  elif [[ $cmd =~ yum ]]; then yum install -y -q $miss
  elif [[ $cmd =~ dnf ]]; then dnf install -y -q $miss
  fi
}
install_pkg && ok "依赖就绪"

# ── 自修复 (必须在 BBR 之前, 避免 systemctl 调用旧的损坏服务) ──
auto_fix

# BBR: skip by default, uncomment below if you want it.
# cat > /etc/sysctl.d/99-bbr.conf <<'EOF'
# net.core.default_qdisc = fq
# net.ipv4.tcp_congestion_control = bbr
# EOF
# sysctl -p /etc/sysctl.d/99-bbr.conf >/dev/null 2>&1

# ── ACME 证书 ──
acme_get_cert() {
  _cyan "申请 Let's Encrypt 证书 for ${ACME_DOMAIN}..."
  mkdir -p "$CERT_DIR"

  # 安装 acme.sh
  if [[ ! -f ~/.acme.sh/acme.sh ]]; then
    curl -s https://get.acme.sh | sh -s email=admin@${ACME_DOMAIN} >/dev/null 2>&1
  fi

  # 确保 80 端口可访问 (先临时停用占用)
  systemctl stop xray 2>/dev/null || true
  sleep 1

  # 使用 standalone 模式签发
  ~/.acme.sh/acme.sh --issue -d "$ACME_DOMAIN" --standalone --force 2>&1 | tail -1

  if [[ $? -eq 0 ]]; then
    ~/.acme.sh/acme.sh --install-cert -d "$ACME_DOMAIN" \
      --key-file       "$CERT_DIR/privkey.pem" \
      --fullchain-file "$CERT_DIR/fullchain.pem" 2>&1
    TLS_CERT="$CERT_DIR/fullchain.pem"; TLS_KEY="$CERT_DIR/privkey.pem"
    chmod 600 "$TLS_KEY"
    ok "证书签发成功 (有效期 90 天, acme.sh 自动续期)"
  else
    err "证书签发失败，请检查域名是否已解析到本机"
  fi
}

# ── 协议选择 ──
echo
_cyan "══════ 选择协议组合 ══════"
echo "  1) VLESS + TCP                      直连, 无加密"
echo "  2) VLESS + TCP + TLS                直连, 自签证书 (测试用)"
echo "  3) VLESS + WebSocket                ★ CDN 推荐, TLS 由 CDN 处理"
echo "  4) VLESS + WebSocket + TLS          自签证书 (仅测试/内网)"
echo "  5) VLESS + WebSocket + ACME         ★ 生产推荐, 自动申请可信证书"
echo "  6) VLESS + gRPC                     无加密"
echo "  7) VLESS + gRPC + TLS               自签证书"
echo "  8) 已有域名 + 已有证书               自定义证书路径"
echo "  9) Cloudflare Origin 证书            粘贴 Cloudflare 源站证书"
echo
read -p "  请选择 [1-9, 默认: 3]: " CHOICE; CHOICE=${CHOICE:-3}

case $CHOICE in
  1) PROTO="tcp"   ; TLS_ENABLED="false" ;;
  2) PROTO="tcp"   ; TLS_ENABLED="true"  ;;
  3) PROTO="ws"    ; TLS_ENABLED="false"
     _green "  CDN 模式: 服务端不启用 TLS，由 Cloudflare/CDN 处理加密"
     ;;
  4) PROTO="ws"    ; TLS_ENABLED="true"
     _yellow "  ⚠ 自签证书仅适合测试/内网，公网请选 5 或使用 CDN 的 3"
     ;;
  5) PROTO="ws"    ; TLS_ENABLED="true"
     read -p "  输入你的域名 (需已解析到本机): " ACME_DOMAIN
     [[ -z $ACME_DOMAIN ]] && err "域名不能为空"
     acme_get_cert
     ;;
  6) PROTO="grpc"  ; TLS_ENABLED="false" ;;
  7) PROTO="grpc"  ; TLS_ENABLED="true"  ;;
  8) PROTO="tcp"   ; TLS_ENABLED="true"
     read -p "  证书文件 (fullchain.pem): " TLS_CERT
     read -p "  私钥文件 (privkey.pem):   " TLS_KEY
     [[ ! -f "$TLS_CERT" ]] && err "证书不存在: $TLS_CERT"
     [[ ! -f "$TLS_KEY"  ]] && err "私钥不存在: $TLS_KEY"
     ;;
  9) PROTO="ws"    ; TLS_ENABLED="true"
     read -p "  粘贴 Cloudflare Origin 证书 (PEM, 含 BEGIN/END): " CF_CERT_PEM
     read -p "  粘贴 Cloudflare 私钥 (PEM, 含 BEGIN/END):        " CF_KEY_PEM
     mkdir -p "$CERT_DIR"
     echo "$CF_CERT_PEM" > "$CERT_DIR/fullchain.pem"
     echo "$CF_KEY_PEM"  > "$CERT_DIR/privkey.pem"
     TLS_CERT="$CERT_DIR/fullchain.pem"; TLS_KEY="$CERT_DIR/privkey.pem"
     chmod 600 "$TLS_KEY"
     ok "Cloudflare Origin 证书已保存"
     ;;
  *) err "无效选择" ;;
esac

# 自签证书 (选项 2/4/7)
if [[ $TLS_ENABLED == "true" && -z $TLS_CERT && -z $ACME_DOMAIN ]]; then
  _cyan "生成自签 TLS 证书..."
  mkdir -p "$CERT_DIR"
  openssl req -x509 -days 3650 -nodes -newkey rsa:2048 \
    -keyout "$CERT_DIR/privkey.pem" -out "$CERT_DIR/fullchain.pem" \
    -subj "/CN=self-signed/O=vless/C=US" 2>/dev/null
  TLS_CERT="$CERT_DIR/fullchain.pem"; TLS_KEY="$CERT_DIR/privkey.pem"
  chmod 600 "$TLS_KEY"; ok "自签证书已生成 (仅测试用)"
fi

# CDN 真实 IP 透传
if [[ $PROTO == "ws" || $PROTO == "grpc" ]]; then
  echo
  _cyan "══════ CDN 真实 IP 透传 ══════"
  echo "  当前协议: ${PROTO} — 若套了 Cloudflare 免费 CDN:"
  echo
  echo "  VLESS 不走 HTTP, Cloudflare 免费套餐没有 PROXY Protocol,"
  echo "  CF-Connecting-IP / True-Client-IP 全塞在 HTTP 头里, VLESS 看不见。"
  echo
  echo "  三条路拿到真实 IP:"
  echo
  _green "  ① 推荐: Nginx 反代 ★"
  echo "     CDN → Nginx(读 CF-Connecting-IP 头, 设 X-Real-IP) → Xray"
  echo "     脚本自动装 Nginx + 生成配置, 一站搞定, 免费"
  echo
  _cyan "  ② PROXY Protocol (需付费)"
  echo "     Cloudflare Spectrum ($5/月+) 或 Enterprise 才支持"
  echo
  _yellow "  ③ 不管 IP, 只看 email"
  echo "     来源 IP 显示 '(CDN节点)', 用户识别和流量统计不变"
  echo
  echo "  ────────────────────────────────"
  echo "  1) Nginx 反代, 拿到真实 IP ★"
  echo "  2) 不管 IP, email 识别即可"
  echo
  read -p "  请选择 [1-2, 默认: 1]: " CDN_CHOICE; CDN_CHOICE=${CDN_CHOICE:-1}
  if [[ $CDN_CHOICE == "1" ]]; then
    USE_NGINX="true"
    NGINX_LISTEN_PORT=443
    XRAY_LOCAL_PORT=10001
    _green "  将安装 Nginx 反代: CDN → Nginx :${NGINX_LISTEN_PORT} → Xray :${XRAY_LOCAL_PORT}"
  else
    _yellow "  源 IP 将显示 CDN 节点 (GeoIP 自动标注 'CDN节点')"
  fi
fi

ok "协议: VLESS + ${PROTO}$([[ $TLS_ENABLED == true ]] && echo ' + TLS')$([[ -n $ACME_DOMAIN ]] && echo ' (ACME)' || [[ -n $CF_CERT_PEM ]] && echo ' (Cloudflare)')$([[ $USE_NGINX == true ]] && echo ' + Nginx 反代')"

# ── 注册口令 ──
echo
_cyan "══════ 用户自助注册 ══════"
echo "  用户可通过网页自助申请 VLESS 连接"
read -p "  申请口令 (留空关闭): " REGISTER_SECRET
[[ -z $REGISTER_SECRET ]] && _yellow "  已关闭自助注册" || _green "  口令: $REGISTER_SECRET"

# ── 创建目录 ──
mkdir -p "$XRAY_DIR" "$AUDIT_DIR" "$LOG_DIR" "$CERT_DIR"

# ── 下载 Xray ──
_cyan "下载 Xray-core..."
if [[ -z $CORE_VER ]]; then
  CORE_VER=$(curl -s https://api.github.com/repos/XTLS/Xray-core/releases/latest | jq -r .tag_name)
fi
XRAY_URL="https://github.com/XTLS/Xray-core/releases/download/${CORE_VER}/Xray-${CORE_ARCH}.zip"
_wget "$tmpcore" "$XRAY_URL" || err "下载 Xray 失败, 请检查网络或使用 -p 代理"
mkdir -p "$XRAY_DIR"
unzip -qo "$tmpcore" -d "$XRAY_DIR"; chmod +x "$XRAY_DIR/xray"
ok "Xray ${CORE_VER}"

# ── 安装 vless-audit ──
_cyan "安装 vless-audit..."
mkdir -p "$AUDIT_DIR"
if [[ -f "$SCRIPT_DIR/bin/$BIN_ARCH" ]]; then
  cp "$SCRIPT_DIR/bin/$BIN_ARCH" "$AUDIT_DIR/vless-audit"
elif [[ -f "$SCRIPT_DIR/vless-audit" ]]; then
  cp "$SCRIPT_DIR/vless-audit" "$AUDIT_DIR/vless-audit"
else
  err "未找到 vless-audit 二进制, 请确认部署包完整"
fi
chmod +x "$AUDIT_DIR/vless-audit"; ok "vless-audit 就绪"

# ── 安装 Nginx 反代 ──
install_nginx() {
  _cyan "安装 Nginx..."
  if [[ $cmd =~ apt ]]; then
    apt install -y -qq nginx
  elif [[ $cmd =~ yum ]]; then
    yum install -y -q nginx || yum install -y -q epel-release && yum install -y -q nginx
  elif [[ $cmd =~ dnf ]]; then
    dnf install -y -q nginx
  fi
  ok "Nginx 已安装"
}

gen_nginx_config() {
  _cyan "生成 Nginx 反代配置..."
  local NGINX_CONF="/etc/nginx/conf.d/vless-cdn.conf"

  # Determine SSL mode: use HTTPS if cert exists, otherwise plain HTTP (CDN handles TLS)
  local SSL_BLOCK=""
  if [[ -f "$TLS_CERT" && -f "$TLS_KEY" ]]; then
    NGINX_LISTEN_PORT=443
    SSL_BLOCK=$(cat <<SSLEOF
    listen 443 ssl;
    ssl_certificate     ${TLS_CERT};
    ssl_certificate_key ${TLS_KEY};
    ssl_protocols TLSv1.2 TLSv1.3;
SSLEOF
)
  else
    NGINX_LISTEN_PORT=80
    _yellow "  无 TLS 证书, Nginx 监听 HTTP :80 (CDN 负责加密)"
  fi

  cat > "$NGINX_CONF" <<NGEOF
# VLESS WebSocket 反代 — 从 Cloudflare 读真实 IP
server {
    ${SSL_BLOCK:-listen 80;}
    server_name _;

    set_real_ip_from 0.0.0.0/0;
    real_ip_header    CF-Connecting-IP;
    real_ip_recursive on;

    location /ws {
        proxy_pass http://127.0.0.1:${XRAY_LOCAL_PORT};
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_read_timeout 86400s;
        proxy_send_timeout 86400s;
    }
}
NGEOF

  # Remove default site to free port 80
  rm -f /etc/nginx/sites-enabled/default 2>/dev/null

  nginx -t 2>/dev/null && ok "Nginx 配置检查通过" || _yellow "Nginx 配置检查有警告, 请手动检查"
  systemctl enable nginx 2>/dev/null
  systemctl restart nginx 2>/dev/null
  ok "Nginx 已启动"
}

# ── Nginx 反代 ──
if [[ $USE_NGINX == "true" ]]; then
  install_nginx
fi

# ── 生成 Xray 配置 ──
_cyan "生成配置文件..."

if [[ $TLS_ENABLED == true ]]; then
  STREAM=$(cat <<S
"streamSettings": {
    "network": "${PROTO}",
    "security": "tls",
    "tlsSettings": { "certificates": [{ "certificateFile": "${TLS_CERT}", "keyFile": "${TLS_KEY}" }] }
  }
S
)
else
  STREAM="\"streamSettings\": { \"network\": \"${PROTO}\" }"
fi

EXTRA=""
[[ $PROTO == "ws"   ]] && EXTRA=', "wsSettings": { "path": "'"${WS_PATH}"'" }'
[[ $PROTO == "grpc" ]] && EXTRA=', "grpcSettings": { "serviceName": "/vless" }'

# Tweak Xray listen address if behind Nginx.
XRAY_LISTEN="0.0.0.0"
XRAY_PORT=$VLESS_PORT
if [[ $USE_NGINX == "true" ]]; then
  XRAY_LISTEN="127.0.0.1"
  XRAY_PORT=$XRAY_LOCAL_PORT
fi

cat > "$XRAY_DIR/config.json" <<EOF
{
  "log": { "loglevel": "warning", "access": "${LOG_DIR}/access.log", "error": "${LOG_DIR}/error.log" },
  "api": { "tag": "api", "services": ["StatsService", "HandlerService"] },
  "stats": {},
  "policy": { "levels": { "0": { "statsUserUplink": true, "statsUserDownlink": true } } },
  "inbounds": [
    { "tag": "api", "listen": "127.0.0.1", "port": ${API_PORT}, "protocol": "dokodemo-door", "settings": { "address": "127.0.0.1" } },
    {
      "tag": "vless-in", "listen": "${XRAY_LISTEN}", "port": ${XRAY_PORT}, "protocol": "vless",
      "settings": { "clients": [{ "id": "${UUID}", "email": "admin@user", "level": 0 }], "decryption": "none" },
      ${STREAM}${EXTRA},
      "sniffing": { "enabled": true, "destOverride": ["http", "tls"] }
    }
  ],
  "outbounds": [ { "protocol": "freedom", "tag": "direct" } ],
  "routing": { "rules": [ { "type": "field", "inboundTag": ["api"], "outboundTag": "api" } ] }
}
EOF

# ── Nginx 配置 ──
if [[ $USE_NGINX == "true" ]]; then
  gen_nginx_config
fi

cat > "$AUDIT_DIR/config.json" <<EOF
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
EOF
chmod 600 "$AUDIT_DIR/config.json"
ok "配置文件已生成"

# ── 防火墙 ──
# 默认跳过。如需自动开端口，取消下面注释。
_cyan "跳过防火墙 (VPS 通常已配好或另管)"
# if type -P ufw &>/dev/null; then
#   ufw allow $VLESS_PORT/tcp; ufw allow $AUDIT_PORT/tcp; ufw --force enable
# elif type -P firewall-cmd &>/dev/null; then
#   firewall-cmd --permanent --add-port=$VLESS_PORT/tcp; firewall-cmd --reload
# fi

# ── systemd 服务 ──
_cyan "注册 systemd 服务..."

cat > /etc/systemd/system/xray.service <<'SVCEOF'
[Unit]
Description=Xray-core VLESS Service
After=network.target

[Service]
Type=simple
User=root
LimitNOFILE=1048576
ExecStart=XRAY_BIN_PLACEHOLDER run -config XRAY_CFG_PLACEHOLDER
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
SVCEOF
sed -i "s|XRAY_BIN_PLACEHOLDER|${XRAY_DIR}/xray|g" /etc/systemd/system/xray.service
sed -i "s|XRAY_CFG_PLACEHOLDER|${XRAY_DIR}/config.json|g" /etc/systemd/system/xray.service

cat > /etc/systemd/system/vless-audit.service <<'SVCEOF'
[Unit]
Description=Vless-Audit Monitor
After=network.target xray.service
Requires=xray.service

[Service]
Type=simple
User=root
ExecStart=AUDIT_BIN_PLACEHOLDER -config AUDIT_CFG_PLACEHOLDER
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
SVCEOF
sed -i "s|AUDIT_BIN_PLACEHOLDER|${AUDIT_DIR}/vless-audit|g" /etc/systemd/system/vless-audit.service
sed -i "s|AUDIT_CFG_PLACEHOLDER|${AUDIT_DIR}/config.json|g" /etc/systemd/system/vless-audit.service

cat > /etc/logrotate.d/vless-audit <<EOF
${LOG_DIR}/*.log {
    daily
    rotate 30
    missingok
    notifempty
    compress
    copytruncate
}
EOF

systemctl daemon-reload
systemctl enable xray vless-audit &>/dev/null
ok "服务已注册"

# ── 启动 ──
_cyan "启动服务..."
# Ensure log dir exists (Xray won't create it).
mkdir -p "$LOG_DIR"
systemctl restart xray; sleep 2
systemctl restart vless-audit 2>/dev/null || true
sleep 2
systemctl is-active --quiet xray && ok "Xray 运行中" || _red "Xray 启动失败, 查看: journalctl -u xray -n 20"
systemctl is-active --quiet vless-audit 2>/dev/null && ok "vless-audit 运行中" || _yellow "vless-audit 未启动"

# ── 完成 ──
IP=$(curl -s4 ip.sb 2>/dev/null || curl -s4 ifconfig.me 2>/dev/null || hostname -I 2>/dev/null | awk '{print $1}')
# Use domain if available (ACME or custom cert implies a domain).
if [[ -n $ACME_DOMAIN ]]; then HOST_NAME=$ACME_DOMAIN
elif [[ -n $DOMAIN ]]; then HOST_NAME=$DOMAIN
else HOST_NAME=$IP
fi
TOKEN=$(jq -r .auth_token "$AUDIT_DIR/config.json" 2>/dev/null || echo "查看: journalctl -u vless-audit")
SECURITY="none"; [[ $TLS_ENABLED == true ]] && SECURITY="tls"
PUBLIC_PORT=$VLESS_PORT
[[ $USE_NGINX == "true" ]] && PUBLIC_PORT=$NGINX_LISTEN_PORT
VLESS_URL="vless://${UUID}@${HOST_NAME}:${PUBLIC_PORT}?encryption=none&security=${SECURITY}&type=${PROTO}"
[[ $PROTO == "ws"   ]] && VLESS_URL="${VLESS_URL}&path=${WS_PATH}"
[[ $PROTO == "grpc" ]] && VLESS_URL="${VLESS_URL}&serviceName=/vless"
VLESS_URL="${VLESS_URL}#VLESS-${HOST_NAME}"

echo
echo -e "${green}╔══════════════════════════════════════════════╗${none}"
echo -e "${green}║          Vless-Audit 部署完成                ║${none}"
echo -e "${green}╚══════════════════════════════════════════════╝${none}"
echo
echo -e "  协议:        ${cyan}VLESS + ${PROTO}$([[ $TLS_ENABLED == true ]] && echo ' + TLS')$([[ $USE_NGINX == true ]] && echo ' + Nginx 反代')${none}"
[[ $USE_NGINX == true ]] && echo -e "  架构:        ${cyan}CDN → Nginx → Xray (真实 IP)${none}"
echo -e "  端口:        ${cyan}${VLESS_PORT}${none}"
echo -e "  审计面板:    ${cyan}http://${HOST_NAME}:${AUDIT_PORT}/app/${none}"
echo -e "  用户申请:    ${cyan}http://${HOST_NAME}:${AUDIT_PORT}/app/register.html${none}"
echo -e "  登录密码:    ${cyan}${TOKEN}${none}"
[[ -n $REGISTER_SECRET ]] && echo -e "  注册口令:    ${cyan}${REGISTER_SECRET}${none}"
echo
echo -e "  VLESS 链接:"
echo -e "  ${cyan}${VLESS_URL}${none}"
echo
echo -e "  管理: ${yellow}systemctl status xray${none}"
echo -e "  管理: ${yellow}systemctl status vless-audit${none}"
echo -e "  日志: ${yellow}journalctl -u vless-audit -f${none}"
echo
