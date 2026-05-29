package cycle

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jazz1x/rallish/pkg/contract"
)

// LedgerFileSync persists append-only harness ledger events as JSONL.
type LedgerFileSync struct {
	path string
}

// NewLedgerFileSync creates a ledger syncer for the given JSONL file path.
func NewLedgerFileSync(path string) *LedgerFileSync {
	return &LedgerFileSync{path: path}
}

// Append writes one ledger event to the end of the file.
func (s *LedgerFileSync) Append(entry contract.HarnessLedgerEntry) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("mkdir ledger: %w", err)
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal ledger entry: %w", err)
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open ledger: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write ledger entry: %w", err)
	}
	return nil
}

// ReadAll returns every ledger event in append order.
func (s *LedgerFileSync) ReadAll() ([]contract.HarnessLedgerEntry, error) {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open ledger: %w", err)
	}
	defer func() { _ = f.Close() }()

	var entries []contract.HarnessLedgerEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var entry contract.HarnessLedgerEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			return nil, fmt.Errorf("unmarshal ledger entry: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan ledger: %w", err)
	}
	return entries, nil
}

// Path returns the configured ledger file path.
func (s *LedgerFileSync) Path() string { return s.path }
