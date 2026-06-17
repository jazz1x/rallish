package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jazz1x/rallish/pkg/contract"
)

func TestRunCycleWatch_SSEParsing(t *testing.T) {
	// Build an SSE stream with two events then a halt.
	evt1 := contract.CycleEvent{CycleID: "cyc_1", Phase: contract.CyclePhaseAudit, CompletedCycles: 0, Halted: false, At: time.Now().UnixMilli()}
	evt2 := contract.CycleEvent{CycleID: "cyc_1", Phase: contract.CyclePhaseCommit, CompletedCycles: 1, Halted: false, At: time.Now().UnixMilli()}
	evt3 := contract.CycleEvent{CycleID: "cyc_1", Phase: contract.CyclePhaseHalted, CompletedCycles: 1, Halted: true, HaltReason: contract.HaltSuccess, At: time.Now().UnixMilli()}

	b1, _ := json.Marshal(evt1)
	b2, _ := json.Marshal(evt2)
	b3, _ := json.Marshal(evt3)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { //nolint:revive // standard handler signature
		if r.URL.Path != "/cycles/cyc_1/events" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b1)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b2)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b3)
	}))
	defer ts.Close()

	home := t.TempDir()
	if err := writePortFile(home, ts.URL); err != nil {
		t.Fatalf("writePortFile: %v", err)
	}

	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := runCycleWatch(ctx, home, "cyc_1", "", &out); err != nil {
		t.Fatalf("runCycleWatch: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "phase=audit") {
		t.Errorf("expected audit phase in output, got:\n%s", got)
	}
	if !strings.Contains(got, "phase=commit") {
		t.Errorf("expected commit phase in output, got:\n%s", got)
	}
	if !strings.Contains(got, "halted=true") {
		t.Errorf("expected halted=true in output, got:\n%s", got)
	}
	if !strings.Contains(got, "halt_reason: success") {
		t.Errorf("expected halt_reason success in output, got:\n%s", got)
	}
}

func TestRunCycleWatch_WithLogFile(t *testing.T) {
	evt := contract.CycleEvent{CycleID: "cyc_2", Phase: contract.CyclePhaseHalted, CompletedCycles: 2, Halted: true, HaltReason: contract.HaltMaxCyclesReached, At: time.Now().UnixMilli()}
	b, _ := json.Marshal(evt)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { //nolint:revive // standard handler signature
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
	}))
	defer ts.Close()

	home := t.TempDir()
	if err := writePortFile(home, ts.URL); err != nil {
		t.Fatalf("writePortFile: %v", err)
	}

	logFile := filepath.Join(home, "watch.log")
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := runCycleWatch(ctx, home, "cyc_2", logFile, &out); err != nil {
		t.Fatalf("runCycleWatch: %v", err)
	}

	// Verify log file was written.
	data, err := os.ReadFile(logFile) //nolint:gosec // test temp file
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(data), "halted=true") {
		t.Errorf("expected halted=true in log file, got:\n%s", string(data))
	}
	if !strings.Contains(string(data), "halt_reason: max-cycles-reached") {
		t.Errorf("expected halt_reason in log file, got:\n%s", string(data))
	}
}

func TestRunCycleWatch_NonOKStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { //nolint:revive // standard handler signature
		http.Error(w, "cycle not found", http.StatusNotFound)
	}))
	defer ts.Close()

	home := t.TempDir()
	if err := writePortFile(home, ts.URL); err != nil {
		t.Fatalf("writePortFile: %v", err)
	}

	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := runCycleWatch(ctx, home, "missing", "", &out)
	if err == nil {
		t.Fatal("expected error for 404 status")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 in error, got: %v", err)
	}
}

func TestRunCycleNewSendsLocalGates(t *testing.T) {
	var got contract.NewCycleRequest
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { //nolint:revive // standard handler signature
		if r.URL.Path != "/cycles" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(contract.CycleState{
			ID:            "cyc_local_gate",
			Branch:        got.Branch,
			MaxCycles:     got.MaxCycles,
			NextCycleGoal: got.Goal,
			LocalGates:    got.LocalGates,
		})
	}))
	defer ts.Close()

	home := t.TempDir()
	if err := writePortFile(home, ts.URL); err != nil {
		t.Fatalf("writePortFile: %v", err)
	}

	var out bytes.Buffer
	err := runCycleNew(
		context.Background(),
		home,
		"feat: local gate",
		"feat/local-gate",
		2,
		30,
		true,
		"",
		"",
		[]string{"bun test", "cargo clippy"},
		"",
		"",
		&out,
	)
	if err != nil {
		t.Fatalf("runCycleNew: %v", err)
	}

	want := []string{"bun test", "cargo clippy"}
	if len(got.LocalGates) != len(want) {
		t.Fatalf("local_gates = %v, want %v", got.LocalGates, want)
	}
	for i := range want {
		if got.LocalGates[i] != want[i] {
			t.Fatalf("local_gates = %v, want %v", got.LocalGates, want)
		}
	}
	gotOut := out.String()
	for _, want := range []string{"cycle created: cyc_local_gate", "  local_gates: 2", "    - bun test", "    - cargo clippy"} {
		if !strings.Contains(gotOut, want) {
			t.Fatalf("output missing %q:\n%s", want, gotOut)
		}
	}
}

func TestRunCycleStatusPrintsLocalGates(t *testing.T) {
	state := contract.CycleState{
		ID:              "cyc_status_local_gate",
		Phase:           contract.CyclePhasePreflight,
		CompletedCycles: 1,
		MaxCycles:       3,
		Branch:          "feat/local-gate",
		BaselineSHA:     "abc123",
		NextCycleGoal:   "feat: continue",
		LocalGates:      []string{"bun test", "cargo clippy"},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { //nolint:revive // standard handler signature
		if r.URL.Path != "/cycles/cyc_status_local_gate" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(state)
	}))
	defer ts.Close()

	home := t.TempDir()
	if err := writePortFile(home, ts.URL); err != nil {
		t.Fatalf("writePortFile: %v", err)
	}

	var out bytes.Buffer
	if err := runCycleStatus(context.Background(), home, "cyc_status_local_gate", &out); err != nil {
		t.Fatalf("runCycleStatus: %v", err)
	}

	got := out.String()
	for _, want := range []string{"local_gates:  2", "  - bun test", "  - cargo clippy"} {
		if !strings.Contains(got, want) {
			t.Fatalf("status output missing %q:\n%s", want, got)
		}
	}
}

func TestRunCycleStatusPrintsLedgerSummary(t *testing.T) {
	state := contract.CycleState{
		ID:              "cyc_status_ledger",
		Phase:           contract.CyclePhaseCommit,
		CompletedCycles: 1,
		MaxCycles:       3,
		Branch:          "feat/ledger",
		NextCycleGoal:   "feat: continue",
	}
	entries := []contract.HarnessLedgerEntry{
		contract.NewHarnessLedgerEntry(123, "cyc_status_ledger", contract.LedgerEventCycleCreated, "cycle created", nil),
		contract.NewHarnessLedgerEntry(456, "cyc_status_ledger", contract.LedgerEventGatePassed, "ok", nil),
	}
	entries[1].Gate = "make check-all"
	stateBody, _ := json.Marshal(state)
	ledgerBody, _ := json.Marshal(entries)

	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body []byte
		switch r.URL.Path {
		case "/cycles/cyc_status_ledger":
			body = stateBody
		case "/cycles/cyc_status_ledger/ledger":
			body = ledgerBody
		default:
			t.Fatalf("path = %s", r.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(body)),
			Request:    r,
		}, nil
	})}

	var out bytes.Buffer
	if err := runCycleStatusWithClient(context.Background(), brokerClient{URL: "http://rallish.local", Client: client}, "cyc_status_ledger", &out); err != nil {
		t.Fatalf("runCycleStatus: %v", err)
	}

	got := out.String()
	for _, want := range []string{"ledger:       2 entries", "ledger_last:  gate_passed gate=make check-all"} {
		if !strings.Contains(got, want) {
			t.Fatalf("status output missing %q:\n%s", want, got)
		}
	}
}

func TestRunCycleLedgerPrintsJSON(t *testing.T) {
	entries := []contract.HarnessLedgerEntry{
		contract.NewHarnessLedgerEntry(123, "cyc_ledger", contract.LedgerEventCycleCreated, "cycle created", []string{"a.go"}),
		contract.NewHarnessLedgerEntry(456, "cyc_ledger", contract.LedgerEventCycleHalted, "user-requested", nil),
	}
	body, _ := json.Marshal(entries)
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/cycles/cyc_ledger/ledger" {
			t.Fatalf("path = %s, want /cycles/cyc_ledger/ledger", r.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(body)),
			Request:    r,
		}, nil
	})}

	var out bytes.Buffer
	if err := runCycleLedgerWithClient(context.Background(), brokerClient{URL: "http://rallish.local", Client: client}, "cyc_ledger", &out); err != nil {
		t.Fatalf("runCycleLedger: %v", err)
	}

	var got []contract.HarnessLedgerEntry
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, out.String())
	}
	if len(got) != 2 {
		t.Fatalf("entries = %#v, want 2", got)
	}
	if got[0].Type != contract.LedgerEventCycleCreated || got[1].Type != contract.LedgerEventCycleHalted {
		t.Fatalf("entries = %#v", got)
	}
	if !strings.Contains(out.String(), "\n  {") {
		t.Fatalf("output is not pretty JSON:\n%s", out.String())
	}
}

// TestCycleNewRejectsBlankOverride verifies that an explicitly passed but
// empty/whitespace-only --audit-cmd or --polish-test-cmd fails loudly at
// `cycle new` time (README: "An empty or whitespace-only ... fails loudly").
// It also guards against false positives: an unset flag and a valid override
// must both be accepted and reach the broker.
func TestCycleNewRejectsBlankOverride(t *testing.T) {
	hit := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { //nolint:revive // standard handler signature
		hit = true
		var got contract.NewCycleRequest
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(contract.CycleState{
			ID:            "cyc_blank_override",
			Branch:        got.Branch,
			MaxCycles:     got.MaxCycles,
			NextCycleGoal: got.Goal,
			AuditCmd:      got.AuditCmd,
			PolishTestCmd: got.PolishTestCmd,
		})
	}))
	defer ts.Close()

	home := t.TempDir()
	if err := writePortFile(home, ts.URL); err != nil {
		t.Fatalf("writePortFile: %v", err)
	}
	t.Setenv("HOME", home)

	// rejected: explicitly passed blank overrides must error before any broker call.
	rejected := []struct {
		name string
		args []string
	}{
		{"audit empty", []string{"new", "--goal", "feat: x", "--branch", "feat/work", "--audit-cmd", ""}},
		{"audit whitespace", []string{"new", "--goal", "feat: x", "--branch", "feat/work", "--audit-cmd", "   "}},
		{"polish empty", []string{"new", "--goal", "feat: x", "--branch", "feat/work", "--polish-test-cmd", ""}},
		{"polish whitespace", []string{"new", "--goal", "feat: x", "--branch", "feat/work", "--polish-test-cmd", "\t "}},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			hit = false
			cmd := CycleCmd()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected error for blank override, got nil")
			}
			if !strings.Contains(err.Error(), "empty or whitespace-only") {
				t.Fatalf("error = %q, want 'empty or whitespace-only'", err.Error())
			}
			if hit {
				t.Fatalf("broker was called despite blank override; should fail before request")
			}
		})
	}

	// accepted (false-positive guards): unset and valid overrides must succeed.
	accepted := []struct {
		name string
		args []string
	}{
		{"unset uses default", []string{"new", "--goal", "feat: x", "--branch", "feat/work"}},
		{"valid audit override", []string{"new", "--goal", "feat: x", "--branch", "feat/work", "--audit-cmd", "npm test"}},
		{"valid polish override", []string{"new", "--goal", "feat: x", "--branch", "feat/work", "--polish-test-cmd", "cargo test"}},
	}
	for _, tc := range accepted {
		t.Run(tc.name, func(t *testing.T) {
			hit = false
			cmd := CycleCmd()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tc.args)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("unexpected error for valid args %v: %v", tc.args, err)
			}
			if !hit {
				t.Fatalf("broker was not called for valid args %v", tc.args)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
