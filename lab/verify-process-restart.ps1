param(
    [string]$OutputPath
)

$ErrorActionPreference = 'Stop'
$originalLocation = Get-Location
$previousLabPort = [Environment]::GetEnvironmentVariable('DAMOCLINE_LAB_PORT', 'Process')
$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $repositoryRoot

$project = 'damocline-restart-' + [guid]::NewGuid().ToString('N').Substring(0, 12)
$portProbe = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
$portProbe.Start()
$env:DAMOCLINE_LAB_PORT = [string]$portProbe.LocalEndpoint.Port
$portProbe.Stop()
$metricsEndpoint = "http://127.0.0.1:$($env:DAMOCLINE_LAB_PORT)/metrics"
if ([string]::IsNullOrWhiteSpace($OutputPath)) {
    $stamp = [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssfffZ')
    $OutputPath = "lab/artifacts/process-restart-evidence-$stamp.json"
}
$resolvedOutput = [System.IO.Path]::GetFullPath((Join-Path $repositoryRoot $OutputPath))
$artifactRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot 'artifacts'))
if (-not $resolvedOutput.StartsWith($artifactRoot + [System.IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'OutputPath must remain inside lab/artifacts.'
}
if (Test-Path -LiteralPath $resolvedOutput) {
    throw "Refusing to overwrite existing evidence: $resolvedOutput"
}
$temporaryEnvFile = Join-Path ([System.IO.Path]::GetTempPath()) ($project + '.env')
$temporaryToken = [guid]::NewGuid().ToString('N') + [guid]::NewGuid().ToString('N')
[System.IO.File]::WriteAllText($temporaryEnvFile, "DAMOCLINE_SENSOR_TOKEN=$temporaryToken`n", [System.Text.UTF8Encoding]::new($false))
$temporaryToken = $null
$compose = @('compose', '--env-file', $temporaryEnvFile, '-p', $project, '-f', 'lab/compose.yaml')

function Invoke-Compose([string[]]$Arguments) {
    & docker @compose @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Docker Compose failed: $($Arguments -join ' ')"
    }
}

function Read-ProcessStartTime {
    for ($attempt = 0; $attempt -lt 20; $attempt++) {
        try {
            $response = Invoke-WebRequest -UseBasicParsing -TimeoutSec 2 $metricsEndpoint
            if ($response.StatusCode -eq 200 -and $response.Content -match '(?m)^process_start_time_seconds ([0-9.]+)$') {
                return [double]$Matches[1]
            }
        }
        catch {
            Start-Sleep -Milliseconds 500
        }
    }
    throw 'Synthetic target metrics endpoint did not become ready within 10 seconds.'
}

$started = $false
try {
    $started = $true
    Invoke-Compose @('up', '--build', '-d', 'target')
    $first = Read-ProcessStartTime
    Invoke-Compose @('restart', 'target')
    $second = Read-ProcessStartTime
    if ($second -le $first) {
        throw "Expected a newer process start timestamp; observed $first then $second."
    }

    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $resolvedOutput) | Out-Null
    $evidence = [ordered]@{
        schema_version = 1
        test = 'synthetic-target-controlled-restart'
        endpoint = $metricsEndpoint
        docker_server_version = (& docker version --format '{{.Server.Version}}')
        first_process_start_time_seconds = $first
        second_process_start_time_seconds = $second
        exporter_reported_process_restarts = 1
        verified = $true
    }
    $json = ConvertTo-Json -InputObject $evidence -Depth 4
    [System.IO.File]::WriteAllText($resolvedOutput, $json + [Environment]::NewLine, [System.Text.UTF8Encoding]::new($false))
    Write-Output "Evidence: $resolvedOutput"
    Write-Output "process_start_time_seconds: $first -> $second (one exporter-reported process restart)"
}
finally {
    if ($started) {
        & docker @compose down --remove-orphans
        if ($LASTEXITCODE -ne 0) {
            Write-Warning 'Compose cleanup reported an error; inspect the isolated restart-check project.'
        }
    }
    if ($null -eq $previousLabPort) {
        Remove-Item Env:DAMOCLINE_LAB_PORT -ErrorAction SilentlyContinue
    }
    else {
        $env:DAMOCLINE_LAB_PORT = $previousLabPort
    }
    Remove-Item -LiteralPath $temporaryEnvFile -Force -ErrorAction SilentlyContinue
    Set-Location $originalLocation
}
