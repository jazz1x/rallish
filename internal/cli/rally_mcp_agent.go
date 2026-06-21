package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jazz1x/rallish/pkg/contract"
	"github.com/spf13/cobra"
)

const (
	mcpClientDefaultTimeout  = 30 * time.Second
	mcpClientJoinTimeoutSlop = 5 * time.Second
)

// mcpAgentOptions holds the flags for `rallish rally mcp-agent`.
type mcpAgentOptions struct {
	Mode         string
	Participants string
	Repo         string
	Task         string
	FirstHolder  string
	SessionID    string
	As           string
	Timeout      time.Duration
	Note         string
	HandoffTo    string
}

// RallyMCPAgentCmd returns the `rally mcp-agent` subcommand.
func RallyMCPAgentCmd() *cobra.Command {
	var opts mcpAgentOptions
	cmd := &cobra.Command{
		Use:   "mcp-agent",
		Short: "One-shot MCP client for rally baton-passing",
		Long: `One-shot MCP client that talks to the rallish daemon's MCP surface.

Modes:
  create     Create a new rally session.
  join       Wait until the baton arrives for the named participant.
  done       Pass the baton to the next participant.
  status     Print the current rally session snapshot.
  interrupt  Forcibly interrupt a rally session.

The command prints raw JSON to stdout and exits.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("get home dir: %w", err)
			}
			return runRallyMCPAgent(cmd.Context(), home, opts, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&opts.Mode, "mode", "", "Mode: create, join, done, status")
	cmd.Flags().StringVar(&opts.Participants, "participants", "", "Comma-separated participant names (create)")
	cmd.Flags().StringVar(&opts.Repo, "repo", "", "Repository path (create)")
	cmd.Flags().StringVar(&opts.Task, "task", "", "Task description (create)")
	cmd.Flags().StringVar(&opts.FirstHolder, "first", "", "Pre-assign baton holder (create)")
	cmd.Flags().StringVar(&opts.SessionID, "session-id", "", "Rally session ID (join/done/status)")
	cmd.Flags().StringVar(&opts.As, "as", "", "Participant name (join/done)")
	cmd.Flags().DurationVar(&opts.Timeout, "timeout", 30*time.Second, "Max wait for baton (join)")
	cmd.Flags().StringVar(&opts.Note, "note", "", "Note to pass with the baton (done)")
	cmd.Flags().StringVar(&opts.HandoffTo, "handoff-to", "", "Explicit next holder (done)")
	_ = cmd.MarkFlagRequired("mode")
	return cmd
}

func runRallyMCPAgent(ctx context.Context, homeDir string, opts mcpAgentOptions, out io.Writer) error {
	switch opts.Mode {
	case "create":
		return runMCPAgentCreate(ctx, homeDir, opts, out)
	case "join":
		return runMCPAgentJoin(ctx, homeDir, opts, out)
	case "done":
		return runMCPAgentDone(ctx, homeDir, opts, out)
	case "status":
		return runMCPAgentStatus(ctx, homeDir, opts, out)
	case "interrupt":
		return runMCPAgentInterrupt(ctx, homeDir, opts, out)
	default:
		return fmt.Errorf("unknown mode %q: must be one of create, join, done, status, interrupt", opts.Mode)
	}
}

func runMCPAgentCreate(ctx context.Context, homeDir string, opts mcpAgentOptions, out io.Writer) error {
	names := splitParticipants(opts.Participants)
	if len(names) < 2 {
		return fmt.Errorf("--participants requires at least 2 comma-separated names: %w", contract.ErrTooFewParticipants)
	}
	client, err := newMCPClient(homeDir, mcpClientDefaultTimeout)
	if err != nil {
		return err
	}
	defer func() { _ = client.close() }()

	if err := client.handshake(ctx); err != nil {
		return err
	}

	args := contract.RallyCreateToolArgs{
		Participants: names,
		Repo:         opts.Repo,
		Task:         opts.Task,
		FirstHolder:  opts.FirstHolder,
	}
	text, err := client.callTool(ctx, "rally_create", args)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(out, text)
	return nil
}

func runMCPAgentJoin(ctx context.Context, homeDir string, opts mcpAgentOptions, out io.Writer) error {
	if _, err := contract.NewSessionID(opts.SessionID); err != nil {
		return err
	}
	if _, err := contract.NewParticipantName(opts.As); err != nil {
		return err
	}

	clientTimeout := opts.Timeout
	if clientTimeout <= 0 {
		clientTimeout = 30 * time.Second
	}
	client, err := newMCPClient(homeDir, clientTimeout+mcpClientJoinTimeoutSlop)
	if err != nil {
		return err
	}
	defer func() { _ = client.close() }()

	if err := client.handshake(ctx); err != nil {
		return err
	}

	args := contract.RallyJoinToolArgs{
		SessionID: opts.SessionID,
		As:        opts.As,
		TimeoutMS: int(opts.Timeout.Milliseconds()),
	}
	text, err := client.callTool(ctx, "rally_join", args)
	if err != nil {
		return err
	}

	var timeout contract.MCPTimeoutResult
	if unmarshalErr := json.Unmarshal([]byte(text), &timeout); unmarshalErr == nil && timeout.Timeout {
		_, _ = fmt.Fprintln(out, text)
		return ErrTimeoutWaitingForBaton
	}

	_, _ = fmt.Fprintln(out, text)
	return nil
}

func runMCPAgentDone(ctx context.Context, homeDir string, opts mcpAgentOptions, out io.Writer) error {
	if _, err := contract.NewSessionID(opts.SessionID); err != nil {
		return err
	}
	if _, err := contract.NewParticipantName(opts.As); err != nil {
		return err
	}
	client, err := newMCPClient(homeDir, mcpClientDefaultTimeout)
	if err != nil {
		return err
	}
	defer func() { _ = client.close() }()

	if err := client.handshake(ctx); err != nil {
		return err
	}

	args := contract.RallyDoneToolArgs{
		SessionID: opts.SessionID,
		As:        opts.As,
		Note:      opts.Note,
		HandoffTo: opts.HandoffTo,
	}
	text, err := client.callTool(ctx, "rally_done", args)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(out, text)
	return nil
}

func runMCPAgentStatus(ctx context.Context, homeDir string, opts mcpAgentOptions, out io.Writer) error {
	if _, err := contract.NewSessionID(opts.SessionID); err != nil {
		return err
	}
	client, err := newMCPClient(homeDir, mcpClientDefaultTimeout)
	if err != nil {
		return err
	}
	defer func() { _ = client.close() }()

	if err := client.handshake(ctx); err != nil {
		return err
	}

	args := contract.RallyStatusToolArgs{SessionID: opts.SessionID}
	text, err := client.callTool(ctx, "rally_status", args)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(out, text)
	return nil
}

func runMCPAgentInterrupt(ctx context.Context, homeDir string, opts mcpAgentOptions, out io.Writer) error {
	if _, err := contract.NewSessionID(opts.SessionID); err != nil {
		return err
	}
	client, err := newMCPClient(homeDir, mcpClientDefaultTimeout)
	if err != nil {
		return err
	}
	defer func() { _ = client.close() }()

	if err := client.handshake(ctx); err != nil {
		return err
	}

	args := contract.RallyInterruptToolArgs{SessionID: opts.SessionID}
	text, err := client.callTool(ctx, "rally_interrupt", args)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(out, text)
	return nil
}

// mcpClient is a one-shot MCP 2025-03-26 client over SSE.
type mcpClient struct {
	baseURL  string
	client   *http.Client
	endpoint string
	resp     *http.Response
	scanner  *bufio.Scanner
	nextID   int
}

func newMCPClient(homeDir string, timeout time.Duration) (*mcpClient, error) {
	bc, err := resolveBrokerClient(homeDir, timeout)
	if err != nil {
		return nil, err
	}
	return &mcpClient{
		baseURL: bc.URL,
		client:  bc.Client,
		nextID:  0,
	}, nil
}

func (c *mcpClient) close() error {
	if c.resp != nil {
		return c.resp.Body.Close()
	}
	return nil
}

// handshake opens the SSE transport and performs the MCP initialize exchange.
func (c *mcpClient) handshake(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/mcp/sse", nil)
	if err != nil {
		return fmt.Errorf("build sse request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("mcp sse request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("mcp sse unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	c.resp = resp
	c.scanner = bufio.NewScanner(resp.Body)

	if err := c.readEndpoint(); err != nil {
		return err
	}

	if _, err := c.call(ctx, "initialize", contract.MCPInitializeRequest{
		ProtocolVersion: contract.MCPProtocolVersion,
		Capabilities:    contract.MCPCapabilities{},
		ClientInfo:      contract.MCPClientInfo{Name: "rallish-mcp-agent", Version: "0.1.0"},
	}); err != nil {
		return err
	}
	return nil
}

func (c *mcpClient) readEndpoint() error {
	for c.scanner.Scan() {
		line := c.scanner.Text()
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			c.endpoint = c.baseURL + line[len("data: "):]
			return nil
		}
	}
	if err := c.scanner.Err(); err != nil {
		return fmt.Errorf("read endpoint event: %w", err)
	}
	return errors.New("no endpoint event in mcp sse stream")
}

func (c *mcpClient) callTool(ctx context.Context, name string, args any) (string, error) {
	argBytes, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("marshal tool arguments: %w", err)
	}
	params := contract.MCPToolCallRequest{Name: name, Arguments: argBytes}
	result, err := c.call(ctx, "tools/call", params)
	if err != nil {
		return "", err
	}

	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		return "", errors.New("invalid tool result: missing content")
	}
	first, ok := content[0].(map[string]any)
	if !ok {
		return "", errors.New("invalid tool result: malformed content item")
	}
	typ, _ := first["type"].(string)
	if typ != "text" {
		return "", fmt.Errorf("unsupported content type %q", typ)
	}
	text, _ := first["text"].(string)
	if isErr, _ := result["isError"].(bool); isErr {
		return "", fmt.Errorf("tool error: %s", text)
	}
	return text, nil
}

func (c *mcpClient) call(ctx context.Context, method string, params any) (map[string]any, error) {
	c.nextID++
	id := c.nextID

	raw, err := marshalRaw(params)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(contract.MCPJSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  raw,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal jsonrpc request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build jsonrpc request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jsonrpc request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("jsonrpc unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	for {
		msg, err := c.readMessage()
		if err != nil {
			return nil, err
		}
		if msg.ID == float64(id) {
			if msg.Error != nil {
				return nil, fmt.Errorf("mcp error %d: %s", msg.Error.Code, msg.Error.Message)
			}
			return msg.Result, nil
		}
	}
}

func (c *mcpClient) readMessage() (*contract.MCPJSONRPCResponse, error) {
	var data string
	for c.scanner.Scan() {
		line := c.scanner.Text()
		if line == "" {
			if data != "" {
				var msg contract.MCPJSONRPCResponse
				if err := json.Unmarshal([]byte(data), &msg); err != nil {
					return nil, fmt.Errorf("unmarshal mcp message: %w", err)
				}
				return &msg, nil
			}
			continue
		}
		if strings.HasPrefix(line, ": ") {
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			data = line[len("data: "):]
		}
	}
	if err := c.scanner.Err(); err != nil {
		return nil, fmt.Errorf("read mcp message: %w", err)
	}
	return nil, errors.New("mcp sse stream closed before response")
}

func marshalRaw(v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}
	return b, nil
}
