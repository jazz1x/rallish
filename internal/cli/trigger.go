package cli

import (
	"fmt"
	"time"

	"github.com/jazz1x/rallish/internal/skills"
	"github.com/spf13/cobra"
)

// TriggerCmd returns the `trigger` subcommand.
func TriggerCmd() *cobra.Command {
	var (
		goal            string
		maxCycles       int
		maxDurationMins int
		agents          string
		autoGoal        bool
		dryRun          bool
	)
	cmd := &cobra.Command{
		Use:   "trigger <phrase>",
		Short: "Trigger a skill by natural-language phrase",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			phrase := args[0]
			skillName, err := skills.MatchSkillByTrigger(phrase)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "matched skill: %s\n", skillName)

			switch skillName {
			case "autonomous-cycle":
				if goal == "" {
					goal = "feat: autonomous-cycle run"
				}
				logFile := fmt.Sprintf("tmp/autonomous-%s.log", time.Now().Format("20060102-1504"))
				if dryRun {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "dry-run: would execute:\n")
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  rallish cycle start \\\n")
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "    --goal %q \\\n", goal)
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "    --max-cycles %d \\\n", maxCycles)
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "    --max-duration %d \\\n", maxDurationMins)
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "    --auto-goal=%v \\\n", autoGoal)
					if agents != "" {
						_, _ = fmt.Fprintf(cmd.OutOrStdout(), "    --agents %q \\\n", agents)
					}
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "    --log-file %q\n", logFile)
					return nil
				}
				startCmd := CycleStartCmd()
				startCmd.SetOut(cmd.OutOrStdout())
				startCmd.SetErr(cmd.ErrOrStderr())
				startCmd.SetArgs([]string{})
				_ = startCmd.Flags().Set("goal", goal)
				_ = startCmd.Flags().Set("max-cycles", fmt.Sprintf("%d", maxCycles))
				_ = startCmd.Flags().Set("max-duration", fmt.Sprintf("%d", maxDurationMins))
				_ = startCmd.Flags().Set("auto-goal", fmt.Sprintf("%v", autoGoal))
				if agents != "" {
					_ = startCmd.Flags().Set("agents", agents)
				}
				_ = startCmd.Flags().Set("log-file", logFile)
				return startCmd.RunE(cmd, []string{})
			default:
				return fmt.Errorf("skill %q has no registered action", skillName)
			}
		},
	}
	cmd.Flags().StringVar(&goal, "goal", "", "Override the default goal for the triggered skill")
	cmd.Flags().IntVar(&maxCycles, "max-cycles", 10, "Maximum cycles (autonomous-cycle default: 10)")
	cmd.Flags().IntVar(&maxDurationMins, "max-duration", 240, "Maximum runtime in minutes (default: 240 = 4h)")
	cmd.Flags().StringVar(&agents, "agents", "", "Comma-separated adapter names for multi-agent orchestration")
	cmd.Flags().BoolVar(&autoGoal, "auto-goal", true, "Automatically discover next goal after each cycle")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would be executed without running it")
	return cmd
}
