@echo off
chcp 65001 >nul
REM ===================================
REM 直连运行 - Crypto Arbitrage Monitor
REM ===================================

echo.
echo ======================================
echo   Crypto Arbitrage Monitor (直连模式)
echo ======================================
echo.

REM 清除代理环境变量，确保直连
set HTTPS_PROXY=
set HTTP_PROXY=

echo 模式: 直连（不使用代理）
echo.

REM 检查程序文件是否存在
if not exist "monitor.exe" (
    echo [错误] monitor.exe 不存在，请先编译程序
    echo 运行命令: go build -o monitor.exe cmd/monitor/main.go
    pause
    exit /b 1
)

echo [启动] 正在启动监控程序...
echo.

REM 运行程序
monitor.exe

echo.
echo [退出] 程序已结束
pause
