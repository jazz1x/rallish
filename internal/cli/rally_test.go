package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
			got := nameRe.MatchString(c.name)
			if got != c.ok {
				t.Fatalf("nameRe.MatchString(%q) = %v want %v", c.name, got, c.ok)
			}
		})
	}
}

// TestRallyNewClientSideValidation verifies the new command rejects bad
// participant lists before hitting the broker.
func TestRallyNewClientSideValidation(t *testing.T) {
	t.Run("less than two participants", func(t *testing.T) {
		err := runRallyNew(context.Background(), t.TempDir(), "alice", "", "", &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "at least 2") {
			t.Fatalf("expected at-least-2 error, got %v", err)
		}
	})
	t.Run("invalid name rejected", func(t *testing.T) {
		err := runRallyNew(context.Background(), t.TempDir(), "alice,bad name", "", "", &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "invalid participant name") {
			t.Fatalf("expected invalid-name error, got %v", err)
		}
	})
}

// TestRallyNewHappyPath wires runRallyNew against an httptest broker stub and
// confirms it prints the session id returned by the broker.
func TestRallyNewHappyPath(t *testing.T) {
	want := contract.RallySession{ID: "rly_test_0001"}

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
	if err := runRallyNew(context.Background(), homeDir, "alice,bob", "", "smoke", &out); err != nil {
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

	err := runRallyDone(context.Background(), homeDir, "rly_x", "bob", "", "", &bytes.Buffer{})
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
