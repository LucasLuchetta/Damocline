param(
    [string] $Matrix = '',
    [string] $Scenario = (Join-Path $PSScriptRoot 'scenario.json'),
    [string] $Damocline = '',
    [string] $LabTarget = '',
    [string] $OutputDirectory = '',
    [int] $MaxParallel = 4,
    [int] $Seed = 20261022,
    [string] $WorkerTask = ''
)

$ErrorActionPreference = 'Stop'
if ($WorkerTask) {
    $task = Get-Content -LiteralPath $WorkerTask -Raw | ConvertFrom-Json
    $variant = $task.item.variant
    $slug = ([string]$variant.name -replace '[^A-Za-z0-9_-]', '-')
    $tag = '{0:D3}-{1}-r{2:D2}' -f $task.item.index, $slug, $task.item.replicate
    $port = 20000 + [int]$task.item.index
    $scenarioPath = Join-Path $task.output "$tag-scenario.json"
    $campaignPath = Join-Path $task.output "$tag-campaign.json"
    $auditPath = Join-Path $task.output "$tag-audit.json"
    $logPath = Join-Path $task.output "$tag.log"
    $target = $null
    try {
        $scenarioMap = [ordered]@{}
        foreach ($property in $task.base_scenario.PSObject.Properties) { $scenarioMap.Add([string]$property.Name, $property.Value) }
        $scenarioMap['base_url'] = "http://127.0.0.1:$port"
        $scenarioMap['environment'] = "local-matrix-seed-$($task.seed)-concurrency-$($task.batch_size)"
        $scenarioMap['workload_revision'] = "matrix-v1-$slug"
        $scenarioMap['users'] = [int]$variant.users
        $scenarioMap['max_rps'] = [int]$variant.max_rps
        $scenarioMap['p95_limit_ms'] = [float]$variant.p95_limit_ms
        $scenarioMap['max_error_rate'] = [float]$variant.max_error_rate
        [IO.File]::WriteAllText($scenarioPath, ($scenarioMap | ConvertTo-Json -Depth 20), [Text.UTF8Encoding]::new($false))
        $targetArgs = @('-listen', "127.0.0.1:$port", '-delay', "$([int]$variant.target_delay_ms)ms", '-parallel', [string][int]$variant.target_parallel, '-queue', [string][int]$variant.target_queue)
        $target = Start-Process -FilePath $task.target -ArgumentList $targetArgs -WindowStyle Hidden -PassThru
        $ready = $false
        for ($attempt = 0; $attempt -lt 50; $attempt++) {
            if ($target.HasExited) { throw "Lab target exited with code $($target.ExitCode)." }
            & curl.exe --fail --silent --output NUL --max-time 1 "http://127.0.0.1:$port/healthz"
            if ($LASTEXITCODE -eq 0) { $ready = $true; break }
            Start-Sleep -Milliseconds 100
        }
        if (-not $ready) { throw 'Lab target did not become ready within five seconds.' }
        $campaignArgs = @('campaign', '--config', $scenarioPath, '--warmup', '0s', '--observation', $task.observation, '--recovery', $task.recovery, '--repeats', '2', '--budget', '5m', '--app-metrics-endpoint', "http://127.0.0.1:$port/metrics", '--app-metrics-interval', '2s', '--authorized', '--out', $campaignPath)
        $campaignOutput = & $task.damocline @campaignArgs 2>&1
        $campaignExit = $LASTEXITCODE
        $campaignOutput | Out-File -LiteralPath $logPath -Encoding utf8
        if (-not (Test-Path -LiteralPath $campaignPath)) { throw "Campaign exited $campaignExit without writing an artifact." }
        $auditOutput = & $task.damocline audit --campaign $campaignPath --out $auditPath 2>&1
        $auditExit = $LASTEXITCODE
        $auditOutput | Out-File -LiteralPath $logPath -Append -Encoding utf8
        if ($auditExit -ne 0) { throw "Offline audit failed with exit code $auditExit." }
        $campaign = Get-Content -LiteralPath $campaignPath -Raw | ConvertFrom-Json
        $audit = Get-Content -LiteralPath $auditPath -Raw | ConvertFrom-Json
        $result = [pscustomobject]@{ index = $task.item.index; execution_order = $task.item.execution_order; variant = $variant.name; replicate = $task.item.replicate; port = $port; campaign_outcome = $campaign.outcome; campaign_exit = $campaignExit; audit_verdict = $audit.state; measurement_quality = $audit.audit.measurement_quality; campaign_file = [IO.Path]::GetFileName($campaignPath); audit_file = [IO.Path]::GetFileName($auditPath); log_file = [IO.Path]::GetFileName($logPath); error = $null }
    }
    catch {
        $result = [pscustomobject]@{ index = $task.item.index; execution_order = $task.item.execution_order; variant = $variant.name; replicate = $task.item.replicate; port = $port; campaign_outcome = 'NOT_COMPLETED'; campaign_exit = $null; audit_verdict = $null; measurement_quality = $null; campaign_file = [IO.Path]::GetFileName($campaignPath); audit_file = [IO.Path]::GetFileName($auditPath); log_file = [IO.Path]::GetFileName($logPath); error = $_.Exception.Message; error_detail = $_.ScriptStackTrace }
    }
    finally {
        if ($target -and -not $target.HasExited) { Stop-Process -Id $target.Id -Force }
    }
    $resultPath = Join-Path $task.output "$tag-result.json"
    [IO.File]::WriteAllText($resultPath, ($result | ConvertTo-Json -Depth 10), [Text.UTF8Encoding]::new($false))
    if ($result.error) { exit 1 }
    exit 0
}
if (-not $Matrix) { throw 'Specify -Matrix with a matrix JSON file.' }
foreach ($path in @($Matrix, $Scenario)) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw "Required file not found: $path" }
}
if ($MaxParallel -lt 1 -or $MaxParallel -gt 8) { throw 'MaxParallel must be between 1 and 8.' }

$matrixData = Get-Content -LiteralPath $Matrix -Raw | ConvertFrom-Json
if ($matrixData.schema_version -ne 1 -or @($matrixData.variants).Count -eq 0) {
    throw 'Matrix must use schema_version 1 and contain at least one variant.'
}
$replicates = [int]$matrixData.replicates
if ($replicates -lt 1 -or $replicates -gt 20) { throw 'replicates must be between 1 and 20.' }
$duration = [string]$matrixData.observation
$recovery = [string]$matrixData.recovery
if ($duration -notmatch '^\d+s$' -or [int]($duration -replace 's$', '') -lt 10 -or [int]($duration -replace 's$', '') -gt 60 -or
    $recovery -notmatch '^\d+s$' -or [int]($recovery -replace 's$', '') -lt 1 -or [int]($recovery -replace 's$', '') -gt 60) {
    throw 'Bounded matrix durations are observation 10-60s and recovery 1-60s.'
}

$experimentStartedAt = (Get-Date).ToUniversalTime().ToString('o')
$runStamp = (Get-Date).ToUniversalTime().ToString('yyyyMMddTHHmmssfffZ')
if (-not $OutputDirectory) { $OutputDirectory = Join-Path $PSScriptRoot "artifacts\matrix\$runStamp" }
$outputPath = [IO.Path]::GetFullPath($OutputDirectory)
New-Item -ItemType Directory -Force -Path $outputPath | Out-Null
if (@(Get-ChildItem -LiteralPath $outputPath -Force).Count -gt 0) { throw 'OutputDirectory must be empty to preserve earlier experiment artifacts.' }
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
if (-not $Damocline) {
    $Damocline = Join-Path $outputPath 'damocline.exe'
    Push-Location $repoRoot
    try { & go build -o $Damocline ./cmd/damocline; if ($LASTEXITCODE -ne 0) { throw 'Could not build damocline.' } }
    finally { Pop-Location }
}
if (-not $LabTarget) {
    $LabTarget = Join-Path $outputPath 'damocline-labtarget.exe'
    Push-Location $repoRoot
    try { & go build -o $LabTarget ./cmd/damocline-labtarget; if ($LASTEXITCODE -ne 0) { throw 'Could not build damocline-labtarget.' } }
    finally { Pop-Location }
}
foreach ($path in @($Damocline, $LabTarget)) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw "Required executable not found: $path" }
}
$baseScenario = Get-Content -LiteralPath $Scenario -Raw | ConvertFrom-Json
$items = [Collections.Generic.List[object]]::new()
$index = 0
foreach ($variant in $matrixData.variants) {
    if (-not $variant.name -or [int]$variant.users -lt 1 -or [int]$variant.users -gt 1000 -or
        [int]$variant.max_rps -lt 1 -or [int]$variant.max_rps -gt 1000 -or [int]$variant.target_delay_ms -lt 1 -or [int]$variant.target_delay_ms -gt 1000 -or
        [int]$variant.target_parallel -lt 1 -or [int]$variant.target_parallel -gt 16 -or
        [int]$variant.target_queue -lt [int]$variant.target_parallel -or [int]$variant.target_queue -gt 64 -or
        [float]$variant.p95_limit_ms -le 0 -or [float]$variant.max_error_rate -lt 0 -or [float]$variant.max_error_rate -gt 1) {
        throw "Variant '$($variant.name)' has values outside the bounded lab-target or campaign limits."
    }
    foreach ($replicate in 1..$replicates) {
        $index++
        $items.Add([pscustomobject]@{ index = $index; variant = $variant; replicate = $replicate })
    }
}
if ($items.Count -gt 100) { throw 'Matrix is limited to 100 bounded workers per invocation.' }
if (20000 + $items.Count - 1 -gt 60000) { throw 'Matrix is too large for the reserved local port range.' }

Get-Random -SetSeed $Seed | Out-Null
$randomized = @($items | Sort-Object { Get-Random })
$executionOrder = 0
foreach ($item in $randomized) { $executionOrder++; $item | Add-Member -NotePropertyName execution_order -NotePropertyValue $executionOrder }
$results = [Collections.Generic.List[object]]::new()
for ($offset = 0; $offset -lt $randomized.Count; $offset += $MaxParallel) {
    $batch = @($randomized | Select-Object -Skip $offset -First $MaxParallel)
    $processes = foreach ($item in $batch) {
        $taskPath = Join-Path $outputPath ('{0:D3}-task.json' -f $item.index)
        $task = [pscustomobject]@{ item = $item; base_scenario = $baseScenario; damocline = [IO.Path]::GetFullPath($Damocline); target = [IO.Path]::GetFullPath($LabTarget); output = $outputPath; observation = $duration; recovery = $recovery; seed = $Seed; max_parallel = $MaxParallel; batch_size = $batch.Count }
        [IO.File]::WriteAllText($taskPath, ($task | ConvertTo-Json -Depth 20), [Text.UTF8Encoding]::new($false))
        $workerScript = Join-Path $PSScriptRoot 'run-matrix.ps1'
        $workerArgs = "-NoLogo -NoProfile -NonInteractive -File `"$workerScript`" -WorkerTask `"$taskPath`""
        Start-Process -FilePath (Join-Path $PSHOME 'powershell.exe') -ArgumentList $workerArgs -WindowStyle Hidden -PassThru
    }
    foreach ($process in $processes) { $process.WaitForExit() }
    foreach ($item in $batch) {
        $resultPath = Join-Path $outputPath ('{0:D3}-{1}-r{2:D2}-result.json' -f $item.index, ($item.variant.name -replace '[^A-Za-z0-9_-]', '-'), $item.replicate)
        if (Test-Path -LiteralPath $resultPath) {
            $results.Add((Get-Content -LiteralPath $resultPath -Raw | ConvertFrom-Json))
        }
        else {
            $results.Add([pscustomobject]@{ index = $item.index; variant = $item.variant.name; replicate = $item.replicate; campaign_outcome = 'NOT_COMPLETED'; campaign_exit = $null; audit_verdict = $null; measurement_quality = $null; campaign_file = $null; audit_file = $null; log_file = $null; error = 'Worker process did not write a result artifact.' })
        }
    }
}

$manifest = [pscustomobject]@{
    schema_version = 1
    started_at = $experimentStartedAt
    completed_at = (Get-Date).ToUniversalTime().ToString('o')
    randomization_seed = $Seed
    max_parallel_workers = $MaxParallel
    worker_count = $items.Count
    observation = $duration
    recovery = $recovery
    concurrency_note = 'Workers use separate local target processes and ports. They still share this physical host; concurrent worker contention is an experimental factor and results do not establish isolated-host capacity.'
    results = @($results | Sort-Object execution_order)
}
$manifestPath = Join-Path $outputPath 'matrix-manifest.json'
[IO.File]::WriteAllText($manifestPath, ($manifest | ConvertTo-Json -Depth 20), [Text.UTF8Encoding]::new($false))
$manifest.results | Format-Table index, variant, replicate, campaign_outcome, audit_verdict, measurement_quality, error -AutoSize
Write-Output "Matrix manifest: $manifestPath"
if (@($manifest.results | Where-Object { $_.error }).Count -gt 0) { exit 1 }
