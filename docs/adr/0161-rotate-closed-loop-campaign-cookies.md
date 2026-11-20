# ADR 0161: Rotate closed-loop campaign cookies

Closed-loop campaigns can opt into `--rotate-vu-cookies`. With
`per_vu_cookie_env_prefix` set to `TEST_SESSION_`, the first two
trials read `TEST_SESSION_1_1` and `TEST_SESSION_2_1` for VU 1.

The CLI loads and validates every trial's full base or spike VU set
before any traffic. Canonical duplicates are rejected across all sets.
The CLI selects a trial's set using the existing synchronous campaign
checkpoint and seeds each phase from that set. The campaign artifacts
have no new credential fields. This applies to scenario-wide per-VU
cookies in the closed-loop campaign command. Distinct cookie values
provide no verification of distinct accounts or fresh server state.
