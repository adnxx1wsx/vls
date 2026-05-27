# Vless-Audit 部署包

Vless 流量审计系统 — Xray VLESS 协议 + 用户行为监控面板，一键部署。

## 系统要求

- Linux (amd64 / arm64)
- Debian 10+ / Ubuntu 20.04+ / CentOS 7+
- Root 权限

## 快速安装

```bash
# 上传部署包到服务器
scp vless-audit-deploy.tar.gz root@你的服务器:/tmp/

# SSH 到服务器
ssh root@你的服务器
cd /tmp && tar xzf vless-audit-deploy.tar.gz && cd vless-audit-deploy

# 一键安装
sudo bash install.sh
```

安装过程会提示：
- 设置用户自助注册口令（留空则关闭）

## 安装后

```
审计面板:   http://你的IP:8080/app/
用户申请:   http://你的IP:8080/app/register.html
```

登录密码在安装完成时会打印出来，也可通过以下命令查看：

```bash
jq -r .auth_token /opt/vless-audit/audit/config.json
```

## 目录结构

```
/opt/vless-audit/
├── xray/
│   ├── xray              # Xray 可执行文件
│   └── config.json       # Xray 配置
├── audit/
│   ├── vless-audit       # 监控系统可执行文件
│   ├── config.json       # 监控系统配置（含密码）
│   └── vless-audit.db    # SQLite 数据库
└── log/
    ├── access.log        # Xray 访问日志
    └── error.log         # Xray 错误日志
```

## 管理命令

```bash
systemctl status xray           # 查看 Xray 状态
systemctl restart xray          # 重启 Xray
journalctl -u xray -f           # 实时 Xray 日志
journalctl -u vless-audit -f    # 实时审计日志
```

## 配置修改

```bash
# 修改审计面板端口或密码
vi /opt/vless-audit/audit/config.json
systemctl restart vless-audit

# 修改 VLESS 端口
vi /opt/vless-audit/xray/config.json
systemctl restart xray
```

## 数据保留

默认保留 365 天。过期数据自动归档到 `*_archive` 表，不会丢失。
修改 `config.json` 中 `retention_days` 可调整（0 = 永不过期）。
