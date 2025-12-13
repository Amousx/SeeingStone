@echo off
echo ========================================
echo   Crypto Arbitrage Monitor
echo ========================================
echo.

REM Check if .env exists
if not exist .env (
    echo [ERROR] .env file not found!
    echo Please copy .env.example to .env and configure it.
    echo.
    echo Run: copy .env.example .env
    echo.
    pause
    exit /b 1
)

REM Check if executable exists
if not exist arbitrage-monitor.exe (
    echo Building application...
    go build -o arbitrage-monitor.exe ./cmd/monitor
    if errorlevel 1 (
        echo [ERROR] Build failed!
        pause
        exit /b 1
    )
    echo Build successful!
    echo.
)

echo Starting monitor...
echo.
arbitrage-monitor.exe

pause
