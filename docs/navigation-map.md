# Documentation Navigation Map

This page maps the most common user journeys to the right starting document.
Every entry links to an existing guide; nothing is duplicated here.

> **How to use:** Find the journey that matches your goal and follow the link.
> When you are unsure which journey applies, use the
> [Troubleshooting Index](./troubleshooting-index.md) instead — it routes by
> symptom rather than goal.

---

## Journey 1 — Debug a failed Soroban transaction

**You have a transaction hash and want to understand why it failed.**

| Step | Document |
|---|---|
| Run the debug command | [Debug command reference](./debug-command.md) |
| Connect to a custom RPC endpoint | [Debug command — Core Flags](./debug-command.md#core-flags) |
| Replay offline from a local XDR file | [Debug command — Local envelope file](./debug-command.md#local-envelope-file) |
| Map failures to Rust source lines | [Source Mapping](./source-mapping.md) |
| Fix a source-discovery error | [Source Mapping Troubleshooting](./source-mapping-troubleshooting.md) |
| View CPU / memory flamegraph | [Trace Profiling](./trace-profiling.md) |

---

## Journey 2 — Save and resume a debug session

**You want to persist work between CLI invocations or share context with a teammate.**

| Step | Document |
|---|---|
| All `session` subcommands in one place | [Session subcommand reference](./session-reference.md) |
| Session save, resume, list, delete, recover | [Session subcommand reference — save / resume / list](./session-reference.md#session-save) |
| Understand advisory locks and revision conflicts | [Session Locking](./session-locking.md) |
| Share a session with another machine | [Session Portability](./session-portability.md) |
| Import a shared `.gbx` archive | [Session subcommand reference — import](./session-reference.md#session-import) |
| Recover after a crash | [Session subcommand reference — recover](./session-reference.md#session-recover) |
| Check sessions for schema problems | [Session doctor examples](./session-doctor-examples.md) |

---

## Journey 3 — Sign and verify an audit log

**You want to produce a signed, deterministic audit record of a debug run.**

| Step | Document |
|---|---|
| Software signing (Ed25519) and PKCS#11 HSM signing | [Audit Signing](./audit-signing.md) |
| AWS KMS signing | [Audit KMS Signing](./audit-kms-signing.md) |
| Canonical payload serialization | [Audit Canonicalization](./audit-canonicalization.md) |
| Verifying a signed log | [Audit Verify Command](./audit-verify-command.md) |
| Trust policy and directory verification | [Audit Verify Policy](./audit-verify-policy.md) |
| Audit bundle contents | [Audit Bundle Contents](./audit-bundle-contents.md) |

---

## Journey 4 — Analyze authorization failures

**You see a multi-sig or threshold-authorization error and want to trace which signatures failed.**

| Step | Document |
|---|---|
| `auth-debug` command flags and examples | [Auth Debug Command](./auth-debug-command.md) |
| Understanding the authorization trace | [Auth Debug Command — Output](./auth-debug-command.md#output) |
| Troubleshoot RPC connectivity | [Operator Runbook — RPC connection failures](./operator-runbook.md#rpc-connection-failures) |

---

## Journey 5 — Set up the development environment

**You are a new contributor or setting up Glassbox for the first time.**

| Step | Document |
|---|---|
| Prerequisites, build steps, and running tests | [README](../README.md#getting-started) |
| Command development conventions | [Command Development Guide](./command-development-guide.md) |
| Strict linting rules | README — Code Quality & Linting |
| Binary size thresholds | README — Binary Size Tracking |
| Changelog fragment requirements | [Changelog Fragments](./changelog-fragments.md) |

---

## Journey 6 — Automate with machine-readable output

**You are integrating Glassbox into CI or a monitoring pipeline.**

| Step | Document |
|---|---|
| `--json` / `--format json` output schema | [JSON CLI Output](./json-output.md) |
| NDJSON progress events | [Progress Events](./progress-events.md) |
| Stable error codes (`error.code`) | [Stable Error Codes](./stable-error-codes.md) |
| Exit code registry | [Exit Codes](./exit-codes.md) |
| API compatibility matrix | [Compatibility Matrix](./compatibility-matrix.md) |
| Generating and validating ABI bindings | [Binding Validation](./binding-validation.md) |
| Regression test harness | [Regression Test Guide](./regression-test-guide.md) |

---

## Journey 7 — Investigate performance or observability

**You want metrics, traces, or flamegraphs from a debug run.**

| Step | Document |
|---|---|
| `--show-metrics` RPC and simulation stats | [Debug command — Performance Metrics](./debug-command.md#performance-metrics) |
| CPU / memory flamegraph (`--profile`) | [Trace Profiling](./trace-profiling.md) |
| OpenTelemetry, Prometheus, and telemetry events | [Observability Troubleshooting](./observability-troubleshooting.md) |
| Telemetry data collected | [Telemetry Metadata](./telemetry-metadata.md) |
| Telemetry sampling strategy | [Telemetry Sampling](./telemetry-sampling.md) |

---

## Journey 8 — Understand the trace output

**You have a trace and want to interpret events, token flows, or state changes.**

| Step | Document |
|---|---|
| Trace event schemas | [Event Schemas](./event-schemas.md) |
| Event ordering guarantees | [Event Ordering Contract](./event-ordering-contract.md) |
| Token flow display (XLM and Soroban assets) | [Debug command reference](./debug-command.md) |
| Export the trace to a file | [Trace Export Validation](./trace-export-validation.md) |
| Compare traces across networks | [Trace Comparison Guide](./trace-comparison-guide.md) |
| Incremental viewer state persistence | [Incremental Trace Refresh](./incremental-trace-refresh.md) |

---

## Journey 9 — Manage the protocol handler

**You want to deep-link from a browser or dashboard into a debug session.**

| Step | Document |
|---|---|
| Register, verify, and repair `glassbox://` URI scheme | [README — Protocol Handler](../README.md#protocol-handler) |
| Diagnose registration issues | `glassbox protocol:diagnose` |

---

## Journey 10 — Review security posture

**You want to understand trust boundaries, security warnings, or run a security review.**

| Step | Document |
|---|---|
| Trust boundaries and component levels | [ADR-003 Trust Boundaries](./adr/003-trust-boundaries.md) |
| Security warnings and deprecated host functions | [Security Warnings](./security-warnings.md) |
| Secrets in archives (redaction and scanning) | [Audit Signing](./audit-signing.md) |
| Command security review checklist | [Command Security Review](./command-security-review.md) |
| Docker image hardening | [Docker Security](./docker-security.md) |
| SARIF security rules | [SARIF Diagnostic Rules](./sarif-diagnostic-rules.md) |
| Vulnerability scanner integration | [Vulnerability Scanner](./vulnerability-scanner.md) |

---

## Quick-reference: running `--help`

When you know the command but want to confirm accepted values and defaults:

```bash
glassbox debug --help            # all debug flags with defaults
glassbox session --help          # session subcommands
glassbox audit:sign --help       # signing flags
glassbox auth-debug --help       # authorization tracing flags
glassbox --help                  # global flags (--profile, --log-level, --json, …)
glassbox doctor                  # comprehensive environment health check
```

---

## See also

- [Troubleshooting Index](./troubleshooting-index.md) — route by symptom, not goal
- [Compatibility Matrix](./compatibility-matrix.md) — stability levels and deprecation policy
- [Stable Error Codes](./stable-error-codes.md) — full error code catalogue
- [Exit Codes](./exit-codes.md) — exit code registry
