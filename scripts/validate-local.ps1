$ErrorActionPreference = 'Stop'

$repositoryRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $repositoryRoot

$calibrationDirectory = Join-Path $repositoryRoot '.damocline/calibration'
New-Item -ItemType Directory -Force -Path $calibrationDirectory | Out-Null

function Invoke-Checked {
    param(
        [Parameter(Mandatory)] [string] $Name,
        [Parameter(Mandatory)] [scriptblock] $Command
    )

    Write-Host "`n== $Name ==" -ForegroundColor Cyan
    & $Command
    if ($LASTEXITCODE -ne 0) {
        throw "$Name failed with exit code $LASTEXITCODE"
    }
}

Invoke-Checked 'Go test suite' { go test ./... }
Invoke-Checked 'Basic loopback reference check' { go run ./cmd/damocline-calibrate --out .damocline/calibration/reference.json }
Invoke-Checked 'Extended loopback reference check' { go run ./cmd/damocline-calibrate --extended --out .damocline/calibration/reference-extended.json }
Invoke-Checked 'Repeated loopback reference check' { go run ./cmd/damocline-calibrate --repeats 3 --out .damocline/calibration/reference-repeats.json }
Invoke-Checked 'Local HTTP and HTTPS transport matrix' { go run ./cmd/damocline-calibrate --transport-matrix --out .damocline/calibration/transport.json }

$expectedReports = @(
    @{ Path = '.damocline/calibration/reference.json'; Status = 'REFERENCE_SMOKE_PASSED' },
    @{ Path = '.damocline/calibration/reference-extended.json'; Status = 'REFERENCE_SMOKE_PASSED' },
    @{ Path = '.damocline/calibration/reference-repeats.json'; Status = 'REFERENCE_SMOKE_PASSED' },
    @{ Path = '.damocline/calibration/transport.json'; Status = 'TRANSPORT_MATRIX_PASSED' }
)

foreach ($expected in $expectedReports) {
    if (-not (Test-Path -LiteralPath $expected.Path -PathType Leaf)) {
        throw "Expected report was not created: $($expected.Path)"
    }
    $report = Get-Content -LiteralPath $expected.Path -Raw | ConvertFrom-Json
    if ($report.status -ne $expected.Status) {
        throw "Unexpected status '$($report.status)' in $($expected.Path); expected '$($expected.Status)'"
    }
    Write-Host "Verified $($expected.Path): $($report.status)" -ForegroundColor Green
}

Write-Host "`nLOCAL VALIDATION PASSED" -ForegroundColor Green
Write-Host "Reports are local under .damocline/calibration and are ignored by Git."
