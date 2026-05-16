package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/jazz1x/rallish/internal/safepath"
	"github.com/jazz1x/rallish/pkg/contract"
	"github.com/spf13/cobra"
)

// nameRe mirrors the broker's participant name validation (PRD §6).
var nameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,16}$`)

// RallyCmd returns the parent `rally` cobra command with subcommands registered.
func RallyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rally",
		Short: "Live baton-passing between interactive CLI sessions",
	}
	cmd.AddCommand(
		RallyNewCmd(),
		RallyJoinCmd(),
		RallyDoneCmd(),
		RallyStatusCmd(),
	)
	return cmd
}

// RallyNewCmd returns the `rally new` subcommand.
func RallyNewCmd() *cobra.Command {
	var (
		participants string
		repo         string
		task         string
	)
	cmd := &cobra.Command{
		Use:   "new",
		Short: "Create a new rally session",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("get home dir: %w", err)
			}
			return runRallyNew(cmd.Context(), home, participants, repo, task, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&participants, "participants", "", "Comma-separated participant names (required)")
	cmd.Flags().StringVar(&repo, "repo", "", "Repository path (optional)")
	cmd.Flags().StringVar(&task, "task", "", "Task description (optional)")
	_ = cmd.MarkFlagRequired("participants")
	return cmd
}

// RallyJoinCmd returns the `rally join` subcommand.
func RallyJoinCmd() *cobra.Command {
	var (
		sessionID string
		as        string
	)
	cmd := &cobra.Command{
		Use:   "join",
		Short: "Join a rally session and wait for the baton",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("get home dir: %w", err)
			}
			return runRallyJoin(cmd.Context(), home, sessionID, as, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&sessionID, "session-id", "", "Rally session ID (required)")
	cmd.Flags().StringVar(&as, "as", "", "Participant name (required)")
	_ = cmd.MarkFlagRequired("session-id")
	_ = cmd.MarkFlagRequired("as")
	return cmd
}

// RallyDoneCmd returns the `rally done` subcommand.
func RallyDoneCmd() *cobra.Command {
	var (
		sessionID string
		as        string
		note      string
		handoffTo string
	)
	cmd := &cobra.Command{
		Use:   "done",
		Short: "Pass the baton to the next participant",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("get home dir: %w", err)
			}
			return runRallyDone(cmd.Context(), home, sessionID, as, note, handoffTo, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&sessionID, "session-id", "", "Rally session ID (required)")
	cmd.Flags().StringVar(&as, "as", "", "Participant name (required)")
	cmd.Flags().StringVar(&note, "note", "", "Note to pass with the baton (optional)")
	cmd.Flags().StringVar(&handoffTo, "handoff-to", "", "Specific participant to hand off to (optional)")
	_ = cmd.MarkFlagRequired("session-id")
	_ = cmd.MarkFlagRequired("as")
	return cmd
}

// RallyStatusCmd returns the `rally status` subcommand.
func RallyStatusCmd() *cobra.Command {
	var sessionID string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the status of a rally session",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("get home dir: %w", err)
			}
			return runRallyStatus(cmd.Context(), home, sessionID, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&sessionID, "session-id", "", "Rally session ID (required)")
	_ = cmd.MarkFlagRequired("session-id")
	return cmd
}

// ---- implementation ----

func runRallyNew(ctx context.Context, homeDir, participants, repo, task string, out io.Writer) error {
	// Parse and validate participants.
	names := splitParticipants(participants)
	if len(names) < 2 {
		return fmt.Errorf("--participants requires at least 2 comma-separated names")
	}
	for _, name := range names {
		if !nameRe.MatchString(name) {
			return fmt.Errorf("invalid participant name %q: must match ^[a-zA-Z0-9_-]{1,16}$", name)
		}
	}

	// Validate --repo if supplied: must be an existing directory with no traversal.
	if repo != "" {
		cleanedRepo, cleanErr := safepath.Clean(repo)
		if cleanErr != nil {
			return fmt.Errorf("--repo: %w", cleanErr)
		}
		info, statErr := os.Stat(cleanedRepo)
		if statErr != nil {
			return fmt.Errorf("--repo %q: %w", repo, statErr)
		}
		if !info.IsDir() {
			return fmt.Errorf("--repo %q: not a directory", repo)
		}
		repo = cleanedRepo
	}

	bc, err := resolveBrokerClient(homeDir, 30*time.Second)
	if err != nil {
		return err
	}

	reqBody := contract.NewRallyRequest{
		Participants: names,
		Repo:         repo,
		Task:         task,
	}
	bodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, bc.URL+"/rally/sessions", bytes.NewReader(bodyJSON))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := bc.Client.Do(req)
	if err != nil {
		return fmt.Errorf("POST /rally/sessions: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create rally session failed: %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var sess contract.RallySession
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	_, _ = fmt.Fprintln(out, sess.ID)
	return nil
}

func runRallyJoin(ctx context.Context, homeDir, sessionID, as string, out io.Writer) error {
	if !nameRe.MatchString(as) {
		return fmt.Errorf("invalid participant name %q: must match ^[a-zA-Z0-9_-]{1,16}$", as)
	}

	// Use a connect timeout but no read timeout — waiting for a turn can be
	// arbitrarily long.
	bc, err := resolveBrokerClient(homeDir, 30*time.Second)
	if err != nil {
		return err
	}
	// Remove the overall timeout so SSE reads block indefinitely.
	bc.Client.Timeout = 0

	url := fmt.Sprintf("%s/rally/sessions/%s/baton?as=%s", bc.URL, sessionID, as)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	// Handle SIGINT/SIGTERM: cancel the context to close the connection cleanly.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	resp, err := bc.Client.Do(req.WithContext(ctx))
	if err != nil {
		if ctx.Err() != nil {
			return nil // cancelled by signal
		}
		return fmt.Errorf("GET baton SSE: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("join rally failed: %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	scanner := bufio.NewScanner(resp.Body)
	var dataLine string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, ": ") {
			// SSE comment (heartbeat ping) — ignore.
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			dataLine = strings.TrimPrefix(line, "data: ")
			continue
		}
		if line == "" && dataLine != "" {
			// End of SSE event block.
			if err := handleBatonEvent(dataLine, sessionID, as, out); err != nil {
				return err
			}
			dataLine = ""
		}
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return nil // cancelled cleanly
		}
		return fmt.Errorf("reading SSE stream: %w", err)
	}

	_, _ = fmt.Fprintln(out, "session closed")
	return nil
}

func handleBatonEvent(data, sessionID, as string, out io.Writer) error {
	// Check for a close sentinel.
	var closed struct {
		Closed bool `json:"closed"`
	}
	if err := json.Unmarshal([]byte(data), &closed); err == nil && closed.Closed {
		_, _ = fmt.Fprintln(out, "session closed")
		return io.EOF // signal caller to stop reading
	}

	var evt contract.BatonEvent
	if err := json.Unmarshal([]byte(data), &evt); err != nil {
		// Ignore unparseable events (e.g. ping payloads).
		return nil
	}

	from := evt.From
	if from == "" {
		from = "(start)"
	}
	note := evt.Note
	if note == "" {
		note = "(no note)"
	}

	_, _ = fmt.Fprintf(out, "\U0001f3d3 your turn (turn %d, from %s): %s\n", evt.TurnN, from, note)
	_, _ = fmt.Fprintf(out, "   -> work in your CLI (e.g. claude). When done, in any terminal:\n")
	_, _ = fmt.Fprintf(out, "   ->   rallish rally done --session-id %s --as %s --note \"<summary>\"\n", sessionID, as)
	return nil
}

func runRallyDone(ctx context.Context, homeDir, sessionID, as, note, handoffTo string, out io.Writer) error {
	if !nameRe.MatchString(as) {
		return fmt.Errorf("invalid participant name %q: must match ^[a-zA-Z0-9_-]{1,16}$", as)
	}
	if handoffTo != "" && !nameRe.MatchString(handoffTo) {
		return fmt.Errorf("invalid handoff-to name %q: must match ^[a-zA-Z0-9_-]{1,16}$", handoffTo)
	}

	bc, err := resolveBrokerClient(homeDir, 30*time.Second)
	if err != nil {
		return err
	}

	doneReq := contract.DoneRequest{
		Note:      note,
		HandoffTo: handoffTo,
	}
	bodyJSON, err := json.Marshal(doneReq)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/rally/sessions/%s/done?as=%s", bc.URL, sessionID, as)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyJSON))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := bc.Client.Do(req)
	if err != nil {
		return fmt.Errorf("POST done: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		var sess contract.RallySession
		if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
		_, _ = fmt.Fprintf(out, "ok — baton passed to %s (turn %d)\n", sess.Holder, sess.TurnN)
		return nil
	case http.StatusConflict:
		body, _ := io.ReadAll(resp.Body)
		// Extract the holder from the error message if possible.
		msg := strings.TrimSpace(string(body))
		_, _ = fmt.Fprintf(out, "not your turn — %s\n", msg)
		return fmt.Errorf("not your turn")
	default:
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("done failed: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
}

func runRallyStatus(ctx context.Context, homeDir, sessionID string, out io.Writer) error {
	bc, err := resolveBrokerClient(homeDir, 30*time.Second)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/rally/sessions/%s", bc.URL, sessionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	resp, err := bc.Client.Do(req)
	if err != nil {
		return fmt.Errorf("GET status: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status failed: %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var sess contract.RallySession
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	printRallyStatus(out, sess)
	return nil
}

func printRallyStatus(out io.Writer, sess contract.RallySession) {
	_, _ = fmt.Fprintf(out, "Session:      %s\n", sess.ID)
	_, _ = fmt.Fprintf(out, "Status:       %s\n", sess.Status)
	_, _ = fmt.Fprintf(out, "Holder:       %s\n", holderDisplay(sess.Holder))
	_, _ = fmt.Fprintf(out, "Turn:         %d\n", sess.TurnN)
	if sess.Task != "" {
		_, _ = fmt.Fprintf(out, "Task:         %s\n", sess.Task)
	}
	if sess.Repo != "" {
		_, _ = fmt.Fprintf(out, "Repo:         %s\n", sess.Repo)
	}
	nowMS := time.Now().UnixMilli()
	_, _ = fmt.Fprintln(out, "Participants:")
	for _, name := range sess.Participants {
		ls, ok := sess.LastSeen[name]
		stale := ""
		if ok && (nowMS-ls) > 30_000 {
			stale = " [stale]"
		} else if !ok {
			stale = " [not yet connected]"
		}
		var lastSeenStr string
		if ok {
			lastSeenStr = fmt.Sprintf("last seen %ds ago", (nowMS-ls)/1000)
		} else {
			lastSeenStr = "never seen"
		}
		_, _ = fmt.Fprintf(out, "  - %s (%s)%s\n", name, lastSeenStr, stale)
	}

	if len(sess.History) > 0 {
		_, _ = fmt.Fprintln(out, "History (last 5):")
		start := 0
		if len(sess.History) > 5 {
			start = len(sess.History) - 5
		}
		for _, h := range sess.History[start:] {
			note := h.Note
			if note == "" {
				note = "-"
			}
			_, _ = fmt.Fprintf(out, "  turn %d: %s -> %s  note: %q\n", h.TurnN, h.From, h.To, note)
		}
	}
}

func holderDisplay(holder string) string {
	if holder == "" {
		return "(none)"
	}
	return holder
}

func splitParticipants(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
