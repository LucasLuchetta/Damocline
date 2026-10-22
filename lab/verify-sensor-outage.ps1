$ErrorActionPreference = 'Stop'
$originalLocation = Get-Location
$previousLabPort = [Environment]::GetEnvironmentVariable('DAMOCLINE_LAB_PORT', 'Process')
$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $repositoryRoot

$envFile = Join-Path $PSScriptRoot '.env'
if (-not (Test-Path -LiteralPath $envFile)) {
    throw 'Create lab/.env as documented in lab/README.md before running the sensor outage check.'
}

$stamp = [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssfffZ')
$campaignName = "sensor-outage-$stamp.json"
$campaignPath = Join-Path $PSScriptRoot "artifacts/$campaignName"
$auditPath = Join-Path $PSScriptRoot "artifacts/sensor-outage-$stamp-audit.json"
foreach ($path in @($campaignPath, $auditPath)) {
    if (Test-Path -LiteralPath $path) {
        throw "Refusing to overwrite existing evidence: $path"
    }
}

$portProbe = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
$portProbe.Start()
$env:DAMOCLINE_LAB_PORT = [string]$portProbe.LocalEndpoint.Port
$portProbe.Stop()
$project = 'damocline-outage-' + [guid]::NewGuid().ToString('N').Substring(0, 12)
$compose = @('compose', '--env-file', 'lab/.env', '-p', $project, '-f', 'lab/compose.yaml')
$started = $false

try {
    $started = $true
    & docker @compose up --build -d target sensor
    if ($LASTEXITCODE -ne 0) {
        throw 'Could not start the isolated synthetic target and sensor.'
    }

    & docker @compose stop sensor
    if ($LASTEXITCODE -ne 0) {
        throw 'Could not stop the isolated sensor for the outage check.'
    }

    & docker @compose --profile run run --rm --no-deps generator `
        campaign --config /lab/scenario.json `
        --warmup 0s --observation 10s --recovery 0s --repeats 2 `
        --metrics-endpoint http://127.0.0.1:8080/metrics --metrics-interval 3s `
        --host-sensor-endpoint http://127.0.0.1:9467/v1/host `
        --host-sensor-token-env DAMOCLINE_SENSOR_TOKEN --host-sensor-interval 3s `
        --authorized --out "/reports/$campaignName"
    $campaignExit = $LASTEXITCODE
    if ($campaignExit -gt 1) {
        throw "The bounded generator command failed unexpectedly with exit code $campaignExit."
    }

    if (-not (Test-Path -LiteralPath $campaignPath)) {
        throw 'The campaign did not write its expected local artifact.'
    }
    $campaign = Get-Content -LiteralPath $campaignPath -Raw | ConvertFrom-Json
    $observationPhases = @($campaign.trials | ForEach-Object { $_.phases } | Where-Object { $_.name -eq 'observation' })
    if ($campaign.outcome -ne 'INCONCLUSIVE' -or $observationPhases.Count -lt 1) {
        throw "Expected an inconclusive campaign with at least one observation; got $($campaign.outcome)."
    }
    foreach ($phase in $observationPhases) {
        if ($phase.run.outcome -ne 'PASS' -or $phase.host_telemetry.coverage -ne 'INCOMPLETE' -or $phase.host_telemetry.failures.Count -lt 1) {
            throw 'The outage must preserve HTTP PASS while marking missing host samples INCOMPLETE.'
        }
    }

    go run ./cmd/damocline audit --campaign $campaignPath --out $auditPath
    if ($LASTEXITCODE -ne 0) {
        throw 'Offline audit failed for the sensor outage artifact.'
    }
    $audit = Get-Content -LiteralPath $auditPath -Raw | ConvertFrom-Json
    if ($audit.state -ne 'INCONCLUSIVE') {
        throw "Expected the offline audit to remain INCONCLUSIVE, got $($audit.state)."
    }

    Write-Output "Campaign evidence: $campaignPath"
    Write-Output "Offline audit: $auditPath"
    Write-Output "HTTP outcome: PASS; host telemetry: INCOMPLETE; campaign and audit: INCONCLUSIVE"
    Write-Output "Completed trials: $($campaign.trials.Count) of $($campaign.repeats); generator exit code: $campaignExit"
}
finally {
    if ($started) {
        & docker @compose down --remove-orphans
        if ($LASTEXITCODE -ne 0) {
            Write-Warning 'Isolated outage stack cleanup reported an error.'
        }
    }
    if ($null -eq $previousLabPort) {
        Remove-Item Env:DAMOCLINE_LAB_PORT -ErrorAction SilentlyContinue
    }
    else {
        $env:DAMOCLINE_LAB_PORT = $previousLabPort
    }
    Set-Location $originalLocation
}
