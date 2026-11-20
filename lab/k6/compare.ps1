param([Parameter(Mandatory = $true)][string]$OutputDirectory)

$ErrorActionPreference = 'Stop'
$artifact = (New-Item -ItemType Directory -Path $OutputDirectory).FullName
Push-Location (Join-Path $PSScriptRoot '../..')
try {
    foreach ($name in @('damocline', 'damocline-labtarget')) {
        go build -o (Join-Path $artifact "$name.exe") "./cmd/$name"
        if ($LASTEXITCODE) { throw "Build failed for $name" }
    }
    $fixture = Start-Process (Join-Path $artifact 'damocline-labtarget.exe') -ArgumentList '-listen 127.0.0.1:18086 -delay 200ms -parallel 2 -queue 6' -PassThru -WindowStyle Hidden
    try {
        Start-Sleep -Milliseconds 500
        if ($fixture.HasExited) { throw 'Fixture failed to bind loopback' }
        foreach ($vus in @(1, 4, 20)) {
            $step = @{ method='GET'; path='/work'; expected_status=@(200); must_contain='synthetic work complete' }
            $scenario = @{
                name = 'Synthetic closed-loop comparison'
                base_url = 'http://127.0.0.1:18086'
                mode = 'closed'
                users = $vus
                duration = '20s'
                ramp_up = '0s'
                think_time = '1s'
                request_timeout = '3s'
                max_rps = 100
                p95_limit_ms = 1000
                max_error_rate = 0.01
                min_samples_per_step = 1
                steps = @(@{name='first'} + $step; @{name='second'} + $step)
            }
            $config = Join-Path $artifact "scenario-$vus.json"
            $nativeReport = Join-Path $artifact "damocline-$vus.json"
            $scenario | ConvertTo-Json -Depth 6 | Set-Content -Encoding ascii $config
            & (Join-Path $artifact 'damocline.exe') run --config $config --authorized --out $nativeReport
            if (!(Test-Path -LiteralPath $nativeReport)) { throw "Damocline produced no report at $vus VUs" }
            k6 run --env "LAB_VUS=$vus" --summary-export (Join-Path $artifact "k6-$vus.json") lab/k6/compare.js
            if ($LASTEXITCODE) { throw "k6 failed at $vus VUs" }
        }
    } finally {
        if (!$fixture.HasExited) { Stop-Process -InputObject $fixture }
    }
} finally {
    Pop-Location
}
