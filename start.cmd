@echo off
setlocal
pushd "%~dp0" || exit /b 1
go run ./cmd/damocline start
set "DAMOCLINE_STATUS=%ERRORLEVEL%"
popd
exit /b %DAMOCLINE_STATUS%
