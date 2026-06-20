package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jazz1x/rallish/pkg/contract"
)

// TestRallyDoneInterruptedSession verifies that runRallyDone distinguishes a
// session-interrupted 409 from a "not your turn" 409.
func TestRallyDoneInterruptedSession(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`session "rly_1000000000000_abcd": ` + contract.ErrSessionInterrupted.Error()))
	}))
	t.Cleanup(stub.Close)

	homeDir := t.TempDir()
	if err := writePortFile(homeDir, stub.URL); err != nil {
		t.Fatalf("write port file: %v", err)
	}

	var out bytes.Buffer
	err := runRallyDone(context.Background(), homeDir, "rly_1000000000000_abcd", "alice", "", "", &out)
	if err == nil {
		t.Fatal("expected error for 409, got nil")
	}
	if !strings.Contains(err.Error(), "session interrupted") {
		t.Fatalf("error should indicate session interrupted, got: %v", err)
	}
	if strings.Contains(out.String(), "not your turn") {
		t.Fatalf("output should not say 'not your turn' for an interrupted session; got: %q", out.String())
	}
}

// TestRallyJoinInterruptedSession verifies that runRallyJoin on an interrupted
// session prints "session closed" and returns nil when the broker emits the
// close sentinel.
func TestRallyJoinInterruptedSession(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		_, _ = w.Write([]byte("data: {\"closed\":true}\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
	}))
	t.Cleanup(stub.Close)

	homeDir := t.TempDir()
	if err := writePortFile(homeDir, stub.URL); err != nil {
		t.Fatalf("write port file: %v", err)
	}

	var out bytes.Buffer
	err := runRallyJoin(context.Background(), homeDir, "rly_1000000000000_abcd", "alice", false, 0, &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("runRallyJoin on interrupted session returned error: %v", err)
	}
	if !strings.Contains(out.String(), "session closed") {
		t.Fatalf("expected 'session closed' in output, got: %q", out.String())
	}
}

// TestRallyDoneHandoffToNotMember verifies that runRallyDone surfaces a 400
// handoff_to-not-member broker error.
func TestRallyDoneHandoffToNotMember(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`handoff_to "eve": ` + contract.ErrHandoffToNotMember.Error()))
	}))
	t.Cleanup(stub.Close)

	homeDir := t.TempDir()
	if err := writePortFile(homeDir, stub.URL); err != nil {
		t.Fatalf("write port file: %v", err)
	}

	var out bytes.Buffer
	err := runRallyDone(context.Background(), homeDir, "rly_1000000000000_abcd", "alice", "", "eve", &out)
	if err == nil {
		t.Fatal("expected error for 400, got nil")
	}
	if !strings.Contains(err.Error(), contract.ErrHandoffToNotMember.Error()) {
		t.Fatalf("error should surface broker message, got: %v", err)
	}
}
