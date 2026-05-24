package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
