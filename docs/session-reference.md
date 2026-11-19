# `glassbox session` — Subcommand Reference

This page is the single reference for every `glassbox session` subcommand.
Flags, arguments, exit codes, and worked examples are drawn directly from
`internal/cmd/session*.go` and companion files.  Navigating here from the
troubleshooting index or `--help` output gives you the full picture without
reconstructing context from several separate guides.

---

## Synopsis

```
glassbox session <subcommand> [flags] [args]
```

Available subcommands:

| Subcommand | Purpose |
|---|---|
| `save` | Persist the active session to disk |
| `resume` / `load` | Restore a saved session |
| `list` | List all saved sessions |
| `delete` | Remove a saved session |
| `recover` | Restore a session interrupted by a crash |
| `doctor` | Check sessions for schema and integrity problems |
| `lock:status` | Print the advisory lock status for a session |
| `gc` | List and clean up expired or excess sessions |
| `import` | Import a `.gbx` archive into the local store |
| `share` | Export a session as a portable `.gbx` archive |
| `load` (archive) | Load a `.gbx` archive as the active session |
| `migration` | Show the schema migration status for a session |
| `provenance` | Show a session's provenance timeline |

---

## Global session flags

These persistent flags apply to every subcommand that opens the session store:

| Flag | Environment variable | Default | Purpose |
|---|---|---|---|
| `--encrypt` | `GLASSBOX_SESSION_ENCRYPTION` | `false` | Enable at-rest encryption for the session store |
| `--key-provider` | `GLASSBOX_SESSION_KEY_PROVIDER` | _(none)_ | Key provider for session encryption |
| `--key-passphrase` | `GLASSBOX_SESSION_KEY_PASSPHRASE` | _(none)_ | Passphrase for the key provider |

---

## `session save`

Persist the active debug session to disk.

**Requirements:** An active session must exist — run `glassbox debug <tx-hash>` first.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--id` | _(auto-generated)_ | Explicit session ID |
| `--name` | _(none)_ | Bookmark name (max 128 characters) |
| `--pin-endpoint` | _(none)_ | Pin a custom RPC URL with this session |
| `--force` | `false` | Overwrite a conflicting concurrent save without merging |
| `--plan` | `false` | Print what `save` would do without writing anything |

### Concurrency safety

Two mechanisms protect concurrent saves:

1. **Advisory lock** — a per-session lock file in `~/.glassbox/locks/` ensures only one writer at a time. A lock from a crashed process clears automatically after 5 minutes.
2. **Revision check** — each session carries a monotonically increasing revision counter. A conflict is detected when another process saved a newer revision between your last load and now.

A conflict surfaces as a `SESSION_WRITE_CONFLICT` error:
```
Session conflict: session <id> was modified by another process
  (expected revision N, found N+1).
  Run 'glassbox session resume <id>' to reload and re-apply, or
  re-run with --force to overwrite it.
```

### Validation

Before writing, the following fields are checked:

- Transaction hash is present
- Network is one of: `testnet`, `mainnet`, `futurenet`
- Status is a recognized value (auto-set to `active` if empty)
- Session name, when provided, must not exceed 128 characters
- Horizon URL is auto-populated from the network when not set

### Examples

```bash
# Save with an auto-generated ID
glassbox session save

# Save with a custom ID
glassbox session save --id my-debug-session

# Save a named bookmark
glassbox session save --name payroll-bug

# Pin a custom RPC endpoint with this session
glassbox session save --pin-endpoint https://soroban-testnet.stellar.org

# Preview what save would do without writing
glassbox session save --plan

# Force-overwrite a revision conflict
glassbox session save --force
```

### Exit codes

| Outcome | Exit code |
|---|---|
| Session saved successfully | `0` |
| No active session | `1` (`SIMULATION_LOGIC_ERROR`) |
| Revision conflict (without `--force`) | `1` (`SESSION_WRITE_CONFLICT`) |
| Advisory lock held by another process | `1` (`SESSION_LOCK_HELD`) |
| Store open failure | `2` |

---

## `session resume` / `session load`

Restore a saved session by ID or bookmark name.

`resume` and `load` are aliases for the same command.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--read-only` | `false` | Load without triggering an automatic schema migration |

### Lookup precedence

The argument is matched against:
1. Exact session ID
2. ID prefix (unambiguous)
3. Transaction hash
4. Bookmark name (fuzzy)

### Validation

An integrity check runs before the session is made active. Corrupt sessions are
never activated — each integrity failure is listed with a remediation hint:

```
Session integrity check FAILED for abc123:
  1. [tx_hash] field is empty
     Hint: re-run 'glassbox debug <tx-hash> --network testnet' to regenerate this session
...
This session cannot be resumed safely.
To remove it:  glassbox session delete abc123
To re-debug:   glassbox debug <tx-hash> --network <network>
```

### Examples

```bash
# Resume by ID
glassbox session resume abc123

# Resume by bookmark name (alias)
glassbox session load payroll-bug

# Load without migrating schema (inspect raw stored data)
glassbox session resume abc123 --read-only
```

### Exit codes

| Outcome | Exit code |
|---|---|
| Session restored | `0` |
| Session not found | `1` |
| Integrity check failed | `1` |
| Store open failure | `2` |

---

## `session list`

List all saved sessions ordered by most-recently accessed.

Shows: session ID, bookmark name, network, last access time, transaction hash.
Expired sessions are pruned automatically before listing.

### Examples

```bash
# List all sessions
glassbox session list

# Then resume or delete by the displayed ID
glassbox session resume <session-id>
glassbox session delete <session-id>
```

### Empty store output

```
No saved sessions found.
Tip: run 'glassbox debug <tx-hash> --network testnet' to start a new session,
     then 'glassbox session save' to persist it.
```

### Exit codes

| Outcome | Exit code |
|---|---|
| List rendered (including empty) | `0` |
| Store open failure | `2` |

---

## `session delete`

Remove a saved session by ID or bookmark name. This action cannot be undone.

```bash
# Delete by ID
glassbox session delete abc123

# List sessions first if you need to find the ID
glassbox session list
glassbox session delete <session-id>
```

### Exit codes

| Outcome | Exit code |
|---|---|
| Session deleted | `0` |
| Session not found | `1` |
| Store open failure | `2` |

---

## `session recover`

Restore a session interrupted by an unexpected process exit (crash, SIGKILL,
power loss).

The command looks for an orphaned checkpoint at `~/.glassbox/active_session.json`.
If the owning process is still alive, recovery is refused with guidance to wait.
If the process is gone, the checkpoint is validated, stale advisory locks are
removed, and the session is reloaded from the store.

### What recovery checks

1. **Checkpoint completeness** — session ID, transaction hash, network, and PID are present and well-formed.
2. **Process liveness** — the checkpoint PID must not be running.
3. **Stale lock cleanup** — any advisory lock left by the crashed process is cleared before the store is opened.
4. **Session integrity** — the loaded session passes the same integrity check as `session resume`.

### Examples

```bash
# Check for and restore an orphaned session after a crash
glassbox session recover

# If nothing is found, start fresh
glassbox debug <tx-hash> --network testnet
```

### Output when no checkpoint exists

```
No crash-recovery checkpoint found. Nothing to recover.
```

### Exit codes

| Outcome | Exit code |
|---|---|
| Recovered successfully | `0` |
| No checkpoint found | `0` |
| Checkpoint corrupt | `1` |
| Owning process still alive | `1` |
| Integrity check failed | `1` |
| Store open failure | `2` |

---

## `session doctor`

Check all saved sessions for schema compatibility and data integrity. Reports
problems with actionable hints without modifying any session.

```bash
glassbox session doctor
```

See [session-doctor-examples.md](./session-doctor-examples.md) for annotated
example output covering each class of finding.

---

## `session lock:status`

Print the advisory lock metadata for a session without acquiring the lock.
Use this to diagnose `SESSION_LOCK_HELD` errors.

### Examples

```bash
# Check whether a session is locked
glassbox session lock:status abc123

# If locked by a dead process, run recover
glassbox session recover
```

### Output when not locked

```
Session "abc123": not locked
```

### Output when locked

```
Session "abc123": locked
  PID              : 12345
  Hostname         : dev-machine
  Glassbox version : 1.4.2
  Acquired         : 2026-11-18T10:30:00Z
```

### Exit codes

| Outcome | Exit code |
|---|---|
| Status printed | `0` |
| Session ID missing | `1` |
| Lock metadata read failure | `2` |

---

## `session gc`

List saved sessions with their age and size, and remove those that exceed the
configured retention policy.

**Protected sessions:** Active sessions and bookmarked (named) sessions are
never deleted regardless of age, count, or size.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--dry-run` | `false` | Preview what would be removed without deleting |
| `--max-age` | _(store default)_ | Remove sessions older than this duration (e.g. `168h`) |
| `--max-count` | _(store default)_ | Keep at most this many sessions |
| `--max-size` | `0` (unlimited) | Maximum total retained size in bytes |
| `--status` | _(any)_ | Only remove sessions with this status: `saved`, `expired`, or `recovered` |
| `--exclude` | _(none)_ | Protect additional named sessions (repeatable) |
| `--root` | `~/.glassbox` | Glassbox data directory to clean up |

### Session markers in output

| Marker | Meaning |
|---|---|
| `P` | Pinned (bookmarked) — protected |
| `A` | Active — protected |
| `X` | Eligible for deletion |
| _(space)_ | Retained, not eligible |

### Examples

```bash
# Preview what would be removed
glassbox session gc --dry-run

# Remove sessions older than 7 days, keeping at most 100
glassbox session gc --max-age 168h --max-count 100

# Enforce a 50 MiB total size budget
glassbox session gc --max-size 52428800

# Only consider expired sessions
glassbox session gc --status expired --dry-run

# Clean up a non-default data directory
glassbox session gc --dry-run --root /path/to/.glassbox
```

---

## `session import`

Import a `.gbx` archive produced by `glassbox session share` into the local
store with explicit conflict resolution.

Unlike `session load` (which only makes the archive the active in-memory
session), `import` persists the session to disk and resolves any ID conflict.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--on-conflict` | `fail` | Conflict resolution policy: `fail`, `rename`, `merge`, or `replace` |
| `--preview` | `false` | Show conflicts without importing anything |

### Conflict policies

| Policy | Behavior |
|---|---|
| `fail` | Reject the import and list every conflicting field (default; non-destructive) |
| `rename` | Keep both sessions by assigning the import a freshly generated ID |
| `merge` | Combine mergeable metadata (bookmark name, annotations) into the existing session |
| `replace` | Overwrite the existing session with the incoming data; preserves `CreatedAt` and audit chain from the existing record (destructive) |

### Examples

```bash
# Preview conflicts before deciding a policy
glassbox session import ./shared-session.gbx --preview

# Default: fail on conflict (non-destructive)
glassbox session import ./shared-session.gbx

# Keep both sessions under separate IDs
glassbox session import ./shared-session.gbx --on-conflict rename

# Merge annotations and bookmark into the existing session
glassbox session import ./shared-session.gbx --on-conflict merge

# Replace the existing session entirely (destructive)
glassbox session import ./shared-session.gbx --on-conflict replace
```

---

## `session share`

Export a session as a self-contained `.gbx` archive for sharing with another
machine or team member.

The archive contains all replay inputs, simulation results, and metadata needed
to reproduce the session. The session data is validated before export.

### Flags

| Flag | Default | Description |
|---|---|---|
| `-o` / `--output` | _(auto-generated `.gbx`)_ | Output archive path |
| `--redact` | `full` | Redaction profile: `strict`, `balanced`, or `full` (unredacted) |
| `--preview` | `false` | Show what `--redact` would remove without writing an archive |
| `--secret-scan-mode` | _(none)_ | Secret scanning: `opt-in` (warn only) or `strict` (block export) |
| `--secret-scan-override` | _(none)_ | Paths allowed to contain secrets (for test fixtures; repeatable) |

### Redaction profiles

| Profile | Effect |
|---|---|
| `full` | No redaction; all fields exported as-is (default) |
| `balanced` | Pseudonymizes identifiers while preserving structure |
| `strict` | Removes all identifying fields |

### Examples

```bash
# Export the active session (auto-generated filename)
glassbox session share

# Export a specific saved session
glassbox session share abc123 --output ./debug-session.gbx

# Preview what balanced redaction would remove before exporting
glassbox session share abc123 --redact balanced --preview

# Export with strict redaction for external sharing
glassbox session share abc123 --redact strict --output ./redacted-session.gbx

# Block export if any secrets are detected
glassbox session share abc123 --secret-scan-mode strict
```

---

## `session load` (archive)

Load a `.gbx` archive created by `glassbox session share` as the active
in-memory session. The session is not persisted until you run
`glassbox session save`.

```bash
# Load an archive and make it the active session
glassbox session load ./glassbox-session-abc123.gbx

# Then persist it
glassbox session save --id restored-session
```

---

## `session migration`

Show the schema migration status for a session without modifying it.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--json` | `false` | Emit migration status as JSON |

### Examples

```bash
# Check migration status by session ID
glassbox session migration abc123

# Machine-readable JSON output
glassbox session migration abc123 --json
```

### Exit codes

| Outcome | Exit code |
|---|---|
| Session is current | `0` |
| Session requires migration | `1` |
| Session version is unsupported | `2` |

To apply a migration: `glassbox session resume <id>`
To recreate an unsupported session: `glassbox debug <tx-hash> --network <network>`

---

## `session provenance`

Show the append-only history of state transitions for a session: when it was
fetched, replayed, annotated, exported, imported, or migrated.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--json` | `false` | Emit the timeline as JSON |

### Examples

```bash
# Show provenance timeline
glassbox session provenance abc123

# JSON output for automation
glassbox session provenance abc123 --json
```

---

## Error codes

| Code | Trigger |
|---|---|
| `SESSION_WRITE_CONFLICT` | Concurrent process saved a newer revision |
| `SESSION_LOCK_HELD` | Advisory lock held by a live process |
| `VALIDATION_FAILED` | Input validation failure (bad ID format, name too long, etc.) |
| `SIMULATION_LOGIC_ERROR` | No active session when `save` or `share` is called |

For the full error code catalogue see [stable-error-codes.md](./stable-error-codes.md).

---

## See also

- [Session locking](./session-locking.md) — advisory lock lifecycle and revision checks
- [Session bookmarking](./session-bookmarking.md) — named bookmarks and lookup
- [Session doctor examples](./session-doctor-examples.md) — annotated doctor output
- [Session portability](./session-portability.md) — `.gbx` archive format
- [Session recovery decision](./session-recovery-decision.md) — when to recover vs. re-debug
- [Debug command](./debug-command.md) — how to create a session with `glassbox debug`
- [Troubleshooting index](./troubleshooting-index.md) — symptom routing
