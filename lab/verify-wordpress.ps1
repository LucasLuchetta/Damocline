param(
    [ValidateRange(0, 120)]
    [int]$WarmupSeconds = 30,
    [ValidateRange(1, 50)]
    [int]$Users = 2,
    [ValidateRange(1, 100)]
    [int]$MaxRps = 100,
    [ValidateRange(10, 180)]
    [int]$RecoverySeconds = 30,
    [ValidateSet('0.75', '1.0', '1.5')]
    [string]$WordPressCpuLimit = '0.75'
)

$ErrorActionPreference = 'Stop'
$originalLocation = Get-Location
$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$envFile = Join-Path $PSScriptRoot '.env.wordpress'
if (-not (Test-Path -LiteralPath $envFile)) {
    throw 'Create lab/.env.wordpress as documented in lab/README.md before running the WordPress lab.'
}

$stamp = [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssfffZ')
$calibrationName = "wordpress-generator-calibration-$stamp.json"
$campaignName = "wordpress-campaign-$stamp.json"
$auditName = "wordpress-audit-$stamp.json"
$apacheName = "wordpress-apache-observations-$stamp.json"
$dockerStatsName = "wordpress-docker-stats-$stamp.json"
$scenarioName = "wordpress-scenario-$stamp.json"
$artifactPaths = @(
    (Join-Path $PSScriptRoot "artifacts/$calibrationName"),
    (Join-Path $PSScriptRoot "artifacts/$campaignName"),
    (Join-Path $PSScriptRoot "artifacts/$auditName"),
    (Join-Path $PSScriptRoot "artifacts/$apacheName"),
    (Join-Path $PSScriptRoot "artifacts/$dockerStatsName"),
    (Join-Path $PSScriptRoot "artifacts/$scenarioName")
)
foreach ($path in $artifactPaths) {
    if (Test-Path -LiteralPath $path) {
        throw "Refusing to overwrite existing evidence: $path"
    }
}

$portProbe = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
$portProbe.Start()
$env:DAMOCLINE_WORDPRESS_PORT = [string]$portProbe.LocalEndpoint.Port
$env:DAMOCLINE_WORDPRESS_CPU_LIMIT = $WordPressCpuLimit
$portProbe.Stop()
$project = 'damocline-wordpress-' + [guid]::NewGuid().ToString('N').Substring(0, 12)
$tlsDirectory = Join-Path $PSScriptRoot "artifacts/.wordpress-tls-$stamp"
$compose = @('compose', '--env-file', 'lab/.env.wordpress', '-p', $project, '-f', 'lab/compose.wordpress.yaml')
$composeAttempted = $false
$dockerStatsJob = $null

function Convert-DockerStatsBytes([string]$value) {
    if ($value -notmatch '^\s*(?<number>[0-9]+(?:\.[0-9]+)?)\s*(?<unit>B|kB|KB|KiB|MB|MiB|GB|GiB)\s*$') {
        return $null
    }
    $number = [double]::Parse($Matches.number, [Globalization.CultureInfo]::InvariantCulture)
    $scale = switch ($Matches.unit) {
        'B' { 1L }
        { $_ -in 'kB', 'KB' } { 1000L }
        'KiB' { 1024L }
        'MB' { 1000000L }
        'MiB' { 1048576L }
        'GB' { 1000000000L }
        'GiB' { 1073741824L }
    }
    return [long][math]::Round($number * $scale)
}

function Get-WordPressCgroupSnapshot([string]$ProjectName) {
    $containerIds = @(& docker ps --quiet --filter "label=com.docker.compose.project=$ProjectName" 2>$null)
    if ($LASTEXITCODE -ne 0 -or $containerIds.Count -eq 0) {
        return @()
    }
    $inspectLines = @(& docker inspect @containerIds 2>$null)
    if ($LASTEXITCODE -ne 0) {
        return @()
    }
    try {
        $containers = ConvertFrom-Json -InputObject ($inspectLines -join [Environment]::NewLine) -ErrorAction Stop
    }
    catch {
        return @()
    }
    @(
        foreach ($container in $containers) {
            $service = $container.Config.Labels.'com.docker.compose.service'
            if ($service -notin @('wordpress', 'database')) {
                continue
            }
            $lines = @(& docker exec $container.Id sh -c 'echo __cpu_max__ $(cat /sys/fs/cgroup/cpu.max); echo __cpu_stat__; cat /sys/fs/cgroup/cpu.stat; echo __memory_max__ $(cat /sys/fs/cgroup/memory.max); echo __memory_current__ $(cat /sys/fs/cgroup/memory.current); echo __memory_events__; cat /sys/fs/cgroup/memory.events' 2>$null)
            if ($LASTEXITCODE -ne 0) {
                continue
            }
            $cpuQuota = $null
            $cpuPeriod = $null
            $memoryLimit = $null
            $memoryCurrent = $null
            $cpuCounters = @{}
            $memoryCounters = @{}
            $section = ''
            foreach ($line in $lines) {
                if ([string]$line -match '^__cpu_max__\s+(?<quota>\S+)\s+(?<period>[0-9]+)$') {
                    if ($Matches.quota -ne 'max') { $cpuQuota = [long]$Matches.quota }
                    $cpuPeriod = [long]$Matches.period
                    $section = ''
                    continue
                }
                if ([string]$line -match '^__memory_max__\s+(?<value>[0-9]+|max)$') {
                    if ($Matches.value -ne 'max') { $memoryLimit = [long]$Matches.value }
                    $section = ''
                    continue
                }
                if ([string]$line -match '^__memory_current__\s+(?<value>[0-9]+)$') {
                    $memoryCurrent = [long]$Matches.value
                    $section = ''
                    continue
                }
                if ([string]$line -eq '__cpu_stat__') { $section = 'cpu'; continue }
                if ([string]$line -eq '__memory_events__') { $section = 'memory'; continue }
                if ([string]$line -match '^(?<name>[a-z_]+)\s+(?<value>[0-9]+)$') {
                    if ($section -eq 'cpu') { $cpuCounters[$Matches.name] = [long]$Matches.value }
                    if ($section -eq 'memory') { $memoryCounters[$Matches.name] = [long]$Matches.value }
                }
            }
            if ($null -ne $cpuPeriod -and $null -ne $memoryLimit -and $cpuCounters.ContainsKey('usage_usec')) {
                [pscustomobject]@{
                    service = $service
                    cpu_quota_micros = $cpuQuota
                    cpu_period_micros = $cpuPeriod
                    cpu_usage_usec = $cpuCounters.usage_usec
                    cpu_nr_periods = $cpuCounters.nr_periods
                    cpu_nr_throttled = $cpuCounters.nr_throttled
                    cpu_throttled_usec = $cpuCounters.throttled_usec
                    memory_limit_bytes = $memoryLimit
                    memory_current_bytes = $memoryCurrent
                    memory_max_events = $memoryCounters.max
                    memory_oom_events = $memoryCounters.oom
                    memory_oom_kill_events = $memoryCounters.oom_kill
                }
            }
        }
    )
}

function Invoke-WordPressRoute([string]$Uri, [hashtable]$Headers) {
    $lastError = $null
    for ($attempt = 1; $attempt -le 10; $attempt++) {
        try {
            return Invoke-WebRequest -Uri $Uri -Headers $Headers -UseBasicParsing -TimeoutSec 5
        }
        catch {
            $lastError = $_
            Start-Sleep -Seconds 1
        }
    }
    throw "WordPress route did not become available after 10 attempts: $Uri. Last error: $($lastError.Exception.Message)"
}

try {
    Set-Location $repositoryRoot
    New-Item -ItemType Directory -Path $tlsDirectory | Out-Null
    $tlsVolumePath = [System.IO.Path]::GetFullPath($tlsDirectory).Replace('\', '/')
    $env:DAMOCLINE_WORDPRESS_SENSOR_TLS_DIR = $tlsVolumePath
    & docker run --rm --mount "type=bind,source=$tlsDirectory,target=/out" alpine:3.22 sh -c 'set -e; apk add --no-cache openssl >/dev/null; openssl req -x509 -newkey rsa:2048 -sha256 -nodes -days 2 -keyout /out/sensor-key.pem -out /out/sensor-cert.pem -subj /CN=wordpress -addext subjectAltName=DNS:wordpress >/dev/null 2>&1; chmod 644 /out/sensor-key.pem'
    if ($LASTEXITCODE -ne 0) {
        throw 'Could not generate the private, short-lived sensor certificate for the isolated Compose network.'
    }
    $composeAttempted = $true
    & docker @compose up --build -d database wordpress
    if ($LASTEXITCODE -ne 0) {
        throw 'Could not start the isolated WordPress application.'
    }

    $adminPassword = [guid]::NewGuid().ToString('N') + 'Aa9!'
    & docker @compose --profile tools run --rm wordpress-cli wp core install `
        '--url=http://127.0.0.1' `
        '--title=Synthetic WordPress capacity fixture' `
        '--admin_user=local_fixture_admin' `
        "--admin_password=$adminPassword" `
        '--admin_email=fixture@example.invalid' '--skip-email'
    if ($LASTEXITCODE -ne 0) {
        throw 'Could not initialize the local-only WordPress fixture.'
    }
    Remove-Variable adminPassword

    & docker @compose --profile tools run --rm wordpress-cli wp rewrite structure '/%postname%/'
    if ($LASTEXITCODE -ne 0) {
        throw 'Could not enable WordPress rewrite rules for the read-only REST workflow.'
    }

    & docker @compose --profile tools run --rm wordpress-cli wp post create `
        '--post_title=Synthetic capacity test post' `
        '--post_content=Generated local-only fixture content.' '--post_status=publish'
    if ($LASTEXITCODE -ne 0) {
        throw 'Could not create the synthetic read-only workflow fixture.'
    }

    & docker @compose up -d sensor
    if ($LASTEXITCODE -ne 0) {
        throw 'Could not start the Linux sensor in the finalized WordPress network namespace.'
    }

    $routeHeaders = @{ Host = "127.0.0.1:$env:DAMOCLINE_WORDPRESS_PORT" }
    $homeResponse = Invoke-WordPressRoute "http://127.0.0.1:$env:DAMOCLINE_WORDPRESS_PORT/" $routeHeaders
    $posts = Invoke-WordPressRoute "http://127.0.0.1:$env:DAMOCLINE_WORDPRESS_PORT/wp-json/wp/v2/posts?per_page=5" $routeHeaders
    $databaseMetrics = Invoke-WordPressRoute "http://127.0.0.1:$env:DAMOCLINE_WORDPRESS_PORT/damocline-metrics.php" $routeHeaders
    if ($homeResponse.StatusCode -ne 200 -or $posts.StatusCode -ne 200 -or
        $databaseMetrics.StatusCode -ne 200 -or $databaseMetrics.Content -notmatch '(?m)^damocline_mariadb_up 1(?:\.0+)?$') {
        throw 'A read-only WordPress workflow route did not return HTTP 200.'
    }

    & docker @compose --profile run run --build --rm --no-deps `
        --entrypoint /usr/local/bin/damocline-calibrate generator --out "/reports/$calibrationName"
    $calibrationExit = $LASTEXITCODE
    $calibrationPath = $artifactPaths[0]
    if (-not (Test-Path -LiteralPath $calibrationPath)) {
        throw "The bounded Linux generator preflight did not write its artifact (exit $calibrationExit)."
    }
    $calibration = Get-Content -LiteralPath $calibrationPath -Raw | ConvertFrom-Json
    if ($calibrationExit -ne 0 -or $calibration.preflight.status -ne 'REFERENCE_SMOKE_PASSED') {
        throw 'The bounded generator preflight did not pass; no application campaign will run.'
    }

    $campaignScenario = Get-Content -LiteralPath (Join-Path $PSScriptRoot 'scenario-wordpress.json') -Raw | ConvertFrom-Json
    $campaignScenario.base_url = 'http://wordpress'
    $campaignScenario.users = $Users
    $campaignScenario.max_rps = $MaxRps
    if ($WordPressCpuLimit -ne '0.75') {
        $campaignScenario.environment = "isolated-docker-compose-linux-wp-cpu-$WordPressCpuLimit"
        $campaignScenario.application_revision = "wordpress-7.1.3-php8.3-apache-cpu$WordPressCpuLimit"
    }
    [System.IO.File]::WriteAllText(
        $artifactPaths[5],
        ($campaignScenario | ConvertTo-Json -Depth 12),
        [System.Text.UTF8Encoding]::new($false)
    )
    $cgroupStart = @(Get-WordPressCgroupSnapshot -ProjectName $project)

    $dockerStatsJob = Start-Job -ArgumentList $project, $artifactPaths[4] -ScriptBlock {
        param([string]$projectName, [string]$samplePath)
        $samples = [System.Collections.Generic.List[object]]::new()
        $deadline = [DateTime]::UtcNow.AddMinutes(8)
        while ([DateTime]::UtcNow -lt $deadline) {
            $timer = [Diagnostics.Stopwatch]::StartNew()
            $containerIds = @(& docker ps --quiet --filter "label=com.docker.compose.project=$projectName" 2>$null)
            if ($LASTEXITCODE -eq 0 -and $containerIds.Count -gt 0) {
                $jsonRows = @(& docker stats --no-stream --format '{{json .}}' @containerIds 2>$null)
                if ($LASTEXITCODE -eq 0) {
                    $timer.Stop()
                    $sampledAt = [DateTime]::UtcNow.ToString('o')
                    foreach ($jsonRow in $jsonRows) {
                        try { $row = [string]$jsonRow | ConvertFrom-Json -ErrorAction Stop } catch { continue }
                        if ([string]$row.Name -match '-(?<service>wordpress|database|sensor)-\d+$') {
                            $serviceName = $Matches.service
                        }
                        elseif ([string]$row.Name -match '-generator-run-') {
                            $serviceName = 'generator'
                        }
                        else {
                            continue
                        }
                        if ($serviceName) {
                            $samples.Add([pscustomobject]@{
                                sampled_at = $sampledAt
                                service = $serviceName
                                cpu_percent_text = [string]$row.CPUPerc
                                memory_usage_text = [string]$row.MemUsage
                                memory_percent_text = [string]$row.MemPerc
                                collection_ms = [math]::Round($timer.Elapsed.TotalMilliseconds, 3)
                            })
                        }
                    }
                    $samples | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $samplePath -Encoding utf8
                }
            }
            Start-Sleep -Milliseconds 1000
        }
    }

    & docker @compose --profile run run --rm --no-deps generator `
        campaign --config "/reports/$scenarioName" `
        --warmup "${WarmupSeconds}s" --observation 60s --recovery "${RecoverySeconds}s" --repeats 2 --budget 7m `
        --allow-insecure-http `
        --app-metrics-endpoint http://wordpress/damocline-metrics.php --app-metrics-interval 5s `
        --host-sensor-endpoint https://wordpress:9467/v1/host `
        --host-sensor-token-env DAMOCLINE_SENSOR_TOKEN --host-sensor-interval 2s `
        --calibration "/reports/$calibrationName" `
        --authorized --out "/reports/$campaignName"
    $campaignExit = $LASTEXITCODE
    if ($campaignExit -gt 1) {
        throw "The bounded WordPress campaign failed unexpectedly with exit code $campaignExit."
    }

    Stop-Job -Job $dockerStatsJob -ErrorAction SilentlyContinue
    Receive-Job -Job $dockerStatsJob -ErrorAction SilentlyContinue | Out-Null
    Remove-Job -Job $dockerStatsJob -Force -ErrorAction SilentlyContinue
    $dockerStatsJob = $null
    if (Test-Path -LiteralPath $artifactPaths[4]) {
        $dockerStatsSamples = ConvertFrom-Json -InputObject (Get-Content -LiteralPath $artifactPaths[4] -Raw)
    }
    else {
        $dockerStatsSamples = @()
    }
    $cgroupEnd = @(Get-WordPressCgroupSnapshot -ProjectName $project)

    $campaignPath = $artifactPaths[1]
    $auditPath = $artifactPaths[2]
    if (-not (Test-Path -LiteralPath $campaignPath)) {
        throw 'The campaign did not write its expected local artifact.'
    }
    $campaign = Get-Content -LiteralPath $campaignPath -Raw | ConvertFrom-Json
    $observations = @($campaign.trials | ForEach-Object { $_.phases } | Where-Object { $_.name -eq 'observation' })
    $requiredMetricsPhases = @($campaign.trials | ForEach-Object { $_.phases } | Where-Object { $_.name -in @('observation', 'recovery') })
    if ($observations.Count -lt 1) {
        throw 'The WordPress campaign has no saved observation phase.'
    }
    foreach ($phase in $requiredMetricsPhases) {
        $appMetrics = $phase.run.application_metrics
        if ($null -eq $appMetrics -or $appMetrics.failed_collections -ne 0 -or $appMetrics.samples.Count -lt 1) {
            throw 'Application metrics are missing, incomplete, or have failed collections in a required phase.'
        }
        foreach ($snapshot in $appMetrics.samples) {
            $dbUp = @($snapshot.samples | Where-Object { $_.name -eq 'damocline_mariadb_up' })
            if ($dbUp.Count -ne 1 -or $dbUp[0].value -ne 1) {
                throw 'A required-phase status snapshot does not report exactly one healthy MariaDB query.'
            }
        }
        if ($phase.host_telemetry.coverage -ne 'COMPLETE') {
            throw 'Linux host telemetry is incomplete for a required phase.'
        }
    }
    foreach ($phase in $observations) {
        if ($phase.run.arrivals_dropped -ne 0 -or
            ($null -ne $phase.run.arrivals_cancelled_before_admission -and $phase.run.arrivals_cancelled_before_admission -ne 0)) {
            throw 'The generator dropped or cancelled planned work before admission.'
        }
        $generator = $phase.run.generator
        if (-not $generator.cpu_capacity_known -or -not $generator.process_cpu_valid -or
            $generator.process_samples -lt 10 -or $generator.mean_process_cpu_utilization_ratio -ge 0.9) {
            throw 'Generator capacity or process headroom was unknown, invalid, undersampled, or near saturation.'
        }
    }

    & go run ./cmd/damocline audit --campaign $campaignPath --out $auditPath
    if ($LASTEXITCODE -ne 0) {
        throw 'Offline audit failed for the WordPress campaign artifact.'
    }
    $audit = Get-Content -LiteralPath $auditPath -Raw | ConvertFrom-Json
    if ($audit.state -eq 'INVALID_EVIDENCE' -or $audit.audit.state -eq 'INVALID_EVIDENCE') {
        throw 'Offline audit rejected the WordPress campaign artifact.'
    }

    $apacheLines = @(& docker @compose exec -T wordpress cat /tmp/damocline-http.log 2>&1)
    if ($LASTEXITCODE -ne 0) {
        throw 'Could not read the isolated WordPress Apache access log.'
    }
    $apacheRows = @(
        foreach ($line in $apacheLines) {
            if ([string]$line -match '^DAMOCLINE_HTTP (?<start>[0-9]+) (?<duration>[0-9]+) (?<status>[0-9]{3}) (?<method>[A-Z]+) (?<path>/[^ ]*)$') {
                $route = switch ($Matches.path) {
                    { $_ -eq '/' -or $_ -eq '/index.php' } { '/' }
                    '/wp-json/wp/v2/posts' { '/wp-json/wp/v2/posts' }
                    default { $null }
                }
                if ($null -ne $route) {
                    [pscustomobject]@{
                        started_at_unix_us = [long]$Matches.start
                        duration_us = [long]$Matches.duration
                        ended_at_unix_us = [long]$Matches.start + [long]$Matches.duration
                        status = [int]$Matches.status
                        method = $Matches.method
                        route = $route
                    }
                }
            }
        }
    )
    $apacheWindows = @()
    foreach ($phase in $observations) {
        $phaseStart = [DateTimeOffset]::Parse($phase.run.started_at)
        for ($windowIndex = 0; $windowIndex -lt $phase.run.windows.Count; $windowIndex++) {
            $window = $phase.run.windows[$windowIndex]
            $windowStart = $phaseStart.AddSeconds($window.start_seconds)
            $windowEnd = $phaseStart.AddSeconds($window.end_seconds)
            $windowRows = @(
                $apacheRows | Where-Object {
                    $requestAt = [DateTimeOffset]::FromUnixTimeMilliseconds([long][math]::Floor($_.ended_at_unix_us / 1000)).AddTicks(($_.ended_at_unix_us % 1000) * 10)
                    $requestAt -ge $windowStart -and $requestAt -lt $windowEnd
                }
            )
            foreach ($route in @('/', '/wp-json/wp/v2/posts')) {
                $routeRows = @($windowRows | Where-Object route -eq $route)
                $stepName = if ($route -eq '/') { 'browse_homepage' } else { 'list_recent_posts' }
                $clientStep = $window.steps | Where-Object name -eq $stepName
                $durations = @($routeRows | ForEach-Object duration_us | Sort-Object)
                $statusCounts = @{}
                foreach ($row in $routeRows) {
                    $key = [string]$row.status
                    if (-not $statusCounts.ContainsKey($key)) { $statusCounts[$key] = 0 }
                    $statusCounts[$key]++
                }
                $p50 = $null
                $p95 = $null
                $maximum = $null
                if ($durations.Count -gt 0) {
                    $p50 = [math]::Round($durations[[math]::Ceiling($durations.Count * 0.50) - 1] / 1000.0, 3)
                    $p95 = [math]::Round($durations[[math]::Ceiling($durations.Count * 0.95) - 1] / 1000.0, 3)
                    $maximum = [math]::Round($durations[-1] / 1000.0, 3)
                }
                $apacheWindows += [pscustomobject]@{
                    phase_started_at = $phase.run.started_at
                    window_start_seconds = $window.start_seconds
                    window_end_seconds = $window.end_seconds
                    route = $route
                    requests = $routeRows.Count
                    client_request_count = $clientStep.metrics.total
                    server_client_count_delta = $routeRows.Count - $clientStep.metrics.total
                    client_p95_ms = $clientStep.metrics.p95_ms
                    status_counts = $statusCounts
                    server_duration_p50_ms = $p50
                    server_duration_p95_ms = $p95
                    server_duration_max_ms = $maximum
                }
            }
        }
    }
    if (@($apacheWindows | Where-Object requests -le 0).Count -gt 0) {
        $routeSummary = @($apacheRows | Group-Object route | ForEach-Object { "$($_.Name)=$($_.Count)" }) -join ', '
        $timeSummary = 'no parsed timestamps'
        if ($apacheRows.Count -gt 0) {
            $firstLogMs = [long][math]::Floor(($apacheRows.started_at_unix_us | Measure-Object -Minimum).Minimum / 1000)
            $lastLogMs = [long][math]::Floor(($apacheRows.started_at_unix_us | Measure-Object -Maximum).Maximum / 1000)
            $timeSummary = '{0} .. {1}' -f [DateTimeOffset]::FromUnixTimeMilliseconds($firstLogMs).ToString('o'), [DateTimeOffset]::FromUnixTimeMilliseconds($lastLogMs).ToString('o')
        }
        $missing = @($apacheWindows | Where-Object requests -le 0 | ForEach-Object { "$($_.route)@$($_.window_start_seconds)-$($_.window_end_seconds)s" }) -join ', '
        throw "Apache route/window evidence is incomplete. Read $($apacheLines.Count) lines; parsed $($apacheRows.Count) allowlisted rows ($routeSummary); timestamps $timeSummary; empty windows: $missing"
    }
    $apacheEvidence = [pscustomobject]@{
        schema_version = 1
        source = 'Apache mod_log_config access log'
        scope = 'Allowlisted route and status only; no query string, headers, cookies, or bodies.'
        timing = 'Windows use estimated server completion time (Apache start time plus duration) to match client completion-bucket semantics; wall-clock alignment and body-read tail are not calibrated.'
        campaign_file = $campaignName
        campaign_evidence_id = $audit.audit.evidence_id
        phases = @(
            foreach ($phase in $observations) {
                [pscustomobject]@{
                    phase = $phase.name
                    started_at = $phase.run.started_at
                    observation_seconds = $phase.run.observation_seconds
                    windows = @($apacheWindows | Where-Object phase_started_at -eq $phase.run.started_at)
                }
            }
        )
    }

    $dockerStatsRows = @(
        foreach ($sample in $dockerStatsSamples) {
            $cpuValue = 0.0
            $cpuText = ([string]$sample.cpu_percent_text).Trim().TrimEnd('%').Trim()
            if (-not [double]::TryParse($cpuText, [Globalization.NumberStyles]::Float, [Globalization.CultureInfo]::InvariantCulture, [ref]$cpuValue)) {
                $cpuValue = $null
            }
            $memoryPair = @($sample.memory_usage_text -split '\s*/\s*', 2)
            $memoryUsed = $null
            $memoryLimit = $null
            if ($memoryPair.Count -eq 2) {
                $memoryUsed = Convert-DockerStatsBytes $memoryPair[0]
                $memoryLimit = Convert-DockerStatsBytes $memoryPair[1]
            }
            $sampledAt = [DateTimeOffset]::MinValue
            if (-not [DateTimeOffset]::TryParse([string]$sample.sampled_at, [Globalization.CultureInfo]::InvariantCulture, [Globalization.DateTimeStyles]::AssumeUniversal, [ref]$sampledAt)) {
                throw "Invalid Docker stats timestamp: $($sample.sampled_at)"
            }
            [pscustomobject]@{
                sampled_at = $sampledAt
                service = $sample.service
                cpu_percent = $cpuValue
                memory_used_bytes = $memoryUsed
                memory_limit_bytes = $memoryLimit
                memory_percent_text = $sample.memory_percent_text
                collection_ms = $sample.collection_ms
            }
        }
    )
    $dockerStatsWindows = @()
    foreach ($phase in $observations) {
        $phaseStart = [DateTimeOffset]::Parse($phase.run.started_at)
        foreach ($window in $phase.run.windows) {
            $windowStart = $phaseStart.AddSeconds($window.start_seconds)
            $windowEnd = $phaseStart.AddSeconds($window.end_seconds)
            foreach ($service in @('wordpress', 'database')) {
                $serviceRows = @($dockerStatsRows | Where-Object {
                    $_.service -eq $service -and $_.sampled_at -ge $windowStart -and $_.sampled_at -lt $windowEnd
                })
                if ($serviceRows.Count -lt 3) {
                    throw "Docker Engine stats are incomplete for $service in observation window $($window.start_seconds)-$($window.end_seconds)s."
                }
                $cpuValues = @($serviceRows | Where-Object { $null -ne $_.cpu_percent } | ForEach-Object cpu_percent | Sort-Object)
                $memoryValues = @($serviceRows | Where-Object { $null -ne $_.memory_used_bytes } | ForEach-Object memory_used_bytes)
                $limitValues = @($serviceRows | Where-Object { $null -ne $_.memory_limit_bytes } | ForEach-Object memory_limit_bytes | Select-Object -Unique)
                if ($cpuValues.Count -lt 3 -or $memoryValues.Count -lt 3 -or $limitValues.Count -ne 1) {
                    $memoryExamples = @($serviceRows | Select-Object -First 3 -ExpandProperty memory_percent_text) -join ', '
                    $usageExamples = @($dockerStatsSamples | Where-Object service -eq $service | Select-Object -First 3 -ExpandProperty memory_usage_text) -join ', '
                    throw "Docker Engine evidence is incomplete for $service in window $($window.start_seconds)-$($window.end_seconds)s: rows=$($serviceRows.Count), CPU=$($cpuValues.Count), parsed_memory=$($memoryValues.Count), distinct_limits=$($limitValues.Count), MemUsage examples=[$usageExamples], MemPerc examples=[$memoryExamples]."
                }
                $p50Index = [math]::Floor(($cpuValues.Count - 1) * 0.50)
                $p95Index = [math]::Ceiling($cpuValues.Count * 0.95) - 1
                $collectionValues = @($serviceRows | ForEach-Object collection_ms)
                $dockerStatsWindows += [pscustomobject]@{
                    phase_started_at = $phase.run.started_at
                    window_start_seconds = $window.start_seconds
                    window_end_seconds = $window.end_seconds
                    service = $service
                    samples = $serviceRows.Count
                    cpu_percent_p50 = [math]::Round($cpuValues[$p50Index], 3)
                    cpu_percent_p95 = [math]::Round($cpuValues[$p95Index], 3)
                    cpu_percent_max = [math]::Round(($cpuValues | Measure-Object -Maximum).Maximum, 3)
                    memory_used_max_bytes = [long](($memoryValues | Measure-Object -Maximum).Maximum)
                    memory_limit_bytes = [long]$limitValues[0]
                    stats_collection_max_ms = [math]::Round(($collectionValues | Measure-Object -Maximum).Maximum, 3)
                }
            }
        }
    }
    $eventStart = [DateTimeOffset]::Parse($observations[0].run.started_at)
    $eventEnd = [DateTimeOffset]::UtcNow
    $targetContainerIds = @(& docker ps --quiet --filter "label=com.docker.compose.project=$project" 2>$null)
    $inspectCoverage = 'INCOMPLETE'
    $containerStates = @()
    if ($LASTEXITCODE -eq 0 -and $targetContainerIds.Count -gt 0) {
        $inspectLines = @(& docker inspect @targetContainerIds 2>$null)
        if ($LASTEXITCODE -eq 0) {
            try {
                $inspectObjects = ConvertFrom-Json -InputObject ($inspectLines -join [Environment]::NewLine) -ErrorAction Stop
                $containerStates = @(
                    foreach ($container in $inspectObjects) {
                        $service = $container.Config.Labels.'com.docker.compose.service'
                        if ($service -in @('wordpress', 'database')) {
                            [pscustomobject]@{
                                service = $service
                                status = $container.State.Status
                                restart_count = $container.RestartCount
                                oom_killed = $container.State.OOMKilled
                                exit_code = $container.State.ExitCode
                                started_at = $container.State.StartedAt
                                finished_at = $container.State.FinishedAt
                            }
                        }
                    }
                )
                if (@($containerStates | Select-Object -ExpandProperty service -Unique).Count -eq 2) {
                    $inspectCoverage = 'COMPLETE'
                }
            }
            catch {
                $containerStates = @()
            }
        }
    }
    $eventArgs = @(
        'events', '--since', $eventStart.ToString('o'), '--until', $eventEnd.ToString('o'),
        '--filter', "label=com.docker.compose.project=$project", '--format', '{{json .}}'
    )
    $eventLines = @(& docker @eventArgs 2>$null)
    $eventCoverage = if ($LASTEXITCODE -eq 0) { 'COMPLETE' } else { 'INCOMPLETE' }
    $lifecycleEvents = @(
        foreach ($eventLine in $eventLines) {
            try { $event = ConvertFrom-Json -InputObject ([string]$eventLine) -ErrorAction Stop } catch { continue }
            $service = $event.Actor.Attributes.'com.docker.compose.service'
            $action = [string]$event.Action
            if ($service -in @('wordpress', 'database') -and $action -in @('oom', 'die', 'restart', 'start', 'stop', 'kill', 'destroy')) {
                [pscustomobject]@{
                    service = $service
                    action = $action
                    time_unix_nano = [long]$event.timeNano
                }
            }
        }
    )
    $dockerLifecycleEvidence = [pscustomobject]@{
        source = 'Docker Engine inspect and events APIs'
        scope = 'Only WordPress and database containers selected by this unique Compose project label; container IDs and names are omitted.'
        window_started_at = $eventStart.ToString('o')
        window_ended_at = $eventEnd.ToString('o')
        state_coverage = $inspectCoverage
        container_states = $containerStates
        event_coverage = $eventCoverage
        events = $lifecycleEvents
        note = 'Restart counts and OOMKilled state are point-in-time inspect values; lifecycle events are filtered to the campaign window. Missing coverage is unknown, not zero.'
    }
    $cgroupDeltaRows = @(
        foreach ($service in @('wordpress', 'database')) {
            $before = $cgroupStart | Where-Object service -eq $service | Select-Object -First 1
            $after = $cgroupEnd | Where-Object service -eq $service | Select-Object -First 1
            if ($null -ne $before -and $null -ne $after) {
                [pscustomobject]@{
                    service = $service
                    cpu_quota_cores = if ($null -ne $after.cpu_quota_micros) { [math]::Round($after.cpu_quota_micros / $after.cpu_period_micros, 3) } else { $null }
                    memory_limit_bytes = $after.memory_limit_bytes
                    cpu_usage_usec_delta = $after.cpu_usage_usec - $before.cpu_usage_usec
                    cpu_periods_delta = $after.cpu_nr_periods - $before.cpu_nr_periods
                    cpu_throttled_periods_delta = $after.cpu_nr_throttled - $before.cpu_nr_throttled
                    cpu_throttled_usec_delta = $after.cpu_throttled_usec - $before.cpu_throttled_usec
                    memory_max_events_delta = $after.memory_max_events - $before.memory_max_events
                    memory_oom_events_delta = $after.memory_oom_events - $before.memory_oom_events
                    memory_oom_kill_events_delta = $after.memory_oom_kill_events - $before.memory_oom_kill_events
                }
            }
        }
    )
    $cgroupCoverage = if ($cgroupDeltaRows.Count -eq 2) { 'COMPLETE' } else { 'INCOMPLETE' }
    $dockerStatsEvidence = [pscustomobject]@{
        schema_version = 1
        source = 'Docker Engine stats API via docker stats --no-stream'
        scope = 'Only services in this unique Compose project; whole-container CPU and memory; no process arguments or application data.'
        requested_poll_delay_ms = 1000
        timing = 'Host UTC timestamp recorded after each stats response; the next poll starts after the response plus requested delay, so effective interval includes stats-call duration. Phase windows use campaign wall-clock start and engine monotonic bounds; boundary alignment is approximate.'
        campaign_file = $campaignName
        campaign_evidence_id = $audit.audit.evidence_id
        samples_retained = $dockerStatsRows.Count
        lifecycle = $dockerLifecycleEvidence
        cgroup = [pscustomobject]@{
            source = 'Read-only cgroup-v2 counters inside only the WordPress and database containers'
            scope = 'CPU quota/usage/throttling and memory limit/max/OOM event deltas from a pre-campaign baseline to collection end.'
            coverage = $cgroupCoverage
            baseline = $cgroupStart
            final = $cgroupEnd
            deltas = $cgroupDeltaRows
            note = 'Baseline is before warm-up and final is after all campaign phases; counters are cumulative over the full campaign, not phase-specific.'
        }
        phases = @(
            foreach ($phase in $observations) {
                [pscustomobject]@{
                    phase = $phase.name
                    started_at = $phase.run.started_at
                    windows = @($dockerStatsWindows | Where-Object phase_started_at -eq $phase.run.started_at)
                }
            }
        )
    }
    $apacheEvidence | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $artifactPaths[3] -Encoding utf8
    $dockerStatsEvidence | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $artifactPaths[4] -Encoding utf8

    Write-Output "WordPress routes: homepage HTTP $($homeResponse.StatusCode); posts API HTTP $($posts.StatusCode)"
    Write-Output "Application metrics: COMPLETE in $($observations[0].run.application_metrics.samples.Count) observation snapshots"
    Write-Output "Configured workload: $Users closed-loop VUs; max_rps ceiling $MaxRps; warm-up ${WarmupSeconds}s; recovery ${RecoverySeconds}s"
    Write-Output "Generator preflight: $($calibration.preflight.status)"
    Write-Output "Campaign outcome: $($campaign.outcome); offline audit: $($audit.state)"
    Write-Output "Campaign evidence: $campaignPath"
    Write-Output "Offline audit: $auditPath"
    Write-Output "Apache route timing evidence: $($artifactPaths[3])"
    Write-Output "Docker Engine container stats evidence: $($artifactPaths[4])"
    Write-Output 'The result is local evidence for this WordPress image and synthetic read-only workflow; Apache timings are not WordPress/PHP instrumentation, and no transferable capacity claim is made.'
}
catch {
    if ($composeAttempted) {
        Write-Warning 'WordPress verifier failed; capturing isolated service state before cleanup.'
        & docker @compose ps
        & docker @compose logs --tail 80 wordpress
        & docker @compose logs --tail 80 sensor
    }
    throw
}
finally {
    if ($null -ne $dockerStatsJob) {
        Stop-Job -Job $dockerStatsJob -ErrorAction SilentlyContinue
        Remove-Job -Job $dockerStatsJob -Force -ErrorAction SilentlyContinue
    }
    if ($composeAttempted) {
        & docker @compose down --remove-orphans --volumes
        if ($LASTEXITCODE -ne 0) {
            Write-Warning 'Isolated WordPress stack cleanup reported an error.'
        }
    }
    Remove-Item Env:DAMOCLINE_WORDPRESS_PORT -ErrorAction SilentlyContinue
    Remove-Item Env:DAMOCLINE_WORDPRESS_CPU_LIMIT -ErrorAction SilentlyContinue
    Remove-Item Env:DAMOCLINE_WORDPRESS_SENSOR_TLS_DIR -ErrorAction SilentlyContinue
    if (Test-Path -LiteralPath $tlsDirectory) {
        Remove-Item -LiteralPath $tlsDirectory -Recurse -Force
    }
    Set-Location $originalLocation
}
