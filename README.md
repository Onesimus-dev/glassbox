# Glassbox

**Glassbox** is a premium developer toolset for the Stellar network, designed to provide high-fidelity "glass-box" debugging and simulation for Soroban smart contracts.

> **Status**: Active Development (Phase 4: Advanced Diagnostics)
> **Documentation**: [https://dotandev-glassbox-75.mintlify.app/](https://dotandev-glassbox-75.mintlify.app/)
> **Focus**: High-Fidelity Simulation, Auth Tracing, and Security Auditing

## Scope & Objective

The primary goal of `Glassbox` is to eliminate the opaque "black box" experience of failed Stellar smart contract transactions. By providing local-first, high-fidelity replay and tracing, `Glassbox` maps generic network errors back to human-readable diagnostic events and source code.

**Core Features (Planned):**

1.  **Transaction Replay**: Fetch a failed transaction's envelope and ledger state from an RPC provider.
2.  **Local Simulation**: Re-execute the transaction logically in a local environment.
3.  **Trace decoding**: Map execution steps and failures back to readable instructions or Rust source lines.
4.  **Source Mapping**: Map WASM instruction failures to specific Rust source code lines using debug symbols.
5.  **GitHub Source Links**: Automatically generate clickable GitHub links to source code locations in traces (when in a Git repository).
6.  **Error Suggestions**: Heuristic-based engine that suggests potential fixes for common Soroban errors.

## Usage

### Debugging a Transaction

Fetches a transaction envelope from the Stellar network and simulates it locally.

```bash
# Debug on mainnet (network is auto-detected when the flag is omitted)
glassbox debug <transaction-hash>

# Debug explicitly on testnet
glassbox debug --network testnet <transaction-hash>

# Debug with a custom RPC endpoint
glassbox debug --network testnet --rpc-url https://soroban-testnet.stellar.org <transaction-hash>
```

Debug an offline envelope from a local XDR file (no RPC required):

```bash
glassbox debug --xdr-file tx.xdr
```

Or from a JSON envelope file:

```bash
glassbox debug --json-file tx.json
```

Expected output:

```
Debugging transaction: 5c0a...
Network: testnet
Transaction fetched successfully. Envelope size: 312 bytes

  ────────────────────────────────────────────────────────────
  Result for  testnet

  ✓  Status: success
  ℹ  Snapshot: complete
  ── Resource Usage
    CPU Instructions: 12345 / 100000000  (0.01%)
    Memory Bytes:     1024 / 41943040    (0.00%)
    Operations:       1

Session created: sess_abc123
Run 'glassbox session save' to persist this session.
```

### Local WASM Replay

Test a contract locally without any network connection:

```bash
glassbox debug --wasm ./target/wasm32-unknown-unknown/release/my_contract.wasm
```

Pass mock arguments:

```bash
glassbox debug --wasm ./contract.wasm --args "arg1" --args "arg2"
```

Enable hot-reload to automatically re-run when the WASM binary changes:

```bash
glassbox debug --wasm ./contract.wasm --hot-reload
```

### Demo Mode

Print sample output to test color detection without any network or WASM:

```bash
glassbox debug --demo
```

### Performance Profiling

Generate interactive flamegraphs to visualize CPU and memory consumption during contract execution.
The `--profile` flag is a global (root-level) flag:

```bash
glassbox --profile debug <transaction-hash>
```

Export format options:

```bash
# Interactive HTML (default)
glassbox --profile --profile-format html debug <transaction-hash>

# Raw SVG
glassbox --profile --profile-format svg debug <transaction-hash>
```

The flamegraph is written to `<tx-hash-prefix>.flamegraph.html` (or `.svg`) in the current directory.

See [docs/trace-profiling.md](docs/trace-profiling.md) for detailed documentation and [docs/examples/sample_flamegraph.html](docs/examples/sample_flamegraph.html) for a live demo.

### Dry-Run Validation

Validate all inputs and check the environment without running a simulation:

```bash
glassbox debug --dry-run --network testnet <transaction-hash>
```

This checks the transaction hash format, network validity, RPC reachability, simulator
binary presence, and protocol version — no simulation is executed.

### Comparing Networks

Run the same transaction through two networks and diff the results:

```bash
glassbox debug --network testnet --compare-network mainnet <transaction-hash>
```

### Watch Mode

Poll for a pending transaction and debug it once it lands on-chain:

```bash
glassbox debug --watch --watch-timeout 60 --network testnet <transaction-hash>
```

### Snapshot-Based Offline Replay

Save ledger state during a debug run for later offline replay:

```bash
# Save snapshot registry while debugging
glassbox debug --save-snapshots ./snapshots/my-tx.json --network testnet <transaction-hash>

# Replay later without any network connection
glassbox debug --load-snapshots ./snapshots/my-tx.json
```

### Audit Log Signing (software / HSM)

`Glassbox` can generate a deterministic, signed audit log from a JSON payload.

#### Software signing (Ed25519 private key)

Provide a PKCS#8 PEM Ed25519 private key via environment variable or flag:

- Env: `GLASSBOX_AUDIT_PRIVATE_KEY_PEM`
- Flag: `--software-private-key <pem-or-path>`

Example:

```bash
glassbox audit:sign \
  --payload '{"input":{},"state":{},"events":[],"timestamp":"2026-01-01T00:00:00.000Z"}' \
  --software-private-key "$(cat ./ed25519-private-key.pem)"
```

Read the payload from a file:

```bash
glassbox audit:sign --payload-file payload.json \
  --software-private-key ./ed25519-private-key.pem
```

#### PKCS#11 HSM signing

Select the PKCS#11 provider with `--signing-provider pkcs11` and configure the module,
token, and key via flags or environment variables.

Required:

- `--pkcs11-module` / `GLASSBOX_PKCS11_MODULE` — path to the PKCS#11 `.so`/`.dylib`/`.dll`
- `--pkcs11-pin` / `GLASSBOX_PKCS11_PIN` — user PIN
- `--pkcs11-key-label` / `GLASSBOX_PKCS11_KEY_LABEL` **or** `--pkcs11-key-id` / `GLASSBOX_PKCS11_KEY_ID` (hex)

Optional:

- `GLASSBOX_PKCS11_SLOT` — numeric slot index (default `0`; must be a non-negative integer)
- `GLASSBOX_PKCS11_TOKEN_LABEL` — select token by label
- `GLASSBOX_PKCS11_PUBLIC_KEY_PEM` — SPKI PEM public key embedded in the signed audit log

The PKCS#11 signer keeps the module, session, and key handle alive for the lifetime of
the signer instance. Stale sessions are retried once automatically before returning an
error.

Example:

```bash
export GLASSBOX_PKCS11_MODULE=/usr/lib/softhsm/libsofthsm2.so
export GLASSBOX_PKCS11_PIN=1234
export GLASSBOX_PKCS11_KEY_LABEL=glassbox-audit-key

glassbox audit:sign \
  --signing-provider pkcs11 \
  --payload '{"input":{},"state":{},"events":[],"timestamp":"2026-01-01T00:00:00.000Z"}'
```

#### Validating PKCS#11 configuration

Run a preflight check before signing to surface configuration errors with actionable hints:

```bash
glassbox audit:sign --signing-provider pkcs11 --validate-only \
  --pkcs11-module /usr/lib/softhsm/libsofthsm2.so --pkcs11-pin 1234
```

This verifies module loading, slot enumeration, PIN authentication, key lookup, and a test
signing operation — without touching any payload.

The command prints the signed audit log JSON to stdout so it can be redirected to a file.

For platform-specific module paths, YubiKey setup, and troubleshooting, see [docs/audit-signing.md](docs/audit-signing.md).

### Protocol Handler

Glassbox registers a custom `glassbox://` URI scheme, allowing external tools (browsers,
dashboards) to deep-link directly into a debug session.

Register the protocol handler:

```bash
glassbox protocol:register
```

Preview what registration would do without writing any OS state:

```bash
glassbox protocol:register --dry-run
```

Open a debug session via URI:

```bash
glassbox protocol:handle "glassbox://debug/<transaction-hash>?network=testnet"
```

With an optional operation index and view mode:

```bash
glassbox protocol:handle "glassbox://debug/<transaction-hash>?network=mainnet&op=0&view=flamegraph"
```

Verify the registration is working:

```bash
glassbox protocol:verify
```

Diagnose registration issues:

```bash
glassbox protocol:diagnose
```

Repair a broken registration:

```bash
glassbox protocol:repair
```

Check current registration status:

```bash
glassbox protocol:status
```

Unregister the handler when no longer needed:

```bash
glassbox protocol:unregister
```

### Session Management

```bash
# Debug a transaction and save the session
glassbox debug --network testnet <transaction-hash>
glassbox session save

# Save with a name for easy reference
glassbox session save --name payroll-bug

# List all saved sessions
glassbox session list

# Resume a session
glassbox session resume <session-id>

# Delete a session
glassbox session delete <session-id>

# Recover a session left by a crashed process
glassbox session recover

# Check sessions for schema and integrity problems
glassbox session doctor
```

### Cache Management

```bash
# Check cache usage
glassbox cache status

# Include RPC cache statistics
glassbox cache status --rpc

# Clean old entries (LRU)
glassbox cache clean

# Clean without confirmation prompt
glassbox cache clean --force

# Remove RPC cache entries older than 7 days
glassbox cache rpc --older-than 7

# Remove all testnet RPC cache entries
glassbox cache rpc --network testnet

# Clear all cached data
glassbox cache clear --force
```

### Telemetry

```bash
# Show current telemetry state and how to disable it
glassbox telemetry
```

Telemetry is **opt-in only** and is disabled by default. To opt in, set `telemetry_enabled = true`
in `~/.Glassbox/config.json` or run with `--telemetry`. To disable for the current shell session:

```bash
export GLASSBOX_TELEMETRY=false
```

No secrets are exported — transaction hashes, contract IDs, and file paths are sanitized
client-side before any data leaves the machine.

### Version Information

```bash
# Human-readable output
glassbox version

# Machine-readable JSON
glassbox version --json
```

## Documentation

- **[Navigation Map](docs/navigation-map.md)**: Goal-oriented journey index — find the right document for debugging, session management, signing, automation, observability, and security review without searching every guide.
- **[Session Subcommand Reference](docs/session-reference.md)**: Single-page reference for every `glassbox session` subcommand: flags, examples, exit codes, and conflict policies.
- **[Troubleshooting Index](docs/troubleshooting-index.md)**: Symptom-oriented index that routes common setup, RPC, source-map, session, and signing symptoms to the right existing troubleshooting page without duplicating procedures.
- **[Observability Troubleshooting](docs/observability-troubleshooting.md)**: Practical guide to logs, Prometheus metrics, OpenTelemetry traces, telemetry events, correlation IDs, and collection failure diagnosis.
- **[Source Mapping](docs/source-mapping.md)**: Implementation details for mapping WASM failures to Rust source code.
- **[JSON CLI Output](docs/json-output.md)**: Machine-readable `--json` / `--format json` options for automation.
- **[Audit Log Signing](docs/audit-signing.md)**: Software and HSM signing for audit logs.
- **[Audit KMS Signing](docs/audit-kms-signing.md)**: AWS KMS signing integration.
- **[Canonicalization](docs/audit-canonicalization.md)**: Deterministic JSON serialization for audit log hashing.
- **[Trace Profiling](docs/trace-profiling.md)**: CPU/memory flamegraph generation from contract traces.
- **[Trace Export Validation](docs/trace-export-validation.md)**: Validated `--trace-output` and format options.
- **[Incremental Trace Refresh](docs/incremental-trace-refresh.md)**: Incremental trace viewer state persistence.
- **[Snapshot Deduplication](docs/snapshot-deduplication.md)**: How ledger snapshots are deduplicated.
- **[Binding Validation](docs/binding-validation.md)**: ABI binding generation and validation.
- **[Runtime Binding Validation](docs/runtime-binding-validation.md)**: Runtime validators for command inputs, trace payloads, audit records, and session envelopes.
- **[Sandboxed Replay](docs/sandboxed-replay.md)**: Isolated WASM replay in a sandboxed environment.
- **[Session Bookmarking](docs/session-bookmarking.md)**: Persistent session management and bookmarks.
- **[Security Warnings](docs/security-warnings.md)**: Deprecated host functions and security findings.
- **[Telemetry Metadata](docs/telemetry-metadata.md)**: What telemetry data is collected and how.
- **[Telemetry Sampling](docs/telemetry-sampling.md)**: Sampling strategy for telemetry events.
- **[Watch Mode](docs/WATCH_MODE.md)**: `--watch` and `--watch-files` polling modes.
- **[Regression Test Guide](docs/regression-test-guide.md)**: How to write structured regression tests.
- **[Interactive Trace Showcase](docs/showcase/README.md)**: Try out the interactive trace explorer online.

## Technical Analysis

### The Challenge

Stellar's `soroban-env-host` executes WASM. When it traps (crashes), the specific reason is often sanitized or lost in the XDR result to keep the ledger size small.

### The Solution Architecture

`Glassbox` operates by:

1.  **Fetching Data**: Using the Stellar RPC to get the `TransactionEnvelope` and `LedgerFootprint` (read/write set) for the block where the tx failed.
2.  **Simulation Environment**: A Rust binary (`glassbox-sim`) that integrates with `soroban-env-host` to replay transactions.
3.  **Execution**: Feeding the inputs into the VM and capturing `diagnostic_events`.

## How to Contribute

We are building this open-source to help the entire Stellar community. All contributions, from bug reports to new features, are welcome. Please follow our guidelines to ensure code quality and consistency.

### Prerequisites

- Go 1.24.0+
- Rust 1.70+ (for building the simulator binary)
- Stellar CLI (for comparing results)
- `make` (for running standard development tasks)

### Getting Started

1.  Clone the repo:
    ```bash
    git clone https://github.com/dotandev/glassbox.git
    cd glassbox
    ```

2.  Install dependencies:
    ```bash
    go mod download
    cd simulator && cargo fetch && cd ..
    ```

3.  Build the Rust simulator:
    ```bash
    cd simulator
    cargo build --release
    cd ..
    ```

4.  Run tests:
    ```bash
    go test ./...
    cargo test --release -p glassbox-sim
    ```

## Development

### Code Quality & Linting

This project enforces strict linting rules to maintain code quality. See [docs/STRICT_LINTING.md](docs/STRICT_LINTING.md) for details.

Quick commands:

```bash
# Run all strict linting (Go + Rust)
make lint-all-strict

# Go linting only
make lint-strict

# Rust linting only
make rust-lint-strict

# Install pre-commit hooks
pip install pre-commit && pre-commit install
```

The CI pipeline fails immediately on:
- Unused variables, imports, or functions
- Dead code
- Any linting warnings

### Telemetry and Privacy

Glassbox includes optional telemetry to help diagnose runtime issues. Privacy-preserving defaults and explicit opt-in are enforced:

- **Opt-in by default**: Telemetry is disabled unless explicitly enabled via config or environment.
- **Config options**: Set `telemetry_enabled` and `telemetry_endpoint` in your Glassbox config (`~/.Glassbox/config.json`), or use the environment variables `GLASSBOX_TELEMETRY` and `GLASSBOX_TELEMETRY_ENDPOINT`.
- **No secrets**: Identifiers such as transaction hashes, contract IDs, and file paths are sanitized or fingerprinted client-side before export.
- **Session control**: Run `glassbox telemetry` to view the current state and follow the printed instructions to disable telemetry for your shell session.

If you have additional privacy concerns, file an issue and we will work with you to provide stricter controls.

### Code Standards

#### Go Code Style

- **Formatting**: Run `go fmt ./...` before committing
- **Linting**: Must pass `golangci-lint` without errors:
  ```bash
  golangci-lint run ./...
  ```
- **Naming Conventions**:
  - Use `PascalCase` for exported identifiers (types, functions, constants)
  - Use `camelCase` for unexported identifiers
  - Use `UPPER_SNAKE_CASE` for constants
  - Interface names should end with `-er`: `Reader`, `Writer`, `Logger`
- **Error Handling**:
  - Always check and handle errors explicitly
  - Wrap errors with context using `fmt.Errorf`: `fmt.Errorf("operation failed: %w", err)`
  - Never use bare `panic()` in production code
- **Documentation**:
  - All exported functions and types must have documentation comments
  - Comments should be complete sentences starting with the name
  - Example: `// Logger provides structured logging for diagnostic events.`

#### Rust Code Style

- **Formatting**: Run `cargo fmt --all` before committing
- **Linting**: Must pass `cargo clippy`:
  ```bash
  cargo clippy --all-targets --release -- -D warnings
  ```
- **Naming Conventions**:
  - Use `snake_case` for functions and variables
  - Use `PascalCase` for types and traits
  - Use `UPPER_SNAKE_CASE` for constants
- **Error Handling**:
  - Prefer `Result<T, E>` over panics
  - Use custom error types for domain-specific errors
  - Avoid unwrapping in production code except for obvious invariants
- **Documentation**:
  - Document all public functions with doc comments (`///`)
  - Include examples for complex functions
  - Use `cargo doc --open` to review generated documentation

### Commit Message Convention

Follow the [Conventional Commits](https://www.conventionalcommits.org/) specification:

```
<type>(<scope>): <subject>

<body>

<footer>
```

**Types**:
- `feat`: A new feature
- `fix`: A bug fix
- `test`: Adding or improving tests
- `docs`: Documentation changes
- `refactor`: Code refactoring without feature changes
- `perf`: Performance improvements
- `chore`: Build, CI, or dependency updates
- `ci`: CI/CD configuration changes

**Scopes**: Use specific areas like `sim`, `cli`, `updater`, `trace`, `analyzer`, etc.

**Examples**:
```
feat(sim): Add protocol version spoofing for harness
test(sim): Add 1000+ transaction regression suite
fix(updater): Handle network timeouts gracefully
docs: Add comprehensive contribution guidelines
```

**Rules**:
- Keep subject line under 50 characters
- Use imperative mood ("add", not "added" or "adds")
- No period at the end of the subject
- Provide detailed explanation in the body if the change is non-obvious
- Reference related issues: `Closes #350, refs #343`

### Pull Request Structure

1. **Title**: Follow commit message convention (this becomes the squashed commit)
2. **Description**:
   - Brief summary of changes
   - Link to related issues: `Closes #XXX`
   - Explain the "why" behind the changes
   - Highlight any breaking changes
3. **PR Checks**:
   - All CI checks must pass
   - Code coverage must not decrease
   - All tests must pass locally before submitting
4. **Format**:
   ```markdown
   ## Description
   Brief explanation of the changes.

   ## Related Issues
   Closes #350, relates to #343

   ## Testing
   How was this tested? Include specific test cases.

   ## Checklist
   - [ ] Code follows style guidelines
   - [ ] Tests added/updated
   - [ ] Documentation updated
   - [ ] No new warnings or errors
   ```

### Testing Requirements

- **Unit Tests**: All new functions must have unit tests
- **Coverage**: Aim for 80%+ coverage. Critical paths should have 90%+ coverage
- **Integration Tests**: Include tests that verify feature interactions
- **Regression Tests**: See [docs/regression-test-guide.md](docs/regression-test-guide.md) for the structured regression template
- **Running Tests**:
  ```bash
  # Go tests
  go test -v -race ./...
  go test -v -race -cover ./...

  # Rust tests
  cargo test --all
  cargo test --all --release
  ```
- **Bench Tests**: For performance-critical code, include benchmarks:
  ```bash
  go test -bench=. -benchmem ./...
  ```

### Development Workflow

1. **Create a branch**:
   ```bash
   git checkout -b feat/my-feature
   # or for bug fixes:
   git checkout -b fix/issue-description
   ```

2. **Make changes** and test locally:
   ```bash
   go test ./...
   go fmt ./...
   golangci-lint run ./...
   cargo clippy --all-targets -- -D warnings
   cargo fmt --all
   ```

3. **Commit with conventional messages**:
   ```bash
   git add .
   git commit -m "feat(scope): description"
   ```

4. **Push and create PR**:
   ```bash
   git push origin feat/my-feature
   # Then create PR on GitHub with detailed description
   ```

5. **Address feedback**:
   - Make requested changes
   - Commit with descriptive messages

### Linting and Formatting

Run the provided scripts before submitting:

```bash
# Format Go code
go fmt ./...

# Run linters
golangci-lint run ./...

# Format Rust code
cargo fmt --all

# Check Rust with clippy
cargo clippy --all-targets --release -- -D warnings

# Run all checks
make lint
make format
```

### Documentation Check

A script is provided to verify that all command invocations in the README match the
actual CLI surface:

```bash
scripts/check-readme-commands.sh
```

Run it before submitting a PR that touches README.md or any `internal/cmd/*.go` file.
It exits non-zero and prints each unknown command reference if any are found.

### Binary Size Tracking

To prevent regressions in artifact sizes, Glassbox tracks the compiled sizes of both the Go CLI and Rust simulator.

- **Configuring Thresholds**: Adjust maximum size thresholds (in bytes) inside the `size_thresholds.conf` file at the root of the repository.
- **Local Checks**: After building, run `make size-check` to measure your artifacts against the configured limits.
- **CI Pipeline**: Size checks automatically run on all Pull Requests and pushes to `main` or `develop` via the `size-check.yml` GitHub workflow. Builds will fail if size thresholds are exceeded.

### Development Roadmap

See [docs/proposal.md](docs/proposal.md) for the detailed proposal.

1.  [x] **Phase 1**: Research RPC endpoints for fetching historical ledger keys.
2.  [x] **Phase 2**: Build a basic "Replay Harness" that can execute a loaded WASM file.
3.  [x] **Phase 3**: Connect the harness to live mainnet data.
4.  [ ] **Phase 4**: Advanced Diagnostics & Source Mapping (Current Focus).

### Common Development Tasks

#### Running a single test
```bash
go test -run TestName ./package/...
```

#### Profiling a test
```bash
go test -cpuprofile=cpu.prof -memprofile=mem.prof ./...
go tool pprof cpu.prof
```

#### Building for a specific OS
```bash
GOOS=linux GOARCH=amd64 go build -o glassbox-linux-amd64 ./cmd/glassbox
```

#### Cleaning build artifacts
```bash
go clean
cargo clean
make clean
```

### Code Review Checklist

When reviewing PRs, ensure:
- [ ] Code follows naming and style conventions
- [ ] Error handling is appropriate
- [ ] Tests are adequate and pass
- [ ] Documentation is clear and complete
- [ ] No unnecessary dependencies added
- [ ] Performance implications considered
- [ ] Security implications reviewed
- [ ] Commit messages follow convention

### Getting Help

- **Questions?** Open a GitHub Discussion
- **Found a bug?** Create an Issue with reproduction steps
- **Have an idea?** Start a Discussion before implementing
- **Documentation issue?** Create an Issue with details

### Important Guidelines

- **No Emojis**: Commit messages and PR titles should not contain emojis
- **No "Slops"**: Avoid vague language like "fixes stuff" or "updates things"
- **Clear Messages**: Every commit should have a clear, descriptive message
- **Lint-Free**: Only suppress linting errors if they are objectively false positives. Always explain suppression with `// nolint:rule-name` comments
- **Assume Bad Faith in Code**: Write code defensively, validate inputs, handle edge cases

## Contributors

Thanks goes to these wonderful people:

<!-- ALL-CONTRIBUTORS-LIST:START - Do not remove or modify this section -->
<!-- prettier-ignore-start -->
<!-- markdownlint-disable -->
<table>
  <tbody>
    <tr>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/dotandev"><img src="https://avatars.githubusercontent.com/u/105521093?v=4" width="100px;" alt="dotdev."/><br /><sub><b>dotdev.</b></sub></a><br /><a href="#code-dotandev" title="Code">Code</a> <a href="#doc-dotandev" title="Documentation">Documentation</a> <a href="#ideas-dotandev" title="Ideas & Planning">Ideas & Planning</a></td>
    </tr>
  </tbody>
</table>

<!-- markdownlint-restore -->
<!-- prettier-ignore-end -->

<!-- ALL-CONTRIBUTORS-LIST:END -->

This project follows the [all-contributors](https://github.com/all-contributors/all-contributors) specification. Contributions of any kind welcome!

---

_Glassbox is an open-source initiative. Contributions, PRs, and Issues are welcome._
