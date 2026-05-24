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
		CycleStatusCmd(),
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
	)
	cmd := &cobra.Command{
		Use:   "new",
		Short: "Create a new autonomous cycle",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("get home dir: %w", err)
			}
			return runCycleNew(cmd.Context(), home, goal, branch, maxCycles, maxDurationMinutes, autoGoal, agents, workingDir, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&goal, "goal", "", "One-sentence objective for the first cycle (required)")
	cmd.Flags().StringVar(&branch, "branch", "", "Git branch to work on (default: feat/autonomous-cycle)")
	cmd.Flags().IntVar(&maxCycles, "max-cycles", 10, "Maximum number of cycles (0 = unlimited)")
	cmd.Flags().IntVar(&maxDurationMinutes, "max-duration", 0, "Maximum runtime in minutes (0 = unlimited)")
	cmd.Flags().BoolVar(&autoGoal, "auto-goal", false, "Automatically discover next goal after each cycle")
	cmd.Flags().StringVar(&agents, "agents", "", "Comma-separated adapter names for multi-agent orchestration")
	cmd.Flags().StringVar(&workingDir, "working-dir", "", "Working directory for the cycle")
	_ = cmd.MarkFlagRequired("goal")
	return cmd
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
	var cycleID string
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch cycle events via SSE",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("get home dir: %w", err)
			}
			return runCycleWatch(cmd.Context(), home, cycleID, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&cycleID, "cycle-id", "", "Cycle ID (required)")
	_ = cmd.MarkFlagRequired("cycle-id")
	return cmd
}

func runCycleNew(ctx context.Context, home, goal, branch string, maxCycles, maxDurationMinutes int, autoGoal bool, agents, workingDir string, out io.Writer) error {
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
	_, _ = fmt.Fprintf(out, "  state_file:   tmp/cycle-%s.json\n", state.ID)
	return nil
}

func runCycleStatus(ctx context.Context, home, cycleID string, out io.Writer) error {
	bc, err := resolveBrokerClient(home, 0)
	if err != nil {
		return err
	}

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
		// Fallback: read local state file.
		data, err := os.ReadFile(filepath.Join("tmp", fmt.Sprintf("cycle-%s.json", cycleID)))
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

	var state contract.CycleState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	_, _ = fmt.Fprintf(out, "cycle halted: %s\n", state.ID)
	_, _ = fmt.Fprintf(out, "halt_reason:  %s\n", state.HaltReason)
	return nil
}

func runCycleWatch(ctx context.Context, home, cycleID string, out io.Writer) error {
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

	_, _ = fmt.Fprintf(out, "watching cycle %s events...\n", cycleID)
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			var evt contract.CycleEvent
			if err := json.Unmarshal([]byte(line[6:]), &evt); err == nil {
				_, _ = fmt.Fprintf(out, "[%s] phase=%s cycles=%d halted=%v\n",
					time.Now().Format("15:04:05"),
					evt.Phase,
					evt.CompletedCycles,
					evt.Halted,
				)
				if evt.Halted {
					_, _ = fmt.Fprintf(out, "halt_reason: %s\n", evt.HaltReason)
					return nil
				}
			} else {
				_, _ = fmt.Fprintln(out, line)
			}
		}
	}
	return scanner.Err()
}
