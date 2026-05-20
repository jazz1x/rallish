package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

func TestSplitParticipants(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", []string{}},
		{"alice,bob", []string{"alice", "bob"}},
		{"alice, bob ,carol", []string{"alice", "bob", "carol"}},
		{",,alice,,", []string{"alice"}},
		{"   ", []string{}},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := splitParticipants(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("len(%v)=%d want %d", got, len(got), len(c.want))
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("[%d]=%q want %q", i, got[i], c.want[i])
				}
			}
		})
	}
}

func TestNameRegex(t *testing.T) {
	cases := []struct {
		name string
		ok   bool
	}{
		{"alice", true},
		{"bob_1", true},
		{"a-b-c", true},
		{"AB123", true},
		{"sixteenchars1234", true},
		{"seventeenchars123", false},
		{"", false},
		{"with space", false},
		{"with.dot", false},
		{"한글", false},
		{"alice/bob", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := contract.NewParticipantName(c.name)
			got := err == nil
			if got != c.ok {
				t.Fatalf("contract.NewParticipantName(%q) ok=%v want %v", c.name, got, c.ok)
			}
		})
	}
}

// TestRallyNewClientSideValidation verifies the new command rejects bad
// participant lists before hitting the broker.
func TestRallyNewClientSideValidation(t *testing.T) {
	t.Run("less than two participants", func(t *testing.T) {
		err := runRallyNew(context.Background(), t.TempDir(), "alice", "", "", "", &bytes.Buffer{})
		if err == nil || !errors.Is(err, contract.ErrTooFewParticipants) {
			t.Fatalf("expected ErrTooFewParticipants, got %v", err)
		}
	})
	t.Run("invalid name rejected", func(t *testing.T) {
		err := runRallyNew(context.Background(), t.TempDir(), "alice,bad name", "", "", "", &bytes.Buffer{})
		if err == nil || !errors.Is(err, contract.ErrInvalidName) {
			t.Fatalf("expected ErrInvalidName, got %v", err)
		}
	})
}

// TestRallyNewHappyPath wires runRallyNew against an httptest broker stub and
// confirms it prints the session id returned by the broker.
func TestRallyNewHappyPath(t *testing.T) {
	want := contract.RallySession{ID: "rly_1000000000000_abcd"}

	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rally/sessions" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("Content-Type = %q want application/json", ct)
		}
		var req contract.NewRallyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if len(req.Participants) != 2 || req.Participants[0] != "alice" || req.Participants[1] != "bob" {
			t.Fatalf("participants = %v want [alice bob]", req.Participants)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(want)
	}))
	t.Cleanup(stub.Close)

	homeDir := t.TempDir()
	if err := writePortFile(homeDir, stub.URL); err != nil {
		t.Fatalf("write port file: %v", err)
	}

	var out bytes.Buffer
	if err := runRallyNew(context.Background(), homeDir, "alice,bob", "", "smoke", "", &out); err != nil {
		t.Fatalf("runRallyNew: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != want.ID {
		t.Fatalf("stdout = %q want %q", got, want.ID)
	}
}

// TestRallyDoneNonHolder verifies the CLI surfaces the broker's 409 message
// and exits with an error.
func TestRallyDoneNonHolder(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"not your turn: holder is alice"}`))
	}))
	t.Cleanup(stub.Close)

	homeDir := t.TempDir()
	if err := writePortFile(homeDir, stub.URL); err != nil {
		t.Fatalf("write port file: %v", err)
	}

	err := runRallyDone(context.Background(), homeDir, "rly_1000000000000_abcd", "bob", "", "", &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for 409, got nil")
	}
	if !strings.Contains(err.Error(), "not your turn") {
		t.Fatalf("error should surface broker message, got: %v", err)
	}
}

// writePortFile drops a ~/.rallish/port file pointing at the httptest server
// URL so resolveBrokerClient falls back to TCP.
func writePortFile(homeDir, serverURL string) error {
	idx := strings.LastIndex(serverURL, ":")
	if idx < 0 {
		return nil
	}
	port := serverURL[idx+1:]
	dir := filepath.Join(homeDir, ".rallish")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "port"), []byte(port), 0o600)
}

// TestRallyJoinReceivesBaton tests that runRallyJoin prints a "your turn" cue
// line when the stub broker emits a single SSE baton event, and returns once
// the stream closes.
func TestRallyJoinReceivesBaton(t *testing.T) {
	evt := contract.BatonEvent{TurnN: 1, From: "start", Note: "begin"}
	evtJSON, err := json.Marshal(evt)
	if err != nil {
		t.Fatal(err)
	}

	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/rally/sessions/") {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		_, _ = w.Write([]byte("data: " + string(evtJSON) + "\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Close stream immediately — simulates broker ending the SSE.
	}))
	t.Cleanup(stub.Close)

	homeDir := t.TempDir()
	if err := writePortFile(homeDir, stub.URL); err != nil {
		t.Fatalf("write port file: %v", err)
	}

	var out bytes.Buffer
	err = runRallyJoin(context.Background(), homeDir, "rly_1000000000001_abcd", "alice", false, 0, &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("runRallyJoin returned error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "your turn") {
		t.Fatalf("expected 'your turn' in output, got: %q", output)
	}
}

// TestRallyStatusFormatting checks that runRallyStatus prints participant names
// and history entries from a stub broker.
func TestRallyStatusFormatting(t *testing.T) {
	sess := contract.RallySession{
		ID:           "rly_1000000000002_abcd",
		Status:       contract.RallyState("alice_turn"),
		Holder:       "alice",
		TurnN:        2,
		Participants: []string{"alice", "bob"},
		LastSeen:     map[string]int64{},
		History: []contract.BatonHandoff{
			{From: "alice", To: "bob", TurnN: 1, Note: "first pass", At: 0},
		},
	}

	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sess)
	}))
	t.Cleanup(stub.Close)

	homeDir := t.TempDir()
	if err := writePortFile(homeDir, stub.URL); err != nil {
		t.Fatalf("write port file: %v", err)
	}

	var out bytes.Buffer
	err := runRallyStatus(context.Background(), homeDir, sess.ID, &out)
	if err != nil {
		t.Fatalf("runRallyStatus: %v", err)
	}

	output := out.String()
	for _, want := range []string{"alice", "bob", "first pass", "History"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in status output; got:\n%s", want, output)
		}
	}
}

// TestRallyDoneHappyPath verifies that runRallyDone prints the expected
// "ok — baton passed to <next>" line when the broker returns 200.
func TestRallyDoneHappyPath(t *testing.T) {
	nextSess := contract.RallySession{
		ID:     "rly_1000000000003_abcd",
		Holder: "bob",
		TurnN:  2,
		Status: contract.RallyState("bob_turn"),
	}

	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(nextSess)
	}))
	t.Cleanup(stub.Close)

	homeDir := t.TempDir()
	if err := writePortFile(homeDir, stub.URL); err != nil {
		t.Fatalf("write port file: %v", err)
	}

	var out bytes.Buffer
	err := runRallyDone(context.Background(), homeDir, nextSess.ID, "alice", "done note", "bob", &out)
	if err != nil {
		t.Fatalf("runRallyDone: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "baton passed to bob") {
		t.Fatalf("expected 'baton passed to bob' in output, got: %q", output)
	}
}

// TestRallyNewWithRepo tests that runRallyNew validates a --repo path and
// sends it to the broker.
func TestRallyNewWithRepo(t *testing.T) {
	// Use a real temp directory so Stat succeeds.
	repoDir := t.TempDir()

	var capturedRepo string
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req contract.NewRallyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		capturedRepo = req.Repo
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(contract.RallySession{ID: "rly_test_0004"})
	}))
	t.Cleanup(stub.Close)

	homeDir := t.TempDir()
	if err := writePortFile(homeDir, stub.URL); err != nil {
		t.Fatalf("write port file: %v", err)
	}

	var out bytes.Buffer
	if err := runRallyNew(context.Background(), homeDir, "alice,bob", repoDir, "", "", &out); err != nil {
		t.Fatalf("runRallyNew with valid repo: %v", err)
	}
	if capturedRepo != repoDir {
		t.Fatalf("broker received repo %q, want %q", capturedRepo, repoDir)
	}
}

// TestRallyNewWithBadRepo tests that runRallyNew rejects a non-existent repo path.
func TestRallyNewWithBadRepo(t *testing.T) {
	homeDir := t.TempDir()
	err := runRallyNew(context.Background(), homeDir, "alice,bob", "/definitely/does/not/exist/12345", "", "", &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for non-existent repo, got nil")
	}
}

// TestRallyCmdBuilders exercises the Cobra command constructors to ensure they
// build without panicking and register the expected subcommands / flags.
func TestRallyCmdBuilders(t *testing.T) {
	cmd := RallyCmd()
	if cmd.Use != "rally" {
		t.Fatalf("RallyCmd Use = %q, want rally", cmd.Use)
	}

	newCmd := RallyNewCmd()
	if newCmd.Use != "new" {
		t.Fatalf("RallyNewCmd Use = %q, want new", newCmd.Use)
	}
	if newCmd.Flags().Lookup("participants") == nil {
		t.Fatal("RallyNewCmd missing --participants flag")
	}
	if newCmd.Flags().Lookup("repo") == nil {
		t.Fatal("RallyNewCmd missing --repo flag")
	}

	joinCmd := RallyJoinCmd()
	if joinCmd.Use != "join" {
		t.Fatalf("RallyJoinCmd Use = %q, want join", joinCmd.Use)
	}
	if joinCmd.Flags().Lookup("session-id") == nil {
		t.Fatal("RallyJoinCmd missing --session-id flag")
	}

	doneCmd := RallyDoneCmd()
	if doneCmd.Use != "done" {
		t.Fatalf("RallyDoneCmd Use = %q, want done", doneCmd.Use)
	}

	statusCmd := RallyStatusCmd()
	if statusCmd.Use != "status" {
		t.Fatalf("RallyStatusCmd Use = %q, want status", statusCmd.Use)
	}
}

// TestHolderDisplayEdgeCases covers holderDisplay for the empty and non-empty cases.
func TestHolderDisplayEdgeCases(t *testing.T) {
	if got := holderDisplay(""); got != "(none)" {
		t.Fatalf("holderDisplay(\"\") = %q, want \"(none)\"", got)
	}
	if got := holderDisplay("alice"); got != "alice" {
		t.Fatalf("holderDisplay(\"alice\") = %q, want \"alice\"", got)
	}
}

// TestRallyNewWithFirstFlag verifies that runRallyNew sends first_holder in the
// POST body when --first is provided.
func TestRallyNewWithFirstFlag(t *testing.T) {
	var capturedFirstHolder string
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rally/sessions" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var req contract.NewRallyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		capturedFirstHolder = req.FirstHolder
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(contract.RallySession{ID: "rly_test_first"})
	}))
	t.Cleanup(stub.Close)

	homeDir := t.TempDir()
	if err := writePortFile(homeDir, stub.URL); err != nil {
		t.Fatalf("write port file: %v", err)
	}

	var out bytes.Buffer
	if err := runRallyNew(context.Background(), homeDir, "server,returner", "", "", "server", &out); err != nil {
		t.Fatalf("runRallyNew: %v", err)
	}
	if capturedFirstHolder != "server" {
		t.Fatalf("broker received first_holder=%q, want %q", capturedFirstHolder, "server")
	}
}

// TestRallyJoinOnce verifies that --once causes runRallyJoin to return nil after
// the first baton event, with stdout containing "your turn".
func TestRallyJoinOnce(t *testing.T) {
	evt := contract.BatonEvent{TurnN: 1, From: "start", Note: "x"}
	evtJSON, err := json.Marshal(evt)
	if err != nil {
		t.Fatal(err)
	}

	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		_, _ = w.Write([]byte("data: " + string(evtJSON) + "\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Keep connection open — --once should exit before we close.
		<-r.Context().Done()
	}))
	t.Cleanup(stub.Close)

	homeDir := t.TempDir()
	if err := writePortFile(homeDir, stub.URL); err != nil {
		t.Fatalf("write port file: %v", err)
	}

	var out bytes.Buffer
	err = runRallyJoin(context.Background(), homeDir, "rly_1000000000004_abcd", "server", true, 0, &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("runRallyJoin --once returned error: %v", err)
	}
	if !strings.Contains(out.String(), "your turn") {
		t.Fatalf("expected 'your turn' in output, got: %q", out.String())
	}
}

// TestRallyJoinTimeout verifies that --timeout causes runRallyJoin to return
// ErrTimeoutWaitingForBaton when no event arrives within the deadline,
// and that stderr contains "no baton within".
func TestRallyJoinTimeout(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Never send any event — just block until the client disconnects.
		<-r.Context().Done()
		// Drain so the handler exits cleanly.
		_, _ = io.Copy(io.Discard, r.Body)
	}))
	t.Cleanup(stub.Close)

	homeDir := t.TempDir()
	if err := writePortFile(homeDir, stub.URL); err != nil {
		t.Fatalf("write port file: %v", err)
	}

	var out, errBuf bytes.Buffer
	err := runRallyJoin(context.Background(), homeDir, "rly_1000000000005_abcd", "server", true, 100*time.Millisecond, &out, &errBuf)
	if !errors.Is(err, ErrTimeoutWaitingForBaton) {
		t.Fatalf("expected ErrTimeoutWaitingForBaton, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "no baton within") {
		t.Fatalf("expected 'no baton within' in stderr, got: %q", errBuf.String())
	}
}
