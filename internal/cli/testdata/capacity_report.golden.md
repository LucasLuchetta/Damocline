# Capacity assessment

## Executive summary

- **Assessment:** INSUFFICIENT_EVIDENCE
- **Observed campaign:** OBSERVED_PASS_ONLY
- **Measurement quality:** LIMITED
- **Workload:** synthetic local fixture at 2 modeled active users
- **Observed passing levels:** [2]; **failing levels:** []
- **Demand inputs:** peak 2 active users, growth 25%, reserve 10%
- **Calculated target:** 3 workload-equivalent active actors; **plan state:** INSUFFICIENT_EVIDENCE

This assessment describes the saved evidence under its declared conditions. It does not certify universal or human-user capacity, establish dependence-aware confidence, or recommend infrastructure changes.

## Technical evidence

- **Campaign evidence ID:** `sha256:campaign-fixture`
- **Campaign schema:** 12
- **Campaign execution state:** COMPLETED
- **Scenario:** synthetic local fixture
- **Declared environment:** development
- **Target origin:** http://127.0.0.1:18080
- **Start time:** 2026-10-19T12:00:00Z
- **Host type:** NOT_RECORDED
- **Required phase duration reviewed:** 20s
- **Base active users:** 2
- **Workload fingerprint:** `sha256:workload-fixture`
- **Campaign schedule:** warm-up 0s; stabilization 0s; observation 10s; spike 0 users for 0s; recovery 10s; soak 0s; cooldown 0s; repeats 2; wall-time budget 3m20s
- **Effective drift screen policy** (historical default when not persisted): minimum 3 windows, 20 samples/window, p95 increase 20.000% and 5.000 ms, throughput decrease 20.000%
- **Generator guardrails:** disabled
- **Exact calibration required:** no exact coverage requirement

### Recorded phase workload controls

| Trial | Phase | Active users | Duration | max_rps | p95 limit (ms) | max error rate | Request timeout | Connection policy | Workload fingerprint |
| ---: | --- | ---: | --- | ---: | ---: | ---: | --- | --- | --- |
| 1 | observation | 2 | 10s | 20 | 500.000 | 0.01000 | 2s | reuse | `sha256:workload-fixture` |

### Generator environment by phase

Runtime identity consistency: ****; effective CPU capacity consistency: ****. Physical host type: **NOT_RECORDED**.

| Trial | Phase | OS / architecture | Go version | Runner identity | Logical CPUs | GOMAXPROCS | Effective CPU cores | CPU evidence source |
| ---: | --- | --- | --- | --- | ---: | ---: | ---: | --- |

The phase configuration and fingerprint do not contain a complete request definition. Preserve the private scenario/profile and inputs; verify a saved profile against this campaign with `damocline campaign profile verify --name NAME --campaign <campaign-file>`. Profile verification is offline and generates no traffic.

### Phase replay

| Trial | Phase | Delivery verdict | Measurement quality | Stability | Observation seconds | Phase evidence ID |
| ---: | --- | --- | --- | --- | ---: | --- |
| 1 | observation | PASS_OBSERVED | LIMITED | NO_LARGE_DRIFT_DETECTED | 10.000 | `sha256:phase-fixture` |

### Independent observation trials

| Trial | Requests | Request p95 (ms) | Request errors | Completed journeys | Journey p95 (ms) | Journey errors | Successful journeys/s |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 100 | 125.000 | not recorded | 50 | 250.000 | not recorded | 5.000 |

Repeat-trial pass fraction: **2/2** evaluable; excluded incomplete trials: 0; method: `EXACT_CLOPPER_PEARSON_BINOMIAL`; interval assumption: `INDEPENDENT_STATIONARY_REPEAT_TRIALS`. The interval is conditional and is not capacity confidence.

Confidence dimensions: measurement **LIMITED**, workload representativeness **NOT_ASSESSED**, decision **NOT_ESTABLISHED**.

### Audit findings

- Observed phase SLOs passed; sustainable capacity remains unproven.

### Confidence limits

- The workload mix is not independently validated.

### Known unknowns

- Physical host identity is not verified.

### Offline replay

Source campaign: `campaign.json` (verify its content against campaign evidence ID `sha256:campaign-fixture`).

```powershell
damocline audit --campaign 'campaign.json' --expect-evidence-id 'sha256:campaign-fixture' --peak-users 2 --growth-percent 25 --reserve-percent 10 --out 'replayed-assessment.json' --report-out 'replayed-capacity-report.md'
```

Report generated locally at `capacity.md`. The saved campaign is the source of truth; keep it with this assessment.
