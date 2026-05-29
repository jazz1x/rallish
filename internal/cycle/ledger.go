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

// Append writes one ledger event to the end of the file, linking it into the
// tamper-evidence hash chain (G4): it reads the previous entry's Hash from the
// last line on disk (LedgerGenesisHash when the ledger is empty), records it as
// the new entry's PrevHash, and computes the new entry's Hash over its canonical
// form. Single-writer append-only ordering already holds, so the on-disk tail is
// the authoritative predecessor.
func (s *LedgerFileSync) Append(entry contract.HarnessLedgerEntry) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("mkdir ledger: %w", err)
	}

	prevHash, err := s.lastHash()
	if err != nil {
		return err
	}
	entry.PrevHash = prevHash
	hash, err := contract.ChainHash(entry, prevHash)
	if err != nil {
		return err
	}
	entry.Hash = hash

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

// lastHash returns the Hash of the final entry on disk, or LedgerGenesisHash
// when the ledger does not exist or is empty. It is the predecessor link for the
// next appended entry.
func (s *LedgerFileSync) lastHash() (string, error) {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return contract.LedgerGenesisHash, nil
		}
		return "", fmt.Errorf("open ledger: %w", err)
	}
	defer func() { _ = f.Close() }()

	last := contract.LedgerGenesisHash
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry contract.HarnessLedgerEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return "", fmt.Errorf("unmarshal ledger entry: %w", err)
		}
		last = entry.Hash
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan ledger: %w", err)
	}
	return last, nil
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
