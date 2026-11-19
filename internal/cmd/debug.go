// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dotandev/glassbox/internal/abi"
	"github.com/dotandev/glassbox/internal/clioutput"
	"github.com/dotandev/glassbox/internal/config"
	"github.com/dotandev/glassbox/internal/decenstorage"
	"github.com/dotandev/glassbox/internal/decoder"
	"github.com/dotandev/glassbox/internal/diagnostics"
	"github.com/dotandev/glassbox/internal/errors"
	"github.com/dotandev/glassbox/internal/logger"
	"github.com/dotandev/glassbox/internal/lto"
	"github.com/dotandev/glassbox/internal/perfmetrics"
	"github.com/dotandev/glassbox/internal/plan"
	"github.com/dotandev/glassbox/internal/profile"
	"github.com/dotandev/glassbox/internal/progress"
	"github.com/dotandev/glassbox/internal/replay"
	"github.com/dotandev/glassbox/internal/rpc"
	"github.com/dotandev/glassbox/internal/security"
	"github.com/dotandev/glassbox/internal/session"
	"github.com/dotandev/glassbox/internal/simulator"
	"github.com/dotandev/glassbox/internal/snapshot"
	"github.com/dotandev/glassbox/internal/sourcemap"
	"github.com/dotandev/glassbox/internal/telemetry"
	"github.com/dotandev/glassbox/internal/termctx"
	"github.com/dotandev/glassbox/internal/tokenflow"
	"github.com/dotandev/glassbox/internal/trace"
	simtypes "github.com/dotandev/glassbox/internal/types"
	"github.com/dotandev/glassbox/internal/version"
	"github.com/dotandev/glassbox/internal/visualizer"
	"github.com/dotandev/glassbox/internal/wat"
	"github.com/dotandev/glassbox/internal/watch"

	"strconv"

	"github.com/spf13/cobra"
	"github.com/stellar/go-stellar-sdk/xdr"
)

var (
	networkFlag           string
	rpcURLFlag            string
	rpcTokenFlag          string
	tracingEnabled        bool
	otlpExporterURL       string
	generateTrace         bool
	traceOutputFile       string
	snapshotFlag          string
	compareNetworkFlag    string
	verbose               bool
	wasmPath              string
	args                  []string
	xdrFileFlag           string
	jsonFileFlag          string
	resultMetaFileFlag    string
	mockLedgerEntryFlags  []string
	mockLedgerManifest    string
	themeFlag             string
	noCacheFlag           bool
	demoMode              bool
	watchFlag             bool
	watchFilesFlag        bool
	watchTimeoutFlag      int
	hotReloadFlag         bool
	hotReloadInterval     time.Duration
	snapshotsFlag         bool
	protocolVersionFlag   uint32
	auditKeyFlag          string
	publishIPFSFlag       bool
	publishArweaveFlag    bool
	ipfsNodeFlag          string
	arweaveGatewayFlag    string
	arweaveWalletFlag     string
	mockTimeFlag          int64
	mockBaseFeeFlag       uint32
	mockGasPriceFlag      uint64
	exportSVGFlag         string
	loadSnapshotsFlag     string
	saveSnapshotsFlag     string
	wasmBase64            string
	contractSourceFlag    string
	buildManifestFlag     string
	debugJSONFlag         bool
	debugFormatFlag       string
	skipSourceMappingFlag bool
	traceVerbosityFlag    string

	// Flags not covered by the compact var block above (declared here to
	// satisfy all references throughout this file).
	liveReplayFlag      bool
	opIndexFlag         int
	secureWorkspaceFlag bool
	pinEndpointFlag     string
	showMetricsFlag     bool
	debugTimingsFlag    bool
	debugDryRunFlag     bool
	debugPlanFlag       bool // --plan: print execution plan and exit
	sourceAliasFlag     string

	// Telemetry flags (shared with root but also read in debug run path).
	TelemetryFlag           bool
	TelemetryAnonymizedFlag bool
)

// DebugCommand holds dependencies for the debug command
type DebugCommand struct {
	Runner simulator.RunnerInterface
}

// NewDebugCommand creates a new debug command with dependencies
func NewDebugCommand(runner simulator.RunnerInterface) *cobra.Command {
	debugCmd := &DebugCommand{Runner: runner}
	return debugCmd.createCommand()
}

func (d *DebugCommand) createCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "debug <transaction-hash>",
		Short: "Debug a failed Soroban transaction",
		Long: `Fetch a transaction envelope from the Stellar network and prepare it for simulation.

Example:
  Glassbox debug 5c0a1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab
  Glassbox debug --network testnet <tx-hash>`,
		Args: cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			// Validate network flag
			switch rpc.Network(networkFlag) {
			case rpc.Testnet, rpc.Mainnet, rpc.Futurenet:
				return nil
			default:
				return errors.WrapInvalidNetwork(networkFlag)
			}
		},
		RunE: d.runDebug,
	}

	// Set up flags
	cmd.Flags().StringVarP(&networkFlag, "network", "n", string(rpc.Mainnet), "Stellar network to use (testnet, mainnet, futurenet)")
	cmd.Flags().StringVar(&rpcURLFlag, "rpc-url", "", "Custom Horizon RPC URL to use")
	cmd.Flags().StringVar(&rpcTokenFlag, "rpc-token", "", "RPC authentication token (can also use GLASSBOX_RPC_TOKEN env var)")
	cmd.Flags().BoolVar(&snapshotsFlag, "snapshots", false, "Enable simulator snapshot capture (default: disabled)")
	cmd.Flags().BoolVar(&tracingEnabled, "tracing", false, "Enable OpenTelemetry tracing (overrides config/env)")
	cmd.Flags().StringVar(&otlpExporterURL, "otlp-url", "", "OTLP exporter URL (can also use GLASSBOX_TELEMETRY_ENDPOINT env var)")

	return cmd
}

func (d *DebugCommand) runDebug(cmd *cobra.Command, cmdArgs []string) error {
	ctx, _ := telemetry.EnsureCorrelationID(cmd.Context())
	cmd.SetContext(ctx)
	txHash := cmdArgs[0]

	token := rpcTokenFlag
	if token == "" {
		token = os.Getenv("GLASSBOX_RPC_TOKEN")
	}
	if token == "" {
		cfg, err := config.Load()
		if err == nil && cfg.RPCToken != "" {
			token = cfg.RPCToken
		}
	}

	opts, err := networkClientOptions(networkFlag)
	if err != nil {
		return errors.WrapValidationError(fmt.Sprintf("failed to build client options: %v", err))
	}
	opts = append(opts, rpc.WithToken(token))
	if rpcURLFlag != "" {
		opts = append(opts, rpc.WithHorizonURL(rpcURLFlag))
	}

	client, err := rpc.NewClient(opts...)
	if err != nil {
		return errors.WrapValidationError(fmt.Sprintf("failed to create client: %v", err))
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Debugging transaction: %s\n", txHash)
	fmt.Fprintf(cmd.OutOrStdout(), "Network: %s\n", networkFlag)
	if rpcURLFlag != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "RPC URL: %s\n", rpcURLFlag)
	}

	// Fetch transaction details
	resp, err := client.GetTransaction(cmd.Context(), txHash)
	if err != nil {
		return errors.WrapRPCConnectionFailed(err)
	}

				fmt.Fprintf(cmd.OutOrStdout(), "Transaction fetched successfully. Envelope size: %d bytes\n", len(resp.EnvelopeXdr))

	simReq := &simulator.SimulationRequest{
		EnvelopeXdr:   resp.EnvelopeXdr,
		ResultMetaXdr: resp.ResultMetaXdr,
	}
	_, err = d.Runner.Run(cmd.Context(), simReq)
	if err != nil {
		return errors.WrapSimulationFailed(err, txHash)
	}

	return nil
}

var debugCmd = &cobra.Command{
	Use:     "debug <transaction-hash>",
	Aliases: []string{"db"},
	Short:   "Debug a failed Soroban transaction",
	Long: `Fetch and simulate a Soroban transaction to debug failures and analyze execution.

This command retrieves the transaction envelope from the Stellar network, runs it
through the local simulator, and displays detailed execution traces including:
  - Transaction status and error messages
  - Contract events and diagnostic logs
  - Token flows (XLM and Soroban assets)
  - Execution metadata and state changes

The simulation results are stored in a session that can be saved for later analysis
with 'glassbox session save'.

Network selection:
  --network defaults to mainnet and is auto-detected from the transaction when
  omitted. Accepted values: testnet, mainnet, futurenet.

Input modes (mutually exclusive — omit for network hash mode):
  --wasm <path>            Local WASM replay; no network required
  --xdr-file <path>        Load a raw base64 XDR envelope from disk
  --json-file <path>       Load a structured JSON envelope from disk
  --load-snapshots <path>  Offline replay from a saved snapshot registry
  --demo                   Print sample output; no network or WASM needed

Validation:
  All flag combinations are validated before any network call. Invalid
  combinations, bad hash formats, missing files, and unrecognised network
  names are rejected immediately with an actionable message.

Authorization tracing:
  To analyze multi-sig / threshold failures on a transaction use:
    glassbox auth-debug <transaction-hash>

For the full flag reference see docs/debug-command.md.`,
	Example: `  # Debug a transaction on mainnet (default)
  glassbox debug 5c0a1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab

  # Debug on testnet
  glassbox debug --network testnet abc123...def789

  # Validate inputs and environment without running a simulation
  glassbox debug --dry-run --network testnet abc123...def789

  # Compare execution between testnet and mainnet
  glassbox debug --network testnet --compare-network mainnet <tx-hash>

  # Debug and save the session
  glassbox debug abc123...def789
  glassbox session save --name payroll-bug

  # Local WASM replay (no network required)
  glassbox debug --wasm ./contract.wasm --args "arg1" --args "arg2"

  # Load from a local XDR envelope file (no network required)
  glassbox debug --xdr-file ./envelope.xdr

  # Offline replay from a saved snapshot registry
  glassbox debug --load-snapshots ./registry.json

  # Machine-readable JSON output
  glassbox debug --json <tx-hash>

  # Demo mode (test color output, no network required)
  glassbox debug --demo

  # Analyze authorization failures for the same transaction
  glassbox auth-debug <tx-hash>`,
	Args: cobra.MaximumNArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if hotReloadFlag && wasmPath == "" {
			return errors.WrapValidationError(
				"--hot-reload requires --wasm; provide --wasm <path> to enable hot reload\n" +
					"  Example: glassbox debug --wasm ./contract.wasm --hot-reload")
		}

		// Demo mode, local WASM replay, local envelope file input, and offline
		// registry load don't need a transaction hash or network connectivity.
		if xdrFileFlag != "" && jsonFileFlag != "" {
			return errors.WrapValidationError(
				"only one of --xdr-file or --json-file may be specified\n" +
					"  Fix: remove one of the two flags — they provide the same local envelope input")
		}
		if xdrFileFlag != "" || jsonFileFlag != "" {
			if len(args) > 0 {
				return errors.WrapValidationError(
					"cannot specify both a transaction hash and a local envelope file\n" +
						"  Fix: use either a hash argument or --xdr-file/--json-file, not both")
			}
			if watchFlag {
				return errors.WrapValidationError(
					"--watch cannot be used with local envelope input\n" +
						"  Fix: remove --watch or provide a transaction hash instead of a local file")
			}
			if compareNetworkFlag != "" {
				return errors.WrapValidationError(
					"--compare-network cannot be used with local envelope input\n" +
						"  Fix: remove --compare-network or use a transaction hash")
			}
			// Validate the local file exists before proceeding.
			if xdrFileFlag != "" {
				if err := validateFilePath("xdr-file", xdrFileFlag); err != nil {
					return err
				}
			}
			if jsonFileFlag != "" {
				if err := validateFilePath("json-file", jsonFileFlag); err != nil {
					return err
				}
			}
			return nil
		}

		if demoMode || wasmPath != "" || loadSnapshotsFlag != "" {
			// Validate WASM file exists up-front so we surface a helpful error
			// before the user waits for flag processing.
			if wasmPath != "" {
				if err := validateFilePath("wasm", wasmPath); err != nil {
					return err
				}
			}
			return nil
		}

		if len(args) == 0 {
			return errors.WrapValidationError(
				"transaction hash is required when not using --wasm, --demo, --xdr-file, or --json-file\n" +
					"  Usage: glassbox debug <transaction-hash>\n" +
					"  Example: glassbox debug abc123...def\n" +
					"  Run 'glassbox debug --help' for all available options")
		}

		if err := rpc.ValidateTransactionHash(args[0]); err != nil {
			return errors.WrapValidationError(fmt.Sprintf(
				"invalid transaction hash %q: %v\n"+
					"Transaction hashes must be 64 lowercase hexadecimal characters",
				args[0], err))
		}

		if !cmd.Flags().Changed("network") {
			token := rpcTokenFlag
			if token == "" {
				token = os.Getenv("GLASSBOX_RPC_TOKEN")
			}
			probeCtx, probeCancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer probeCancel()
			if resolved, err := rpc.ResolveNetwork(probeCtx, args[0], token); err == nil {
				networkFlag = string(resolved)
				fmt.Fprintf(cmd.OutOrStdout(), "Auto-detected network: %s\n", networkFlag)
			}
		}

		// Validate primary network flag.
		switch rpc.Network(networkFlag) {
		case rpc.Testnet, rpc.Mainnet, rpc.Futurenet:
			// valid
		default:
			return errors.WrapValidationError(fmt.Sprintf(
				"invalid --network %q; must be one of: testnet, mainnet, futurenet\n"+
					"Use 'glassbox debug --help' to see all available flags",
				networkFlag))
		}

		// Validate compare network flag if present.
		if compareNetworkFlag != "" {
			switch rpc.Network(compareNetworkFlag) {
			case rpc.Testnet, rpc.Mainnet, rpc.Futurenet:
				// valid
			default:
				return errors.WrapValidationError(fmt.Sprintf(
					"invalid --compare-network %q; must be one of: testnet, mainnet, futurenet",
					compareNetworkFlag))
			}
			// Guard: comparing a network against itself produces no useful diff.
			if strings.EqualFold(networkFlag, compareNetworkFlag) {
				return errors.WrapValidationError(fmt.Sprintf(
					"--network and --compare-network must be different networks; both are %q",
					networkFlag))
			}
		}

		// Validate trace-verbosity when set explicitly.
		if cmd.Flags().Changed("trace-verbosity") {
			switch strings.ToLower(traceVerbosityFlag) {
			case "summary", "normal", "verbose":
				// valid
			default:
				return errors.WrapValidationError(fmt.Sprintf(
					"invalid --trace-verbosity %q; must be one of: summary, normal, verbose",
					traceVerbosityFlag))
			}
		}

		// Validate trace export path sanity when --trace-output is provided.
		if traceOutputFile != "" {
			if err := trace.ValidateTraceInputs(traceVerbosityFlag, "", "", traceOutputFile); err != nil {
				return errors.WrapValidationError(err.Error())
			}
		}

		// Validate --theme when set explicitly.
		if cmd.Flags().Changed("theme") && themeFlag != "" {
			validThemes := map[string]bool{
				"dark": true, "light": true, "none": true,
				"default": true, "deuteranopia": true, "protanopia": true,
				"tritanopia": true, "high-contrast": true,
			}
			if !validThemes[strings.ToLower(themeFlag)] {
				return errors.WrapValidationError(fmt.Sprintf(
					"invalid --theme %q; must be one of: dark, light, none, default, deuteranopia, protanopia, tritanopia, high-contrast",
					themeFlag))
			}
		}

		// Validate --format when set explicitly.
		if cmd.Flags().Changed("format") {
			switch strings.ToLower(debugFormatFlag) {
			case "text", "json":
				// valid
			default:
				return errors.WrapValidationError(fmt.Sprintf(
					"invalid --format %q; must be one of: text, json",
					debugFormatFlag))
			}
		}

		// Validate --watch-timeout is positive.
		if watchFlag && watchTimeoutFlag <= 0 {
			return errors.WrapValidationError(fmt.Sprintf(
				"--watch-timeout must be a positive integer (got %d)", watchTimeoutFlag,
			))
		}

		// Validate --trace-verbosity at parse time.
		if traceVerbosityFlag != "" {
			if _, err := trace.ParseVerbosity(traceVerbosityFlag); err != nil {
				return errors.WrapValidationError(fmt.Sprintf(
					"invalid --trace-verbosity %q — must be one of: summary, normal, verbose",
					traceVerbosityFlag,
				))
			}
		}

		// Validate --profile-format when --profile is enabled.
		if ProfileFlag {
			switch strings.ToLower(strings.TrimSpace(ProfileFormatFlag)) {
			case "html", "svg", "":
				// valid
			default:
				return errors.WrapValidationError(fmt.Sprintf(
					"invalid --profile-format %q — must be one of: html, svg\n"+
						"  Fix: use --profile-format html (interactive flamegraph) or svg (raw SVG)",
					ProfileFormatFlag,
				))
			}
		}

		// Validate --format at parse time.
		if debugFormatFlag != "" {
			switch strings.ToLower(strings.TrimSpace(debugFormatFlag)) {
			case "text", "json":
				// valid
			default:
				return errors.WrapValidationError(fmt.Sprintf(
					"invalid --format %q — must be one of: text, json",
					debugFormatFlag,
				))
			}
		}

		if liveReplayFlag && (xdrFileFlag != "" || jsonFileFlag != "") {
			return errors.WrapValidationError("--live/--latest-ledger cannot be used with local envelope input")
		}
		if liveReplayFlag && demoMode {
			return errors.WrapValidationError("--live/--latest-ledger cannot be used with --demo")
		}
		if opIndexFlag < -1 {
			return errors.WrapValidationError(
				"--op must be a non-negative integer or omitted; " +
					"use 0 for the first operation, 1 for the second, etc. " +
					"(omit to process all operations)")
		}
		if secureWorkspaceFlag {
			if contractSourceFlag != "" {
				if _, err := validateSecureArtifactPath(contractSourceFlag); err != nil {
					return err
				}
			}
			if wasmPath != "" {
				if _, err := validateSecureArtifactPath(wasmPath); err != nil {
					return err
				}
			}
		}

		// Validate --wasm file existence and WASM magic bytes before executing.
		if wasmPath != "" {
			wasmBytes, readErr := os.ReadFile(wasmPath)
			if readErr != nil {
				if os.IsNotExist(readErr) {
					return errors.WrapValidationError(fmt.Sprintf(
						"--wasm: file not found: %q\n"+
							"  Build your contract first (e.g. 'cargo build --release --target wasm32-unknown-unknown')\n"+
							"  and ensure the path points to the compiled .wasm output.", wasmPath,
					))
				}
				return errors.WrapValidationError(fmt.Sprintf(
					"--wasm: cannot read %q: %v", wasmPath, readErr,
				))
			}
			if err := abi.ValidateWasmMagic(wasmBytes, wasmPath); err != nil {
				return errors.WrapValidationError(err.Error())
			}
		}

		// Validate --contract-source points to an accessible directory.
		if contractSourceFlag != "" && !secureWorkspaceFlag {
			trimmed := strings.TrimSpace(contractSourceFlag)
			if trimmed == "" {
				return errors.WrapValidationError(fmt.Sprintf(
					"--contract-source: value must not be empty or whitespace\n" +
						"  Provide the path to your contract's source directory (the one containing src/).",
				))
			}
			info, statErr := os.Stat(trimmed)
			if statErr != nil {
				if os.IsNotExist(statErr) {
					return errors.WrapValidationError(fmt.Sprintf(
						"--contract-source: directory not found: %q\n"+
							"  Provide the path to your contract's source directory (the one containing src/).\n"+
							"  Source mapping will be unavailable without a valid path.",
						trimmed,
					))
				}
				return errors.WrapValidationError(fmt.Sprintf(
					"--contract-source: cannot access %q: %v", trimmed, statErr,
				))
			}
			if !info.IsDir() {
				return errors.WrapValidationError(fmt.Sprintf(
					"--contract-source: %q is a file, not a directory\n"+
						"  Provide the path to your contract's source directory, not a file.",
					trimmed,
				))
			}
		}

		// Validate --build-manifest points to a readable, valid manifest file.
		// Artifact hash verification against --wasm is deferred to the resolver
		// so it happens with the actual WASM bytes in hand.
		if buildManifestFlag != "" {
			trimmed := strings.TrimSpace(buildManifestFlag)
			if trimmed == "" {
				return errors.WrapValidationError(
					"--build-manifest: value must not be empty or whitespace\n" +
						"  Provide the path to a glassbox-build-manifest.json file.",
				)
			}
			if strings.ContainsRune(trimmed, 0) {
				return errors.WrapValidationError(
					"--build-manifest: path contains null bytes and cannot be used\n" +
						"  Fix: remove any null bytes from the path.",
				)
			}
			if _, statErr := os.Stat(trimmed); statErr != nil {
				if os.IsNotExist(statErr) {
					return errors.WrapValidationError(fmt.Sprintf(
						"--build-manifest: file not found: %q\n"+
							"  Generate a manifest with 'glassbox generate-manifest' or provide\n"+
							"  a valid path to an existing glassbox-build-manifest.json file.",
						trimmed,
					))
				}
				return errors.WrapValidationError(fmt.Sprintf(
					"--build-manifest: cannot access %q: %v", trimmed, statErr,
				))
			}
			if _, manifestErr := sourcemap.LoadManifest(trimmed); manifestErr != nil {
				return errors.WrapValidationError(fmt.Sprintf("%v", manifestErr))
			}
		}

		// Validate --mock-ledger-manifest file existence (full content validation
		// happens in loadMockLedgerOverrides during RunE).
		if mockLedgerManifest != "" {
			if _, statErr := os.Stat(mockLedgerManifest); statErr != nil {
				if os.IsNotExist(statErr) {
					return errors.WrapValidationError(fmt.Sprintf(
						"--mock-ledger-manifest: file not found: %q\n"+
							"  Ensure the path is correct before running the debug command.",
						mockLedgerManifest,
					))
				}
				return errors.WrapValidationError(fmt.Sprintf(
					"--mock-ledger-manifest: cannot access %q: %v", mockLedgerManifest, statErr,
				))
			}
		}

		// Validate --source-alias file existence and JSON parse.
		if sourceAliasFlag != "" {
			aliasBytes, readErr := os.ReadFile(sourceAliasFlag)
			if readErr != nil {
				if os.IsNotExist(readErr) {
					return errors.WrapValidationError(fmt.Sprintf(
						"--source-alias: file not found: %q\n"+
							"  Provide a JSON file mapping embedded source paths to local filesystem paths.\n"+
							"  Example: {\"my_crate\": \"/path/to/my_crate/src\"}",
						sourceAliasFlag,
					))
				}
				return errors.WrapValidationError(fmt.Sprintf(
					"--source-alias: cannot read %q: %v", sourceAliasFlag, readErr,
				))
			}
			var aliasMap map[string]string
			if jsonErr := json.Unmarshal(aliasBytes, &aliasMap); jsonErr != nil {
				return errors.WrapValidationError(fmt.Sprintf(
					"--source-alias: failed to parse %q as JSON: %v\n"+
						"  The file must be a flat JSON object mapping alias strings to real directory paths.\n"+
						"  Example: {\"my_crate\": \"/path/to/my_crate/src\"}",
					sourceAliasFlag, jsonErr,
				))
			}
			// Warn about alias target paths that don't exist on disk.
			for alias, target := range aliasMap {
				trimmedAlias := strings.TrimSpace(alias)
				trimmedTarget := strings.TrimSpace(target)
				if trimmedAlias == "" {
					continue
				}
				if trimmedTarget == "" {
					fmt.Fprintf(cmd.ErrOrStderr(),
						"Warning: --source-alias: target for %q is empty; source mapping for this alias will be skipped\n",
						trimmedAlias,
					)
					continue
				}
				if _, targetErr := os.Stat(trimmedTarget); targetErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(),
						"Warning: --source-alias: target for %q does not exist: %q — source mapping for this alias will be skipped\n",
						trimmedAlias, trimmedTarget,
					)
				}
			}
		}

		if pinEndpointFlag != "" && rpcURLFlag != "" && pinEndpointFlag != rpcURLFlag {
			return errors.WrapValidationError(
				"--pin-endpoint must match --rpc-url when both are provided; " +
					"set them to the same URL or remove one")
		}

		// Validate source discovery configuration before any network or
		// simulator work begins so users get actionable guidance early.
		if err := validateSourceDiscoveryFlags(); err != nil {
			return err
		}

		// Validate all input paths using the security-aware path normalizer.
		// This rejects null-byte injection, path traversal sequences, and
		// non-existent files before any network or simulator work begins.
		if err := ValidateDebugInputPaths(
			snapshotFlag, wasmPath, xdrFileFlag, jsonFileFlag,
			resultMetaFileFlag, loadSnapshotsFlag, sourceAliasFlag,
		); err != nil {
			return err
		}

		// Validate all output paths — parent directories must exist or be
		// reachable; the target must not already be a directory.
		if err := ValidateDebugOutputPaths(saveSnapshotsFlag, exportSVGFlag, traceOutputFile); err != nil {
			return err
		}

		// Schema pre-flight: when a --snapshot file is provided and it is a
		// PersistedSnapshot, check the schema version before any simulation
		// begins so stale or future-version files surface a clear error early.
		if snapshotFlag != "" {
			if result, schemaErr := snapshot.CheckSchemaVersion(snapshotFlag); schemaErr == nil && result != nil {
				if result.Unsupported || result.NeedsUpgrade {
					return errors.WrapValidationError(fmt.Sprintf(
						"--snapshot: %s\n"+
							"Use 'glassbox snapshot load --path %s' to inspect the snapshot details",
						result.Message, snapshotFlag,
					))
				}
				if result.FromFuture {
					return errors.WrapValidationError(fmt.Sprintf(
						"--snapshot: %s",
						result.Message,
					))
				}
			}
			// Non-nil schemaErr means the file is not a PersistedSnapshot (e.g. a
			// plain ledger-state JSON) — skip the schema check silently; identity
			// and fingerprint validation happens at replay time.
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, cmdArgs []string) error {
		ctx, corrID := telemetry.EnsureCorrelationID(cmd.Context())
		cmd.SetContext(ctx)

		perfCollector := perfmetrics.NewCollector()
		// diagCollector is active only when --timings is set; otherwise it is a
		// zero-cost no-op so the hot path is not affected.
		var diagCollector *diagnostics.Collector
		if debugTimingsFlag {
			diagCollector = diagnostics.NewCollector()
		} else {
			diagCollector = diagnostics.Noop()
		}
		diagCollector.SetCorrelationID(corrID)

		// Build progress sink — NopSink unless --progress-json is active.
		progSink := buildDebugSink()
		progEm := progress.NewEmitter(progSink)
		progEm.Start(progress.PhaseInit, "debug command starting")

		if verbose {
			logger.SetLevel(slog.LevelInfo)
		} else {
			logger.SetLevel(slog.LevelWarn)
		}

		// Dry-run: validate inputs and environment without executing replay.
		// This now uses the shared plan model for consistency with --plan.
		if debugDryRunFlag {
			if demoMode || wasmPath != "" || loadSnapshotsFlag != "" || xdrFileFlag != "" || jsonFileFlag != "" {
				progEm.Error(progress.PhaseInit, "--dry-run combined with incompatible flag", "invalid_dry_run_flags")
				return errors.WrapValidationError(
					"--dry-run cannot be combined with --demo, --wasm, --load-snapshots, or local envelope input; " +
						"--dry-run only validates the network transaction path")
			}
			if showMetricsFlag {
				return errors.WrapValidationError(
					"--show-metrics cannot be used with --dry-run; no simulation is executed in dry-run mode")
			}
			if debugTimingsFlag {
				return errors.WrapValidationError(
					"--timings cannot be used with --dry-run; no simulation is executed in dry-run mode")
			}
			if len(cmdArgs) == 0 {
				return errors.WrapValidationError(
					"transaction hash is required for --dry-run\n" +
						"Usage: glassbox debug --dry-run --network testnet <transaction-hash>")
			}
			// Build and render the execution plan for dry-run validation
			return runDebugDryRunPlan(cmd, cmdArgs[0])
		}

		// Plan: show what the command will do without performing any side effects.
		if debugPlanFlag {
			simBinary := ""
			if simDep := checkSimulator(false); simDep.Installed {
				simBinary = simDep.Path
			}
			simMode := "network"
			if wasmPath != "" {
				simMode = "local"
			}
			rpcEndpoint := rpcURLFlag
			if rpcEndpoint == "" {
				cfg, cfgErr := config.Load()
				if cfgErr == nil {
					if len(cfg.SorobanRpcUrls) > 0 {
						rpcEndpoint = cfg.SorobanRpcUrls[0]
					} else if cfg.RpcUrl != "" {
						rpcEndpoint = cfg.RpcUrl
					}
				}
			}
			var extraEndpoints []string
			if cfg, cfgErr := config.Load(); cfgErr == nil && len(cfg.SorobanRpcUrls) > 1 {
				extraEndpoints = cfg.SorobanRpcUrls[1:]
			}

			var txHash string
			if len(cmdArgs) > 0 {
				txHash = cmdArgs[0]
			}

			execPlan := plan.BuildDebugPlan(plan.DebugPlanOptions{
				TxHash:              txHash,
				Network:             networkFlag,
				RPCEndpoint:         rpcEndpoint,
				AdditionalEndpoints: extraEndpoints,
				PinnedEndpoint:      pinEndpointFlag,
				SimulatorBinary:     simBinary,
				SimulatorMode:       simMode,
				WasmPath:            wasmPath,
				SnapshotPath:        snapshotFlag,
				SaveSnapshotsPath:   saveSnapshotsFlag,
				LoadSnapshotsPath:   loadSnapshotsFlag,
				TraceOutputFile:     traceOutputFile,
				AuditKey:            auditKeyFlag,
				PublishIPFS:         publishIPFSFlag,
				IPFSNode:            ipfsNodeFlag,
				PublishArweave:      publishArweaveFlag,
				ArweaveGateway:      arweaveGatewayFlag,
				ExportSVG:           exportSVGFlag,
				CacheDisabled:       noCacheFlag,
				JSONOutput:          clioutput.WantsJSON(debugJSONFlag, debugFormatFlag),
			})

			out := cmd.OutOrStdout()
			if clioutput.WantsJSON(debugJSONFlag, debugFormatFlag) {
				jsonOut, jsonErr := execPlan.RenderJSON()
				if jsonErr != nil {
					return fmt.Errorf("failed to render plan as JSON: %w", jsonErr)
				}
				fmt.Fprintln(out, jsonOut)
			} else {
				fmt.Fprint(out, execPlan.RenderText())
			}
			return nil
		}

		// Apply theme if specified, otherwise auto-detect
		if themeFlag != "" {
			visualizer.SetTheme(visualizer.Theme(themeFlag))
		} else {
			visualizer.SetTheme(visualizer.DetectTheme())
		}

		// Demo mode: print sample output for testing color detection (no network)
		if demoMode {
			return runDemoMode(cmdArgs)
		}

		// Offline replay from a saved snapshot registry
		if loadSnapshotsFlag != "" {
			return runFromRegistry(cmd.Context(), loadSnapshotsFlag)
		}

		// Local WASM replay mode
		if wasmPath != "" {
			return runLocalWasmReplay()
		}

		localEnvelopeMode := xdrFileFlag != "" || jsonFileFlag != ""
		txHash := ""
		if !localEnvelopeMode {
			txHash = cmdArgs[0]
		}

		// Load persisted viewer state for this transaction (best-effort).
		var uiStore *session.UIStateStore
		if s, err := session.NewUIStateStore(); err == nil {
			uiStore = s
			defer uiStore.Close()
			if prev, err := uiStore.LoadSectionState(ctx, txHash); err == nil && len(prev) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Restoring viewer state: last session showed [%s] for this transaction.\n", strings.Join(prev, ", "))
			}
		}

		// Initialize OpenTelemetry if enabled via flag, env, or config (opt-in)
		telemetryEnabled := tracingEnabled
		if !telemetryEnabled {
			if v := os.Getenv("GLASSBOX_TELEMETRY"); v != "" {
				if b, err := strconv.ParseBool(v); err == nil {
					telemetryEnabled = b
				}
			}
			if !telemetryEnabled {
				if cfg, err := config.Load(); err == nil && cfg.TelemetryEnabled {
					telemetryEnabled = true
					if otlpExporterURL == "" && cfg.TelemetryEndpoint != "" {
						otlpExporterURL = cfg.TelemetryEndpoint
					}
				}
			}
		}

		if telemetryEnabled {
			cleanup, err := telemetry.Init(ctx, telemetry.Config{
				Enabled:     true,
				ExporterURL: otlpExporterURL,
				ServiceName: "glassbox",
			})
			if err != nil {
				return errors.WrapValidationError(fmt.Sprintf("failed to initialize telemetry: %v", err))
			}
			defer cleanup()
		}

		progEm.Complete(progress.PhaseInit, "initialization complete")

		// Start root span
		tracer := telemetry.GetTracer()
		ctx, span := tracer.Start(ctx, "debug_transaction")
		span.SetAttributes(
			telemetry.Attr("transaction.hash", txHash),
			telemetry.Attr("network", networkFlag),
		)
		defer span.End()

		var horizonURL string
		token := rpcTokenFlag
		if token == "" {
			token = os.Getenv("GLASSBOX_RPC_TOKEN")
		}
		if token == "" {
			if cfg, err := config.Load(); err == nil && cfg.RPCToken != "" {
				token = cfg.RPCToken
			}
		}

		opts := []rpc.ClientOption{
			rpc.WithNetwork(rpc.Network(networkFlag)),
			rpc.WithToken(token),
		}

		if rpcURLFlag != "" {
			urls := strings.Split(rpcURLFlag, ",")
			for i := range urls {
				urls[i] = strings.TrimSpace(urls[i])
			}
			opts = append(opts, rpc.WithAltURLs(urls))
			horizonURL = urls[0]
		} else {
			cfg, err := config.Load()
			if err == nil {
				if len(cfg.RpcUrls) > 0 {
					opts = append(opts, rpc.WithAltURLs(cfg.RpcUrls))
					horizonURL = cfg.RpcUrls[0]
				} else if cfg.RpcUrl != "" {
					opts = append(opts, rpc.WithHorizonURL(cfg.RpcUrl))
					horizonURL = cfg.RpcUrl
				}
				if cfg.FailureThreshold > 0 {
					opts = append(opts, rpc.WithCircuitBreakerThreshold(cfg.FailureThreshold))
				}
				if cfg.RetryTimeout > 0 {
					opts = append(opts, rpc.WithCircuitBreakerTimeout(cfg.RetryTimeout))
				}
				// Apply request timeout as the per-attempt pool deadline.
				if cfg.RequestTimeout > 0 {
					opts = append(opts, rpc.WithRequestDeadline(
						time.Duration(cfg.RequestTimeout)*time.Second,
					))
				}
			}
		}

		if pinEndpointFlag != "" {
			if rpcURLFlag == "" {
				opts = append(opts, rpc.WithHorizonURL(pinEndpointFlag))
				horizonURL = pinEndpointFlag
			}
			// Lock the provider pool to the pinned endpoint so replay is deterministic.
			opts = append(opts, rpc.WithReplayPinProvider(pinEndpointFlag))
			fmt.Fprintf(cmd.OutOrStdout(), "Pinned RPC endpoint: %s\n", pinEndpointFlag)
		}

		client, err := rpc.NewClient(opts...)
		if err != nil {
			return errors.WrapValidationError(fmt.Sprintf("failed to create client: %v", err))
		}

		if horizonURL == "" {
			// Extract horizon URL from valid client if not explicitly set
			horizonURL = client.HorizonURL
		}

		if noCacheFlag {
			client.CacheEnabled = false
			fmt.Fprintln(cmd.OutOrStdout(), "🚫 Cache disabled by --no-cache flag")
		}

		if !localEnvelopeMode {
			_ = client.CheckStaleness(ctx, networkFlag)
		}

		if localEnvelopeMode {
			fmt.Fprintln(cmd.OutOrStdout(), "Debugging local transaction envelope")
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Debugging transaction: %s\n", txHash)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Primary Network: %s\n", networkFlag)
		if compareNetworkFlag != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Comparing against Network: %s\n", compareNetworkFlag)
		}

		var watchEvents <-chan struct{}
		if watchFilesFlag {
			paths := []string{"."}
			if contractSourceFlag != "" {
				paths = append(paths, contractSourceFlag)
			}
			cfg := watch.FileWatcherConfig{
				Paths:          paths,
				PollInterval:   500 * time.Millisecond,
				DebounceWindow: 500 * time.Millisecond,
			}
			events, errs := watch.StartFileWatcher(ctx, cfg)
		watchEvents = events
		errWriter := cmd.ErrOrStderr()
		go func() {
			for err := range errs {
				fmt.Fprintf(errWriter, "Watch error: %v\n", err)
			}
		}()
			fmt.Fprintln(cmd.OutOrStdout(), "Watching for file changes to rerun debug session...")
		}

	watchLoop:
		for {
			sessionErr := func() error {
				// Fetch transaction details
				if watchFlag {
					spinner := watch.NewSpinner()
					spinner.Start("Waiting for transaction to appear on-chain...")
					watchCtx, cancelWatch := context.WithTimeout(ctx, time.Duration(watchTimeoutFlag)*time.Second)
					defer cancelWatch()

					statusCh, err := client.WatchTransaction(watchCtx, txHash)
					if err != nil {
						spinner.StopWithError("Failed to start transaction watch")
						return errors.WrapSimulationLogicError(fmt.Sprintf("watch mode error: %v", err))
					}

					var finalStatus *rpc.TxStatus
					for status := range statusCh {
						if status.IsFinal() {
							statusCopy := status
							finalStatus = &statusCopy
							break
						}
					}

					if err := watchCtx.Err(); err != nil {
						spinner.StopWithError("Transaction not found within timeout")
						return errors.WrapTransactionNotFound(fmt.Errorf("not found after %d seconds", watchTimeoutFlag))
					}

					if finalStatus == nil {
						spinner.StopWithError("Transaction watch ended unexpectedly")
						return errors.WrapSimulationLogicError("watch mode ended before a final transaction status was received")
					}

					spinner.StopWithMessage(fmt.Sprintf("Transaction reached %s. Starting debug...", strings.ToLower(finalStatus.Status)))
				}

				var resp *rpc.TransactionResponse
				var localInputNetwork string
				if localEnvelopeMode {
					envelopeXdr, resultMetaXdr, fileNetwork, err := loadTransactionEnvelopeInput(xdrFileFlag, jsonFileFlag, resultMetaFileFlag)
					if err != nil {
						return err
					}
					resp = &rpc.TransactionResponse{EnvelopeXdr: envelopeXdr, ResultMetaXdr: resultMetaXdr}
					localInputNetwork = fileNetwork
					if localInputNetwork != "" && !cmd.Flags().Changed("network") {
						networkFlag = localInputNetwork
					}
					fmt.Fprintf(cmd.OutOrStdout(), "Loaded local transaction envelope from %s\n", func() string {
						if xdrFileFlag != "" {
							return xdrFileFlag
						}
						return jsonFileFlag
					}())
					fmt.Fprintf(cmd.OutOrStdout(), "Envelope size: %d bytes\n", len(resp.EnvelopeXdr))
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "Fetching transaction: %s\n", txHash)
					_t0 := time.Now()
					doneRPC := diagCollector.Start(diagnostics.PhaseRPCFetch)
					resp, err = client.GetTransaction(ctx, txHash)
					if showMetricsFlag {
						perfCollector.RecordRPC("getTransaction", time.Since(_t0), err != nil)
					}
					doneRPC(err)
					if err != nil {
						// Surface structured failover diagnostics before returning.
						poolDiag := client.PoolDiagnostics()
						if len(poolDiag.Attempts) > 0 {
							allFailed := poolDiag.SucceededURL == ""
							summary := rpc.BuildRPCFailureSummary(poolDiag)
							if allFailed {
								states := client.GetHealthReport()
								diag := rpc.AllNodesFailedDiagnostic{
									Summary:        summary,
									Network:        networkFlag,
									Timestamp:      time.Now(),
								}
								if states != nil {
									diag.ProviderStates = make([]rpc.ProviderState, 0)
									// Attach circuit-breaker-aware state from the pool when available.
									if pool := client.ProviderPool(); pool != nil {
										diag.ProviderStates = pool.ProviderStates()
									}
								}
								fmt.Fprint(cmd.ErrOrStderr(), rpc.FormatAllNodesFailedText(diag))
							} else if len(poolDiag.Attempts) > 1 {
								// Partial failover succeeded — report it informatively.
								fmt.Fprintf(cmd.ErrOrStderr(), "Note: RPC failover occurred (%d attempt(s)) — succeeded via %s\n",
									len(poolDiag.Attempts), poolDiag.SucceededURL)
							}
						}
						return errors.WrapRPCConnectionFailed(err)
					}

	fmt.Fprintf(cmd.OutOrStdout(), "Transaction fetched successfully. Envelope size: %d bytes\n", len(resp.EnvelopeXdr))
					// Show which provider succeeded when the pool attempted failover.
					if diag := client.PoolDiagnostics(); len(diag.Attempts) > 1 {
						fmt.Fprintf(cmd.OutOrStdout(), "Provider failover occurred — succeeded via: %s\n", diag.SucceededURL)
					}
				}
				keys, err := extractLedgerKeys(resp.ResultMetaXdr)
				if err != nil {
					return errors.WrapUnmarshalFailed(err, "result meta")
				}

				// Load config to get MaxTraceDepth for decoder calls
				cfg, _ := config.Load()
				maxDepth := 50
				if cfg != nil {
					maxDepth = cfg.MaxTraceDepth
				}

				// Initialize Simulator Runner
				runner, err := simulator.NewRunnerWithMockTime("", tracingEnabled, mockTimeFlag)
				if err != nil {
					return errors.WrapSimulatorNotFound(err.Error())
				}
				// Ensure the child simulator process is killed on Ctrl-C or any early
				// return from this function.  registerRunnerCloseHook wires into the
				// shutdown.Coordinator so the process group is reaped within the
				// shutdownTimeout budget even when the signal fires while runner.Run is
				// blocked waiting for the subprocess.
				defer runner.Close()
				registerRunnerCloseHook("debug-simulator", runner)

				// Determine timestamps to simulate
				timestamps := []int64{TimestampFlag}
				if WindowFlag > 0 && TimestampFlag > 0 {
					// Simulate 5 steps across the window
					step := WindowFlag / 4
					for i := 1; i <= 4; i++ {
						timestamps = append(timestamps, TimestampFlag+int64(i)*step)
					}
				}

				overrideEntries, err := loadMockLedgerOverrides()
				if err != nil {
					return err
				}

				var lastSimResp *simulator.SimulationResponse
				useLiveLedger := liveReplayFlag

				// Collected per-timestamp states written to disk when --save-snapshots is set.
				type snapshotEntry struct {
					ts      int64
					entries map[string]string
				}
				var collectedEntries []snapshotEntry

				for _, ts := range timestamps {
					if len(timestamps) > 1 {
						fmt.Fprintf(cmd.OutOrStdout(), "\n--- Simulating at Timestamp: %d ---\n", ts)
					}

					var simResp *simulator.SimulationResponse
					var ledgerEntries map[string]string

					if compareNetworkFlag == "" {
						// Single Network Run
						if snapshotFlag != "" {
							snap, driftWarn, loadErr := snapshot.LoadWithDiagnostics(snapshotFlag)
							if loadErr != nil {
								return errors.WrapValidationError(fmt.Sprintf("failed to load snapshot: %v", loadErr))
							}
							// Surface drift as a prominent warning — the user should be
							// aware that the ledger state may not match what was originally
							// captured.
							if driftWarn != nil {
								fmt.Fprintf(cmd.ErrOrStderr(), "\nWarning: %s\n\n", driftWarn.Error())
							}
							// Validate the snapshot identity and freshness before replay.
							currentParams := map[string]string{
								"network": networkFlag,
								"tx":      txHash,
							}
							currentSourceHash := ""
							if wasmPath != "" {
								if wasmBytes, rerr := os.ReadFile(wasmPath); rerr == nil {
									currentSourceHash = snapshot.HashWasmSource(wasmBytes)
								}
							}
							// We load the persisted form for identity/staleness checks.
							// If the file is a plain ledger-state JSON (not a PersistedSnapshot),
							// LoadPersisted will fail gracefully and we skip the identity checks.
							if ps, psErr := snapshot.LoadPersisted(snapshotFlag); psErr == nil {
								if valErr := snapshot.ValidateSnapshotBeforeReplay(
									ps, txHash, networkFlag, currentParams, currentSourceHash,
								); valErr != nil {
									return errors.WrapValidationError(fmt.Sprintf(
										"snapshot failed pre-replay validation: %v\n"+
											"Use 'glassbox snapshot load --path %s' to inspect the snapshot details",
										valErr, snapshotFlag))
								}
							}
							ledgerEntries = snap.ToMap()
							fmt.Printf("Loaded %d ledger entries from snapshot\n", len(ledgerEntries))
						} else if useLiveLedger {
							if localEnvelopeMode {
								return errors.WrapValidationError("--live/--latest-ledger cannot be used with local envelope input")
							}
							fmt.Println("Using latest validated ledger state for live replay...")
							ledgerEntries, err = client.GetLedgerEntries(ctx, keys)
							if err != nil {
								return errors.WrapRPCConnectionFailed(err)
							}
						} else {
							// Try to extract from metadata first, fall back to fetching
							ledgerEntries, err = rpc.ExtractLedgerEntriesFromMeta(resp.ResultMetaXdr)
							if err != nil {
								logger.Logger.Warn("Failed to extract ledger entries from metadata")
								if localEnvelopeMode {
									logger.Logger.Info("Offline local envelope mode: skipping network ledger fetch")
									ledgerEntries = map[string]string{}
								} else {
									logger.Logger.Warn("Failed to extract ledger entries from metadata, fetching from network", "error", err)
									ledgerEntries, err = client.GetLedgerEntries(ctx, keys)
									if err != nil {
										return errors.WrapRPCConnectionFailed(err)
									}
								}
							} else {
								logger.Logger.Info("Extracted ledger entries for simulation", "count", len(ledgerEntries))
							}
						}

						if len(overrideEntries) > 0 {
							ledgerEntries = simulator.MergeLedgerOverrides(ledgerEntries, overrideEntries)
							fmt.Printf("Applied %d mock ledger override entries\n", len(overrideEntries))
						}

						if saveSnapshotsFlag != "" {
							collectedEntries = append(collectedEntries, snapshotEntry{ts: ts, entries: ledgerEntries})
						}

						fmt.Printf("Running simulation on %s...\n", networkFlag)
						simReq := &simulator.SimulationRequest{
							EnvelopeXdr:     resp.EnvelopeXdr,
							ResultMetaXdr:   resp.ResultMetaXdr,
							LedgerEntries:   ledgerEntries,
							Timestamp:       ts,
							ProtocolVersion: nil,
							EnableSnapshots: snapshotsFlag,
						}

						// Apply protocol version override if specified
						if protocolVersionFlag > 0 {
							if err := simulator.Validate(protocolVersionFlag); err != nil {
								return fmt.Errorf("invalid protocol version %d: %w", protocolVersionFlag, err)
							}
							simReq.ProtocolVersion = &protocolVersionFlag
							fmt.Printf("Using protocol version override: %d\n", protocolVersionFlag)
						}
						applyDebugSimulationOptions(simReq)
						applySimulationFeeMocks(simReq)

						if showMetricsFlag {
							perfCollector.StartSim()
						}
						doneSim := diagCollector.Start(diagnostics.PhaseSimulator)
						simResp, err = runner.Run(ctx, simReq)
						if showMetricsFlag {
							perfCollector.StopSim()
						}
						doneSim(err)
						if err != nil {
							return errors.WrapSimulationFailed(err, "")
						}
						printSimulationResult(networkFlag, simResp)
						// Budget usage is already rendered inside printSimulationResult; skip duplicate block.

						// Surface a diagnostic when the simulator returned no trace events — this
						// is actionable: users should check their simulator version or the
						// transaction envelope.
						if simResp != nil && len(simResp.DiagnosticEvents) == 0 && len(simResp.Events) == 0 {
							fmt.Fprintf(os.Stderr,
								"\nNote: the simulator did not produce any diagnostic events for this transaction.\n"+
									"Trace accuracy may be limited. Check that:\n"+
									"  • The simulator binary is up-to-date (run 'glassbox doctor --fix')\n"+
									"  • The transaction includes InvokeHostFunction operations\n"+
									"  • The transaction envelope is valid and not malformed\n"+
									"  • The --snapshots flag is set if per-step snapshot data is needed\n",
							)
						}

						// Render colored before/after ledger state diff.
						if postState, diffErr := rpc.ExtractPostStateLedgerEntries(resp.ResultMetaXdr); diffErr == nil {
							visualizer.RenderLedgerStateDiff(ledgerEntries, postState, false)
						}

						// Fetch contract bytecode on demand for any contract calls in the trace; cache via RPC client
						if !localEnvelopeMode && client != nil && simResp != nil && len(simResp.DiagnosticEvents) > 0 {
							contractIDs := collectContractIDsFromDiagnosticEvents(simResp.DiagnosticEvents)
							if len(contractIDs) > 0 {
								_, _ = rpc.FetchBytecodeForTraceContractCalls(ctx, client, contractIDs, nil)
							}
						}
					} else {
						// Comparison Run
						var wg sync.WaitGroup
						var primaryResult, compareResult *simulator.SimulationResponse
						var primaryErr, compareErr error

						wg.Add(2)
						go func() {
							defer wg.Done()
							var entries map[string]string
							var extractErr error
							if useLiveLedger {
								entries, extractErr = client.GetLedgerEntries(ctx, keys)
								if extractErr != nil {
									primaryErr = extractErr
									return
								}
							} else {
								entries, extractErr = rpc.ExtractLedgerEntriesFromMeta(resp.ResultMetaXdr)
								if extractErr != nil {
									entries, extractErr = client.GetLedgerEntries(ctx, keys)
									if extractErr != nil {
										primaryErr = extractErr
										return
									}
								}
							}
							if len(overrideEntries) > 0 {
								entries = simulator.MergeLedgerOverrides(entries, overrideEntries)
								fmt.Printf("Applied %d mock ledger override entries to primary comparison\n", len(overrideEntries))
							}
							primaryReq := &simulator.SimulationRequest{
								EnvelopeXdr:     resp.EnvelopeXdr,
								ResultMetaXdr:   resp.ResultMetaXdr,
								LedgerEntries:   entries,
								Timestamp:       ts,
								EnableSnapshots: snapshotsFlag,
							}
							applyDebugSimulationOptions(primaryReq)
							applySimulationFeeMocks(primaryReq)
							primaryResult, primaryErr = runner.Run(ctx, primaryReq)
						}()

						go func() {
							defer wg.Done()
							compareOpts := []rpc.ClientOption{
								rpc.WithNetwork(rpc.Network(compareNetworkFlag)),
								rpc.WithToken(rpcTokenFlag),
							}
							compareClient, clientErr := rpc.NewClient(compareOpts...)
							if clientErr != nil {
								compareErr = errors.WrapValidationError(fmt.Sprintf("failed to create compare client: %v", clientErr))
								return
							}
							if noCacheFlag {
								compareClient.CacheEnabled = false
							}

							compareResp, txErr := compareClient.GetTransaction(ctx, txHash)
							if txErr != nil {
								compareErr = errors.WrapRPCConnectionFailed(txErr)
								return
							}

							var entries map[string]string
							var extractErr error
							if useLiveLedger {
								entries, extractErr = compareClient.GetLedgerEntries(ctx, keys)
								if extractErr != nil {
									compareErr = extractErr
									return
								}
							} else {
								entries, extractErr = rpc.ExtractLedgerEntriesFromMeta(compareResp.ResultMetaXdr)
								if extractErr != nil {
									entries, extractErr = compareClient.GetLedgerEntries(ctx, keys)
									if extractErr != nil {
										compareErr = extractErr
										return
									}
								}
							}

							if len(overrideEntries) > 0 {
								entries = simulator.MergeLedgerOverrides(entries, overrideEntries)
								fmt.Printf("Applied %d mock ledger override entries to compare comparison\n", len(overrideEntries))
							}

							compareReq := &simulator.SimulationRequest{
								EnvelopeXdr:     compareResp.EnvelopeXdr,
								ResultMetaXdr:   compareResp.ResultMetaXdr,
								LedgerEntries:   entries,
								Timestamp:       ts,
								EnableSnapshots: snapshotsFlag,
							}
							applyDebugSimulationOptions(compareReq)
							applySimulationFeeMocks(compareReq)
							compareResult, compareErr = runner.Run(ctx, compareReq)
						}()

						wg.Wait()
						if primaryErr != nil {
							return errors.WrapRPCConnectionFailed(primaryErr)
						}
						if compareErr != nil {
							return errors.WrapRPCConnectionFailed(compareErr)
						}
						// Fetch contract bytecode on demand for contract calls in the trace; cache via RPC client
						if client != nil && primaryResult != nil && len(primaryResult.DiagnosticEvents) > 0 {
							contractIDs := collectContractIDsFromDiagnosticEvents(primaryResult.DiagnosticEvents)
							if len(contractIDs) > 0 {
								_, _ = rpc.FetchBytecodeForTraceContractCalls(ctx, client, contractIDs, nil)
							}
						}

						simResp = primaryResult // Use primary for further analysis
						printSimulationResult(networkFlag, primaryResult)
						printSimulationResult(compareNetworkFlag, compareResult)
						diffResults(primaryResult, compareResult, networkFlag, compareNetworkFlag)
					}
					lastSimResp = simResp

					if exportSVGFlag != "" && simResp != nil && len(simResp.DiagnosticEvents) > 0 {
						callTree, err := decoder.DecodeDiagnosticEvents(simResp.DiagnosticEvents, maxDepth)
						if err != nil {
							fmt.Printf("%s Error building call tree for SVG: %v\n", visualizer.Symbol("error"), err)
						} else {
							svg := visualizer.GenerateCallGraphSVG(callTree, maxDepth)
							err := os.WriteFile(exportSVGFlag, []byte(svg), 0644)
							if err != nil {
								fmt.Printf("%s Error saving SVG: %v\n", visualizer.Symbol("error"), err)
							} else {
								fmt.Printf("%s Call graph exported to: %s\n", visualizer.Symbol("success"), exportSVGFlag)
							}
						}
					}
				}

				if lastSimResp == nil {
					return errors.WrapSimulationLogicError("no simulation results generated")
				}

				// Persist snapshot registry to disk when --save-snapshots is set.
				if saveSnapshotsFlag != "" && len(collectedEntries) > 0 {
					reg := replay.New(version.Version, txHash, networkFlag, resp.EnvelopeXdr, resp.ResultMetaXdr)
					for _, ce := range collectedEntries {
						reg.Add(ce.ts, snapshot.FromMap(ce.entries))
					}
					if err := reg.SaveToFile(saveSnapshotsFlag); err != nil {
						fmt.Printf("Warning: failed to save snapshot registry: %v\n", err)
					} else {
						fmt.Printf("Snapshot registry saved: %s (%d entr%s)\n",
							saveSnapshotsFlag, len(reg.Entries), pluralIes(len(reg.Entries)))
					}
				}

				// Analysis: Error Suggestions (Heuristic-based)
				if len(lastSimResp.Events) > 0 {
					suggestionEngine := decoder.NewSuggestionEngine()

					// Decode events for analysis
					doneDecode := diagCollector.Start(diagnostics.PhaseDecode)
					callTree, err := decoder.DecodeEvents(lastSimResp.Events, maxDepth)
					var decodeErr error
					if err != nil {
						decodeErr = err
					}
					doneDecode(decodeErr)
					if err == nil && callTree != nil {
						suggestions := suggestionEngine.AnalyzeCallTree(callTree)
						if len(suggestions) > 0 {
							fmt.Print(decoder.FormatSuggestions(suggestions))
						}
					}
				}

				// Analysis: Security
				fmt.Printf("\n=== Security Analysis ===\n")
				secDetector := security.NewDetector()
				findings := secDetector.Analyze(resp.EnvelopeXdr, resp.ResultMetaXdr, lastSimResp.Events, lastSimResp.Logs)
				if contractSourceFlag != "" {
					doneSrcMap := diagCollector.Start(diagnostics.PhaseSourceMap)
					sourceFindings, scanErr := secDetector.ScanSourcePath(contractSourceFlag, nil)
					doneSrcMap(scanErr)
					if scanErr != nil {
						fmt.Fprintf(os.Stderr, "Warning: source vulnerability scan failed: %v\n", scanErr)
					} else {
						findings = append(findings, sourceFindings...)
					}
				}
				printSecurityFindings(findings)

				// Analysis: Token Flows
				hasTokenFlows := false
				if report, err := tokenflow.BuildReport(resp.EnvelopeXdr, resp.ResultMetaXdr); err == nil && len(report.Agg) > 0 {
					hasTokenFlows = true
					fmt.Printf("\nToken Flow Summary:\n")
					for _, line := range report.SummaryLines() {
						fmt.Printf("  %s\n", line)
					}
					fmt.Printf("\nToken Flow Chart (Mermaid):\n")
					fmt.Println(report.MermaidFlowchart())
				}

				// Persist viewer state so the next debug of this transaction restores context.
				if uiStore != nil {
					_ = uiStore.SaveSectionState(ctx, txHash, collectVisibleSections(lastSimResp, findings, hasTokenFlows))
				}

				// Session Management
				simReq := &simulator.SimulationRequest{
					EnvelopeXdr:     resp.EnvelopeXdr,
					ResultMetaXdr:   resp.ResultMetaXdr,
					EnableSnapshots: snapshotsFlag,
				}
				applyDebugSimulationOptions(simReq)
				applySimulationFeeMocks(simReq)
				simReqJSON, err := json.Marshal(simReq)
				if err != nil {
					fmt.Printf("Warning: failed to serialize simulation data: %v\n", err)
				}
				simRespJSON, err := json.Marshal(lastSimResp)
				if err != nil {
					fmt.Printf("Warning: failed to serialize simulation results: %v\n", err)
				}

				sessionData := &session.Data{
					ID:              session.GenerateID(txHash),
					CreatedAt:       time.Now(),
					LastAccessAt:    time.Now(),
					Status:          "active",
					Network:         networkFlag,
					HorizonURL:      horizonURL,
					TxHash:          txHash,
					EnvelopeXdr:     resp.EnvelopeXdr,
					ResultXdr:       resp.ResultXdr,
					ResultMetaXdr:   resp.ResultMetaXdr,
					PinnedEndpoint:  pinEndpointFlag,
					SimRequestJSON:  string(simReqJSON),
					SimResponseJSON: string(simRespJSON),
					ErstVersion:     version.Version,
					SchemaVersion:   session.SchemaVersion,
					EnvFingerprint:  session.BuildEnvFingerprint(),
				}
				SetCurrentSession(sessionData)
				fmt.Printf("\nSession created: %s\n", sessionData.ID)
				fmt.Printf("Run 'Glassbox session save' to persist this session.\n")

				// Publish signed audit trail to decentralised storage when requested.
				if publishIPFSFlag || publishArweaveFlag {
					if auditKeyFlag == "" {
						return errors.WrapCliArgumentRequired("audit-key")
					}
					auditLog, auditErr := Generate(
						txHash,
						resp.EnvelopeXdr,
						resp.ResultMetaXdr,
						lastSimResp.Events,
						lastSimResp.Logs,
						auditKeyFlag,
						nil,
					)
					if auditErr != nil {
						return fmt.Errorf("failed to generate audit log: %w", auditErr)
					}
					auditBytes, auditErr := json.Marshal(auditLog)
					if auditErr != nil {
						return fmt.Errorf("failed to marshal audit log: %w", auditErr)
					}

					pub := decenstorage.New(decenstorage.PublishConfig{
						IPFSNode:       ipfsNodeFlag,
						ArweaveGateway: arweaveGatewayFlag,
						ArweaveWallet:  arweaveWalletFlag,
					})

					fmt.Printf("\n=== Decentralised Storage ===\n")

					if publishIPFSFlag {
						result, ipfsErr := pub.PublishIPFS(ctx, auditBytes)
						if ipfsErr != nil {
							fmt.Printf("IPFS publish failed: %v\n", ipfsErr)
						} else {
							fmt.Printf("IPFS CID : %s\n", result.CID)
							fmt.Printf("IPFS URL : %s\n", result.URL)
						}
					}

					if publishArweaveFlag {
						result, arErr := pub.PublishArweave(ctx, auditBytes)
						if arErr != nil {
							fmt.Printf("Arweave publish failed: %v\n", arErr)
						} else {
							fmt.Printf("Arweave TXID : %s\n", result.TXID)
							fmt.Printf("Arweave URL  : %s\n", result.URL)
						}
					}
				}

				// ── Trace export ────────────────────────────────────────────────────────
				// Build an ExecutionTrace from the last simulation's diagnostic events
				// whenever trace output was requested (--generate-trace / --trace-output).
				if (generateTrace || traceOutputFile != "") && lastSimResp != nil {
					outPath := traceOutputFile
					if outPath == "" {
						// Default filename when only --generate-trace was supplied.
						outPath = "trace-" + txHash[:min(8, len(txHash))] + ".json"
					}
					execTrace := trace.BuildExecutionTraceFromSimResponse(txHash, lastSimResp)
					if len(execTrace.States) == 0 {
						fmt.Fprintf(os.Stderr,
							"Warning: trace export skipped — the simulator produced no diagnostic events for this transaction.\n"+
								"  Ensure the simulator is up-to-date and the transaction includes InvokeHostFunction operations.\n"+
								"  Run 'glassbox doctor --fix' to refresh the simulator binary.\n",
						)
					} else {
						targetFmt := strings.ToLower(strings.TrimSpace(debugFormatFlag))
						if targetFmt == "" || targetFmt == "text" || targetFmt == "json" {
							// Default trace export format is JSON regardless of the display format.
							targetFmt = "json"
						}
						fmt.Fprintf(os.Stderr, "Exporting %d-step trace as %s to %s...\n",
							len(execTrace.States), targetFmt, outPath)

						// Guard: abort export immediately if the context was already
						// cancelled so we never write a partial file.
						if ctx.Err() != nil {
							fmt.Fprintf(os.Stderr, "Trace export skipped: operation was cancelled.\n")
						} else {
							doneExport := diagCollector.Start(diagnostics.PhaseTraceExport)
							exportErr := trace.ExportWithCompatibility(execTrace, targetFmt, outPath, trace.ExportOptions{}, trace.DefaultCompatibilityOptions())
							doneExport(exportErr)
							if exportErr != nil {
								// Remove any partial file the exporter may have created
								// before the error was returned, then tell the user.
								_ = os.Remove(outPath)
								if ctx.Err() != nil {
									fmt.Fprintf(os.Stderr,
										"Trace export cancelled — partial file removed: %s\n", outPath)
								} else {
									fmt.Fprintf(os.Stderr,
										"%s Trace export failed: %v\n"+
											"  Fix: check that the output directory exists and you have write permissions.\n"+
											"  Path: %s\n",
										visualizer.Symbol("error"), exportErr, outPath,
									)
								}
							} else {
								sizeStr := traceExportedFileSize(outPath)
								fmt.Printf("%s Trace exported to: %s%s\n", visualizer.Symbol("success"), outPath, sizeStr)
							}
						}
					}
				}

				// ── Flamegraph / pprof profiling ─────────────────────────────────────
				// Produce a flamegraph when the global --profile flag was set.
				if ProfileFlag && lastSimResp != nil {
					execTrace := trace.BuildExecutionTraceFromSimResponse(txHash, lastSimResp)
					if len(execTrace.States) == 0 {
						fmt.Fprintf(os.Stderr,
							"Warning: flamegraph skipped — no diagnostic events available.\n"+
								"  Run with --snapshots or ensure the simulator produces diagnostic events.\n",
						)
					} else {
						format := visualizer.ExportFormat(strings.ToLower(strings.TrimSpace(ProfileFormatFlag)))
						if format != visualizer.FormatHTML && format != visualizer.FormatSVG {
							format = visualizer.FormatHTML
						}
						flamegraphPath := txHash[:min(8, len(txHash))] + format.GetFileExtension()

						// Two code paths: if the simulator returned a raw SVG flamegraph use
						// that; otherwise generate one from the trace gas data via profile.
						var flamegraphContent string
						if lastSimResp.Flamegraph != "" {
							flamegraphContent = visualizer.ExportFlamegraph(lastSimResp.Flamegraph, format)
						} else {
							if format == visualizer.FormatHTML {
								// Use profile.GenerateHTML which writes to an io.Writer.
								var profileBuf strings.Builder
								if genErr := profile.GenerateHTML(execTrace, &profileBuf); genErr != nil {
									fmt.Fprintf(os.Stderr,
										"Warning: failed to generate flamegraph HTML: %v\n",
										genErr,
									)
								} else {
									flamegraphContent = profileBuf.String()
								}
							} else {
								// SVG: not producible without the inferno crate output.
								// Emit a clear diagnostic rather than an empty file.
								fmt.Fprintf(os.Stderr,
									"Warning: --profile-format=svg requires the simulator to return a raw flamegraph SVG.\n"+
										"  The simulator did not return a flamegraph for this transaction.\n"+
										"  Use --profile-format=html to generate an interactive flamegraph from trace data instead.\n",
								)
							}
						}

						if flamegraphContent != "" {
							if err := os.WriteFile(flamegraphPath, []byte(flamegraphContent), 0o644); err != nil {
								fmt.Fprintf(os.Stderr,
									"%s Failed to write flamegraph to %q: %v\n"+
										"  Fix: ensure the output directory exists and you have write permissions.\n",
									visualizer.Symbol("error"), flamegraphPath, err,
								)
							} else {
								fmt.Printf("%s Flamegraph exported to: %s\n", visualizer.Symbol("success"), flamegraphPath)
							}
						}
					}
				}

				if showMetricsFlag {
					if clioutput.WantsJSON(debugJSONFlag, debugFormatFlag) {
						_ = perfCollector.PrintJSON(cmd.OutOrStdout())
					} else {
						perfCollector.Print(cmd.OutOrStdout())
					}
				}

				// --timings: write phase timing table to stderr so it never pollutes
				// stdout / JSON output. For JSON mode we embed timings as a structured
				// object on stderr (one JSON line) so CI tooling can parse it separately.
				if debugTimingsFlag {
					if clioutput.WantsJSON(debugJSONFlag, debugFormatFlag) {
						enc := json.NewEncoder(cmd.ErrOrStderr())
						enc.SetIndent("", "  ")
						_ = enc.Encode(map[string]interface{}{
							"timings": diagCollector.BuildTimingsBlock(),
						})
					} else {
						diagCollector.PrintHuman(cmd.ErrOrStderr())
					}
				}

				// Translate context cancellation into the canonical ErrInterrupted so
				// the caller (run() in main.go) emits "Interrupted." instead of a raw
				// context error, and the exit code is 130 rather than 3.
				if ctx.Err() != nil {
					return ErrInterrupted
				}
				return nil
			}()

			if sessionErr != nil {
				if !watchFilesFlag {
					return sessionErr
				}
				fmt.Printf("Debug session error: %v\n", sessionErr)
			} else if !watchFilesFlag {
				return nil
			}

			fmt.Println("Waiting for file changes...")
			select {
			case <-ctx.Done():
				break watchLoop
			case <-watchEvents:
				fmt.Println("File change detected, rerunning...")
				// Add a small separator
				fmt.Println("==================================================")
			}
		}
		return nil
	},
}

// runDemoMode prints sample output without network/WASM - for testing color detection.
func runDemoMode(cmdArgs []string) error {
	txHash := "5c0a1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab"
	if len(cmdArgs) > 0 && len(cmdArgs[0]) == 64 {
		txHash = cmdArgs[0]
	}

	fmt.Printf("Fetching transaction: %s\n", txHash)
	fmt.Printf("Transaction fetched successfully. Envelope size: 256 bytes\n")
	fmt.Printf("\n--- Result for %s ---\n", networkFlag)
	fmt.Printf("Status: success\n")
	fmt.Printf("\nResource Usage:\n")
	fmt.Printf("  CPU Instructions: 12345\n")
	fmt.Printf("  Memory Bytes: 1024\n")
	fmt.Printf("  Operations: 5\n")
	fmt.Printf("\nEvents: 2, Logs: 3\n")
	fmt.Printf("\n=== Security Analysis ===\n")
	fmt.Printf("%s No security issues detected\n", visualizer.Success())
	fmt.Printf("\nToken Flow Summary:\n")
	fmt.Printf("  %s XLM transferred\n", visualizer.Symbol("arrow_r"))
	fmt.Printf("\nSession ready. Use 'Glassbox session save' to persist.\n")
	return nil
}

func runLocalWasmReplay() error {
	fmt.Printf("%s  WARNING: Using Mock State (not mainnet data)\n", visualizer.Warning())
	fmt.Println()

	// Verify WASM file exists
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		return errors.WrapValidationError(fmt.Sprintf("WASM file not found or unreadable: %s", wasmPath))
	}
	wasmBase64 = base64.StdEncoding.EncodeToString(wasmBytes)

	fmt.Printf("%s Local WASM Replay Mode\n", visualizer.Symbol("wrench"))
	fmt.Printf("WASM File: %s\n", wasmPath)
	fmt.Printf("Arguments: %v\n", args)
	fmt.Println()

	// Analyze WASM binary size and emit warnings for large contracts.
	if sizeAnalysis, sizeErr := abi.AnalyzeWasmSize(wasmBytes); sizeErr != nil {
		// AnalyzeWasmSize validates the magic bytes and section structure.
		// Surface the error explicitly rather than silently continuing.
		return errors.WrapValidationError(fmt.Sprintf(
			"--wasm: %q is not a valid WASM binary: %v\n"+
				"  Ensure you are passing a compiled .wasm file produced by 'cargo build --release'.",
			wasmPath, sizeErr,
		))
	} else if msg := abi.FormatWasmSizeWarnings(sizeAnalysis); msg != "" {
		fmt.Fprintf(os.Stderr, "%s\n", msg)
	}

	// Check for LTO in the project that produced the WASM
	checkLTOWarning(wasmPath)

	// Create simulator runner
	runner, err := simulator.NewRunner("", tracingEnabled)
	if err != nil {
		return errors.WrapSimulatorNotFound(err.Error())
	}
	defer runner.Close()

	ctx := context.Background()
	if hotReloadFlag {
		return runLocalWasmReplaySession(ctx, runner, os.Stdin, os.Stdout)
	}
	return runLocalWasmReplayOnce(ctx, runner, false)
}

func newLocalWasmSimulationRequest(forceNoCache bool) *simulator.SimulationRequest {
	req := &simulator.SimulationRequest{
		EnvelopeXdr:       "",  // Empty for local replay
		ResultMetaXdr:     "",  // Empty for local replay
		LedgerEntries:     nil, // Mock state will be generated
		WasmPath:          &wasmPath,
		NoCache:           noCacheFlag || forceNoCache,
		MockArgs:          &args,
		ContractWasm:      &wasmBase64, // Pass the WASM binary for source mapping
		EnableSnapshots:   snapshotsFlag,
		SkipSourceMapping: skipSourceMappingFlag,
	}
	if contractSourceFlag != "" {
		req.ContractSourcePath = &contractSourceFlag
	}
	applyDebugSimulationOptions(req)
	applySimulationFeeMocks(req)
	return req
}

func loadMockLedgerOverrides() (map[string]string, error) {
	var overrides map[string]string
	if mockLedgerManifest != "" {
		manifestOverrides, err := simulator.LoadLedgerOverrideManifest(mockLedgerManifest)
		if err != nil {
			return nil, errors.WrapValidationError(fmt.Sprintf("failed to load mock ledger manifest: %v", err))
		}
		overrides = simulator.MergeLedgerOverrides(overrides, manifestOverrides)
	}

	if len(mockLedgerEntryFlags) > 0 {
		flagOverrides, err := simulator.ParseLedgerOverrideFlags(mockLedgerEntryFlags)
		if err != nil {
			return nil, errors.WrapValidationError(fmt.Sprintf("failed to parse mock ledger entries: %v", err))
		}
		overrides = simulator.MergeLedgerOverrides(overrides, flagOverrides)
	}

	return overrides, nil
}

func runLocalWasmReplayOnce(ctx context.Context, runner simulator.RunnerInterface, forceNoCache bool) error {
	req := newLocalWasmSimulationRequest(forceNoCache)

	overrideEntries, err := loadMockLedgerOverrides()
	if err != nil {
		return err
	}
	if len(overrideEntries) > 0 {
		req.LedgerEntries = simulator.MergeLedgerOverrides(req.LedgerEntries, overrideEntries)
		fmt.Printf("Applied %d mock ledger override entries for local replay\n", len(overrideEntries))
	}

	// Run simulation
	fmt.Printf("%s Executing contract locally...\n", visualizer.Symbol("play"))
	resp, err := runner.Run(ctx, req)
	if err != nil {
		fmt.Printf("%s Technical failure: %v\n", visualizer.Error(), err)
		return err
	}

	// Display results
	fmt.Println()
	if resp.Status == "error" {
		fmt.Printf("%s Execution failed\n", visualizer.Error())
		if resp.Error != "" {
			fmt.Printf("Error: %s\n", resp.Error)
		}

		if resp.StackTrace != nil {
			printWasmBacktrace(resp.StackTrace)
		}

		if resp.SourceLocation != nil {
			fmt.Printf("%s Top-level Location: %s:%d\n", visualizer.Symbol("location"), resp.SourceLocation.File, resp.SourceLocation.Line)
			displaySourceLocation(resp.SourceLocation)
		}

		// Fallback to WAT disassembly if source mapping is unavailable but we have an offset
		if resp.SourceLocation == nil && resp.WasmOffset != nil {
			fmt.Println()
			wasmBytes, err := os.ReadFile(wasmPath)
			if err == nil {
				fallbackMsg := wat.FormatFallback(wasmBytes, *resp.WasmOffset, 5)
				fmt.Println(fallbackMsg)
			}
		}
	} else {
		fmt.Printf("%s Execution completed successfully\n", visualizer.Success())
	}
	fmt.Println()

	if len(resp.Logs) > 0 {
		fmt.Printf("%s Logs:\n", visualizer.Symbol("logs"))
		for _, log := range resp.Logs {
			fmt.Printf("  %s\n", log)
		}
		fmt.Println()
	}

	if len(resp.Events) > 0 {
		fmt.Printf("%s Events:\n", visualizer.Symbol("events"))
		for _, event := range resp.Events {
			if deprecatedFn, ok := findDeprecatedHostFunction(event); ok {
				fmt.Printf("  %s %s %s\n", event, visualizer.Warning(), visualizer.Colorize("deprecated host fn: "+deprecatedFn, "yellow"))
				continue
			}
			fmt.Printf("  %s\n", event)
		}
		fmt.Println()
	}

	if verbose {
		fmt.Printf("%s Full Response:\n", visualizer.Symbol("magnify"))
		jsonBytes, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(jsonBytes))
	}

	return nil
}

func runLocalWasmReplaySession(ctx context.Context, runner simulator.RunnerInterface, in io.Reader, out io.Writer) error {
	fmt.Println("[watcher] Hot reload enabled")
	if err := runLocalWasmReplayOnce(ctx, runner, false); err != nil {
		return err
	}

	initialFP, err := watch.ComputeWasmFingerprint(wasmPath, 5, 50*time.Millisecond)
	if err != nil {
		return errors.WrapValidationError(fmt.Sprintf("failed to fingerprint initial wasm: %v", err))
	}
	lastAppliedHash := initialFP.Hash

	cfg := watch.DefaultWasmReloaderConfig(wasmPath, hotReloadInterval)
	reloadEvents, reloadErrors, err := watch.StartWasmReloader(ctx, cfg)
	if err != nil {
		return errors.WrapValidationError(fmt.Sprintf("failed to start wasm watcher: %v", err))
	}
	fmt.Println("[watcher] Watching for WASM changes")

	reader := bufio.NewReader(in)
	var pending *watch.ReloadEvent

	for {
		if pending != nil {
			// In non-interactive mode (pipe, CI, --non-interactive) we cannot
			// block for user input. Auto-skip the reload and keep watching.
			if termctx.GlobalNonInteractive() {
				fmt.Fprintln(out, "[watcher] Non-interactive mode: reload skipped (re-run the command manually)")
				pending = drainLatestReloadEvent(reloadEvents, lastAppliedHash)
				continue
			}

			choice, promptErr := promptHotReloadChoice(reader, out)
			if promptErr != nil {
				return promptErr
			}

			switch choice {
			case 'r':
				fmt.Println("[watcher] Re-running simulation with updated WASM")
				if err := runLocalWasmReplayOnce(ctx, runner, true); err != nil {
					fmt.Printf("[watcher] Re-run failed: %v\n", err)
				} else {
					lastAppliedHash = pending.Hash
				}
			case 's':
				fmt.Println("[watcher] Reload skipped")
			case 'q':
				fmt.Println("[watcher] Exiting hot reload session")
				return nil
			}
			pending = drainLatestReloadEvent(reloadEvents, lastAppliedHash)
			continue
		}

		select {
		case <-ctx.Done():
			return nil
		case watchErr, ok := <-reloadErrors:
			if !ok {
				reloadErrors = nil
				continue
			}
			fmt.Printf("[watcher] Warning: %v\n", watchErr)
		case event, ok := <-reloadEvents:
			if !ok {
				return nil
			}
			if event.Hash == lastAppliedHash {
				continue
			}
			fmt.Println("[watcher] WASM updated (hash changed)")
			fmt.Println("[watcher] Reload available")
			pending = &event
		}
	}
}

func promptHotReloadChoice(reader *bufio.Reader, out io.Writer) (byte, error) {
	for {
		fmt.Fprint(out, "Re-run simulation? (r = reload, s = skip, q = quit): ")
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF && strings.TrimSpace(line) != "" {
				// Accept final line without trailing newline.
			} else {
				return 0, err
			}
		}

		choice := strings.ToLower(strings.TrimSpace(line))
		switch choice {
		case "r", "s", "q":
			return choice[0], nil
		default:
			fmt.Fprintln(out, "Invalid choice. Please enter r, s, or q.")
		}
	}
}

func drainLatestReloadEvent(events <-chan watch.ReloadEvent, lastAppliedHash string) *watch.ReloadEvent {
	var latest *watch.ReloadEvent
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return latest
			}
			if event.Hash == lastAppliedHash {
				continue
			}
			ev := event
			latest = &ev
		default:
			return latest
		}
	}
}

func extractLedgerKeys(metaXdr string) ([]string, error) {
	if strings.TrimSpace(metaXdr) == "" {
		return nil, nil
	}

	data, err := base64.StdEncoding.DecodeString(metaXdr)
	if err != nil {
		return nil, err
	}

	var meta xdr.TransactionResultMeta
	if err := xdr.SafeUnmarshal(data, &meta); err != nil {
		return nil, err
	}

	keysMap := make(map[string]struct{})
	addKey := func(k xdr.LedgerKey) {
		b, _ := k.MarshalBinary()
		keysMap[base64.StdEncoding.EncodeToString(b)] = struct{}{}
	}

	collectChanges := func(changes xdr.LedgerEntryChanges) {
		for _, c := range changes {
			switch c.Type {
			case xdr.LedgerEntryChangeTypeLedgerEntryCreated:
				k, err := c.Created.LedgerKey()
				if err == nil {
					addKey(k)
				}
			case xdr.LedgerEntryChangeTypeLedgerEntryUpdated:
				k, err := c.Updated.LedgerKey()
				if err == nil {
					addKey(k)
				}
			case xdr.LedgerEntryChangeTypeLedgerEntryRemoved:
				if c.Removed != nil {
					addKey(*c.Removed)
				}
			case xdr.LedgerEntryChangeTypeLedgerEntryState:
				k, err := c.State.LedgerKey()
				if err == nil {
					addKey(k)
				}
			}
		}
	}

	// 1. Fee processing changes
	collectChanges(meta.FeeProcessing)

	// 2. Transaction apply processing changes
	switch meta.TxApplyProcessing.V {
	case 0:
		if meta.TxApplyProcessing.Operations != nil {
			for _, op := range *meta.TxApplyProcessing.Operations {
				collectChanges(op.Changes)
			}
		}
	case 1:
		if v1 := meta.TxApplyProcessing.V1; v1 != nil {
			collectChanges(v1.TxChanges)
			for _, op := range v1.Operations {
				collectChanges(op.Changes)
			}
		}
	case 2:
		if v2 := meta.TxApplyProcessing.V2; v2 != nil {
			collectChanges(v2.TxChangesBefore)
			collectChanges(v2.TxChangesAfter)
			for _, op := range v2.Operations {
				collectChanges(op.Changes)
			}
		}
	case 3:
		if v3 := meta.TxApplyProcessing.V3; v3 != nil {
			collectChanges(v3.TxChangesBefore)
			collectChanges(v3.TxChangesAfter)
			for _, op := range v3.Operations {
				collectChanges(op.Changes)
			}
		}
	}

	res := make([]string, 0, len(keysMap))
	for k := range keysMap {
		res = append(res, k)
	}
	return res, nil
}

// collectContractIDsFromDiagnosticEvents returns unique contract IDs from diagnostic events (trace).
func collectContractIDsFromDiagnosticEvents(events []simulator.DiagnosticEvent) []string {
	seen := make(map[string]struct{})
	var ids []string
	for _, e := range events {
		if e.ContractID != nil && *e.ContractID != "" {
			if _, ok := seen[*e.ContractID]; !ok {
				seen[*e.ContractID] = struct{}{}
				ids = append(ids, *e.ContractID)
			}
		}
	}
	return ids
}

func printSimulationResult(network string, res *simulator.SimulationResponse) {
	if clioutput.WantsJSON(debugJSONFlag, debugFormatFlag) {
		payload := map[string]interface{}{
			"network": network,
			"result":  res,
		}
		if traceVerbosityFlag != "" {
			if v, err := trace.ParseVerbosity(traceVerbosityFlag); err == nil {
				payload["verbosity"] = traceVerbosityFlag
				if res != nil {
					filtered := *res
					filtered.DiagnosticEvents = trace.FilterDiagnosticEvents(res.DiagnosticEvents, v)
					payload["result"] = &filtered
				}
			}
		}
		_ = clioutput.WriteStdout("debug", payload)
		return
	}

	// Section header
	sep := strings.Repeat("─", 60)
	fmt.Printf("\n%s\n", visualizer.Colorize("  "+sep, "dim"))
	fmt.Printf("  %s  %s\n",
		visualizer.Colorize("Result for", "bold"),
		visualizer.Colorize(network, "cyan"),
	)
	fmt.Printf("  %s\n\n", visualizer.Colorize(sep, "dim"))

	// Status line — green for success, red for failure
	statusColor := "green"
	statusIcon := visualizer.Success()
	if res.Status != "success" {
		statusColor = "red"
		statusIcon = visualizer.Error()
	}
	fmt.Printf("  %s  Status: %s\n", statusIcon, visualizer.Colorize(res.Status, statusColor))

	// Determine and display snapshot status
	hasOOM := res.BudgetUsage != nil && res.BudgetUsage.MemoryUsagePercent >= 99.0
	snapshotStatus := simtypes.DetermineSnapshotStatus(
		len(res.Events)+len(res.DiagnosticEvents),
		len(res.DiagnosticEvents),
		hasOOM,
	)
	snapshotColor := "green"
	if !snapshotStatus.IsHealthy() {
		snapshotColor = "yellow"
	}
	fmt.Printf("  %s  Snapshot: %s\n", visualizer.Info(), visualizer.Colorize(string(snapshotStatus), snapshotColor))
	if !snapshotStatus.IsHealthy() {
		fmt.Printf("       %s\n", visualizer.Colorize(snapshotStatus.StatusMessage(), "yellow"))
	}

	// Error message — always red
	if res.Error != "" {
		fmt.Printf("\n  %s  %s\n", visualizer.Error(), visualizer.Colorize(res.Error, "red"))
	}

	if res.Status != "success" {
		printFailureDiagnostic(res)
	}

	// Stack trace with resolved source locations
	if res.StackTrace != nil && len(res.StackTrace.Frames) > 0 {
		printWasmBacktrace(res.StackTrace)
	}

	// Top-level source location
	if res.SourceLocation != nil {
		fmt.Printf("  %s Location: %s:%d\n",
			visualizer.Symbol("location"),
			visualizer.Colorize(res.SourceLocation.File, "cyan"),
			res.SourceLocation.Line,
		)
		displaySourceLocation(res.SourceLocation)
	}

	// Budget / resource usage
	if res.BudgetUsage != nil {
		fmt.Printf("\n  %s  Resource Usage:\n", visualizer.Colorize("──", "bold"))

		cpuColor, cpuSuffix := budgetIndicator(res.BudgetUsage.CPUUsagePercent)
		fmt.Printf("    CPU Instructions: %s / %d  %s%s\n",
			visualizer.Colorize(fmt.Sprintf("%d", res.BudgetUsage.CPUInstructions), cpuColor),
			res.BudgetUsage.CPULimit,
			visualizer.Colorize(fmt.Sprintf("(%.2f%%)", res.BudgetUsage.CPUUsagePercent), cpuColor),
			cpuSuffix,
		)

		memColor, memSuffix := budgetIndicator(res.BudgetUsage.MemoryUsagePercent)
		fmt.Printf("    Memory Bytes:     %s / %d  %s%s\n",
			visualizer.Colorize(fmt.Sprintf("%d", res.BudgetUsage.MemoryBytes), memColor),
			res.BudgetUsage.MemoryLimit,
			visualizer.Colorize(fmt.Sprintf("(%.2f%%)", res.BudgetUsage.MemoryUsagePercent), memColor),
			memSuffix,
		)

		fmt.Printf("    Operations:       %d\n", res.BudgetUsage.OperationsCount)

		// Add fee estimate
		gasEst, err := simulator.ExtractGasEstimation(&simulator.SimulationResponse{
			BudgetUsage: res.BudgetUsage,
		})
		if err == nil {
			fmt.Printf("    Fee Estimate: %d–%d stroops\n", gasEst.EstimatedFeeLowerBound, gasEst.EstimatedFeeUpperBound)
		}
	}

	// Diagnostic events
	if len(res.DiagnosticEvents) > 0 {
		fmt.Printf("\n  %s  Diagnostic Events: %s\n",
			visualizer.Colorize("──", "bold"),
			visualizer.Colorize(fmt.Sprintf("%d", len(res.DiagnosticEvents)), "cyan"),
		)
		for i, event := range res.DiagnosticEvents {
			if i >= 10 {
				fmt.Printf("    %s\n",
					visualizer.Colorize(fmt.Sprintf("… and %d more events", len(res.DiagnosticEvents)-10), "dim"),
				)
				break
			}
			eventTypeColor := "cyan"
			if strings.Contains(strings.ToLower(event.EventType), "error") ||
				strings.Contains(strings.ToLower(event.EventType), "fail") {
				eventTypeColor = "red"
			}
			fmt.Printf("    [%d] %s", i+1, visualizer.Colorize(event.EventType, eventTypeColor))
			if event.ContractID != nil {
				fmt.Printf("  %s", visualizer.Colorize(*event.ContractID, "dim"))
			}
			// Add resource info: CPU, Mem, and Fee for this event
			if event.CPU != nil || event.Memory != nil {
				var cpuStr, memStr, feeStr string
				if event.CPU != nil {
					cpuStr = fmt.Sprintf("CPU: %d", *event.CPU)
				}
				if event.Memory != nil {
					memStr = fmt.Sprintf("Mem: %d", *event.Memory)
				}
				if event.CPU != nil && event.Memory != nil {
					fee := (*event.CPU / 10000) + (*event.Memory / (64 * 1024))
					feeStr = fmt.Sprintf("Fee: %d stroops", fee)
				}
				parts := []string{}
				if cpuStr != "" {
					parts = append(parts, cpuStr)
				}
				if memStr != "" {
					parts = append(parts, memStr)
				}
				if feeStr != "" {
					parts = append(parts, feeStr)
				}
				if len(parts) > 0 {
					fmt.Printf("  %s", strings.Join(parts, " "))
				}
			}
			if deprecatedFn, ok := deprecatedHostFunctionInDiagnosticEvent(event); ok {
				fmt.Printf("  %s %s",
					visualizer.Warning(),
					visualizer.Colorize("deprecated host fn: "+deprecatedFn, "yellow"),
				)
			}
			fmt.Println()
			if len(event.Topics) > 0 {
				fmt.Printf("         Topics: %s\n", visualizer.Colorize(fmt.Sprintf("%v", event.Topics), "dim"))
			}
			if event.Data != "" && len(event.Data) < 100 {
				fmt.Printf("         Data:   %s\n", visualizer.Colorize(event.Data, "dim"))
			}
		}
	} else {
		fmt.Printf("\n  Events: %s\n",
			visualizer.Colorize(fmt.Sprintf("%d", len(res.Events)), "cyan"),
		)
	}

	// Logs
	if len(res.Logs) > 0 {
		fmt.Printf("\n  %s  Logs: %s\n",
			visualizer.Colorize("──", "bold"),
			visualizer.Colorize(fmt.Sprintf("%d", len(res.Logs)), "cyan"),
		)
		for i, logLine := range res.Logs {
			if i >= 5 {
				fmt.Printf("    %s\n",
					visualizer.Colorize(fmt.Sprintf("… and %d more logs", len(res.Logs)-5), "dim"),
				)
				break
			}
			fmt.Printf("    %s %s\n", visualizer.Colorize("·", "dim"), logLine)
		}
	}

	fmt.Printf("\n  %s\n",
		visualizer.Colorize(
			fmt.Sprintf("Events: %d  Logs: %d", len(res.Events), len(res.Logs)),
			"dim",
		),
	)
}

// budgetIndicator returns a color name and warning suffix for a budget usage percentage.
func printFailureDiagnostic(res *simulator.SimulationResponse) {
	if res == nil || res.Status == "success" {
		return
	}

	diagnostic := simulator.ClassifyFailure(res)
	if diagnostic == nil {
		return
	}

	fmt.Printf("\n  %s  Diagnostic: %s\n",
		visualizer.Colorize("──", "bold"),
		visualizer.Colorize(string(diagnostic.Category), "yellow"),
	)
	fmt.Printf("    %s\n", diagnostic.Summary)

	if diagnostic.ErrorCode != "" {
		fmt.Printf("    Error Code: %s\n", diagnostic.ErrorCode)
	}

	if details := diagnostic.BudgetDetails; details != nil {
		fmt.Printf("    Budget: CPU %d/%d (%.1f%%), Memory %d/%d (%.1f%%)\n",
			details.CPUInstructions, details.CPULimit, details.CPUUsagePercent,
			details.MemoryBytes, details.MemoryLimit, details.MemoryUsagePercent,
		)
		if details.CPUExhausted {
			fmt.Printf("    %s CPU budget exhausted\n", visualizer.Warning())
		}
		if details.MemoryExhausted {
			fmt.Printf("    %s Memory budget exhausted\n", visualizer.Warning())
		}
		if details.HotSpotHint != "" {
			fmt.Printf("    Hot spot: %s\n", details.HotSpotHint)
		}
	}

	if res.SourceLocation != nil {
		fmt.Printf("    Likely source location: %s:%d\n", res.SourceLocation.File, res.SourceLocation.Line)
	}

	if hint := failureRemediationHint(res); hint != "" {
		fmt.Printf("    Suggested fix: %s\n", hint)
	}
}

func failureRemediationHint(res *simulator.SimulationResponse) string {
	if res == nil {
		return ""
	}

	if res.BudgetUsage != nil {
		if res.BudgetUsage.CPUUsagePercent >= 100 {
			return "Optimize contract logic and reduce CPU-heavy loops or limit host function calls to lower instruction usage."
		}
		if res.BudgetUsage.MemoryUsagePercent >= 100 {
			return "Reduce temporary memory allocation and heap usage in contract code to stay under the Soroban memory limit."
		}
	}

	lowerError := strings.ToLower(res.Error)
	if strings.Contains(lowerError, "auth") || strings.Contains(lowerError, "require_auth") {
		return "Ensure the transaction includes all required signatures and authorization entries."
	}
	if strings.Contains(lowerError, "invalid") || strings.Contains(lowerError, "malformed") {
		return "Verify the transaction envelope and argument types for correctness."
	}
	return ""
}

func loadTransactionEnvelopeInput(xdrPath, jsonPath, resultMetaPath string) (string, string, string, error) {
	if xdrPath != "" && jsonPath != "" {
		return "", "", "", errors.WrapValidationError("only one of --xdr-file or --json-file may be specified")
	}

	var envelopeXdr string
	var resultMetaXdr string
	var network string

	if xdrPath != "" {
		data, err := os.ReadFile(xdrPath)
		if err != nil {
			return "", "", "", errors.WrapValidationError(fmt.Sprintf("failed to read XDR file %s: %v", xdrPath, err))
		}
		envelopeXdr = strings.TrimSpace(string(data))
		if _, err := base64.StdEncoding.DecodeString(envelopeXdr); err != nil {
			return "", "", "", errors.WrapValidationError(fmt.Sprintf("invalid base64 XDR in %s: %v", xdrPath, err))
		}
	}

	if jsonPath != "" {
		data, err := os.ReadFile(jsonPath)
		if err != nil {
			return "", "", "", errors.WrapValidationError(fmt.Sprintf("failed to read JSON envelope file %s: %v", jsonPath, err))
		}
		var envelope struct {
			Network       string `json:"network"`
			EnvelopeXDR   string `json:"envelope_xdr"`
			ResultMetaXDR string `json:"result_meta_xdr"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			return "", "", "", errors.WrapValidationError(fmt.Sprintf("failed to parse JSON envelope file %s: %v", jsonPath, err))
		}
		envelopeXdr = strings.TrimSpace(envelope.EnvelopeXDR)
		network = strings.TrimSpace(envelope.Network)
		resultMetaXdr = strings.TrimSpace(envelope.ResultMetaXDR)
		if envelopeXdr == "" {
			return "", "", "", errors.WrapValidationError(fmt.Sprintf("JSON envelope file %s must include envelope_xdr", jsonPath))
		}
	}

	if resultMetaPath != "" {
		data, err := os.ReadFile(resultMetaPath)
		if err != nil {
			return "", "", "", errors.WrapValidationError(fmt.Sprintf("failed to read result meta file %s: %v", resultMetaPath, err))
		}
		trimmed := strings.TrimSpace(string(data))
		if strings.HasPrefix(trimmed, "{") {
			var meta struct {
				ResultMetaXdr string `json:"result_meta_xdr"`
			}
			if err := json.Unmarshal(data, &meta); err != nil {
				return "", "", "", errors.WrapValidationError(fmt.Sprintf("failed to parse JSON result meta file %s: %v", resultMetaPath, err))
			}
			resultMetaXdr = strings.TrimSpace(meta.ResultMetaXdr)
		} else {
			resultMetaXdr = trimmed
		}
		if resultMetaXdr != "" {
			if _, err := base64.StdEncoding.DecodeString(resultMetaXdr); err != nil {
				return "", "", "", errors.WrapValidationError(fmt.Sprintf("invalid base64 result metadata in %s: %v", resultMetaPath, err))
			}
		}
	}

	if envelopeXdr == "" {
		return "", "", "", errors.WrapValidationError("missing local envelope input; provide --xdr-file or --json-file")
	}

	return envelopeXdr, resultMetaXdr, network, nil
}

func parseTransactionEnvelopeOperations(envelopeXdr string) ([]xdr.Operation, error) {
	if strings.TrimSpace(envelopeXdr) == "" {
		return nil, nil
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(envelopeXdr))
	if err != nil {
		return nil, err
	}

	var envelope xdr.TransactionEnvelope
	if err := xdr.SafeUnmarshal(decoded, &envelope); err != nil {
		return nil, err
	}

	switch envelope.Type {
	case xdr.EnvelopeTypeEnvelopeTypeTx:
		return envelope.V1.Tx.Operations, nil
	case xdr.EnvelopeTypeEnvelopeTypeTxV0:
		return envelope.V0.Tx.Operations, nil
	case xdr.EnvelopeTypeEnvelopeTypeTxFeeBump:
		return envelope.FeeBump.Tx.InnerTx.V1.Tx.Operations, nil
	default:
		return nil, fmt.Errorf("unsupported transaction envelope type: %v", envelope.Type)
	}
}

func formatOperationSummary(op xdr.Operation) string {
	typeName := op.Body.Type.String()
	summary := typeName

	switch op.Body.Type {
	case xdr.OperationTypePayment:
		if op.Body.PaymentOp != nil {
			summary = fmt.Sprintf("%s -> %s", typeName, op.Body.PaymentOp.Destination.Address())
		}
	case xdr.OperationTypeManageData:
		if op.Body.ManageDataOp != nil {
			summary = fmt.Sprintf("%s %s", typeName, op.Body.ManageDataOp.DataName)
		}
	case xdr.OperationTypeInvokeHostFunction:
		summary = fmt.Sprintf("%s", typeName)
	}

	return summary
}

func validateSecureArtifactPath(path string) (string, error) {
	if path == "" {
		return "", nil
	}

	if _, err := os.Stat(path); err != nil {
		return "", errors.WrapValidationError(fmt.Sprintf("secure workspace path not accessible: %s: %v", path, err))
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", errors.WrapValidationError(fmt.Sprintf("failed to resolve secure workspace path %q: %v", path, err))
	}
	resolved, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", errors.WrapValidationError(fmt.Sprintf("failed to resolve secure workspace symlink %q: %v", path, err))
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", errors.WrapValidationError(fmt.Sprintf("failed to determine current directory for secure workspace validation: %v", err))
	}
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return "", errors.WrapValidationError(fmt.Sprintf("failed to resolve current directory: %v", err))
	}

	rel, err := filepath.Rel(cwd, resolved)
	if err != nil {
		return "", errors.WrapValidationError(fmt.Sprintf("failed to validate secure workspace path %q: %v", path, err))
	}
	if rel == ".." || strings.HasPrefix(rel, fmt.Sprintf("..%c", filepath.Separator)) {
		return "", errors.WrapValidationError(fmt.Sprintf("secure workspace disallows path outside current workspace: %s", path))
	}

	return resolved, nil
}

func budgetIndicator(pct float64) (color, suffix string) {
	switch {
	case pct >= 95.0:
		return "red", "  " + visualizer.Error() + "  " + visualizer.Colorize("CRITICAL", "red")
	case pct >= 80.0:
		return "yellow", "  " + visualizer.Warning() + "  " + visualizer.Colorize("WARNING", "yellow")
	default:
		return "green", ""
	}
}

func diffResults(res1, res2 *simulator.SimulationResponse, net1, net2 string) {
	sep := strings.Repeat("═", 64)
	fmt.Println()
	fmt.Println(visualizer.Colorize("╔"+sep+"╗", "cyan"))
	title := fmt.Sprintf("  COMPARISON: %s  vs  %s  ", net1, net2)
	pad := len(sep) - len(title)
	if pad < 0 {
		pad = 0
	}
	fmt.Printf(visualizer.Colorize("║", "cyan")+"%s"+strings.Repeat(" ", pad)+visualizer.Colorize("║", "cyan")+"\n", title)
	fmt.Println(visualizer.Colorize("╚"+sep+"╝", "cyan"))
	fmt.Println()

	// ── Status ────────────────────────────────────────────────────────────────
	fmt.Println(visualizer.Colorize("── Execution Status "+strings.Repeat("─", 44), "bold"))
	if res1.Status != res2.Status {
		fmt.Printf("  %s  Status mismatch:\n", visualizer.Error())
		fmt.Printf("    %-12s %s\n", visualizer.Colorize(net1+":", "dim"), visualizer.Colorize(res1.Status, "red"))
		fmt.Printf("    %-12s %s\n", visualizer.Colorize(net2+":", "dim"), visualizer.Colorize(res2.Status, "red"))
	} else {
		statusColor := "green"
		if res1.Status != "success" {
			statusColor = "red"
		}
		fmt.Printf("  %s  Status match: %s\n",
			visualizer.Success(),
			visualizer.Colorize(res1.Status, statusColor),
		)
	}

	// ── Diagnostic / Raw Events ───────────────────────────────────────────────
	fmt.Println()
	fmt.Println(visualizer.Colorize("── Event Counts "+strings.Repeat("─", 47), "bold"))
	if len(res1.DiagnosticEvents) > 0 || len(res2.DiagnosticEvents) > 0 {
		if len(res1.DiagnosticEvents) != len(res2.DiagnosticEvents) {
			fmt.Printf("  %s  Diagnostic events: %s (%s)  vs  %s (%s)\n",
				visualizer.Warning(),
				visualizer.Colorize(fmt.Sprintf("%d", len(res1.DiagnosticEvents)), "yellow"), net1,
				visualizer.Colorize(fmt.Sprintf("%d", len(res2.DiagnosticEvents)), "yellow"), net2,
			)
		} else {
			fmt.Printf("  %s  Diagnostic events: %s (both networks)\n",
				visualizer.Success(),
				visualizer.Colorize(fmt.Sprintf("%d", len(res1.DiagnosticEvents)), "green"),
			)
		}
	} else if len(res1.Events) != len(res2.Events) {
		fmt.Printf("  %s  Events: %s (%s)  vs  %s (%s)\n",
			visualizer.Warning(),
			visualizer.Colorize(fmt.Sprintf("%d", len(res1.Events)), "yellow"), net1,
			visualizer.Colorize(fmt.Sprintf("%d", len(res2.Events)), "yellow"), net2,
		)
	} else {
		fmt.Printf("  %s  Events: %s (both networks)\n",
			visualizer.Success(),
			visualizer.Colorize(fmt.Sprintf("%d", len(res1.Events)), "green"),
		)
	}

	// ── Budget ────────────────────────────────────────────────────────────────
	if res1.BudgetUsage != nil && res2.BudgetUsage != nil {
		fmt.Println()
		fmt.Println(visualizer.Colorize("── Resource Usage "+strings.Repeat("─", 45), "bold"))

		printBudgetComparison("CPU Instructions",
			int64(res1.BudgetUsage.CPUInstructions), int64(res2.BudgetUsage.CPUInstructions),
			net1, net2)
		printBudgetComparison("Memory Bytes",
			int64(res1.BudgetUsage.MemoryBytes), int64(res2.BudgetUsage.MemoryBytes),
			net1, net2)
		printBudgetComparison("Operations",
			int64(res1.BudgetUsage.OperationsCount), int64(res2.BudgetUsage.OperationsCount),
			net1, net2)
	}

	// ── Event-by-event diff ───────────────────────────────────────────────────
	maxEvents := len(res1.Events)
	if len(res2.Events) > maxEvents {
		maxEvents = len(res2.Events)
	}
	if maxEvents > 0 {
		fmt.Println()
		fmt.Println(visualizer.Colorize("── Event Diff "+strings.Repeat("─", 49), "bold"))
		hasMismatch := false
		for i := 0; i < maxEvents; i++ {
			inRes1 := i < len(res1.Events)
			inRes2 := i < len(res2.Events)

			var ev1Raw, ev2Raw string         // raw event strings for comparison
			var ev1Display, ev2Display string // display strings (may be colored)

			if inRes1 {
				ev1Raw = res1.Events[i]
				ev1Display = visualizer.Colorize(ev1Raw, "red")
			} else {
				ev1Raw = ""
				ev1Display = visualizer.Colorize("<missing>", "red")
			}
			if inRes2 {
				ev2Raw = res2.Events[i]
				ev2Display = visualizer.Colorize(ev2Raw, "green")
			} else {
				ev2Raw = ""
				ev2Display = visualizer.Colorize("<missing>", "red")
			}

			if !inRes1 || !inRes2 || ev1Raw != ev2Raw {
				hasMismatch = true
				fmt.Printf("  %s  [%d] %s\n",
					visualizer.Error(),
					i,
					visualizer.Colorize("MISMATCH", "red"),
				)
				fmt.Printf("    %-12s %s\n", visualizer.Colorize(net1+":", "dim"), ev1Display)
				fmt.Printf("    %-12s %s\n", visualizer.Colorize(net2+":", "dim"), ev2Display)
			}
		}
		if !hasMismatch {
			fmt.Printf("  %s  All %s events match\n",
				visualizer.Success(),
				visualizer.Colorize(fmt.Sprintf("%d", maxEvents), "green"),
			)
		}
	}
	fmt.Println()
}

// printBudgetComparison prints a single budget metric comparison row with color.
func printBudgetComparison(label string, v1, v2 int64, net1, net2 string) {
	if v1 != v2 {
		delta := v2 - v1
		sign := "+"
		deltaColor := "yellow"
		if delta < 0 {
			sign = ""
			deltaColor = "green"
		}
		fmt.Printf("  %s  %-20s %s (%s)  vs  %s (%s)  delta: %s\n",
			visualizer.Warning(),
			label+":",
			visualizer.Colorize(fmt.Sprintf("%d", v1), "dim"), net1,
			visualizer.Colorize(fmt.Sprintf("%d", v2), "dim"), net2,
			visualizer.Colorize(fmt.Sprintf("%s%d", sign, delta), deltaColor),
		)
	} else {
		fmt.Printf("  %s  %-20s %s\n",
			visualizer.Success(),
			label+":",
			visualizer.Colorize(fmt.Sprintf("%d (match)", v1), "green"),
		)
	}
}

// collectVisibleSections returns the names of output sections that contained
// data during the last simulation run.
func collectVisibleSections(resp *simulator.SimulationResponse, findings []security.Finding, hasTokenFlows bool) []string {
	var sections []string
	if resp.BudgetUsage != nil {
		sections = append(sections, "budget")
	}
	if len(resp.DiagnosticEvents) > 0 {
		sections = append(sections, "events")
	}
	if len(resp.Logs) > 0 {
		sections = append(sections, "logs")
	}
	if len(findings) > 0 {
		sections = append(sections, "security")
	}
	if hasTokenFlows {
		sections = append(sections, "tokenflow")
	}
	return sections
}

// validateSourceDiscoveryFlags validates source-discovery-related flags for
// code paths (--wasm, --demo, --xdr-file, --json-file) that return early in
// PreRunE before the inline --contract-source / --source-alias checks execute.
// For the normal transaction-hash path those inline checks still apply; this
// function acts as a safety net for the early-return paths.
func validateSourceDiscoveryFlags() error {
	// Only validate when the inline block was skipped (early-return paths).
	// For the transaction-hash path the inline checks already cover both flags.
	// We run an unconditional check here so wasm/demo/local-file modes also get
	// early feedback rather than a silent no-op.

	// --contract-source must be an existing directory.
	if contractSourceFlag != "" {
		trimmed := strings.TrimSpace(contractSourceFlag)
		if trimmed == "" {
			return errors.WrapValidationError(fmt.Sprintf(
				"--contract-source: value must not be empty or whitespace\n" +
					"  Provide the path to your contract's source directory (the one containing src/).",
			))
		}
		if strings.ContainsRune(trimmed, 0) {
			return errors.WrapValidationError(
				"--contract-source: path contains null bytes and cannot be used\n" +
					"  Fix: remove any null bytes from the path.",
			)
		}
		info, statErr := os.Stat(trimmed)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				return errors.WrapValidationError(fmt.Sprintf(
					"--contract-source: directory not found: %q\n"+
						"  Provide the path to your contract's source directory (the one containing src/).\n"+
						"  Source mapping will be unavailable without a valid path.",
					trimmed,
				))
			}
			return errors.WrapValidationError(fmt.Sprintf(
				"--contract-source: cannot access %q: %v", trimmed, statErr,
			))
		}
		if !info.IsDir() {
			return errors.WrapValidationError(fmt.Sprintf(
				"--contract-source: %q is a file, not a directory\n"+
					"  Provide the path to your contract's source directory, not a file.",
				trimmed,
			))
		}
	}

	// --source-alias must be readable valid JSON and point to existing directories.
	if sourceAliasFlag != "" {
		if strings.ContainsRune(sourceAliasFlag, 0) {
			return errors.WrapValidationError(
				"--source-alias: path contains null bytes and cannot be used\n" +
					"  Fix: remove any null bytes from the path.",
			)
		}
		aliasBytes, readErr := os.ReadFile(sourceAliasFlag)
		if readErr != nil {
			if os.IsNotExist(readErr) {
				return errors.WrapValidationError(fmt.Sprintf(
					"--source-alias: file not found: %q\n"+
						"  Provide a JSON file mapping embedded source paths to local filesystem paths.\n"+
						"  Example: {\"my_crate\": \"/path/to/my_crate/src\"}",
					sourceAliasFlag,
				))
			}
			return errors.WrapValidationError(fmt.Sprintf(
				"--source-alias: cannot read %q: %v", sourceAliasFlag, readErr,
			))
		}
		var aliasMap map[string]string
		if jsonErr := json.Unmarshal(aliasBytes, &aliasMap); jsonErr != nil {
			return errors.WrapValidationError(fmt.Sprintf(
				"--source-alias: failed to parse %q as JSON: %v\n"+
					"  The file must be a flat JSON object mapping alias strings to local paths.\n"+
					"  Example: {\"my_crate\": \"/path/to/my_crate/src\"}",
				sourceAliasFlag, jsonErr,
			))
		}
		if _, err := sourcemap.LoadAliasConfig(sourceAliasFlag); err != nil {
			return errors.WrapValidationError(fmt.Sprintf("--source-alias: %v", err))
		}
	}

	// --build-manifest must be a readable, valid glassbox-build-manifest.json.
	// Full schema validation (including artifact hash check against --wasm) is
	// deferred to resolver construction; here we only verify the file is
	// readable and parses as valid JSON with the required fields present so the
	// user gets an immediate, actionable error before any simulation work runs.
	if buildManifestFlag != "" {
		trimmed := strings.TrimSpace(buildManifestFlag)
		if trimmed == "" {
			return errors.WrapValidationError(
				"--build-manifest: value must not be empty or whitespace\n" +
					"  Provide the path to a glassbox-build-manifest.json file.",
			)
		}
		if strings.ContainsRune(trimmed, 0) {
			return errors.WrapValidationError(
				"--build-manifest: path contains null bytes and cannot be used\n" +
					"  Fix: remove any null bytes from the path.",
			)
		}
		if _, statErr := os.Stat(trimmed); statErr != nil {
			if os.IsNotExist(statErr) {
				return errors.WrapValidationError(fmt.Sprintf(
					"--build-manifest: file not found: %q\n"+
						"  Generate a manifest with 'glassbox generate-manifest' or provide\n"+
						"  a valid path to an existing glassbox-build-manifest.json file.",
					trimmed,
				))
			}
			return errors.WrapValidationError(fmt.Sprintf(
				"--build-manifest: cannot access %q: %v", trimmed, statErr,
			))
		}
		// Perform a full schema parse so required-field errors are surfaced here.
		// Artifact hash verification against --wasm happens later in the resolver.
		if _, manifestErr := sourcemap.LoadManifest(trimmed); manifestErr != nil {
			return errors.WrapValidationError(fmt.Sprintf("%v", manifestErr))
		}
	}

	return nil
}

func applyDebugSimulationOptions(req *simulator.SimulationRequest) {
	if req == nil {
		return
	}
	req.SkipSourceMapping = skipSourceMappingFlag
	if contractSourceFlag != "" {
		req.ContractSourcePath = &contractSourceFlag
	}
	// Resolve effective build manifest path: CLI flag > GLASSBOX_BUILD_MANIFEST
	// env var (already parsed into cfg.BuildManifestPath) > no manifest.
	effectiveManifest := buildManifestFlag
	if effectiveManifest == "" {
		if cfg, err := config.Load(); err == nil && cfg.BuildManifestPath != "" {
			effectiveManifest = cfg.BuildManifestPath
		}
	}
	if effectiveManifest != "" {
		v := effectiveManifest
		req.BuildManifestPath = &v
	}
}

func applySimulationFeeMocks(req *simulator.SimulationRequest) {
	if req == nil {
		return
	}

	if mockBaseFeeFlag > 0 {
		baseFee := mockBaseFeeFlag
		req.MockBaseFee = &baseFee
	}
	if mockGasPriceFlag > 0 {
		gasPrice := mockGasPriceFlag
		req.MockGasPrice = &gasPrice
	}
}

var deprecatedSorobanHostFunctions = []string{
	"bytes_copy_from_linear_memory",
	"bytes_copy_to_linear_memory",
	"bytes_new_from_linear_memory",
	"map_new_from_linear_memory",
	"map_unpack_to_linear_memory",
	"symbol_new_from_linear_memory",
	"string_new_from_linear_memory",
	"vec_new_from_linear_memory",
	"vec_unpack_to_linear_memory",
}

func deprecatedHostFunctionInDiagnosticEvent(event simulator.DiagnosticEvent) (string, bool) {
	if name, ok := findDeprecatedHostFunction(strings.Join(event.Topics, " ")); ok {
		return name, true
	}
	return findDeprecatedHostFunction(event.Data)
}

func findDeprecatedHostFunction(input string) (string, bool) {
	lower := strings.ToLower(input)
	for _, fn := range deprecatedSorobanHostFunctions {
		if strings.Contains(lower, strings.ToLower(fn)) {
			return fn, true
		}
	}
	return "", false
}

// runFromRegistry replays a saved time-travel session from a snapshot registry
// file without any network connectivity.
func runFromRegistry(ctx context.Context, path string) error {
	reg, err := replay.LoadFromFile(path)
	if err != nil {
		return fmt.Errorf("failed to load snapshot registry: %w", err)
	}

	if len(reg.Entries) == 0 {
		return errors.WrapValidationError("snapshot registry contains no entries")
	}

	for _, w := range reg.VerifyIntegrity() {
		fmt.Fprintf(os.Stderr, "Warning: integrity check failed: %s\n", w.Error())
	}

	fmt.Printf("Offline replay: %s\n", reg.TxHash)
	fmt.Printf("Network: %s | Created: %s | Entries: %d\n",
		reg.Network, reg.CreatedAt.Format(time.RFC3339), len(reg.Entries))

	runner, err := simulator.NewRunnerWithMockTime("", tracingEnabled, mockTimeFlag)
	if err != nil {
		return errors.WrapSimulatorNotFound(err.Error())
	}
	defer runner.Close()

	for _, entry := range reg.Entries {
		if len(reg.Entries) > 1 {
			fmt.Printf("\n--- Simulating at Timestamp: %d ---\n", entry.Timestamp)
		}

		simReq := &simulator.SimulationRequest{
			EnvelopeXdr:   reg.EnvelopeXdr,
			ResultMetaXdr: reg.ResultMetaXdr,
			LedgerEntries: entry.Snapshot.ToMap(),
			Timestamp:     entry.Timestamp,
		}
		applyDebugSimulationOptions(simReq)
		applySimulationFeeMocks(simReq)

		simResp, err := runner.Run(ctx, simReq)
		if err != nil {
			return errors.WrapSimulationFailed(err, "")
		}
		printSimulationResult(reg.Network, simResp)
	}

	return nil
}

func pluralIes(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

func init() {
	debugCmd.Flags().StringVarP(&networkFlag, "network", "n", "mainnet", "Stellar network (auto-detected when omitted; testnet, mainnet, futurenet)")
	debugCmd.Flags().StringVar(&rpcURLFlag, "rpc-url", "", "Custom RPC URL")
	debugCmd.Flags().StringVar(&rpcTokenFlag, "rpc-token", "", "RPC authentication token (can also use GLASSBOX_RPC_TOKEN env var)")
	debugCmd.Flags().BoolVar(&tracingEnabled, "tracing", false, "Enable tracing")
	debugCmd.Flags().StringVar(&otlpExporterURL, "otlp-url", "http://localhost:4318", "OTLP URL")
	debugCmd.Flags().BoolVar(&generateTrace, "generate-trace", false, "Generate trace file")
	debugCmd.Flags().StringVar(&traceOutputFile, "trace-output", "", "Trace output file")
	debugCmd.Flags().StringVar(&snapshotFlag, "snapshot", "", "Load state from JSON snapshot file")
	debugCmd.Flags().StringVar(&compareNetworkFlag, "compare-network", "", "Network to compare against (testnet, mainnet, futurenet)")
	debugCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
	debugCmd.Flags().StringVar(&wasmPath, "wasm", "", "Path to local WASM file for local replay (no network required)")
	debugCmd.Flags().StringSliceVar(&args, "args", []string{}, "Mock arguments for local replay (JSON array of strings)")
	debugCmd.Flags().StringSliceVar(&mockLedgerEntryFlags, "mock-ledger-entry", []string{}, "Override ledger entries before simulation using key:value; repeatable")
	debugCmd.Flags().StringVar(&mockLedgerManifest, "mock-ledger-manifest", "", "Path to a JSON manifest containing ledger_entries for override state")
	debugCmd.Flags().BoolVar(&noCacheFlag, "no-cache", false, "Disable local ledger state caching")
	debugCmd.Flags().StringVar(&xdrFileFlag, "xdr-file", "", "Load transaction envelope from a local XDR file or base64-encoded XDR")
	debugCmd.Flags().StringVar(&jsonFileFlag, "json-file", "", "Load transaction envelope from a local JSON file containing envelope_xdr")
	debugCmd.Flags().StringVar(&resultMetaFileFlag, "result-meta-file", "", "Load transaction result metadata from a local XDR or JSON file")
	debugCmd.Flags().BoolVar(&demoMode, "demo", false, "Print sample output (no network) - for testing color detection")
	debugCmd.Flags().BoolVar(&watchFilesFlag, "watch-files", false, "Watch for source/config file changes and rerun debug session")
	debugCmd.Flags().BoolVar(&watchFlag, "watch", false, "Poll for transaction on-chain before debugging")
	debugCmd.Flags().IntVar(&watchTimeoutFlag, "watch-timeout", 30, "Timeout in seconds for watch mode")
	debugCmd.Flags().BoolVar(&hotReloadFlag, "hot-reload", false, "Hot reload local WASM changes during debug session (requires --wasm)")
	debugCmd.Flags().DurationVar(&hotReloadInterval, "hot-reload-interval", 500*time.Millisecond, "Polling interval fallback for hot reload (e.g. 500ms)")
	debugCmd.Flags().BoolVar(&snapshotsFlag, "snapshots", false, "Enable simulator snapshot capture (default: disabled)")
	debugCmd.Flags().BoolVar(&liveReplayFlag, "live", false, "Replay transaction against the latest validated ledger state")
	debugCmd.Flags().BoolVar(&liveReplayFlag, "latest-ledger", false, "Alias for --live")
	debugCmd.Flags().IntVar(&opIndexFlag, "op", -1, "Select a specific zero-based operation index for multi-operation transactions")
	debugCmd.Flags().IntVar(&opIndexFlag, "operation", -1, "Legacy alias for --op")
	debugCmd.Flags().BoolVar(&secureWorkspaceFlag, "secure-workspace", false, "Run in a secure workspace mode with trusted read-only artifacts")
	debugCmd.Flags().StringVar(&pinEndpointFlag, "pin-endpoint", "", "Persist a pinned RPC endpoint with the debug session")
	debugCmd.Flags().Uint32Var(&mockBaseFeeFlag, "mock-base-fee", 0, "Override base fee (stroops) for local fee sufficiency checks")
	debugCmd.Flags().Uint64Var(&mockGasPriceFlag, "mock-gas-price", 0, "Override gas price multiplier for local fee sufficiency checks")
	debugCmd.Flags().StringVar(&themeFlag, "theme", "", "Color theme override (dark, light, none)")
	debugCmd.Flags().Int64Var(&mockTimeFlag, "mock-time", 0, "Override ledger timestamp for simulation (Unix seconds)")
	debugCmd.Flags().Uint32Var(&protocolVersionFlag, "protocol-version", 0, "Override protocol version for simulation")
	debugCmd.Flags().StringVar(&exportSVGFlag, "export-svg", "", "Export call graph as SVG to specified file")
	debugCmd.Flags().StringVar(&loadSnapshotsFlag, "load-snapshots", "", "Load simulation from a snapshot registry")
	debugCmd.Flags().StringVar(&saveSnapshotsFlag, "save-snapshots", "", "Save simulation results to a snapshot registry")
	debugCmd.Flags().StringVar(&contractSourceFlag, "contract-source", "", "Explicit path to contract source directory for source mapping (must exist and be a directory)")
	debugCmd.Flags().BoolVar(&debugJSONFlag, "json", false, "Output simulation results as machine-readable JSON")
	debugCmd.Flags().StringVar(&debugFormatFlag, "format", "text", "Output format: text or json")
	debugCmd.Flags().BoolVar(&skipSourceMappingFlag, "skip-source-mapping", false, "Skip DWARF source mapping for faster raw trace replay")
	debugCmd.Flags().StringVar(&traceVerbosityFlag, "trace-verbosity", "normal", "Trace detail level: summary, normal, or verbose")

	// Dry-run and metrics flags
	debugCmd.Flags().BoolVar(&debugDryRunFlag, "dry-run", false, "Validate inputs and check environment without running a simulation")
	debugCmd.Flags().BoolVar(&debugPlanFlag, "plan", false, "Print the execution plan (network requests, files, signing, outputs) and exit without running")
	debugCmd.Flags().BoolVar(&showMetricsFlag, "show-metrics", false, "Print RPC and simulation performance metrics after the run")
	debugCmd.Flags().BoolVar(&debugTimingsFlag, "timings", false, "Print per-phase timing breakdown after the run (stderr for text; structured JSON when --format json)")

	// Decentralised audit storage flags
	debugCmd.Flags().StringVar(&auditKeyFlag, "audit-key", "", "Ed25519 private key (PEM) for signing the audit trail before publishing")
	debugCmd.Flags().BoolVar(&publishIPFSFlag, "publish-ipfs", false, "Publish a signed audit trail to IPFS after simulation")
	debugCmd.Flags().BoolVar(&publishArweaveFlag, "publish-arweave", false, "Publish a signed audit trail to Arweave after simulation")
	debugCmd.Flags().StringVar(&ipfsNodeFlag, "ipfs-node", "", "IPFS node API URL (default: public gateway)")
	debugCmd.Flags().StringVar(&arweaveGatewayFlag, "arweave-gateway", "", "Arweave gateway URL")
	debugCmd.Flags().StringVar(&arweaveWalletFlag, "arweave-wallet", "", "Path to Arweave wallet JSON file")

	// Source alias mapping flag
	debugCmd.Flags().StringVar(&sourceAliasFlag, "source-alias", "", "Path to a JSON file mapping embedded source paths to local directory paths")

	// Reproducible build manifest flag (Issue #45)
	debugCmd.Flags().StringVar(&buildManifestFlag, "build-manifest", "", "Path to a glassbox-build-manifest.json for cross-machine source path remapping")

	// Enum-flag completion registrations (never performs network I/O)
	_ = debugCmd.RegisterFlagCompletionFunc("network", completeNetworkFlag)
	_ = debugCmd.RegisterFlagCompletionFunc("format", completeGeneralFormatFlag)
	_ = debugCmd.RegisterFlagCompletionFunc("theme", completeThemeFlag)
	_ = debugCmd.RegisterFlagCompletionFunc("trace-verbosity", completeTraceVerbosityFlag)
	_ = debugCmd.RegisterFlagCompletionFunc("view", completeViewModeFlag)
	_ = debugCmd.RegisterFlagCompletionFunc("profile-format", completeProfileFormatFlag)

	rootCmd.AddCommand(debugCmd)
}

func printWasmBacktrace(trace *simulator.WasmStackTrace) {
	fmt.Printf("\nBacktrace (%d frames):\n", len(trace.Frames))
	for _, frame := range trace.Frames {
		name := "<unknown>"
		if frame.FuncName != nil {
			name = *frame.FuncName
		} else if frame.FuncIndex != nil {
			name = fmt.Sprintf("func[%d]", *frame.FuncIndex)
		}

		location := ""
		if frame.SourceLocation != nil {
			if frame.SourceLocation.Column > 0 {
				location = fmt.Sprintf(" %s:%d:%d", frame.SourceLocation.File, frame.SourceLocation.Line, frame.SourceLocation.Column)
			} else {
				location = fmt.Sprintf(" %s:%d", frame.SourceLocation.File, frame.SourceLocation.Line)
			}
		} else if frame.WasmOffset != nil {
			location = fmt.Sprintf(" @ 0x%x", *frame.WasmOffset)
		}

		fmt.Printf("  #%d %s%s\n", frame.Index, name, location)
	}

	// Show inline source context for the trap-site frame (index 0).
	if len(trace.Frames) > 0 && trace.Frames[0].SourceLocation != nil {
		displaySourceLocation(trace.Frames[0].SourceLocation)
	}

	fmt.Println()
}

// checkLTOWarning searches the directory tree around a WASM file for
// Cargo.toml files with LTO settings and prints a warning if found.
// It searches the WASM file's parent directory and up to two levels up
// to find the project root.
func checkLTOWarning(wasmFilePath string) {
	dir := filepath.Dir(wasmFilePath)

	// Walk up to 3 levels to find Cargo.toml files
	for i := 0; i < 3; i++ {
		results, err := lto.CheckProjectDir(dir)
		if err != nil {
			logger.Logger.Debug("LTO check failed", "dir", dir, "error", err)
			break
		}
		if lto.HasLTO(results) {
			fmt.Fprintf(os.Stderr, "\n%s\n", lto.FormatWarnings(results))
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
}
func displaySourceLocation(loc *simulator.SourceLocation) {
	fmt.Printf("%s Location: %s:%d:%d\n", visualizer.Symbol("location"), loc.File, loc.Line, loc.Column)

	// Resolve path aliases when --source-alias config is provided.
	filePath := loc.File
	if sourceAliasFlag != "" {
		if aliases, err := sourcemap.LoadAliasConfig(sourceAliasFlag); err == nil {
			filePath = sourcemap.NewAliasResolver(aliases).Resolve(filePath)
		}
	}

	// Try to find the file
	content, err := os.ReadFile(filePath)
	if err != nil {
		// Try override path first when --contract-source is set
		if contractSourceFlag != "" {
			if c, err := os.ReadFile(filepath.Join(contractSourceFlag, filePath)); err == nil {
				content = c
			} else if c, err := os.ReadFile(filepath.Join(contractSourceFlag, filepath.Base(filePath))); err == nil {
				content = c
			}
		}
		// Try to find in current directory or src
		if content == nil {
			if c, err := os.ReadFile(filepath.Join("src", filePath)); err == nil {
				content = c
			} else {
				return
			}
		}
	}

	lines := strings.Split(string(content), "\n")
	if int(loc.Line) > len(lines) {
		return
	}

	// Show context
	start := int(loc.Line) - 3
	if start < 0 {
		start = 0
	}
	end := int(loc.Line) + 2
	if end > len(lines) {
		end = len(lines)
	}

	fmt.Println()
	for i := start; i < end; i++ {
		lineNum := i + 1
		prefix := "  "
		if lineNum == int(loc.Line) {
			prefix = "> "
		}

		fmt.Printf("%s %4d | %s\n", prefix, lineNum, lines[i])

		// Highlight the token if this is the failing line
		if lineNum == int(loc.Line) {
			// Calculate exact indentation to line up with the printed line
			// prefix (2) + lineNum (4) + pipe (3) = 9 spaces
			markerIndent := strings.Repeat(" ", 9)
			offset := int(loc.Column) - 1
			if offset < 0 {
				offset = 0
			}

			highlightLen := 1
			if loc.ColumnEnd != nil && *loc.ColumnEnd > loc.Column {
				highlightLen = int(*loc.ColumnEnd - loc.Column)
			}

			// Don't exceed line length
			if offset < len(lines[i]) {
				if offset+highlightLen > len(lines[i]) {
					highlightLen = len(lines[i]) - offset
				}
				marker := strings.Repeat(" ", offset) + strings.Repeat("^", highlightLen)
				fmt.Printf("      | %s%s\n", markerIndent[:2], marker)
			}
		}
	}
	fmt.Println()
}
