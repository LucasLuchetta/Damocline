# CI stability and flake ledger

CI must separate a real regression from an intermittent test. A red run on
`master` is treated as a regression until a ledger entry below explains it.

## Rules

- A timing-sensitive test must not fail because a loaded host ran it slowly.
  Use controlled delays with margin, fake clocks or sleeps through test
  hooks, statistical tolerances justified by the sample size, and fixed seeds.
- Fixing a flake never weakens what the test verifies and never changes a
  production threshold. When the root cause is a product bug, fix the product.
- Retrying a failed test is not a fix. The only accepted retry is the
  documented hosted-Windows calibration retry in `ci.yml` (ADR 0152), which
  keeps both artifacts.

## Repeated runs

`.github/workflows/flake-hunt.yml` runs nightly and on demand
(`workflow_dispatch`, input `count`, default 5). It repeats
`internal/engine`, `internal/campaign`, `internal/arrivalcampaign`,
`internal/cli` and `internal/generatorhealth` with `-count=N` on Linux and
Windows and uploads the logs of any failing package as an artifact.

Locally:

```sh
for i in 1 2 3 4 5; do go test ./... -count=1 || echo "run $i failed"; done
```

## Flake ledger

| Test | Seen | Root cause | Fix | PR |
| --- | --- | --- | --- | --- |
| `internal/cli` `TestCapacityRecoveryGateWaitsForBaseline` | CI master, 1 run | The gate added real `time.Since` around a faked probe to the reported wait, so `Waited` was `1m0.000000901s` instead of `1m0s` on a slow runner. | Clock injected through the `capacityRecoveryNow` test hook; tests freeze it. Production behaviour unchanged. | this PR |
| `internal/campaign` `TestClosedAndOpenCampaignsMeasureSpikeAndRecoveryAgainstControlledServer/open-arrival` | CI master, 9 runs | A 450 ms saturated delay with 30 workers left little headroom; host stalls exhausted the pool, so the spike ended `INCONCLUSIVE` instead of an observed `FAIL`. | 300 ms delay (still 2.5x the SLO) and 50 workers; 2.5 s windows. | `80583ff`, `63d141f` (before this PR) |
| `internal/campaign` `TestNativeEngineCampaignRecordsStableLoopbackWindows` | CI master, 2 runs | One-second windows held too few samples on a loaded host, so a window fell under the stability minimum and the campaign ended `INCONCLUSIVE`. | Wider windows and stall tolerance. | `63d141f` (before this PR) |
| `internal/cli` `TestExtendedCampaignGuardrailStopIsGeneratorLimitedAndAuditable` | CI master, 2 runs | Product bug: a generator guardrail abort that also had invalid delivery lost its `GENERATOR_LIMITED` abort reason. | Product fix preserving the guardrail abort. | `cf6ae75` (before this PR) |
| `internal/engine` `TestNativeEngineMatchesReferenceWorkloadProfileMatrix`, `TestArrivalProfilesFollowConfiguredWeights` | Suspected | Not reproduced: 25 repetitions each with `-race` while a full `go test ./...` ran in parallel all passed; no failure in the recent master history. | None; watched by the nightly flake hunt. | - |

Not flakes: one master failure was a formatting check (`go fmt` diff), and
cancelled runs were superseded pushes.
