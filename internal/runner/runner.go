package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jazz1x/hocketty/internal/adapter"
	"github.com/jazz1x/hocketty/pkg/contract"
)

// ErrSessionGone is returned when the broker signals the session has terminated.
var ErrSessionGone = errors.New("session gone")

// Loop polls the broker for turns and delegates to an adapter.
type Loop struct {
	adapter   adapter.Adapter
	role      string
	brokerURL string
	sessionID string
	client    *http.Client
}

// NewLoop creates a new runner loop.
func NewLoop(adapter adapter.Adapter, role, brokerURL, sessionID string) *Loop {
	return &Loop{
		adapter:   adapter,
		role:      role,
		brokerURL: strings.TrimSuffix(brokerURL, "/"),
		sessionID: sessionID,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Run loops until the context is done or the adapter signals completion.
func (l *Loop) Run(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		req, err := l.pollNext(ctx)
		if err != nil {
			if errors.Is(err, ErrSessionGone) {
				return nil
			}
			if isTransient(err) {
				slog.WarnContext(ctx, "transient error polling next turn", "error", err)
				continue
			}
			return fmt.Errorf("poll next turn: %w", err)
		}

		resp, err := l.adapter.Run(ctx, req)
		if err != nil {
			return fmt.Errorf("adapter run: %w", err)
		}

		if err := l.postTurn(ctx, resp); err != nil {
			if isTransient(err) {
				slog.WarnContext(ctx, "transient error posting turn", "error", err)
				continue
			}
			return fmt.Errorf("post turn: %w", err)
		}

		if resp.Done {
			return nil
		}
	}
}

func (l *Loop) pollNext(ctx context.Context) (contract.TurnRequest, error) {
	url := fmt.Sprintf("%s/sessions/%s/next?as=%s", l.brokerURL, l.sessionID, l.role)
	//nolint:gosec // local broker URL
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return contract.TurnRequest{}, fmt.Errorf("build request: %w", err)
	}

	resp, err := l.client.Do(httpReq)
	if err != nil {
		return contract.TurnRequest{}, fmt.Errorf("do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusGone {
		return contract.TurnRequest{}, ErrSessionGone
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return contract.TurnRequest{}, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var turnReq contract.TurnRequest
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			data := line[len("data: "):]
			if err := json.Unmarshal([]byte(data), &turnReq); err != nil {
				return contract.TurnRequest{}, fmt.Errorf("unmarshal turn request: %w", err)
			}
			return turnReq, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return contract.TurnRequest{}, fmt.Errorf("read sse: %w", err)
	}
	return contract.TurnRequest{}, fmt.Errorf("no data event in sse")
}

func (l *Loop) postTurn(ctx context.Context, resp contract.TurnResponse) error {
	url := fmt.Sprintf("%s/sessions/%s/turn", l.brokerURL, l.sessionID)
	body, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal turn response: %w", err)
	}

	//nolint:gosec // local broker URL
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	res, err := l.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(res.Body)
		return fmt.Errorf("unexpected status %d: %s", res.StatusCode, string(data))
	}
	return nil
}

func isTransient(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}
