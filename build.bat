@echo off
setlocal
set "PATH=F:\go\bin;%PATH%"
cd /d "%~dp0"

echo ================================
echo   Lanlink - Windows Build
echo ================================
echo.

wails build

if %ERRORLEVEL% EQU 0 (
    echo.
    echo ================================
    echo   Build Succeeded!
    echo   Output: build\bin\lanlink.exe
    echo ================================
) else (
    echo.
    echo Build failed, check errors above.
)
pause
