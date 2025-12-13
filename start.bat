@echo off
echo.
echo ========================================
echo   SeeingStone - Crypto Arbitrage Monitor
echo   Built with Claude Code
echo ========================================
echo.

REM Check if .env exists
if not exist .env (
    echo [ERROR] .env file not found!
    echo Please copy .env.example to .env and configure your API keys.
    echo.
    pause
    exit /b 1
)

REM Check if binary exists
if not exist seeing-stone.exe (
    echo [INFO] Binary not found. Building...
    go build -o seeing-stone.exe ./cmd/monitor
    if errorlevel 1 (
        echo [ERROR] Build failed!
        pause
        exit /b 1
    )
    echo [SUCCESS] Build completed!
    echo.
)

echo [INFO] Starting SeeingStone...
echo.
seeing-stone.exe

pause
