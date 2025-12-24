@echo off
chcp 65001 >nul
REM ===================================
REM 带代理运行 - Crypto Arbitrage Monitor
REM ===================================

echo.
echo ======================================
echo   Crypto Arbitrage Monitor (代理模式)
echo ======================================
echo.

REM 配置代理（根据你的代理软件修改端口）
REM Clash 默认端口: 7890
REM V2Ray 默认端口: 10809
REM SSR 默认端口: 1080

REM 设置代理（取消下面一行的注释并修改端口）
set HTTPS_PROXY=http://127.0.0.1:7890

REM 如果你的代理需要用户名密码，使用这种格式：
REM set HTTPS_PROXY=http://username:password@127.0.0.1:7890

echo 代理配置: %HTTPS_PROXY%
echo.

REM 检查程序文件是否存在
if not exist "crypto-monitor.exe" (
    echo [错误] monitor.exe 不存在，请先编译程序
    echo 运行命令: go build -o crypto-monitor.exe cmd/monitor/main.go
    pause
    exit /b 1
)

echo [启动] 正在启动监控程序...
echo.

REM 运行程序
crypto-monitor.exe

echo.
echo [退出] 程序已结束
pause