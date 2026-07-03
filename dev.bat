@echo off
chcp 65001 >nul
setlocal
set "PATH=F:\go\bin;%PATH%"

echo ================================
echo   Lanlink - Dev Mode
echo ================================
echo.
echo Look for [P2P] lines for connection status.
echo.

wails dev
pause
