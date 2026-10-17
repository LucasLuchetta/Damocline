# Source-based Windows command registration (no downloaded release binary).
$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw 'Go 1.24+ is required. Install from https://go.dev/dl/ and open a new terminal.'
}
$version = (& go version)
if ($LASTEXITCODE -ne 0 -or $version -notmatch 'go1\.(\d+)') { throw "Cannot detect Go version: $version" }
if ([int]$Matches[1] -lt 24) { throw "Damocline requires Go 1.24+; found: $version" }
$bin = Join-Path $env:LOCALAPPDATA 'Damocline\bin'
New-Item -ItemType Directory -Force -Path $bin | Out-Null
$escapedRepo = $repo.Replace('%','%%')
$launcher = @'
@echo off
setlocal
pushd "__REPO__" || exit /b 1
go run ./cmd/damocline %*
set "DAMOCLINE_EXIT=%ERRORLEVEL%"
popd
exit /b %DAMOCLINE_EXIT%
'@
$launcher = $launcher.Replace('__REPO__', $escapedRepo)
[System.IO.File]::WriteAllText((Join-Path $bin 'damocline.cmd'), $launcher + "`r`n", [System.Text.UTF8Encoding]::new($false))
$path = [Environment]::GetEnvironmentVariable('Path', 'User')
$entries = @($path -split ';' | Where-Object { $_ -ne '' })
if (-not ($entries | Where-Object { $_.TrimEnd('\') -ieq $bin.TrimEnd('\') })) {
    [Environment]::SetEnvironmentVariable('Path', (($entries + $bin) -join ';'), 'User')
}
Write-Host 'Damocline source launcher is installed for this Windows user.'
Write-Host "Source directory: $repo"
Write-Host "Launcher directory: $bin"
Write-Host 'Open a NEW PowerShell/CMD window and type: damocline'
Write-Host 'To update the tool, run git pull inside your original clone.'
