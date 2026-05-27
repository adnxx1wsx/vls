@echo off & setlocal enabledelayedexpansion & title Vless-Audit 部署工具
:: ============================================================
::  Vless-Audit Windows 部署脚本
::  集成 Xray-core + VLESS 协议 + 流量审计面板
:: ============================================================

set "INSTALL_DIR=C:\vless-audit"
set "XRAY_DIR=%INSTALL_DIR%\xray"
set "AUDIT_DIR=%INSTALL_DIR%\audit"
set "LOG_DIR=%INSTALL_DIR%\log"

set "VLESS_PORT=443"
set "API_PORT=10086"
set "AUDIT_PORT=8080"
for /f %%i in ('powershell -NoProfile -Command "[guid]::NewGuid().ToString()" 2^>nul') do set "UUID=%%i"
if "%UUID%"=="" set "UUID=b831381d-6324-4d53-ad4f-8cda48b30811"

:menu
cls
echo.
echo   ╔══════════════════════════════════════════════╗
echo   ║     Vless-Audit  部署管理工具 (Windows)      ║
echo   ╚══════════════════════════════════════════════╝
echo.
echo   [1] 全新安装 Xray + vless-audit
echo   [2] 添加 VLESS 用户
echo   [3] 删除 VLESS 用户
echo   [4] 查看运行状态
echo   [5] 重启服务
echo   [6] 停止服务
echo   [7] 卸载
echo   [0] 退出
echo.
set /p "choice=  请选择 [0-7]: "

if "%choice%"=="1" goto install
if "%choice%"=="2" goto add_user
if "%choice%"=="3" goto del_user
if "%choice%"=="4" goto status
if "%choice%"=="5" goto restart
if "%choice%"=="6" goto stop
if "%choice%"=="7" goto uninstall
if "%choice%"=="0" exit /b
goto menu

:install
echo.
echo   [1/6] 创建目录...
mkdir "%XRAY_DIR%" "%AUDIT_DIR%" "%LOG_DIR%" 2>nul

echo   [2/6] 下载 Xray-core...
set "XRAY_URL=https://github.com/XTLS/Xray-core/releases/latest/download/Xray-windows-64.zip"
curl -L# -o "%TEMP%\xray.zip" "%XRAY_URL%"
tar -xf "%TEMP%\xray.zip" -C "%XRAY_DIR%" 2>nul
del "%TEMP%\xray.zip" 2>nul
echo          Xray 安装完成

echo   [3/6] 安装 vless-audit...
if exist "vless-audit.exe" (
  copy /y "vless-audit.exe" "%AUDIT_DIR%\" >nul
  echo          vless-audit 安装完成
) else (
  echo          [WARN] 未找到 vless-audit.exe，请先编译
)

echo   [3.5/6] 设置注册口令...
echo.
echo   ══════ 用户自助注册设置 ══════
echo   用户可以访问 http://服务器IP:%AUDIT_PORT%/app/register.html 自助申请连接
set /p "REGISTER_SECRET=  请输入申请口令 (留空则关闭自助注册): "
if "%REGISTER_SECRET%"=="" (echo         自助注册已关闭) else (echo         口令已设置)

echo   [4/6] 生成配置...
(
echo {
echo   "log": { "loglevel": "warning", "access": "%LOG_DIR:\=/%/access.log", "error": "%LOG_DIR:\=/%/error.log" },
echo   "api": { "tag": "api", "services": ["StatsService", "HandlerService"] },
echo   "stats": {},
echo   "policy": { "levels": { "0": { "statsUserUplink": true, "statsUserDownlink": true } } },
echo   "inbounds": [
echo     { "tag": "api", "listen": "127.0.0.1", "port": %API_PORT%, "protocol": "dokodemo-door", "settings": { "address": "127.0.0.1" } },
echo     { "tag": "vless-in", "listen": "0.0.0.0", "port": %VLESS_PORT%, "protocol": "vless",
echo       "settings": { "clients": [{ "id": "%UUID%", "email": "admin@user", "level": 0 }], "decryption": "none" },
echo       "streamSettings": { "network": "tcp" },
echo       "sniffing": { "enabled": true, "destOverride": ["http", "tls"] } }
echo   ],
echo   "outbounds": [ { "protocol": "freedom", "tag": "direct" } ],
echo   "routing": { "rules": [ { "type": "field", "inboundTag": ["api"], "outboundTag": "api" } ] }
echo }
) > "%XRAY_DIR%\config.json"

(
echo {
echo   "listen": ":%AUDIT_PORT%",
echo   "db_path": "%AUDIT_DIR:\=/%/vless-audit.db",
echo   "xray_api": "127.0.0.1:%API_PORT%",
echo   "access_log": "%LOG_DIR:\=/%/access.log",
echo   "poll_interval_sec": 10,
echo   "retention_days": 365,
echo   "register_secret": "%REGISTER_SECRET%",
echo   "xray_config_path": "%XRAY_DIR:\=/%/config.json",
echo   "xray_bin_path": "%XRAY_DIR:\=/%/xray.exe"
echo }
) > "%AUDIT_DIR%\config.json"

echo         配置生成完成

echo   [5/6] 配置防火墙...
netsh advfirewall firewall add rule name="VLESS TCP" dir=in action=allow protocol=tcp localport=%VLESS_PORT% >nul 2>&1
netsh advfirewall firewall add rule name="VLESS UDP" dir=in action=allow protocol=udp localport=%VLESS_PORT% >nul 2>&1
netsh advfirewall firewall add rule name="Audit Web" dir=in action=allow protocol=tcp localport=%AUDIT_PORT% >nul 2>&1
echo         防火墙配置完成

echo   [6/6] 启动服务...
taskkill /f /im xray.exe >nul 2>&1
taskkill /f /im vless-audit.exe >nul 2>&1
start "Xray" /B "%XRAY_DIR%\xray.exe" run -config "%XRAY_DIR%\config.json"
timeout /t 3 /nobreak >nul
if exist "%AUDIT_DIR%\vless-audit.exe" (
  start "Vless-Audit" /B "%AUDIT_DIR%\vless-audit.exe" -config "%AUDIT_DIR%\config.json"
)

echo.
echo   ╔══════════════════════════════════════════════╗
echo   ║           安装完成！                          ║
echo   ╠══════════════════════════════════════════════╣
echo   ║  审计面板: http://localhost:%AUDIT_PORT%/app/      ║
echo   ║  VLESS 端口: %VLESS_PORT%                            ║
echo   ║  UUID: %UUID%  ║
echo   ╚══════════════════════════════════════════════╝
echo.
pause
goto menu

:add_user
set /p "USER_EMAIL=  用户标识 (email): "
set /p "USER_UUID=  UUID (留空自动生成): "
if "%USER_UUID%"=="" for /f %%i in ('powershell -NoProfile -Command "[guid]::NewGuid().ToString()"') do set "USER_UUID=%%i"
echo   [INFO] 请手动编辑 %XRAY_DIR%\config.json
echo   [INFO] 在 vless-in -^> settings -^> clients 数组中添加:
echo   [INFO]   { "id": "%USER_UUID%", "email": "%USER_EMAIL%", "level": 0 }
echo.
pause
goto menu

:del_user
echo   [INFO] 请手动编辑 %XRAY_DIR%\config.json 删除对应用户
pause
goto menu

:status
echo.
echo   ═══ 服务状态 ═══
tasklist /fi "imagename eq xray.exe" 2>nul | find "xray.exe" >nul && echo   Xray:          ● 运行中 || echo   Xray:          ○ 已停止
tasklist /fi "imagename eq vless-audit.exe" 2>nul | find "vless-audit.exe" >nul && echo   vless-audit:   ● 运行中 || echo   vless-audit:   ○ 已停止
echo.
echo   审计面板: http://localhost:%AUDIT_PORT%/app/
echo.
pause
goto menu

:restart
taskkill /f /im xray.exe >nul 2>&1
taskkill /f /im vless-audit.exe >nul 2>&1
timeout /t 2 /nobreak >nul
start "Xray" /B "%XRAY_DIR%\xray.exe" run -config "%XRAY_DIR%\config.json"
timeout /t 3 /nobreak >nul
if exist "%AUDIT_DIR%\vless-audit.exe" start "Vless-Audit" /B "%AUDIT_DIR%\vless-audit.exe" -config "%AUDIT_DIR%\config.json"
echo   已重启
pause
goto menu

:stop
taskkill /f /im xray.exe >nul 2>&1
taskkill /f /im vless-audit.exe >nul 2>&1
echo   已停止
pause
goto menu

:uninstall
echo   确认卸载? 这将删除 %INSTALL_DIR%
set /p "confirm=  输入 YES 继续: "
if not "%confirm%"=="YES" goto menu
taskkill /f /im xray.exe >nul 2>&1
taskkill /f /im vless-audit.exe >nul 2>&1
netsh advfirewall firewall delete rule name="VLESS TCP" >nul 2>&1
netsh advfirewall firewall delete rule name="VLESS UDP" >nul 2>&1
netsh advfirewall firewall delete rule name="Audit Web" >nul 2>&1
rmdir /s /q "%INSTALL_DIR%" 2>nul
echo   卸载完成
pause
goto menu
