# `glassbox auth-debug` — Authorization Debug Command Reference

The `auth-debug` command fetches a Soroban transaction from the Stellar network and analyzes its multi-signature and threshold-based authorization flow, identifying which signatures or thresholds failed.

---

## Synopsis

```
glassbox auth-debug [flags] <transaction-hash>
glassbox auth-debug --detailed <transaction-hash>
glassbox auth-debug --json <transaction-hash>
glassbox auth-debug --network testnet <transaction-hash>
```

---

## Arguments

| Argument | Description |
|---|---|
| `<transaction-hash>` | 64-character hexadecimal transaction hash. Required. |

**Validation:** The transaction hash is validated **before any network call is made**. A malformed hash produces an explicit error that echoes the offending value and states the expected format (64 hexadecimal characters), rather than surfacing a low-level RPC error later.

---

## Flags

| Flag | Default | Description |
|---|---|---|
| `--network`, `-n` | `mainnet` | Stellar network: `testnet`, `mainnet`, or `futurenet`. Auto-detected from the transaction when omitted. |
| `--rpc-url` | _(config)_ | Custom Horizon RPC URL. Validated for format when provided. Accepts comma-separated URLs for fallback. |
| `--detailed` | `false` | Show detailed analysis, summary metrics, and missing signatures. Has no effect when combined with `--json` (JSON output already contains full detail). |
| `--json` | `false` | Emit the authorization trace as machine-readable JSON on stdout. |

The `GLASSBOX_RPC_TOKEN` environment variable (or the `rpc_token` config value) supplies the RPC authentication token.

---

## Validation

All inputs are checked early in `PreRunE`, before any network connection is opened. The hash and RPC URL validations collect **all problems in a single pass** using `ValidateAuthTraceInputs`, so users see every issue at once instead of one error at a time:

1. **Transaction hash format** — must be exactly 64 hexadecimal characters.
2. **`--rpc-url` format** — when provided, each (optionally comma-separated) URL must use the `http` or `https` scheme and include a host.
3. **Network name** — must be a built-in network (`testnet`, `mainnet`, `futurenet`) or a custom network defined in config.

When `--network` is not supplied explicitly, the command attempts to auto-detect the network from the transaction and prints the resolved value (`Resolved network: testnet`).

Multiple validation failures are reported together in a single numbered list so users can fix all problems before retrying.

---

## Error Handling & Diagnostics

The command returns explicit, actionable errors and propagates the precise underlying failure rather than flattening every problem into a generic "connection failed":

| Failure | Behavior |
|---|---|
| Invalid transaction hash | `invalid transaction hash "…": …` — echoes the value, states the 64-hex-character requirement, and shows an example. |
| Invalid `--rpc-url` | `--rpc-url "…" is not valid: …` — explains the scheme/host requirement with an example. |
| Invalid `--network` | `invalid network "…" — must be one of: testnet, mainnet, futurenet`. |
| Transaction not found | The RPC client's precise "transaction not found" error is surfaced verbatim, with a hint to check the hash and that `--network` matches where the transaction was submitted. |
| RPC connection failure | The RPC client's connection error is surfaced verbatim, with its remediation hint. |
| Empty envelope | `transaction … was fetched but its envelope is empty; authorization cannot be analyzed` — with guidance to confirm the hash and network. |

### No-authorization-data diagnostic

When a transaction contains no Soroban authorization entries, no authorization events are extracted. In that case the report's status of `SUCCEEDED` means only *"no failures were recorded"* — **not** that authorization was verified as successful. To avoid that being misread as a pass, the command prints a warning to **stderr**:

```
warning: no authorization events were extracted from transaction <hash>.
  The report below reflects "no failures recorded", not a verified-successful authorization.
  This is expected for transactions that contain no Soroban authorization entries.
  Verify the hash and --network, or run 'glassbox doctor' if you expected auth data.
```

Because the warning is written to stderr, `--json` output on stdout stays clean and machine-parseable.

---

## Session persistence

An auth-debug run can be recorded as an **auth session** so its inputs and
outcome can be associated with the parent debug session and replayed or audited
later. The persisted record (`session.AuthSession`) captures the transaction
hash, network, RPC endpoint, the `--detailed`/`--json` flags, the number of
authorization events / failures / missing signatures, and the terminal status.

Before an auth session is written or replayed it is checked by
`session.ValidateAuthSession`, which rejects corrupt or inconsistent records
**up front** with explicit, field-level diagnostics rather than letting a
malformed record fail deep in persistence. The validated invariants are:

| Field | Rule |
|---|---|
| `SessionID` | Must be present (links the analysis to a parent debug session). |
| `TxHash` | Must be exactly 64 hexadecimal characters. |
| `Network` | Must be a recognised Stellar network (`testnet`, `mainnet`, `futurenet`). |
| `RPCURL` | When set, must be an `http`/`https` URL with a host. |
| `StartedAt` | Must be non-zero. |
| `CompletedAt` | When set, must not be before `StartedAt`. |
| `AuthEventCount`, `FailureCount`, `MissingSignatureCount` | Must be non-negative. |
| `Status` | Must be one of `completed`, `failed`, `no_auth_data`. |
| `Error` | Required when `Status` is `failed`; must be empty otherwise. |
| `FailureCount` | Must be `0` when `Status` is `no_auth_data`. |

Each violation is reported as a `{Field, Description, Hint}` issue, and
`AuthSessionReport.Error()` renders them as a single actionable, multi-line
message (every problem is listed, not just the first), so a bad record is
surfaced clearly instead of as a low-level persistence error.

---

## Examples

```sh
# Analyze authorization for a transaction (network auto-detected)
glassbox auth-debug 5c0a1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab

# Force a specific network
glassbox auth-debug --network testnet 5c0a1234...ef7890ab

# Detailed analysis with summary metrics and missing signatures
glassbox auth-debug --detailed 5c0a1234...ef7890ab

# Machine-readable JSON output (--detailed has no effect here; JSON always includes full detail)
glassbox auth-debug --json 5c0a1234...ef7890ab

# Use a custom RPC endpoint
glassbox auth-debug --rpc-url https://soroban-testnet.stellar.org --network testnet 5c0a1234...ef7890ab
```

---

## See Also

- [`glassbox debug`](./debug-command.md) — full transaction execution trace and all debug flags
- [`glassbox session` subcommand reference](./session-reference.md) — save, resume, and manage auth-debug sessions
- [Troubleshooting Index — Signing symptoms](./troubleshooting-index.md#signing-symptoms) — HSM and signing error routing
- [Navigation map](./navigation-map.md) — goal-oriented journey index
