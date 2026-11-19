# ADR 0140: require recorded campaign stability thresholds

Campaign readers validate all five fields in `stability_policy` for closed-loop
schema v13+ and open-arrival schema v8+. These versions persist stability controls
alongside the objective contract. Typed decoding alone can turn an omitted or
null numeric field into a zero threshold and give an offline assessment a policy
that the artifact never recorded.

The presence check accepts explicit zero and leaves semantic validation to the
existing policy validator. Older schema rules and the serialized format remain
unchanged. The check runs before decoding through the shared campaign readers.
