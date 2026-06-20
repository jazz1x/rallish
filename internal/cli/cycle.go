package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jazz1x/rallish/internal/safepath"
	"github.com/jazz1x/rallish/pkg/contract"
	"github.com/spf13/cobra"
)

// CycleCmd returns the parent `cycle` cobra command with subcommands registered.
func CycleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cycle",
		Short: "Autonomous-cycle runner with gate pipeline and multi-agent orchestration",
	}
	cmd.AddCommand(
		CycleNewCmd(),
		CycleStartCmd(),
		CycleRunCmd(),
		CycleStatusCmd(),
		CycleLedgerCmd(),
		CycleNextCmd(),
		CycleHaltCmd(),
		CycleWatchCmd(),
	)
	return cmd
}

// CycleNewCmd returns the `cycle new` subcommand.
func CycleNewCmd() *cobra.Command {
	var (
		goal               string
		branch             string
		maxCycles          int
		maxDurationMinutes int
		autoGoal           bool
		agents             string
		workingDir         string
		localGates         []string
		auditCmd           string
		polishTestCmd      string
	)
	cmd := &cobra.Command{
		Use:   "new",
		Short: "Create a new autonomous cycle",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("get home dir: %w", err)
			}
			// An explicitly passed but empty/whitespace-only override is a
			// misconfiguration. omitempty would erase an empty string on the
			// wire, making "passed empty" indistinguishable from "unset", so we
			// reject it here at the CLI boundary where cobra can still tell them
			// apart. Leaving the flag unset uses the documented default.
			if err := rejectBlankOverride(cmd, "audit-cmd", auditCmd); err != nil {
				return err
			}
			if err := rejectBlankOverride(cmd, "polish-test-cmd", polishTestCmd); err != nil {
				return err
			}
			return runCycleNew(cmd.Context(), home, goal, branch, maxCycles, maxDurationMinutes, autoGoal, agents, workingDir, localGates, auditCmd, polishTestCmd, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&goal, "goal", "", "One-sentence objective for the first cycle (required)")
	cmd.Flags().StringVar(&branch, "branch", "", "Git branch to work on (default: feat/autonomous-cycle)")
	cmd.Flags().IntVar(&maxCycles, "max-cycles", 10, "Maximum number of cycles (0 = unlimited)")
	cmd.Flags().IntVar(&maxDurationMinutes, "max-duration", 0, "Maximum runtime in minutes (0 = unlimited)")
	cmd.Flags().BoolVar(&autoGoal, "auto-goal", false, "Automatically discover next goal after each cycle")
	cmd.Flags().StringVar(&agents, "agents", "", "Comma-separated adapter names for multi-agent orchestration")
	cmd.Flags().StringVar(&workingDir, "working-dir", "", "Working directory for the cycle")
	cmd.Flags().StringArrayVar(&localGates, "local-gate", nil, "Repository-specific validation command to run after audit (repeatable)")
	cmd.Flags().StringVar(&auditCmd, "audit-cmd", "", "Override the audit gate command (default: make check-all); e.g. 'npm test', 'cargo test'")
	cmd.Flags().StringVar(&polishTestCmd, "polish-test-cmd", "", "Override the polish gate test command (default: go test -race ./...); e.g. 'npm test', 'cargo test'")
	_ = cmd.MarkFlagRequired("goal")
	return cmd
}

// rejectBlankOverride fails loudly when a gate-command override flag was
// explicitly passed but trims to empty. cobra's Changed reports whether the
// flag appeared on the command line, which distinguishes "passed empty"
// (--audit-cmd ” or '   ') from "unset" — the latter is allowed and uses the
// documented default. This honours the README promise that an empty or
// whitespace-only override fails loudly with no silent fallback to the default.
func rejectBlankOverride(cmd *cobra.Command, flagName, value string) error {
	if !cmd.Flags().Changed(flagName) {
		return nil
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("--%s was set but is empty or whitespace-only; provide a command (e.g. 'npm test') or omit the flag to use the default", flagName)
	}
	return nil
}

// CycleStatusCmd returns the `cycle status` subcommand.
func CycleStatusCmd() *cobra.Command {
	var cycleID string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Get the current state of a cycle",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("get home dir: %w", err)
			}
			return runCycleStatus(cmd.Context(), home, cycleID, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&cycleID, "cycle-id", "", "Cycle ID (required)")
	_ = cmd.MarkFlagRequired("cycle-id")
	return cmd
}

// CycleLedgerCmd returns the `cycle ledger` subcommand.
func CycleLedgerCmd() *cobra.Command {
	var cycleID string
	cmd := &cobra.Command{
		Use:   "ledger",
		Short: "Print the append-only harness ledger for a cycle",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("get home dir: %w", err)
			}
			return runCycleLedger(cmd.Context(), home, cycleID, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&cycleID, "cycle-id", "", "Cycle ID (required)")
	_ = cmd.MarkFlagRequired("cycle-id")
	return cmd
}

// CycleNextCmd returns the `cycle next` subcommand.
func CycleNextCmd() *cobra.Command {
	var (
		cycleID string
		goal    string
	)
	cmd := &cobra.Command{
		Use:   "next",
		Short: "Advance one cycle step (manual debug trigger)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("get home dir: %w", err)
			}
			return runCycleNext(cmd.Context(), home, cycleID, goal, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&cycleID, "cycle-id", "", "Cycle ID (required)")
	cmd.Flags().StringVar(&goal, "goal", "", "Override next_cycle_goal for this step")
	_ = cmd.MarkFlagRequired("cycle-id")
	return cmd
}

// CycleHaltCmd returns the `cycle halt` subcommand.
func CycleHaltCmd() *cobra.Command {
	var (
		cycleID string
		reason  string
	)
	cmd := &cobra.Command{
		Use:   "halt",
		Short: "Gracefully halt a cycle",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("get home dir: %w", err)
			}
			return runCycleHalt(cmd.Context(), home, cycleID, reason, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&cycleID, "cycle-id", "", "Cycle ID (required)")
	cmd.Flags().StringVar(&reason, "reason", "user-requested", "Halt reason")
	_ = cmd.MarkFlagRequired("cycle-id")
	return cmd
}

// CycleWatchCmd returns the `cycle watch` subcommand.
func CycleWatchCmd() *cobra.Command {
	var (
		cycleID string
		logFile string
	)
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch cycle events via SSE",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("get home dir: %w", err)
			}
			return runCycleWatch(cmd.Context(), home, cycleID, logFile, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&cycleID, "cycle-id", "", "Cycle ID (required)")
	cmd.Flags().StringVar(&logFile, "log-file", "", "Append event stream to file (e.g. /tmp/cycle.log)")
	_ = cmd.MarkFlagRequired("cycle-id")
	return cmd
}

// CycleStartCmd returns the `cycle start` subcommand (one-shot create + orchestrate + watch).
func CycleStartCmd() *cobra.Command {
	var (
		goal               string
		branch             string
		maxCycles          int
		maxDurationMinutes int
		autoGoal           bool
		agents             string
		localGates         []string
	)
	cmd := &cobra.Command{
		Use:   "start",
		Short: "One-shot start: create cycle, optionally orchestrate, then watch events",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("get home dir: %w", err)
			}
			var agentList []string
			if agents != "" {
				agentList = strings.Split(agents, ",")
			}
			req := contract.NewCycleRequest{
				Goal:               goal,
				Branch:             branch,
				MaxCycles:          maxCycles,
				MaxDurationMinutes: maxDurationMinutes,
				AutoGoal:           autoGoal,
				LocalGates:         localGates,
			}
			if agents != "" {
				req.Orchestrator = &contract.OrchestratorConfig{
					Agents:     agentList,
					WorkingDir: ".",
				}
			}
			logFile, _ := cmd.Flags().GetString("log-file")
			return runCycleStart(cmd.Context(), home, req, agentList, logFile, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&goal, "goal", "", "One-sentence objective for the first cycle (required)")
	cmd.Flags().StringVar(&branch, "branch", "", "Git branch to work on (default: feat/autonomous-cycle)")
	cmd.Flags().IntVar(&maxCycles, "max-cycles", 0, "Maximum number of cycles (0 = unlimited)")
	cmd.Flags().IntVar(&maxDurationMinutes, "max-duration", 240, "Maximum runtime in minutes (default: 240 = 4 hours)")
	cmd.Flags().BoolVar(&autoGoal, "auto-goal", true, "Automatically discover next goal after each cycle")
	cmd.Flags().StringVar(&agents, "agents", "", "Comma-separated adapter names for multi-agent orchestration")
	cmd.Flags().StringArrayVar(&localGates, "local-gate", nil, "Repository-specific validation command to run after audit (repeatable)")
	cmd.Flags().String("log-file", "", "Append event stream to file (e.g. /tmp/cycle.log)")
	_ = cmd.MarkFlagRequired("goal")
	return cmd
}

func runCycleStart(ctx context.Context, home string, req contract.NewCycleRequest, agents []string, logFile string, out io.Writer) error {
	bc, err := resolveBrokerClient(home, 0)
	if err != nil {
		return fmt.Errorf("broker client: %w", err)
	}

	// 1. Create cycle.
	body, _ := json.Marshal(req)
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, bc.URL+"/cycles", strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	r.Header.Set("Content-Type", "application/json")

	resp, err := bc.Client.Do(r)
	if err != nil {
		return fmt.Errorf("create cycle: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create cycle failed (%d): %s", resp.StatusCode, string(b))
	}

	var state contract.CycleState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	_, _ = fmt.Fprintf(out, "cycle started: %s\n", state.ID)
	_, _ = fmt.Fprintf(out, "  branch:      %s\n", state.Branch)
	_, _ = fmt.Fprintf(out, "  max_cycles:  %d\n", state.MaxCycles)
	_, _ = fmt.Fprintf(out, "  max_duration:%d min\n", state.MaxDurationMinutes)
	_, _ = fmt.Fprintf(out, "  auto_goal:   %v\n", state.AutoGoal)
	if len(state.LocalGates) > 0 {
		_, _ = fmt.Fprintf(out, "  local_gates: %d\n", len(state.LocalGates))
		for _, gate := range state.LocalGates {
			_, _ = fmt.Fprintf(out, "    - %s\n", gate)
		}
	}
	_, _ = fmt.Fprintf(out, "  state_file:  %s\n", cycleStateFileDisplay(state.ID))

	// 2. Start orchestration if agents are configured.
	if len(agents) > 0 {
		orchBody, _ := json.Marshal(contract.OrchestratorConfig{
			Agents:     agents,
			WorkingDir: ".",
		})
		r, err = http.NewRequestWithContext(ctx, http.MethodPost, bc.URL+"/cycles/"+state.ID+"/orchestrate", strings.NewReader(string(orchBody)))
		if err != nil {
			return fmt.Errorf("build orchestrate request: %w", err)
		}
		r.Header.Set("Content-Type", "application/json")

		resp, err = bc.Client.Do(r)
		if err != nil {
			return fmt.Errorf("orchestrate: %w", err)
		}
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusAccepted {
			return fmt.Errorf("orchestrate failed (%d)", resp.StatusCode)
		}
		_, _ = fmt.Fprintf(out, "orchestration started with agents: %v\n", agents)
	}

	// 3. Block and watch events.
	_, _ = fmt.Fprintf(out, "\nwatching events... (Ctrl+C to detach, cycle continues in background)\n\n")
	return runCycleWatch(ctx, home, state.ID, logFile, out)
}

func runCycleNew(ctx context.Context, home, goal, branch string, maxCycles, maxDurationMinutes int, autoGoal bool, agents, workingDir string, localGates []string, auditCmd, polishTestCmd string, out io.Writer) error {
	bc, err := resolveBrokerClient(home, 0)
	if err != nil {
		return err
	}

	req := contract.NewCycleRequest{
		Goal:               goal,
		Branch:             branch,
		MaxCycles:          maxCycles,
		MaxDurationMinutes: maxDurationMinutes,
		AutoGoal:           autoGoal,
		LocalGates:         localGates,
		AuditCmd:           auditCmd,
		PolishTestCmd:      polishTestCmd,
	}
	if agents != "" || workingDir != "" {
		req.Orchestrator = &contract.OrchestratorConfig{
			Agents:     strings.Split(agents, ","),
			WorkingDir: workingDir,
		}
	}

	body, _ := json.Marshal(req)
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, bc.URL+"/cycles", strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	r.Header.Set("Content-Type", "application/json")

	resp, err := bc.Client.Do(r)
	if err != nil {
		return fmt.Errorf("post cycles: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create cycle failed (%d): %s", resp.StatusCode, string(b))
	}

	var state contract.CycleState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	_, _ = fmt.Fprintf(out, "cycle created: %s\n", state.ID)
	_, _ = fmt.Fprintf(out, "  branch:       %s\n", state.Branch)
	_, _ = fmt.Fprintf(out, "  max_cycles:   %d\n", state.MaxCycles)
	_, _ = fmt.Fprintf(out, "  goal:         %s\n", state.NextCycleGoal)
	if len(state.LocalGates) > 0 {
		_, _ = fmt.Fprintf(out, "  local_gates: %d\n", len(state.LocalGates))
		for _, gate := range state.LocalGates {
			_, _ = fmt.Fprintf(out, "    - %s\n", gate)
		}
	}
	_, _ = fmt.Fprintf(out, "  state_file:   %s\n", cycleStateFileDisplay(state.ID))
	return nil
}

// cycleStateFileDisplay returns the human-facing path of a cycle's state file
// (the SSOT ~/.rallish/cycles location the daemon writes to). Falls back to the
// tilde form only if the home dir is unresolvable — display-only, never used for I/O.
func cycleStateFileDisplay(id string) string {
	dir, err := CyclesDir()
	if err != nil {
		return filepath.Join("~", ".rallish", "cycles", fmt.Sprintf("cycle-%s.json", id))
	}
	return filepath.Join(dir, fmt.Sprintf("cycle-%s.json", id))
}

func runCycleStatus(ctx context.Context, home, cycleID string, out io.Writer) error {
	bc, err := resolveBrokerClient(home, 0)
	if err != nil {
		return err
	}
	return runCycleStatusWithClient(ctx, bc, cycleID, out)
}

func runCycleStatusWithClient(ctx context.Context, bc brokerClient, cycleID string, out io.Writer) error {
	// Try broker first.
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, bc.URL+"/cycles/"+cycleID, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	resp, err := bc.Client.Do(r)
	if err != nil {
		return fmt.Errorf("get cycle: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var state contract.CycleState
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	} else {
		// Fallback: read the local state file from the SSOT cycles dir.
		cyclesDir, derr := CyclesDir()
		if derr != nil {
			return derr
		}
		// filepath.Base neutralises any separators in the id so the read stays
		// confined to cyclesDir (no path traversal via a crafted --cycle-id).
		stateFile := filepath.Join(cyclesDir, fmt.Sprintf("cycle-%s.json", filepath.Base(cycleID)))
		data, err := os.ReadFile(stateFile) //nolint:gosec // path confined to ~/.rallish/cycles via filepath.Base
		if err != nil {
			return fmt.Errorf("cycle not found: %w", err)
		}
		if err := json.Unmarshal(data, &state); err != nil {
			return fmt.Errorf("unmarshal state: %w", err)
		}
	}

	_, _ = fmt.Fprintf(out, "cycle:        %s\n", state.ID)
	_, _ = fmt.Fprintf(out, "phase:        %s\n", state.Phase)
	_, _ = fmt.Fprintf(out, "completed:    %d / %d\n", state.CompletedCycles, state.MaxCycles)
	_, _ = fmt.Fprintf(out, "branch:       %s\n", state.Branch)
	_, _ = fmt.Fprintf(out, "baseline:     %s\n", state.BaselineSHA)
	_, _ = fmt.Fprintf(out, "goal:         %s\n", state.NextCycleGoal)
	if len(state.LocalGates) > 0 {
		_, _ = fmt.Fprintf(out, "local_gates:  %d\n", len(state.LocalGates))
		for _, gate := range state.LocalGates {
			_, _ = fmt.Fprintf(out, "  - %s\n", gate)
		}
	}
	if entries, err := readCycleLedgerWithClient(ctx, bc, cycleID); err == nil && len(entries) > 0 {
		last := entries[len(entries)-1]
		_, _ = fmt.Fprintf(out, "ledger:       %d entries\n", len(entries))
		_, _ = fmt.Fprintf(out, "ledger_last:  %s", last.Type)
		if last.Gate != "" {
			_, _ = fmt.Fprintf(out, " gate=%s", last.Gate)
		}
		_, _ = fmt.Fprintln(out)
	}
	if state.ShouldRotateAgent() {
		_, _ = fmt.Fprintf(out, "agent_rotate: yes (3-cycle reset)\n")
	}
	_, _ = fmt.Fprintf(out, "halted:       %v\n", state.Halted)
	if state.Halted {
		_, _ = fmt.Fprintf(out, "halt_reason:  %s\n", state.HaltReason)
	}
	if len(state.ViolationsFound) > 0 {
		_, _ = fmt.Fprintf(out, "violations:   %d\n", len(state.ViolationsFound))
		for _, v := range state.ViolationsFound {
			_, _ = fmt.Fprintf(out, "  - [%s] %s:%d %s\n", v.Type, v.File, v.Line, v.Message)
		}
	}
	return nil
}

func runCycleLedger(ctx context.Context, home, cycleID string, out io.Writer) error {
	bc, err := resolveBrokerClient(home, 0)
	if err != nil {
		return err
	}
	return runCycleLedgerWithClient(ctx, bc, cycleID, out)
}

func runCycleLedgerWithClient(ctx context.Context, bc brokerClient, cycleID string, out io.Writer) error {
	entries, err := readCycleLedgerWithClient(ctx, bc, cycleID)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("encode ledger: %w", err)
	}
	_, _ = fmt.Fprintln(out, string(encoded))
	return nil
}

func readCycleLedgerWithClient(ctx context.Context, bc brokerClient, cycleID string) ([]contract.HarnessLedgerEntry, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, bc.URL+"/cycles/"+cycleID+"/ledger", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := bc.Client.Do(r)
	if err != nil {
		return nil, fmt.Errorf("get cycle ledger: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cycle ledger failed (%d): %s", resp.StatusCode, string(b))
	}

	var entries []contract.HarnessLedgerEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode ledger: %w", err)
	}
	return entries, nil
}

func runCycleNext(ctx context.Context, home, cycleID, goal string, out io.Writer) error {
	bc, err := resolveBrokerClient(home, 0)
	if err != nil {
		return err
	}

	req := contract.StepRequest{Goal: goal}
	body, _ := json.Marshal(req)
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, bc.URL+"/cycles/"+cycleID+"/step", strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	r.Header.Set("Content-Type", "application/json")

	resp, err := bc.Client.Do(r)
	if err != nil {
		return fmt.Errorf("post step: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("step cycle failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var state contract.CycleState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	_, _ = fmt.Fprintf(out, "cycle stepped: %s\n", state.ID)
	_, _ = fmt.Fprintf(out, "phase:        %s\n", state.Phase)
	_, _ = fmt.Fprintf(out, "completed:    %d / %d\n", state.CompletedCycles, state.MaxCycles)
	if state.Halted {
		_, _ = fmt.Fprintf(out, "halted:       true\n")
		_, _ = fmt.Fprintf(out, "halt_reason:  %s\n", state.HaltReason)
	}
	return nil
}

func runCycleHalt(ctx context.Context, home, cycleID, reason string, out io.Writer) error {
	bc, err := resolveBrokerClient(home, 0)
	if err != nil {
		return err
	}

	req := contract.HaltRequest{Reason: reason}
	body, _ := json.Marshal(req)
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, bc.URL+"/cycles/"+cycleID+"/halt", strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	r.Header.Set("Content-Type", "application/json")

	resp, err := bc.Client.Do(r)
	if err != nil {
		return fmt.Errorf("post halt: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("halt cycle failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var state contract.CycleState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	_, _ = fmt.Fprintf(out, "cycle halted: %s\n", state.ID)
	_, _ = fmt.Fprintf(out, "halt_reason:  %s\n", state.HaltReason)
	return nil
}

func runCycleWatch(ctx context.Context, home, cycleID, logFile string, out io.Writer) error {
	bc, err := resolveBrokerClient(home, 0)
	if err != nil {
		return err
	}

	r, err := http.NewRequestWithContext(ctx, http.MethodGet, bc.URL+"/cycles/"+cycleID+"/events", nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	resp, err := bc.Client.Do(r)
	if err != nil {
		return fmt.Errorf("get events: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("events failed (%d): %s", resp.StatusCode, string(b))
	}

	var logOut io.Writer
	if logFile != "" {
		safeLog, cleanErr := safepath.Clean(logFile)
		if cleanErr != nil {
			_, _ = fmt.Fprintf(out, "warn: invalid log file path: %v\n", cleanErr)
		} else {
			f, err := os.OpenFile(safeLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644) //nolint:gosec // path validated by safepath.Clean
			if err != nil {
				_, _ = fmt.Fprintf(out, "warn: cannot open log file: %v\n", err)
			} else {
				defer func() { _ = f.Close() }()
				logOut = f
			}
		}
	}

	_, _ = fmt.Fprintf(out, "watching cycle %s events...\n", cycleID)
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			var evt contract.CycleEvent
			if err := json.Unmarshal([]byte(line[6:]), &evt); err == nil {
				msg := fmt.Sprintf("[%s] phase=%s cycles=%d halted=%v\n",
					time.Now().Format("15:04:05"),
					evt.Phase,
					evt.CompletedCycles,
					evt.Halted,
				)
				_, _ = fmt.Fprint(out, msg)
				if logOut != nil {
					_, _ = fmt.Fprint(logOut, msg)
				}
				if evt.Halted {
					msg = fmt.Sprintf("halt_reason: %s\n", evt.HaltReason)
					_, _ = fmt.Fprint(out, msg)
					if logOut != nil {
						_, _ = fmt.Fprint(logOut, msg)
					}
					return nil
				}
			} else {
				_, _ = fmt.Fprintln(out, line)
				if logOut != nil {
					_, _ = fmt.Fprintln(logOut, line)
				}
			}
		}
	}
	return scanner.Err()
}
