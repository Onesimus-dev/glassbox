# Audit Signing

Glassbox can sign audit logs with either a software Ed25519 key or a PKCS#11
hardware security module (HSM). This document covers setup, environment
variables, platform-specific module paths, and troubleshooting.

## Signing provider flag reference

The flag that selects the signing backend is `--signing-provider`. It has one deprecated alias.

| Flag | Status | Values | Notes |
|------|--------|--------|-------|
| `--signing-provider` | **stable** (since 0.3) | `software`, `pkcs11` | Use this flag in all new scripts |
| `--hsm-provider` | **deprecated** (since 0.3, removes in v2.0) | same values | Alias; accepted today but prints a warning |

The environment variable mirror is `GLASSBOX_SIGNING_PROVIDER` (stable). Its deprecated counterpart is `GLASSBOX_SIGNER_TYPE`.

### Successful path — software provider

```bash
glassbox audit:sign \
  --signing-provider software \
  --payload '{"input":{},"state":{},"events":[],"timestamp":"2026-01-01T00:00:00.000Z"}' \
  --software-private-key "$(cat ./ed25519-private-key.pem)"
# Expected: signed audit log JSON printed to stdout, exit 0
```

### Successful path — PKCS#11 provider

```bash
export GLASSBOX_PKCS11_MODULE=/usr/lib/softhsm/libsofthsm2.so
export GLASSBOX_PKCS11_PIN=1234
export GLASSBOX_PKCS11_KEY_LABEL=glassbox-audit-key

glassbox audit:sign \
  --signing-provider pkcs11 \
  --payload '{"input":{},"state":{},"events":[],"timestamp":"2026-01-01T00:00:00.000Z"}'
# Expected: signed audit log JSON printed to stdout, exit 0
```

### Failure path — deprecated flag warning

```bash
glassbox audit:sign --hsm-provider pkcs11 --validate-only \
  --pkcs11-module /usr/lib/softhsm/libsofthsm2.so --pkcs11-pin 1234
# stderr: warning: --hsm-provider is deprecated; use --signing-provider pkcs11 instead
# The command still runs; use --signing-provider in new scripts.
```

### Failure path — unknown provider value

```bash
glassbox audit:sign --signing-provider kms --payload-file payload.json
# Error: invalid --signing-provider "kms" — must be one of: software, pkcs11
# Exit: 1 (VALIDATION_FAILED)
```

---

```bash
# Software signing (Ed25519 PEM key)
glassbox audit:sign \
  --payload '{"input":{},"state":{},"events":[],"timestamp":"2026-01-01T00:00:00.000Z"}' \
  --software-private-key "$(cat ./ed25519-private-key.pem)"

# PKCS#11 signing
export GLASSBOX_PKCS11_MODULE=/usr/lib/softhsm/libsofthsm2.so
export GLASSBOX_PKCS11_PIN=1234
export GLASSBOX_PKCS11_KEY_LABEL=glassbox-audit-key
glassbox audit:sign --signing-provider pkcs11 --payload-file payload.json

# ...or pass everything as flags (flags override the environment)
glassbox audit:sign --signing-provider pkcs11 --payload-file payload.json \
  --pkcs11-module /usr/lib/softhsm/libsofthsm2.so --pkcs11-pin 1234 \
  --pkcs11-key-label glassbox-audit-key

# Validate PKCS#11 configuration without signing anything (preflight)
glassbox audit:sign --signing-provider pkcs11 --validate-only \
  --pkcs11-module /usr/lib/softhsm/libsofthsm2.so --pkcs11-pin 1234
```

---

## Software signing

Provide a PKCS#8 PEM-encoded Ed25519 private key via one of:

| Method | How |
|--------|-----|
| CLI flag | `--software-private-key "$(cat key.pem)"` |
| CLI flag (file path) | `--software-private-key ./key.pem` |
| Environment variable | `GLASSBOX_AUDIT_PRIVATE_KEY_PEM` |

Generate a key with OpenSSL:

```bash
openssl genpkey -algorithm ed25519 -out ed25519-private-key.pem
openssl pkey -in ed25519-private-key.pem -pubout -out ed25519-public-key.pem
```

### Software provider input validation

The key is validated at configuration time (before signing begins) so format
errors surface immediately with actionable remediation:

**Wrong PEM format — OpenSSH:**
```
the key is in OpenSSH format; Glassbox requires PKCS#8 PEM format
  Fix: convert with: openssl pkey -in key.pem -out key_pkcs8.pem
  Or generate a new key: openssl genpkey -algorithm ed25519 -out key.pem
```

**Wrong PEM format — SEC1 (EC PRIVATE KEY):**
```
the key is in SEC1 (EC PRIVATE KEY) format; Glassbox requires PKCS#8 PEM format
  Fix: convert with: openssl pkcs8 -topk8 -nocrypt -in key.pem -out key_pkcs8.pem
```

**RSA key:**
```
the key is an RSA key; Glassbox requires an Ed25519 PKCS#8 PEM private key
  Fix: generate a new Ed25519 key: openssl genpkey -algorithm ed25519 -out key.pem
```

**Garbled PEM:**
```
invalid Ed25519 private key: ...
  Expected: a PKCS#8 PEM file starting with '-----BEGIN PRIVATE KEY-----'
  Generate: openssl genpkey -algorithm ed25519 -out key.pem
```

**Hex key — wrong length:**
```
GLASSBOX_SOFTWARE_PRIVATE_KEY_HEX has wrong key length: got 10 bytes, expected 32 (seed) or 64 (full key)
  A 32-byte seed = 64 hex characters; a 64-byte full key = 128 hex characters
```

**Hex key — non-hex characters:**
```
GLASSBOX_SOFTWARE_PRIVATE_KEY_HEX is not valid hexadecimal: ...
  Expected: a 64-character hex string (32-byte seed) or 128-character hex string (64-byte full key)
  Fix: re-export the key or set GLASSBOX_AUDIT_PRIVATE_KEY_PEM with a PEM file instead
```

---

## PKCS#11 HSM signing

> **Flag name:** Use `--signing-provider pkcs11`.
> The flag `--hsm-provider` is **deprecated** since v0.3 and will be removed in v2.0.
> Both flags are accepted today, but examples throughout this document use the current name.
> See the [compatibility matrix](./compatibility-matrix.md#currently-deprecated-surfaces) for the removal timeline.

### Required environment variables

| Variable | Description |
|----------|-------------|
| `GLASSBOX_PKCS11_MODULE` | Absolute path to the PKCS#11 shared library |
| `GLASSBOX_PKCS11_PIN` | User PIN for the token |
| `GLASSBOX_PKCS11_KEY_LABEL` | `CKA_LABEL` of the Ed25519 private key |

At least one of `GLASSBOX_PKCS11_KEY_LABEL` or `GLASSBOX_PKCS11_KEY_ID` is required.

### Optional environment variables

| Variable | Description |
|----------|-------------|
| `GLASSBOX_PKCS11_KEY_ID` | Hex-encoded `CKA_ID` of the key (alternative to label) |
| `GLASSBOX_PKCS11_TOKEN_LABEL` | Select token by label instead of slot index |
| `GLASSBOX_PKCS11_SLOT` | Slot index (default `0`; must be a non-negative integer) |

### CLI flags and precedence

Every environment variable has a matching flag; **the flag takes precedence** when both are set:

| Flag | Environment variable |
|------|----------------------|
| `--pkcs11-module` | `GLASSBOX_PKCS11_MODULE` |
| `--pkcs11-pin` | `GLASSBOX_PKCS11_PIN` |
| `--pkcs11-token-label` | `GLASSBOX_PKCS11_TOKEN_LABEL` |
| `--pkcs11-key-label` | `GLASSBOX_PKCS11_KEY_LABEL` |
| `--pkcs11-key-id` | `GLASSBOX_PKCS11_KEY_ID` |

### Input validation

When the pkcs11 provider is selected, the command validates the configuration
**before loading any module**, so misconfiguration fails fast with an explicit
message instead of a low-level error mid-signing:

- `--pkcs11-module` (or `GLASSBOX_PKCS11_MODULE`) and `--pkcs11-pin` (or
  `GLASSBOX_PKCS11_PIN`) are required. A missing value is reported as, e.g.:

  ```
  pkcs11 signing requires --pkcs11-module (or GLASSBOX_PKCS11_MODULE) and --pkcs11-pin (or GLASSBOX_PKCS11_PIN)
    Provide the missing value(s), or run 'glassbox audit:sign --validate-only --signing-provider pkcs11' for a full PKCS#11 preflight report.
  ```

- **Module file existence** is checked before any signing work begins. A missing
  or inaccessible file is reported with a `Fix:` hint:

  ```
  --pkcs11-module: module file not found: "/usr/lib/nonexistent.so"
    Fix: verify the path is correct for your OS and architecture
    Tip: run 'glassbox audit:sign --validate-only --signing-provider pkcs11' for a full preflight report
  ```

  If the path points to a directory instead of a `.so`/`.dylib`/`.dll` file:

  ```
  --pkcs11-module: "/usr/lib/softhsm" is a directory, not a shared library
    Fix: provide the full path to the .so/.dylib/.dll file, not its parent directory
  ```

- **Key selector required** — at least one of `--pkcs11-key-label` (or
  `GLASSBOX_PKCS11_KEY_LABEL`) or `--pkcs11-key-id` (or `GLASSBOX_PKCS11_KEY_ID`)
  must be provided. Without a selector the signing attempt would fail at the
  key-lookup step; this check surfaces the issue before any module is loaded:

  ```
  no PKCS#11 key selector provided — set --pkcs11-key-label (or GLASSBOX_PKCS11_KEY_LABEL) or --pkcs11-key-id (or GLASSBOX_PKCS11_KEY_ID)
    Tip: run 'pkcs11-tool --list-objects --type privkey' to list available key labels
  ```

- `--pkcs11-key-id`, when provided, must be valid hex (`CKA_ID`):

  ```
  --pkcs11-key-id must be a hex-encoded CKA_ID (e.g. a1b2c3): encoding/hex: invalid byte ...
    Fix: provide a valid hex string, e.g. 'a1b2c3' (use pkcs11-tool --list-objects to find the CKA_ID)
  ```

- `GLASSBOX_PKCS11_SLOT`, when provided, must be a non-negative integer. Invalid
  values are rejected before preflight/signing starts so they do not silently
  fall back to slot `0`:

  ```
  GLASSBOX_PKCS11_SLOT must be a non-negative integer, got "not-a-number"
    Fix: set GLASSBOX_PKCS11_SLOT to a numeric slot index such as 0 or 1, or unset it to use the default slot
    Tip: run 'glassbox audit:sign --validate-only --signing-provider pkcs11' to inspect available slot diagnostics
  ```

### Preflight (`--validate-only`)

`--validate-only` checks a PKCS#11 signing configuration and exits. **It never
signs a user payload.** Any `--payload` or `--payload-file` you pass is not read
and no signed audit log is written. The only signing operation is the internal
`sign_test` step, which signs a fixed built-in test vector
(`glassbox-pkcs11-preflight-test-vector`) and discards the result.
`--validate-only` takes precedence over `--plan`.

#### Required and optional settings

Preflight runs only for the pkcs11 provider. Select it with
`--signing-provider pkcs11`, the deprecated `--hsm-provider pkcs11`, or
`GLASSBOX_SIGNING_PROVIDER=pkcs11`. With any other provider the command stops
with `--validate-only applies only to the pkcs11 provider`.

A passing report also requires three settings. Each can be a flag or its
environment variable, and the flag wins when both are set. You need the module
path (`--pkcs11-module` / `GLASSBOX_PKCS11_MODULE`), the user PIN
(`--pkcs11-pin` / `GLASSBOX_PKCS11_PIN`), and one key selector: either the key
label (`--pkcs11-key-label` / `GLASSBOX_PKCS11_KEY_LABEL`) or the hex key ID
(`--pkcs11-key-id` / `GLASSBOX_PKCS11_KEY_ID`). Unlike a real signing run,
preflight does not reject a missing value up front. It runs the steps and
reports the missing value as a `[FAIL]` at the step that needs it.

Two settings are optional and only affect slot selection. Use
`--pkcs11-token-label` / `GLASSBOX_PKCS11_TOKEN_LABEL` to pick the token by
label, or `GLASSBOX_PKCS11_SLOT` to pick a slot index; the default index is
`0`. When a token label is set, it is used instead of the slot index. An
invalid `GLASSBOX_PKCS11_SLOT` (non-numeric or negative) is rejected before
preflight starts.

Some signing-time input checks are **not** part of preflight: that
`--pkcs11-key-id` is valid hex, and that label and ID are not both set. They run
only when you actually sign.

#### Steps

The steps run in a fixed order, and preflight stops at the first failing step.
The one exception is `token_info`, which is informational and never fails the
report.

1. `module_path`: the module path is set and names an existing, accessible
   file that is not a directory. The file must have an extension the current OS
   accepts: `.so` or `.dylib` on Linux and macOS, `.dll` on Windows.
2. `module_load`: the module is loaded and initialized within a 10-second
   timeout per attempt. A failed attempt is retried twice, 500 ms apart.
3. `slot_enum`: slots with a token present are listed. The slot is chosen by
   token label if one is set, otherwise by slot index.
4. `token_info`: the token label is recorded. If it cannot be read, the step
   still passes with an "unavailable (non-fatal)" message.
5. `session_open`: a session is opened on the selected slot.
6. `pin_auth`: the PIN is set and the token accepts it. Repeated wrong PINs
   can lock a token, so do not loop preflight with a guessed PIN.
7. `key_lookup`: a key selector is set and a matching key is found.
8. `sign_test`: the found key signs the built-in test vector.

Each step prints `[PASS]` or `[FAIL]`. A `[FAIL]` line is followed by a
`Remediation:` hint. The command exits non-zero unless every step passed, and
then prints `Result: PKCS#11 configuration is valid and ready for signing.`

#### Limitation in the current build

The step sequence above is what the validator implements and what its unit
tests exercise, using a mock PKCS#11 backend. The CLI, however, runs preflight
through the default OS backend. That backend can stat the module file but has
no native PKCS#11 binding, so it cannot initialize a module. With the CLI as
built today, `module_path` is the only step that can pass. `module_load` then
fails after three attempts, and steps 3–8 are never reached. A `module_load`
failure that mentions `C_Initialize is not available` comes from this
limitation, not from your HSM configuration.

#### Example

The paths, label, and PIN variable below are placeholders. Supply the PIN from
your own secret store rather than typing it on the command line, where it can
end up in shell history.

```bash
export GLASSBOX_PKCS11_MODULE=/opt/vendor-hsm/lib/libvendor-pkcs11.so
export GLASSBOX_PKCS11_PIN="$HSM_USER_PIN"
export GLASSBOX_PKCS11_KEY_LABEL=example-audit-key
export GLASSBOX_PKCS11_TOKEN_LABEL=example-token   # optional

glassbox audit:sign --signing-provider pkcs11 --validate-only
```

Output with the current build when the module file exists (long lines wrapped
here for readability):

```
Running PKCS#11 preflight checks...

  [PASS] module_path    module file found: /opt/vendor-hsm/lib/libvendor-pkcs11.so (<size> bytes)
  [FAIL] module_load    failed to load PKCS#11 module after 3 attempt(s): pkcs11: C_Initialize
                        is not available: the OS PKCS#11 provider requires a cgo binding ...
                        Remediation: verify the module is a valid PKCS#11 shared library; ...
```

A full report in which all eight steps pass is shown under
[Preflight validation](#preflight-validation). It is what the validator
produces with a backend that can drive the module.

---

## Platform-specific module paths

### Linux (Debian / Ubuntu)

```bash
# SoftHSM2 (software HSM for testing)
apt install softhsm2
export GLASSBOX_PKCS11_MODULE=/usr/lib/softhsm/libsofthsm2.so
# or on x86_64:
export GLASSBOX_PKCS11_MODULE=/usr/lib/x86_64-linux-gnu/softhsm/libsofthsm2.so

# OpenSC (smart cards, YubiKey via CCID)
apt install opensc
export GLASSBOX_PKCS11_MODULE=/usr/lib/x86_64-linux-gnu/opensc-pkcs11.so

# YubiKey YKCS11
apt install ykcs11
export GLASSBOX_PKCS11_MODULE=/usr/lib/x86_64-linux-gnu/libykcs11.so
```

### Linux (RHEL / Fedora / CentOS)

```bash
# SoftHSM2
dnf install softhsm
export GLASSBOX_PKCS11_MODULE=/usr/lib64/softhsm/libsofthsm2.so

# OpenSC
dnf install opensc
export GLASSBOX_PKCS11_MODULE=/usr/lib64/opensc-pkcs11.so

# YubiKey YKCS11
dnf install ykcs11
export GLASSBOX_PKCS11_MODULE=/usr/lib64/libykcs11.so
```

### macOS

```bash
# SoftHSM2
brew install softhsm
export GLASSBOX_PKCS11_MODULE=/usr/local/lib/softhsm/libsofthsm2.so
# Apple Silicon:
export GLASSBOX_PKCS11_MODULE=/opt/homebrew/lib/softhsm/libsofthsm2.so

# OpenSC
brew install opensc
export GLASSBOX_PKCS11_MODULE=/usr/local/lib/opensc-pkcs11.so
# Apple Silicon:
export GLASSBOX_PKCS11_MODULE=/opt/homebrew/lib/opensc-pkcs11.so

# YubiKey (yubico-piv-tool)
brew install yubico-piv-tool
export GLASSBOX_PKCS11_MODULE=/usr/local/lib/libykcs11.dylib
# Apple Silicon:
export GLASSBOX_PKCS11_MODULE=/opt/homebrew/lib/libykcs11.dylib
```

### Windows

Windows PKCS#11 modules are typically `.dll` files. Common locations:

```powershell
# SoftHSM2 for Windows (https://github.com/disig/SoftHSM2-for-Windows)
$env:GLASSBOX_PKCS11_MODULE = "C:\SoftHSM2\lib\softhsm2-x64.dll"

# YubiKey Minidriver / YKCS11
$env:GLASSBOX_PKCS11_MODULE = "C:\Program Files\Yubico\Yubico PIV Tool\bin\libykcs11.dll"
```

> Note: Go's `plugin` package does not support Windows. PKCS#11 signing on
> Windows requires a cgo build with a native binding. The `--validate-only`
> flag will surface a clear error if the module cannot be loaded.

---

## SoftHSM2 setup (for testing)

SoftHSM2 is a software-only HSM useful for CI and local development.

```bash
# 1. Initialize a token
softhsm2-util --init-token --slot 0 \
  --label "glassbox-test" \
  --pin 1234 --so-pin 0000

# 2. Generate an Ed25519 key
pkcs11-tool --module /usr/lib/softhsm/libsofthsm2.so \
  --login --pin 1234 \
  --keypairgen --key-type EC:edwards25519 \
  --label glassbox-audit-key --usage-sign

# 3. Verify the key exists
pkcs11-tool --module /usr/lib/softhsm/libsofthsm2.so \
  --login --pin 1234 \
  --list-objects --type privkey

# 4. Run the preflight validator
export GLASSBOX_PKCS11_MODULE=/usr/lib/softhsm/libsofthsm2.so
export GLASSBOX_PKCS11_PIN=1234
export GLASSBOX_PKCS11_KEY_LABEL=glassbox-audit-key
glassbox audit:sign --signing-provider pkcs11 --validate-only
# Note: --hsm-provider is a deprecated alias for --signing-provider. Use --signing-provider.
```

---

## YubiKey PIV setup

YubiKey supports Ed25519 from firmware 5.7+. For older firmware use ECDSA P-256.

```bash
# 1. Generate a key in PIV slot 9c (Digital Signature)
ykman piv keys generate --algorithm ED25519 9c pubkey.pem

# 2. Create a self-signed certificate (required by PIV applet)
ykman piv certificates generate --subject "CN=Glassbox" 9c pubkey.pem

# 3. Set the PIN (default is 123456)
ykman piv access change-pin

# 4. Configure Glassbox
export GLASSBOX_PKCS11_MODULE=/usr/lib/x86_64-linux-gnu/libykcs11.so  # Linux
export GLASSBOX_PKCS11_MODULE=/usr/local/lib/libykcs11.dylib           # macOS
export GLASSBOX_PKCS11_PIN=<your-pin>
export GLASSBOX_PKCS11_KEY_LABEL="Private key for Digital Signature"

# 5. Validate
glassbox audit:sign --signing-provider pkcs11 --validate-only
# Note: --hsm-provider is a deprecated alias for --signing-provider. Use --signing-provider.
```

---

## Preflight validation

The `--validate-only` flag runs the PKCS#11 checks without signing any payload.
It does not modify the token, but repeated runs with a wrong PIN can lock it.
The required settings, step order, and current-build limitation are described
under [Preflight (`--validate-only`)](#preflight---validate-only). The report
below is what a backend that can drive the module produces when every step
passes.

```
$ glassbox audit:sign --signing-provider pkcs11 --validate-only

Running PKCS#11 preflight checks...

  [PASS] module_path    module file found: /usr/lib/softhsm/libsofthsm2.so (1234567 bytes)
  [PASS] module_load    module loaded successfully: /usr/lib/softhsm/libsofthsm2.so
  [PASS] slot_enum      using slot index 0 (slot ID 0, 1 slot(s) total)
  [PASS] token_info     token label: "glassbox-test" (slot 0)
  [PASS] session_open   session opened on slot 0 (handle 1)
  [PASS] pin_auth       PIN authentication successful
  [PASS] key_lookup     signing key "glassbox-audit-key" found (handle 42)
  [PASS] sign_test      test signing operation succeeded

Result: PKCS#11 configuration is valid and ready for signing.
```

When a check fails, the output includes a remediation hint:

```
  [FAIL] pin_auth       PIN authentication failed: pkcs11 C_Login failed (0x000000A0):
                        the PIN is incorrect — verify GLASSBOX_PKCS11_PIN is correct;
                        note that repeated failures may lock the token
```

---

## Key rotation

Rotation has three separate phases: introduce the new key, confirm verifiers can
use it, and only then retire the old one. Run them as separate steps. If you
retire the old key before the new trust configuration has been verified,
verification can be left with no usable key.

### What the tooling does and does not do

`glassbox audit:keygen --rotate` writes a new key pair under a new `--key-name`
and prints a reminder to keep the previous public key. It does not move, delete,
or re-sign anything. It refuses to overwrite existing key files unless you pass
`--force`, so never combine `--force` with the old key's name.

There is no keyring or trust-policy file. Each signed log embeds the hex
`public_key` that signed it. `audit:verify --public-key` pins exactly one key
per invocation, and that pin is your trust configuration (see
[Integrity and acceptance](./audit-verify-command.md#integrity-and-acceptance)).

You can record a key identifier at signing time with `--key-id`, stored as
`provenance.key_id`. Be aware that `audit:verify` reports `[FAIL] Provenance`,
and therefore `INVALID`, for any log whose provenance has `key_id`,
`signer_identity`, or `previous_signature_hash` but no certificate chain. Pass
`--key-id` only together with `--cert-chain`. Otherwise, match logs to keys by
their embedded `public_key`. See
[Audit chain integrity](./audit-verify-command.md#audit-chain-integrity) for the
provenance fields.

### Procedure

The names and paths below are placeholders. No key material appears in them.

**1. Introduce the new key.** Generate it under a new name and leave the old pair
untouched:

```bash
glassbox audit:keygen --key-name audit-2026-q3 --output-dir /secure/keys --rotate
```

For PKCS#11, create the new key on the token under a new label instead. Signing
then selects it with `--pkcs11-key-label`, and you can run
[`--validate-only`](#preflight---validate-only) to check the configuration.

**2. Publish the new public key.** Distribute the new key's hex form to everyone
who verifies logs, alongside the old one. Do not withdraw the old public key.
`audit:verify` takes the 64-hex-character form, which you can derive from the
SPKI PEM:

```bash
openssl pkey -pubin -in /secure/keys/audit-2026-q3.public.pem -outform DER \
  | tail -c 32 | od -An -tx1 | tr -d ' \n'
```

**3. Confirm the new trust configuration works.** Sign a non-production payload
with the new key and verify it pinned to the new key. Separately, check that an
existing log signed by the old key still verifies when pinned to the old key:

```bash
glassbox audit:sign --payload-file docs/examples/audit-verify/payload.json \
  --software-private-key /secure/keys/audit-2026-q3.private.pem > /tmp/rotation-check.json
glassbox audit:verify --audit-log /tmp/rotation-check.json --public-key "$NEW_PUB_HEX"
glassbox audit:verify --audit-log archive/last-old-key.signed.json --public-key "$OLD_PUB_HEX"
```

Both commands must report `Result: VALID` before you continue.

**4. Switch signing to the new key.** Point `--software-private-key`,
`GLASSBOX_AUDIT_PRIVATE_KEY_PEM`, or the PKCS#11 key selector at the new key.
If you sign with `--cert-chain`, update `--key-id` to the new identifier at the
same time. Chaining is unaffected by the key
change. `--previous-signature-hash` refers to the previous log, not the previous
key, so the first log signed with the new key can link to the last log signed
with the old one. Like `--key-id`, a chained log needs `--cert-chain` to pass
the `Provenance` check.

**5. Retire the old private key.** This is a separate, later step, done only
after steps 3 and 4 have succeeded. Destroy or disable the old private key, or
delete the old HSM key object. Keep the old **public** key archived and
available to verifiers.

### Historical verification after rotation

Verifying a log needs only a public key, so retiring the old private key in
step 5 has no effect on logs it already signed. The `audit:verify` tests
establish how those logs behave:

| Verification of a log signed by the old key | Result |
|---|---|
| No `--public-key` (uses the key embedded in the log) | `VALID` (integrity only) |
| `--public-key` set to the old key | `VALID` |
| `--public-key` set to the new key | `INVALID`, `[FAIL] Signature` |

As a result, verifiers who pin keys must pick the key that matches each log.
Compare the log's embedded `public_key` (or its `provenance.key_id`, when
present) with your archived public keys to decide which one to pin. Discarding the old public key leaves verifiers with only the embedded
key, which proves integrity but not that the signer was trusted.

---

## Troubleshooting

### Module file not found

```
[FAIL] module_path  module file not found: /usr/lib/softhsm/libsofthsm2.so
```

The path in `GLASSBOX_PKCS11_MODULE` does not exist. Check:
- The package is installed (`apt install softhsm2`, `brew install softhsm`, etc.)
- The path matches your OS architecture (x86_64 vs arm64, `/usr/lib` vs `/usr/lib64`)
- Run `find /usr -name 'libsofthsm2.so' 2>/dev/null` to locate the file

### No slots with tokens found

```
[FAIL] slot_enum  no slots with tokens found
```

The module loaded but no token is present. For SoftHSM2:

```bash
softhsm2-util --show-slots
# If empty, initialize a token:
softhsm2-util --init-token --slot 0 --label MyToken --pin 1234 --so-pin 0000
```

For a physical token, ensure it is inserted and recognized by the OS:

```bash
pkcs11-tool --module $GLASSBOX_PKCS11_MODULE --list-slots
```

### Token label not found

```
[FAIL] slot_enum  no token with label "MyToken" found (checked 2 slot(s))
```

`GLASSBOX_PKCS11_TOKEN_LABEL` does not match any token. Labels are case-sensitive.
List available tokens:

```bash
pkcs11-tool --module $GLASSBOX_PKCS11_MODULE --list-slots
```

### PIN incorrect

```
[FAIL] pin_auth  PIN authentication failed: the PIN is incorrect
```

- Verify `GLASSBOX_PKCS11_PIN` is set correctly
- Check for leading/trailing whitespace in the variable
- Repeated failures will lock the token; use the SO PIN to unlock:
  ```bash
  pkcs11-tool --module $GLASSBOX_PKCS11_MODULE --init-pin --so-pin 0000
  # YubiKey:
  ykman piv access unblock-pin --puk <puk>
  ```

### PIN locked

```
[FAIL] pin_auth  the token PIN is locked due to too many incorrect attempts
```

Unlock with the Security Officer (SO) PIN:

```bash
# SoftHSM2
pkcs11-tool --module $GLASSBOX_PKCS11_MODULE --init-pin --so-pin 0000

# YubiKey
ykman piv access unblock-pin --puk <puk>
```

### Key not found

```
[FAIL] key_lookup  signing key "my-key" not found
```

- List available private keys: `pkcs11-tool --module $GLASSBOX_PKCS11_MODULE --login --pin $GLASSBOX_PKCS11_PIN --list-objects --type privkey`
- Verify `GLASSBOX_PKCS11_KEY_LABEL` matches the `CKA_LABEL` exactly (case-sensitive)
- Ensure the key was created with `--usage-sign` / `CKA_SIGN=true`
- Ensure the key type is Ed25519 (`EC:edwards25519`); RSA and ECDSA P-256 are not supported

### Mechanism not supported (CKM_EDDSA)

```
[FAIL] sign_test  test signing operation failed: the CKM_EDDSA mechanism is not supported
```

The module does not support Ed25519 signing. Check:
- SoftHSM2 requires version 2.5.0 or later
- YubiKey requires firmware 5.2.3 or later for Ed25519
- Some enterprise HSMs require a separate license for EdDSA; consult vendor docs

### Permission denied loading module

```
[FAIL] module_load  failed to load PKCS#11 module: permission denied
```

The process user does not have read/execute permission on the module file:

```bash
ls -la $GLASSBOX_PKCS11_MODULE
# Fix:
sudo chmod o+r $GLASSBOX_PKCS11_MODULE
# Or add the user to the group that owns the file
```

### Module hangs on initialization

The validator enforces a 10-second timeout on module initialization. If the
module hangs (common with misconfigured smart card readers), the validator
will retry up to 2 times and then fail with a clear message. Check:
- The HSM device is connected and powered
- No other process holds an exclusive lock on the token
- The PKCS#11 middleware daemon (e.g. `pcscd`) is running:
  ```bash
  systemctl status pcscd
  systemctl start pcscd
  ```

### Signing error diagnostics

When a signing operation fails, Glassbox maps the raw PKCS#11 return code to a
human-readable message with actionable remediation. For example:

```
pkcs11 C_SignInit failed (0x00000070): the CKM_EDDSA mechanism is not supported
  — verify the module supports Ed25519 signing; SoftHSM2 requires version 2.5.0+;
  YubiKey requires firmware 5.2.3+
```

Common signing errors and their meanings:

| Error | Meaning | Action |
|-------|---------|--------|
| `CKR_MECHANISM_INVALID` | Ed25519 not supported | Update SoftHSM2 or check firmware |
| `CKR_PIN_LOCKED` | Token PIN locked | Unlock with SO PIN |
| `CKR_KEY_FUNCTION_NOT_PERMITTED` | Key lacks sign permission | Recreate key with `CKA_SIGN=true` |
| `CKR_SESSION_CLOSED` | Session expired | Reinitialize session |
| `CKR_DEVICE_REMOVED` | Token unplugged | Reinsert token and retry |

---

## Architecture Decision Records

The following ADRs govern the design decisions behind audit signing:

- [ADR-003: Trust Boundaries and Component Trust Levels](adr/003-trust-boundaries.md) — classifies the signing provider tier (Tier 3) and the controls that govern provider selection.
- [ADR-006: Provider Isolation](adr/006-provider-isolation.md) — documents the isolation properties of software, PKCS#11, and KMS providers; explains why only the hash digest crosses the provider boundary.
- [ADR-005: Canonicalization Ownership](adr/005-canonicalization-ownership.md) — specifies the canonical form and hashing algorithm whose output is passed to the signing provider.
- [ADR-007: Offline Guarantees](adr/007-offline-guarantees.md) — documents which providers work without network access and describes the air-gapped signing pipeline.

---

## See also

- [Compatibility Matrix — CLI flags](./compatibility-matrix.md#1-cli-flags) — `--signing-provider` (stable) and `--hsm-provider` (deprecated) stability levels and removal timeline
- [Audit Verify Command](./audit-verify-command.md) — verifying a signed log
- [Audit KMS Signing](./audit-kms-signing.md) — AWS KMS signing integration
- [Navigation map — Journey 3](./navigation-map.md#journey-3--sign-and-verify-an-audit-log) — full signing workflow overview
