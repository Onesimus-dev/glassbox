# Troubleshooting Index

This index maps common **symptoms** to the existing troubleshooting page that
covers them. It does not duplicate any procedure — every entry links to an
existing guide. Use this page as the entry point when you know *what* is going
wrong but not *which document* explains how to fix it.

> **How to use:** Find the symptom that matches what you are seeing, then follow
> the link to the relevant guide. Each guide contains the full procedure, error
> messages, and remediation steps.

---

## Setup symptoms

| Symptom | What you might see | See |
|---|---|---|
| Simulator binary missing | `simulator binary not found`; `glassbox doctor --fix` suggests building the simulator | [Operator Runbook — Simulator binary not found](./operator-runbook.md#simulator-binary-not-found) |
| Build artifact missing / invalid WASM | `--wasm: file not found`; `not a valid WASM binary (bad magic bytes)` | [Debug command — Build Artifact Discovery](./debug-command.md#build-artifact-discovery) |
| Source discovery exhausted | `contract source not found: all discovery stages exhausted` | [Source Mapping Troubleshooting — Step 6](./source-mapping-troubleshooting.md#step-6--registry-and-github-fallback) |
| Configuration invalid / unreadable | Config file unreadable or invalid (`CONFIG_ERROR`) | [Stable Error Codes — General](./stable-error-codes.md#general) |
| Watch mode hangs / pending / timeout | Watcher stays pending, network timeout, cancellation | [Watch Mode — Troubleshooting](./WATCH_MODE.md#troubleshooting-watch-mode) |

---

## RPC symptoms

| Symptom | What you might see | See |
|---|---|---|
| RPC connection failed | `RPC connection failed: dial tcp ...`; all failover endpoints report the same error | [Operator Runbook — RPC connection failures](./operator-runbook.md#rpc-connection-failures) |
| RPC timeout | Debug run hangs then fails with timeout; `--show-metrics` shows high `rpc_avg_ms` | [Operator Runbook — RPC timeouts](./operator-runbook.md#rpc-timeouts) |
| Transaction not found | `transaction not found` after successful RPC connection | [Operator Runbook — Transaction not found](./operator-runbook.md#transaction-not-found) |
| Rate limit exceeded | `rate limit exceeded` from RPC; multiple rapid requests fail | [Operator Runbook — Rate limiting](./operator-runbook.md#rate-limiting) |
| Unauthorized / invalid auth token | `unauthorized` or `missing/invalid auth token` | [Operator Runbook — Authentication errors](./operator-runbook.md#authentication-errors) |
| All RPC endpoints failed | `ALL_RPC_FAILED`; every endpoint in the failover pool is down | [Operator Runbook — RPC connection failures](./operator-runbook.md#rpc-connection-failures) |

---

## Source-map symptoms

| Symptom | What you might see | See |
|---|---|---|
| Missing build artifacts | `source discovery: local build directory not found`; `no .wasm files found` | [Source Mapping Troubleshooting — Symptom table](./source-mapping-troubleshooting.md#symptom-table) |
| Invalid / corrupt WASM | `does not have a valid WASM magic number`; `could not read ... (skipped)` | [Source Mapping Troubleshooting — Symptom table](./source-mapping-troubleshooting.md#symptom-table) |
| Hash mismatch | `build mismatch: local WASM hash ... does not match on-chain hash ...` | [Source Mapping Troubleshooting — Symptom table](./source-mapping-troubleshooting.md#symptom-table) |
| DWARF debug info missing | `mapping_quality: heuristic` or `unknown`; WAT shown instead of source | [Source Mapping Troubleshooting — Step 3](./source-mapping-troubleshooting.md#step-3--dwarf-debug-symbols-present) |
| Wrong source paths | `Location: <path>:L:C` printed with no source excerpt; DWARF resolves to `unknown` | [Source Mapping Troubleshooting — Step 4](./source-mapping-troubleshooting.md#step-4--project-root-and-source-paths) |
| Stale source alias | `alias: target for "..." must point to an existing directory` | [Source Mapping Troubleshooting — Symptom table](./source-mapping-troubleshooting.md#symptom-table) |
| Mapping disabled | `skip_source_mapping_env` preflight flag | [Source Mapping Troubleshooting — Symptom table](./source-mapping-troubleshooting.md#symptom-table) |

---

## Session symptoms

| Symptom | What you might see | See |
|---|---|---|
| Session integrity failure | `Session integrity check FAILED` on `session resume`; checkpoint validation errors after a crash | [Operator Runbook — Session integrity failures](./operator-runbook.md#session-integrity-failures) |
| Crash recovery | Session left in partially saved state after crash/SIGKILL/power loss | [Debug command — Session Recovery and Integrity](./debug-command.md#session-recovery-and-integrity) |
| Session write conflict | `SESSION_WRITE_CONFLICT`; concurrent writer saved a newer revision | [Session Locking](./session-locking.md#optimistic-revision-check) |
| Advisory lock held | `SESSION_LOCK_HELD`; lock file held by a live process | [Session Locking](./session-locking.md#advisory-lock-lifecycle) |
| Stale lock after crash | Lock file remains after owning process died | [Session Locking — Stale-lock cleanup](./session-locking.md#stale-lock-cleanup) |

---

## Signing symptoms

| Symptom | What you might see | See |
|---|---|---|
| Module file not found (PKCS#11) | `module file not found: ...` | [Audit Signing — Troubleshooting](./audit-signing.md#troubleshooting) |
| No slots with tokens / token label not found | `no slots with tokens found`; `no token with label "..." found` | [Audit Signing — Troubleshooting](./audit-signing.md#troubleshooting) |
| PIN incorrect / locked | `PIN authentication failed`; `the token PIN is locked` | [Audit Signing — Troubleshooting](./audit-signing.md#troubleshooting) |
| Key not found | `signing key "..." not found` | [Audit Signing — Troubleshooting](./audit-signing.md#troubleshooting) |
| Mechanism not supported (CKM_EDDSA) | `the CKM_EDDSA mechanism is not supported` | [Audit Signing — Troubleshooting](./audit-signing.md#troubleshooting) |
| Software key wrong format | OpenSSH / SEC1 / RSA key instead of PKCS#8 Ed25519 PEM | [Audit Signing — Software provider input validation](./audit-signing.md#software-provider-input-validation) |
| PKCS#11 validation limitation | `module_load` fails with `C_Initialize is not available` in the current build | [Audit Signing — Limitation in the current build](./audit-signing.md#limitation-in-the-current-build) |

---

## Stable error-code reference

Every structured Glassbox error carries a stable `Code` (automation contract) that
you can key on instead of parsing free-form text. Codes are grouped below by
symptom area; the full catalogue with exit codes is in
[Stable Error Codes](./stable-error-codes.md).

| Area | Code | Meaning |
|---|---|---|
| Setup | `SIMULATOR_NOT_FOUND` | Simulator binary absent or not executable |
| Setup | `SOURCE_DISCOVERY_FAILED` | Source mapping unavailable for contract |
| RPC | `RPC_CONNECTION_FAILED` | Endpoint unreachable |
| RPC | `RPC_TIMEOUT` | Request timed out |
| RPC | `ALL_RPC_FAILED` | All failover endpoints failed |
| RPC | `RPC_ERROR` | RPC server returned a non-200 / error response |
| RPC | `TRANSACTION_NOT_FOUND` | TX hash not found on the selected network |
| RPC | `LEDGER_NOT_FOUND` | Ledger sequence unavailable |
| RPC | `LEDGER_ARCHIVED` | Ledger has been archived |
| RPC | `RATE_LIMIT_EXCEEDED` | RPC rate limit hit |
| RPC | `UNAUTHORIZED` | Missing or invalid auth token |
| RPC | `INVALID_NETWORK` | Unrecognised `--network` value |
| RPC | `NETWORK_NOT_FOUND` | Named network has no known endpoint |
| Session | `SESSION_WRITE_CONFLICT` | Concurrent writer saved a newer revision |
| Session | `SESSION_LOCK_HELD` | Advisory lock held by a live process |

Exit-code taxonomy: 0 = success, 1 = user error, 2 = config/environment error,
3 = internal/runtime error, 130 = interrupted (SIGINT). See
[Exit Codes](./exit-codes.md) for the full mapping.

---

## Command-help reference

When you know the flag or command but want to confirm accepted values and
defaults, run:

```bash
glassbox debug --help          # all debug flags and their defaults
glassbox --help                # global flags (--profile, --log-level, --json, ...)
glassbox session --help        # session subcommands
glassbox audit:sign --help     # signing flags
glassbox protocol:diagnose     # glassbox:// registration status
glassbox doctor                # comprehensive environment health check
```

For machine-readable output pipe through `--json` or `--format json`, then key
on `error.code` in the JSON envelope (see
[Stable Error Codes — Output adapters](./stable-error-codes.md#output-adapters)).

---

## See also

- [Navigation map](./navigation-map.md) — goal-oriented journey index for all major workflows
- [Debug command](./debug-command.md) — complete `glassbox debug` reference
- [Session subcommand reference](./session-reference.md) — all `glassbox session` subcommands in one place
- [Operator runbook](./operator-runbook.md) — symptom-based RPC/simulator/session remediation
- [Source mapping troubleshooting](./source-mapping-troubleshooting.md) — source-mapping wizard
- [Audit signing](./audit-signing.md) — software and PKCS#11 HSM signing
- [Watch mode](./WATCH_MODE.md) — `--watch` and `--watch-files` symptoms
- [Session locking](./session-locking.md) — concurrency and revision handling
- [Stable error codes](./stable-error-codes.md) — error code catalogue
- [Exit codes](./exit-codes.md) — exit-code registry
