# ADR 0165: Plan capacity from observed delivery

The closed-loop capacity command reuses the native baseline, explorer,
campaign, and offline auditor. A bounded baseline runs before workload
traffic. A calibration adapter validates a supplied artifact and forwards
it to campaign. Automatic scenario calibration can replace that adapter.

At the highest repeatedly passing level, use the slowest measured sample
rate across repeats and required series. Divide the larger of the
zero-failure binomial and p95 rank sample floors by that rate, rounding up
to seconds. Plan three full observation windows and at least one full
recovery window. Refuse to start the campaign if the duration or budget is
outside supported ceilings. The estimate is conditional on delivery rates
persisting and does not guarantee passing statistical bounds.

Keep stages in a private output directory. Resume requires the original
scenario digest and scheduling controls, rejects concurrent runs, and
reuses readable completed stages. Archive interrupted artifacts and start a
fresh bounded stage following explicit authorization. Do not join partial
trial counters with a new run.
