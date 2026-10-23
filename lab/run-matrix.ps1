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
    $errorLogPath = Join-Path $task.output "$tag-stderr.log"
    $statusPath = Join-Path $task.output "$tag-worker-status.json"
    $resourcePath = Join-Path $task.output "$tag-resources.json"
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
        $status = [pscustomobject]@{ schema_version = 1; worker_pid = $PID; target_pid = $target.Id; campaign_pid = $null; stage = 'target_ready'; updated_at = (Get-Date).ToUniversalTime().ToString('o') }
        [IO.File]::WriteAllText($statusPath, ($status | ConvertTo-Json), [Text.UTF8Encoding]::new($false))
        $campaignArgLine = 'campaign --config "{0}" --warmup 0s --observation {1} --recovery {2} --repeats 2 --budget 5m --app-metrics-endpoint "http://127.0.0.1:{3}/metrics" --app-metrics-interval 2s --authorized --out "{4}"' -f $scenarioPath, $task.observation, $task.recovery, $port, $campaignPath
        $campaignProcess = Start-Process -FilePath $task.damocline -ArgumentList $campaignArgLine -WindowStyle Hidden -PassThru -RedirectStandardOutput $logPath -RedirectStandardError $errorLogPath
        $campaignExit = $null
        $status.campaign_pid = $campaignProcess.Id
        $status.stage = 'campaign_running'
        $status.updated_at = (Get-Date).ToUniversalTime().ToString('o')
        [IO.File]::WriteAllText($statusPath, ($status | ConvertTo-Json), [Text.UTF8Encoding]::new($false))
        $campaignProcess.WaitForExit()
        $campaignProcess.Refresh()
        $campaignExit = [int]$campaignProcess.ExitCode
        if (-not (Test-Path -LiteralPath $campaignPath)) { throw "Campaign exited $campaignExit without writing an artifact." }
        $auditArgLine = 'audit --campaign "{0}" --out "{1}"' -f $campaignPath, $auditPath
        $auditOutput = Start-Process -FilePath $task.damocline -ArgumentList $auditArgLine -WindowStyle Hidden -PassThru -Wait -RedirectStandardOutput (Join-Path $task.output "$tag-audit.log") -RedirectStandardError (Join-Path $task.output "$tag-audit-stderr.log")
        $auditOutput.Refresh()
        $auditExit = $auditOutput.ExitCode
        if ($auditExit -ne 0) { throw "Offline audit failed with exit code $auditExit." }
        $campaign = Get-Content -LiteralPath $campaignPath -Raw | ConvertFrom-Json
        $audit = Get-Content -LiteralPath $auditPath -Raw | ConvertFrom-Json
        $status.stage = 'audit_complete'
        $status.updated_at = (Get-Date).ToUniversalTime().ToString('o')
        [IO.File]::WriteAllText($statusPath, ($status | ConvertTo-Json), [Text.UTF8Encoding]::new($false))
        $result = [pscustomobject]@{ index = $task.item.index; execution_order = $task.item.execution_order; variant = $variant.name; replicate = $task.item.replicate; port = $port; campaign_outcome = $campaign.outcome; campaign_exit = $campaignExit; audit_verdict = $audit.state; measurement_quality = $audit.audit.measurement_quality; campaign_file = [IO.Path]::GetFileName($campaignPath); audit_file = [IO.Path]::GetFileName($auditPath); log_file = [IO.Path]::GetFileName($logPath); error_file = [IO.Path]::GetFileName($errorLogPath); resource_file = [IO.Path]::GetFileName($resourcePath); error = $null }
    }
    catch {
        $result = [pscustomobject]@{ index = $task.item.index; execution_order = $task.item.execution_order; variant = $variant.name; replicate = $task.item.replicate; port = $port; campaign_outcome = 'NOT_COMPLETED'; campaign_exit = $campaignExit; audit_verdict = $null; measurement_quality = $null; campaign_file = [IO.Path]::GetFileName($campaignPath); audit_file = [IO.Path]::GetFileName($auditPath); log_file = [IO.Path]::GetFileName($logPath); error_file = [IO.Path]::GetFileName($errorLogPath); resource_file = [IO.Path]::GetFileName($resourcePath); error = $_.Exception.Message; error_detail = $_.ScriptStackTrace }
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
    if (-not $variant.name -or [int]$variant.users -lt 1 -or [int]$variant.users -gt 50 -or
        [int]$variant.max_rps -lt 1 -or [int]$variant.max_rps -gt 100 -or [int]$variant.target_delay_ms -lt 1 -or [int]$variant.target_delay_ms -gt 1000 -or
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
    $workerRecords = [Collections.Generic.List[object]]::new()
    foreach ($item in $batch) {
        $taskPath = Join-Path $outputPath ('{0:D3}-task.json' -f $item.index)
        $task = [pscustomobject]@{ item = $item; base_scenario = $baseScenario; damocline = [IO.Path]::GetFullPath($Damocline); target = [IO.Path]::GetFullPath($LabTarget); output = $outputPath; observation = $duration; recovery = $recovery; seed = $Seed; max_parallel = $MaxParallel; batch_size = $batch.Count }
        [IO.File]::WriteAllText($taskPath, ($task | ConvertTo-Json -Depth 20), [Text.UTF8Encoding]::new($false))
        $workerScript = Join-Path $PSScriptRoot 'run-matrix.ps1'
        $workerArgs = "-NoLogo -NoProfile -NonInteractive -File `"$workerScript`" -WorkerTask `"$taskPath`""
        $workerProcess = Start-Process -FilePath (Join-Path $PSHOME 'powershell.exe') -ArgumentList $workerArgs -WindowStyle Hidden -PassThru
        $workerRecords.Add([pscustomobject]@{ item = $item; process = $workerProcess; samples = [Collections.Generic.List[object]]::new(); previous_cpu = @{}; previous_sample_at = $null; sampling_failures = 0 })
    }

    while ($true) {
        $activeWorkers = @()
        foreach ($workerRecord in $workerRecords) {
            $workerRecord.process.Refresh()
            if (-not $workerRecord.process.HasExited) { $activeWorkers += $workerRecord }
        }
        if ($activeWorkers.Count -eq 0) { break }

        $sampleTime = (Get-Date).ToUniversalTime()
        $processSnapshot = @()
        $processorSnapshot = @()
        $memorySnapshot = $null
        $sampleError = $null
        try {
            $processSnapshot = @(Get-CimInstance Win32_Process -Property ProcessId, ParentProcessId, Name, WorkingSetSize)
        }
        catch {
            $sampleError = "process inventory: $($_.Exception.Message)"
        }
        try { $processorSnapshot = @(Get-CimInstance Win32_Processor -Property LoadPercentage) }
        catch { $sampleError = @($sampleError, "host CPU: $($_.Exception.Message)") -join '; ' }
        try { $memorySnapshot = Get-CimInstance Win32_OperatingSystem -Property FreePhysicalMemory, TotalVisibleMemorySize }
        catch { $sampleError = @($sampleError, "host memory: $($_.Exception.Message)") -join '; ' }

        foreach ($workerRecord in $activeWorkers) {
            $workerRecord.process.Refresh()
            if ($workerRecord.process.HasExited) { continue }
            $rootPID = [int]$workerRecord.process.Id
            $processIDs = [Collections.Generic.HashSet[int]]::new()
            [void]$processIDs.Add($rootPID)
            $changed = $true
            while ($changed) {
                $changed = $false
                foreach ($entry in $processSnapshot) {
                    if ($processIDs.Contains([int]$entry.ParentProcessId) -and $processIDs.Add([int]$entry.ProcessId)) { $changed = $true }
                }
            }

            $intervalSeconds = $null
            if ($null -ne $workerRecord.previous_sample_at) {
                $intervalSeconds = ($sampleTime - $workerRecord.previous_sample_at).TotalSeconds
                if ($intervalSeconds -le 0) { $intervalSeconds = $null }
            }
            $processRows = [Collections.Generic.List[object]]::new()
            $processObjects = @($processSnapshot | Where-Object { $processIDs.Contains([int]$_.ProcessId) })
            foreach ($processObject in $processObjects) {
                $processName = [IO.Path]::GetFileNameWithoutExtension([string]$processObject.Name)
                $role = switch -Regex ($processName.ToLowerInvariant()) {
                    '^damocline-labtarget$' { 'target'; break }
                    '^damocline$' { 'generator'; break }
                    '^(powershell|pwsh)$' { 'orchestrator'; break }
                    default { 'child' }
                }
                $cpuTicks = $null
                try {
                    $localProcess = Get-Process -Id ([int]$processObject.ProcessId) -ErrorAction Stop
                    $cpuTicks = $localProcess.TotalProcessorTime.Ticks
                }
                catch { }
                $deltaCPUSeconds = $null
                $cpuPercentOneCore = $null
                $cpuPercentHost = $null
                $pidKey = [string]$processObject.ProcessId
                if ($null -ne $cpuTicks -and $null -ne $intervalSeconds -and $workerRecord.previous_cpu.ContainsKey($pidKey)) {
                    $deltaCPUSeconds = [Math]::Max(0, ($cpuTicks - [long]$workerRecord.previous_cpu[$pidKey]) / 10000000.0)
                    $cpuPercentOneCore = $deltaCPUSeconds / $intervalSeconds * 100
                    $logicalProcessors = [Math]::Max(1, [Environment]::ProcessorCount)
                    $cpuPercentHost = $cpuPercentOneCore / $logicalProcessors
                }
                if ($null -ne $cpuTicks) { $workerRecord.previous_cpu[$pidKey] = $cpuTicks }
                $processRows.Add([pscustomobject]@{ process_id = $processObject.ProcessId; process_name = $processName; role = $role; delta_cpu_seconds = $deltaCPUSeconds; cpu_percent_one_core = $cpuPercentOneCore; cpu_percent_host_capacity = $cpuPercentHost; working_set_bytes = [long]$processObject.WorkingSetSize })
            }

            $targetRows = @($processRows | Where-Object { $_.role -eq 'target' })
            $generatorRows = @($processRows | Where-Object { $_.role -eq 'generator' })
            $cpuLoadValues = @($processorSnapshot | Where-Object { $null -ne $_.LoadPercentage } | ForEach-Object { [double]$_.LoadPercentage })
            $hostCPU = $null
            if ($cpuLoadValues.Count -gt 0) { $hostCPU = ($cpuLoadValues | Measure-Object -Average).Average }
            $hostMemoryUsed = $null
            $hostMemoryTotal = $null
            if ($null -ne $memorySnapshot) {
                $hostMemoryTotal = [long]$memorySnapshot.TotalVisibleMemorySize * 1024
                $hostMemoryUsed = $hostMemoryTotal - ([long]$memorySnapshot.FreePhysicalMemory * 1024)
            }
            $treeCPU = $null
            if ($null -ne $intervalSeconds) {
                $treeDeltas = @($processRows | Where-Object { $null -ne $_.delta_cpu_seconds } | ForEach-Object { [double]$_.delta_cpu_seconds })
                if ($treeDeltas.Count -gt 0) { $treeCPU = (($treeDeltas | Measure-Object -Sum).Sum / $intervalSeconds / [Math]::Max(1, [Environment]::ProcessorCount)) * 100 }
            }
            $workerRecord.samples.Add([pscustomobject]@{
                observed_at = $sampleTime.ToString('o')
                interval_seconds = $intervalSeconds
                worker_pid = $rootPID
                active_process_count = $processRows.Count
                host_cpu_load_percent = $hostCPU
                host_memory_used_bytes = $hostMemoryUsed
                host_memory_total_bytes = $hostMemoryTotal
                target_cpu_percent_host_capacity = if ($targetRows.Count -gt 0 -and $null -ne $intervalSeconds) { (@($targetRows | Where-Object { $null -ne $_.cpu_percent_host_capacity } | ForEach-Object cpu_percent_host_capacity | Measure-Object -Sum).Sum) } else { $null }
                target_working_set_bytes = if ($targetRows.Count -gt 0) { [long](@($targetRows | Measure-Object working_set_bytes -Sum).Sum) } else { $null }
                generator_cpu_percent_host_capacity = if ($generatorRows.Count -gt 0 -and $null -ne $intervalSeconds) { (@($generatorRows | Where-Object { $null -ne $_.cpu_percent_host_capacity } | ForEach-Object cpu_percent_host_capacity | Measure-Object -Sum).Sum) } else { $null }
                generator_working_set_bytes = if ($generatorRows.Count -gt 0) { [long](@($generatorRows | Measure-Object working_set_bytes -Sum).Sum) } else { $null }
                worker_tree_cpu_percent_host_capacity = $treeCPU
                worker_tree_working_set_bytes = if ($processRows.Count -gt 0) { [long](@($processRows | Measure-Object working_set_bytes -Sum).Sum) } else { $null }
                process_samples = @($processRows)
                phase_membership = @()
                collection_error = $sampleError
            })
            if ($sampleError -or @($processRows | Where-Object { $_.role -in @('target', 'generator') -and $null -eq $_.delta_cpu_seconds }).Count -gt 0) { $workerRecord.sampling_failures++ }
            $workerRecord.previous_sample_at = $sampleTime
        }
        Start-Sleep -Seconds 2
    }

    foreach ($item in $batch) {
        $resultPath = Join-Path $outputPath ('{0:D3}-{1}-r{2:D2}-result.json' -f $item.index, ($item.variant.name -replace '[^A-Za-z0-9_-]', '-'), $item.replicate)
        $workerRecord = @($workerRecords | Where-Object { $_.item.index -eq $item.index })[0]
        if (Test-Path -LiteralPath $resultPath) {
            $result = Get-Content -LiteralPath $resultPath -Raw | ConvertFrom-Json
        }
        else {
            $result = [pscustomobject]@{ index = $item.index; execution_order = $item.execution_order; variant = $item.variant.name; replicate = $item.replicate; campaign_outcome = 'NOT_COMPLETED'; campaign_exit = $null; audit_verdict = $null; measurement_quality = $null; campaign_file = $null; audit_file = $null; log_file = $null; error = 'Worker process did not write a result artifact.' }
        }

        $campaignArtifact = $null
        if ($result.campaign_file) {
            $candidateCampaign = Join-Path $outputPath $result.campaign_file
            if (Test-Path -LiteralPath $candidateCampaign) { $campaignArtifact = Get-Content -LiteralPath $candidateCampaign -Raw | ConvertFrom-Json }
        }
        foreach ($sample in $workerRecord.samples) {
            $memberships = [Collections.Generic.List[string]]::new()
            if ($null -ne $campaignArtifact) {
                foreach ($trial in $campaignArtifact.trials) {
                    foreach ($phase in $trial.phases) {
                        if ($null -eq $phase.run.started_at -or $null -eq $phase.run.elapsed_seconds) { continue }
                        try {
                            $phaseStart = [DateTimeOffset]::Parse([string]$phase.run.started_at)
                            $phaseEnd = $phaseStart.AddSeconds([double]$phase.run.elapsed_seconds)
                            $pointTime = [DateTimeOffset]::Parse([string]$sample.observed_at)
                            if ($pointTime -ge $phaseStart -and $pointTime -le $phaseEnd) { $memberships.Add("trial-$($trial.number):$($phase.name)") }
                        }
                        catch { }
                    }
                }
            }
            $sample.phase_membership = @($memberships)
        }

        $phaseNames = @($workerRecord.samples | ForEach-Object { $_.phase_membership } | Where-Object { $_ } | Sort-Object -Unique)
        $phaseSummaries = foreach ($phaseName in $phaseNames) {
            $phaseSamples = @($workerRecord.samples | Where-Object { $_.phase_membership -contains $phaseName })
            $hostCPUs = @($phaseSamples | Where-Object { $null -ne $_.host_cpu_load_percent } | ForEach-Object host_cpu_load_percent)
            $hostMemories = @($phaseSamples | Where-Object { $null -ne $_.host_memory_used_bytes } | ForEach-Object host_memory_used_bytes)
            $targetCPUs = @($phaseSamples | Where-Object { $null -ne $_.target_cpu_percent_host_capacity } | ForEach-Object target_cpu_percent_host_capacity)
            $generatorCPUs = @($phaseSamples | Where-Object { $null -ne $_.generator_cpu_percent_host_capacity } | ForEach-Object generator_cpu_percent_host_capacity)
            $targetMemories = @($phaseSamples | Where-Object { $null -ne $_.target_working_set_bytes } | ForEach-Object target_working_set_bytes)
            $generatorMemories = @($phaseSamples | Where-Object { $null -ne $_.generator_working_set_bytes } | ForEach-Object generator_working_set_bytes)
            [pscustomobject]@{
                phase = $phaseName
                sample_count = $phaseSamples.Count
                host_cpu_load_percent_mean = if ($hostCPUs.Count -gt 0) { ($hostCPUs | Measure-Object -Average).Average } else { $null }
                host_cpu_load_percent_max = if ($hostCPUs.Count -gt 0) { ($hostCPUs | Measure-Object -Maximum).Maximum } else { $null }
                host_memory_used_bytes_mean = if ($hostMemories.Count -gt 0) { ($hostMemories | Measure-Object -Average).Average } else { $null }
                host_memory_used_bytes_max = if ($hostMemories.Count -gt 0) { ($hostMemories | Measure-Object -Maximum).Maximum } else { $null }
                target_cpu_percent_host_capacity_mean = if ($targetCPUs.Count -gt 0) { ($targetCPUs | Measure-Object -Average).Average } else { $null }
                target_cpu_percent_host_capacity_max = if ($targetCPUs.Count -gt 0) { ($targetCPUs | Measure-Object -Maximum).Maximum } else { $null }
                target_working_set_bytes_max = if ($targetMemories.Count -gt 0) { ($targetMemories | Measure-Object -Maximum).Maximum } else { $null }
                generator_cpu_percent_host_capacity_mean = if ($generatorCPUs.Count -gt 0) { ($generatorCPUs | Measure-Object -Average).Average } else { $null }
                generator_cpu_percent_host_capacity_max = if ($generatorCPUs.Count -gt 0) { ($generatorCPUs | Measure-Object -Maximum).Maximum } else { $null }
                generator_working_set_bytes_max = if ($generatorMemories.Count -gt 0) { ($generatorMemories | Measure-Object -Maximum).Maximum } else { $null }
            }
        }
        $processorInfo = @()
        $osCaption = $null
        try { $processorInfo = @(Get-CimInstance Win32_Processor -Property Name, NumberOfCores, NumberOfLogicalProcessors) } catch { }
        try { $osCaption = (Get-CimInstance Win32_OperatingSystem -Property Caption).Caption } catch { }
        $resourceArtifact = [pscustomobject]@{
            schema_version = 1
            worker = [pscustomobject]@{ index = $item.index; variant = $item.variant.name; replicate = $item.replicate; worker_pid = $workerRecord.process.Id; target_pid = if (Test-Path -LiteralPath (Join-Path $outputPath ('{0:D3}-{1}-r{2:D2}-worker-status.json' -f $item.index, ($item.variant.name -replace '[^A-Za-z0-9_-]', '-'), $item.replicate))) { (Get-Content (Join-Path $outputPath ('{0:D3}-{1}-r{2:D2}-worker-status.json' -f $item.index, ($item.variant.name -replace '[^A-Za-z0-9_-]', '-'), $item.replicate)) -Raw | ConvertFrom-Json).target_pid } else { $null } }
            host = [pscustomobject]@{ os_caption = $osCaption; processor_name = if ($processorInfo.Count -gt 0) { $processorInfo[0].Name } else { $null }; physical_cores = if ($processorInfo.Count -gt 0) { @($processorInfo | Measure-Object NumberOfCores -Sum)[0].Sum } else { $null }; logical_processors = [Environment]::ProcessorCount; cpu_load_source = 'Win32_Processor.LoadPercentage'; memory_source = 'Win32_OperatingSystem.FreePhysicalMemory and TotalVisibleMemorySize' }
            sampling = [pscustomobject]@{ requested_interval_seconds = 2; actual_intervals_seconds = @($workerRecord.samples | Where-Object { $null -ne $_.interval_seconds } | ForEach-Object interval_seconds); sample_count = $workerRecord.samples.Count; failed_samples = $workerRecord.sampling_failures; coverage = if ($workerRecord.samples.Count -gt 0 -and $workerRecord.sampling_failures -eq 0) { 'SAMPLED' } elseif ($workerRecord.samples.Count -gt 0) { 'SAMPLED_WITH_GAPS' } else { 'NOT_OBSERVED' }; note = 'Windows host and process telemetry is sampled while each worker process tree is active. Process CPU uses cumulative TotalProcessorTime ticks from Get-Process; exited processes may be absent from a snapshot. Host CPU load is the coarse WMI percentage. Sampling overhead and phase-boundary alignment are not measured.' }
            phase_summaries = @($phaseSummaries)
            samples = @($workerRecord.samples)
        }
        $resourcePath = Join-Path $outputPath ('{0:D3}-{1}-r{2:D2}-resources.json' -f $item.index, ($item.variant.name -replace '[^A-Za-z0-9_-]', '-'), $item.replicate)
        [IO.File]::WriteAllText($resourcePath, ($resourceArtifact | ConvertTo-Json -Depth 20), [Text.UTF8Encoding]::new($false))
        $result | Add-Member -Force -NotePropertyName resource_file -NotePropertyValue ([IO.Path]::GetFileName($resourcePath))
        $result | Add-Member -Force -NotePropertyName resource_sample_count -NotePropertyValue $workerRecord.samples.Count
        $result | Add-Member -Force -NotePropertyName resource_coverage -NotePropertyValue $resourceArtifact.sampling.coverage
        [IO.File]::WriteAllText($resultPath, ($result | ConvertTo-Json -Depth 10), [Text.UTF8Encoding]::new($false))
        $results.Add($result)
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
    resource_telemetry = 'Per-worker Windows host CPU/memory and process-tree CPU/working-set samples are written to each resource sidecar and assigned approximately to campaign phases. Reported cadence includes WMI collection overhead; process CPU values are null where cumulative counters were unavailable.'
    telemetry_limits = 'Workers share one physical host. Host CPU and memory are repeated host-wide snapshots, not worker-attributed measurements. Process inventory is sampled once per active batch and process CPU uses cumulative TotalProcessorTime ticks where available. Phase mapping uses wall-clock timestamps and is approximate; sampler overhead is unmeasured.'
    results = @($results | Sort-Object execution_order)
}
$manifestPath = Join-Path $outputPath 'matrix-manifest.json'
[IO.File]::WriteAllText($manifestPath, ($manifest | ConvertTo-Json -Depth 20), [Text.UTF8Encoding]::new($false))
$manifest.results | Format-Table index, variant, replicate, campaign_outcome, audit_verdict, measurement_quality, error -AutoSize
Write-Output "Matrix manifest: $manifestPath"
if (@($manifest.results | Where-Object { $_.error }).Count -gt 0) { exit 1 }
