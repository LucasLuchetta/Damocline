$ErrorActionPreference = 'Stop'
$originalLocation = Get-Location
$previousLabPort = [Environment]::GetEnvironmentVariable('DAMOCLINE_LAB_PORT', 'Process')
$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$envFile = Join-Path $PSScriptRoot '.env'
if (-not (Test-Path -LiteralPath $envFile)) {
    throw 'Create lab/.env as documented in lab/README.md before running the spike recovery check.'
}

$stamp = [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssfffZ')
$campaignName = "spike-recovery-$stamp.json"
$campaignPath = Join-Path $PSScriptRoot "artifacts/$campaignName"
$auditPath = Join-Path $PSScriptRoot "artifacts/spike-recovery-$stamp-audit.json"
foreach ($path in @($campaignPath, $auditPath)) {
    if (Test-Path -LiteralPath $path) {
        throw "Refusing to overwrite existing evidence: $path"
    }
}

$portProbe = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
$portProbe.Start()
$env:DAMOCLINE_LAB_PORT = [string]$portProbe.LocalEndpoint.Port
$portProbe.Stop()
$project = 'damocline-recovery-' + [guid]::NewGuid().ToString('N').Substring(0, 12)
$compose = @('compose', '--env-file', 'lab/.env', '-p', $project, '-f', 'lab/compose.yaml')
$started = $false

try {
    Set-Location $repositoryRoot
    $started = $true
    & docker @compose up --build -d target sensor
    if ($LASTEXITCODE -ne 0) {
        throw 'Could not start the isolated synthetic target and sensor.'
    }

    & docker @compose --profile run run --rm --no-deps generator `
        campaign --config /lab/scenario.json `
        --warmup 0s --observation 10s --spike-users 20 --spike-duration 10s `
        --recovery 10s --repeats 2 `
        --metrics-endpoint http://127.0.0.1:8080/metrics --metrics-interval 2s `
        --host-sensor-endpoint http://127.0.0.1:9467/v1/host `
        --host-sensor-token-env DAMOCLINE_SENSOR_TOKEN --host-sensor-interval 2s `
        --authorized --out "/reports/$campaignName"
    $campaignExit = $LASTEXITCODE
    if ($campaignExit -gt 1) {
        throw "The bounded generator command failed unexpectedly with exit code $campaignExit."
    }
    if (-not (Test-Path -LiteralPath $campaignPath)) {
        throw 'The campaign did not write its expected local artifact.'
    }

    $campaign = Get-Content -LiteralPath $campaignPath -Raw | ConvertFrom-Json
    if ($campaign.outcome -ne 'FAIL' -or $campaign.trials.Count -ne 1 -or $campaign.trials[0].phases.Count -ne 3) {
        throw 'Expected a failed spike followed by a bounded recovery and no second trial.'
    }
    $observation, $spike, $recovery = $campaign.trials[0].phases
    if ($observation.name -ne 'observation' -or $spike.name -ne 'spike' -or $recovery.name -ne 'recovery') {
        throw 'Campaign phase sequence did not preserve observation, spike, and recovery.'
    }
    if ($spike.evidence_state -ne 'FAIL_OBSERVED' -or $spike.run.outcome -ne 'FAIL' -or [int]$spike.run.metrics.http_diagnostics.status_codes.'503' -le 0) {
        throw 'The spike did not preserve its observed HTTP 503/SLO failure.'
    }
    if ($recovery.run.outcome -ne 'PASS' -or $recovery.run.config.users -ne $campaign.base_users -or $recovery.telemetry.coverage -ne 'COMPLETE' -or $recovery.host_telemetry.coverage -ne 'COMPLETE') {
        throw 'Recovery did not pass at base users with complete application and host telemetry.'
    }
    foreach ($phase in @($observation, $spike, $recovery)) {
        if ($phase.run.arrivals_dropped -ne 0 -or ($null -ne $phase.run.arrivals_cancelled_before_admission -and $phase.run.arrivals_cancelled_before_admission -ne 0)) {
            throw "Phase $($phase.name) had dropped or cancelled planned work."
        }
    }
    $generator = $spike.run.generator
    if (-not $generator.cpu_capacity_known -or -not $generator.process_cpu_valid -or $generator.process_samples -lt 10 -or $generator.mean_process_cpu_utilization_ratio -ge 0.9) {
        throw 'Generator headroom was unknown, invalid, undersampled, or near saturation.'
    }

    go run ./cmd/damocline audit --campaign $campaignPath --out $auditPath
    if ($LASTEXITCODE -ne 0) {
        throw 'Offline audit failed for the spike recovery artifact.'
    }
    $audit = Get-Content -LiteralPath $auditPath -Raw | ConvertFrom-Json
    if ($audit.audit.observed_outcome -ne 'FAIL') {
        throw 'Offline audit did not preserve the campaign failure.'
    }
    Write-Output "Campaign evidence: $campaignPath"
    Write-Output "Offline audit: $auditPath"
    Write-Output "Spike outcome: $($spike.run.outcome); recovery outcome: $($recovery.run.outcome); capacity assessment: $($audit.state)."
    Write-Output "Completed trials: $($campaign.trials.Count) of $($campaign.repeats); generator exit code: $campaignExit"
}
finally {
    if ($started) {
        & docker @compose down --remove-orphans
        if ($LASTEXITCODE -ne 0) {
            Write-Warning 'Isolated spike recovery stack cleanup reported an error.'
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
