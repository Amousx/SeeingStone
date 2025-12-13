@echo off
echo Testing data calculation...
echo.
go run test_calc.go
echo.
echo.
echo If you see arbitrage opportunities above, the data layer works fine.
echo Now testing the full program...
echo.
pause
crypto-arbitrage-monitor.exe
