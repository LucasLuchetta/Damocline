$ErrorActionPreference = 'Stop'
$originalLocation = Get-Location
$previousLabPort = [Environment]::GetEnvironmentVariable('DAMOCLINE_LAB_PORT', 'Process')
$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path

$envFile = Join-Path $PSScriptRoot '.env'
if (-not (Test-Path -LiteralPath $envFile)) {
    throw 'Create lab/.env as documented in lab/README.md before running the overload check.'
}

$stamp = [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssfffZ')
$campaignName = "target-overload-$stamp.json"
$campaignPath = Join-Path $PSScriptRoot "artifacts/$campaignName"
$auditPath = Join-Path $PSScriptRoot "artifacts/target-overload-$stamp-audit.json"
$reconciliationPath = Join-Path $PSScriptRoot "artifacts/target-overload-$stamp-reconciliation.json"
$calibrationName = "generator-calibration-$stamp.json"
$calibrationPath = Join-Path $PSScriptRoot "artifacts/$calibrationName"
foreach ($path in @($campaignPath, $auditPath, $reconciliationPath, $calibrationPath)) {
    if (Test-Path -LiteralPath $path) {
        throw "Refusing to overwrite existing evidence: $path"
    }
}

$portProbe = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
$portProbe.Start()
$env:DAMOCLINE_LAB_PORT = [string]$portProbe.LocalEndpoint.Port
$portProbe.Stop()
$project = 'damocline-overload-' + [guid]::NewGuid().ToString('N').Substring(0, 12)
$compose = @('compose', '--env-file', 'lab/.env', '-p', $project, '-f', 'lab/compose.yaml')
$started = $false

try {
    Set-Location $repositoryRoot
    $started = $true
    & docker @compose up --build -d target sensor
    if ($LASTEXITCODE -ne 0) {
        throw 'Could not start the isolated synthetic target and sensor.'
    }

    & docker @compose --profile run run --rm --no-deps --entrypoint /usr/local/bin/damocline-calibrate generator `
        --out "/reports/$calibrationName"
    $calibrationExit = $LASTEXITCODE
    if (-not (Test-Path -LiteralPath $calibrationPath)) {
        throw "The Linux generator preflight did not write its expected artifact (exit $calibrationExit)."
    }
    $calibration = Get-Content -LiteralPath $calibrationPath -Raw | ConvertFrom-Json
    if ($calibrationExit -ne 0 -or $calibration.preflight.status -ne 'REFERENCE_SMOKE_PASSED') {
        throw 'The Linux generator preflight did not pass; no target campaign will run.'
    }

    & docker @compose --profile run run --rm --no-deps generator `
        campaign --config /lab/scenario-overload.json `
        --warmup 0s --observation 10s --recovery 10s --repeats 2 `
        --metrics-endpoint http://127.0.0.1:8080/metrics --metrics-interval 2s `
        --host-sensor-endpoint http://127.0.0.1:9467/v1/host `
        --host-sensor-token-env DAMOCLINE_SENSOR_TOKEN --host-sensor-interval 2s `
        --calibration "/reports/$calibrationName" `
        --authorized --out "/reports/$campaignName"
    $campaignExit = $LASTEXITCODE
    if ($campaignExit -gt 1) {
        throw "The bounded generator command failed unexpectedly with exit code $campaignExit."
    }

    if (-not (Test-Path -LiteralPath $campaignPath)) {
        throw 'The campaign did not write its expected local artifact.'
    }
    $campaign = Get-Content -LiteralPath $campaignPath -Raw | ConvertFrom-Json
    $observations = @($campaign.trials | ForEach-Object { $_.phases } | Where-Object { $_.name -eq 'observation' })
    if ($campaign.outcome -ne 'FAIL' -or $observations.Count -lt 1) {
        throw "Expected an observed target failure, got $($campaign.outcome)."
    }

    $independentComparisons = @()
    $trialNumber = 0
    foreach ($phase in $observations) {
        $trialNumber++
        $run = $phase.run
        $generator = $run.generator
        $statusCodes = $run.metrics.http_diagnostics.status_codes
        $status503 = [int]$statusCodes.'503'
        if ($phase.evidence_state -ne 'FAIL_OBSERVED' -or $status503 -le 0 -or $run.error_rate -le $run.config.max_error_rate) {
            throw 'The overload did not retain an observed HTTP 503/SLO breach.'
        }
        if ($run.arrivals_dropped -ne 0 -or ($null -ne $run.arrivals_cancelled_before_admission -and $run.arrivals_cancelled_before_admission -ne 0)) {
            throw 'The overload had dropped or cancelled planned work; target failure is not isolated.'
        }
        if (-not $generator.cpu_capacity_known -or -not $generator.process_cpu_valid -or $generator.process_samples -lt 10 -or $generator.mean_process_cpu_utilization_ratio -ge 0.9) {
            throw 'Generator headroom was unknown, invalid, undersampled, or near saturation.'
        }
        if ($phase.telemetry.coverage -ne 'COMPLETE') {
            throw 'Target exporter telemetry is not complete.'
        }
        foreach ($outcome in @(
            [pscustomobject]@{ label = 'completed'; status = 200 }
            [pscustomobject]@{ label = 'rejected'; status = 503 }
        )) {
            $values = @($phase.telemetry.snapshots | ForEach-Object {
                $_.samples | Where-Object { $_.name -eq 'lab_requests_total' -and $_.labels.outcome -eq $outcome.label } | Select-Object -ExpandProperty value
            })
            if ($values.Count -lt 2) {
                throw "Target exporter has fewer than two $($outcome.label) counter samples."
            }
            for ($i = 1; $i -lt $values.Count; $i++) {
                if ([double]$values[$i] -lt [double]$values[$i - 1]) {
                    throw "Target $($outcome.label) counter reset during the observation; its phase delta is not comparable."
                }
            }
            $delta = [long]([double]$values[-1] - [double]$values[0])
            $statusProperty = $statusCodes.PSObject.Properties[[string]$outcome.status]
            $clientCount = if ($null -eq $statusProperty) { 0 } else { [int]$statusProperty.Value }
            if ($delta -gt $clientCount) {
                throw "Independent target $($outcome.label) counter delta ($delta) exceeds client-observed HTTP $($outcome.status) responses ($clientCount)."
            }
            if ($outcome.label -eq 'rejected' -and $delta -le 0) {
                throw 'Target exporter telemetry did not show a rejected-request counter increase.'
            }
            $independentComparisons += [pscustomobject]@{
                trial = $trialNumber
                phase = $phase.name
                telemetry_coverage = $phase.telemetry.coverage
                counter_name = 'lab_requests_total'
                counter_label_schema = "outcome=$($outcome.label)"
                expected_http_status = $outcome.status
                counter_samples = $values.Count
                counter_first_observed = [long]$values[0]
                counter_last_observed = [long]$values[-1]
                independent_counter_delta = $delta
                client_responses_with_expected_status = $clientCount
                difference_not_captured_between_metric_snapshots = $clientCount - $delta
                comparison = 'COUNTER_DELTA_WITHIN_MATCHING_HTTP_STATUS_COUNT'
            }
        }
    }

    $reconciliation = [pscustomobject]@{
        schema_version = 1
        campaign_file = $campaignName
        scenario = $campaign.scenario
        target_class = 'bounded synthetic local target'
        comparisons = $independentComparisons
        interpretation = 'For each counter label, the independently exported monotonic delta is no greater than the client-observed count of its fixture-defined HTTP status (completed=200, rejected=503). Remaining differences are not resolved by periodic snapshots and are reported rather than treated as exact reconciliation.'
    }
    $reconciliation | ConvertTo-Json -Depth 8 | Set-Content -Encoding utf8 -LiteralPath $reconciliationPath

    go run ./cmd/damocline audit --campaign $campaignPath --out $auditPath
    if ($LASTEXITCODE -ne 0) {
        throw 'Offline audit failed for the overload artifact.'
    }
    $audit = Get-Content -LiteralPath $auditPath -Raw | ConvertFrom-Json
    if ($audit.state -ne 'INCONCLUSIVE' -or $audit.audit.observed_outcome -ne 'FAIL' -or @($audit.audit.observed_failing_levels) -notcontains $campaign.base_users) {
		throw "Expected an inconclusive capacity verdict retaining the observed overload failure, got $($audit.state)/$($audit.audit.observed_outcome)."
    }

    Write-Output "Campaign evidence: $campaignPath"
    Write-Output "Offline audit: $auditPath"
    Write-Output "Independent HTTP/exporter comparison: $reconciliationPath"
    Write-Output "Same-container Linux generator preflight: $calibrationPath ($($calibration.preflight.status))"
    Write-Output 'HTTP 503/SLO breach observed; completed/rejected counter deltas were bounded by matching HTTP statuses; unsampled differences are explicit; delivery complete; generator headroom valid.'
    Write-Output 'Capacity audit: INCONCLUSIVE because measurement quality/stationarity do not support a capacity boundary.'
    Write-Output "Completed trials: $($campaign.trials.Count) of $($campaign.repeats); generator exit code: $campaignExit"
}
finally {
    if ($started) {
        & docker @compose down --remove-orphans
        if ($LASTEXITCODE -ne 0) {
            Write-Warning 'Isolated overload stack cleanup reported an error.'
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
