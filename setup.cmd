@echo off
setlocal
where go >nul 2>nul
if errorlevel 1 (
  echo ERROR: Go 1.24+ is required. Install it from https://go.dev/dl/
  exit /b 1
)
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\install.ps1"
exit /b %ERRORLEVEL%
