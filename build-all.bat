@echo off
setlocal enabledelayedexpansion
set "PATH=F:\go\bin;%PATH%"
cd /d "%~dp0"

echo ========================================
echo   Lanlink - Multi-Platform Build
echo ========================================
echo.

set SUCCESS=0
set FAILED=0

echo [1/4] Building Windows amd64...
wails build -platform windows/amd64
if !ERRORLEVEL! EQU 0 (
    echo   [OK] Windows amd64 built
    set /a SUCCESS+=1
) else (
    echo   [FAIL] Windows amd64
    set /a FAILED+=1
)
echo.

echo [2/4] Building Linux amd64...
set CGO_ENABLED=0
wails build -platform linux/amd64
set CGO_ENABLED=
if !ERRORLEVEL! EQU 0 (
    echo   [OK] Linux amd64 built
    set /a SUCCESS+=1
) else (
    echo   [FAIL] Linux amd64
    set /a FAILED+=1
)
echo.

echo [3/4] Building macOS amd64...
set CGO_ENABLED=0
wails build -platform darwin/amd64
set CGO_ENABLED=
if !ERRORLEVEL! EQU 0 (
    echo   [OK] macOS amd64 built
    set /a SUCCESS+=1
) else (
    echo   [FAIL] macOS amd64
    set /a FAILED+=1
)
echo.

echo [4/4] Building macOS arm64...
set CGO_ENABLED=0
wails build -platform darwin/arm64
set CGO_ENABLED=
if !ERRORLEVEL! EQU 0 (
    echo   [OK] macOS arm64 built
    set /a SUCCESS+=1
) else (
    echo   [FAIL] macOS arm64
    set /a FAILED+=1
)
echo.

echo ========================================
echo   Build Summary
echo ========================================
echo   Succeeded: !SUCCESS!/4
echo   Failed:    !FAILED!/4
echo.
echo   Output: build\bin\
if !FAILED! GTR 0 (
    echo.
    echo NOTE: macOS builds should be done ON a Mac.
    echo       Use build.bat for Windows-only.
)
echo ========================================
pause
