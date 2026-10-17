@echo off
setlocal
pushd "%~dp0" || exit /b 1
go run ./cmd/damocline %*
set "DAMOCLINE_STATUS=%ERRORLEVEL%"
popd
exit /b %DAMOCLINE_STATUS%
