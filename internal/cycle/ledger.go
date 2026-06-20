package cycle

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/jazz1x/rallish/pkg/contract"
)

// ledgerLocks serializes the read-modify-write of the hash chain PER LEDGER FILE
// (keyed by absolute path), not per LedgerFileSync instance. The broker creates a
// fresh LedgerFileSync on every appendLedger call, and an async orchestrate
// goroutine can race a step handler writing the SAME cycle ledger; an
// instance-level mutex would not see the other instance. Without this, two
// concurrent Append calls both read the same on-disk tail as PrevHash and fork
// the chain (VerifyChain then fails). A process-wide registry keyed by path is
// the SSOT lock for a given ledger.
var ledgerLocks sync.Map // map[string]*sync.Mutex

func lockForLedger(path string) *sync.Mutex {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path // best-effort key; a relative path still locks consistently within a process
	}
	m, _ := ledgerLocks.LoadOrStore(abs, &sync.Mutex{})
	return m.(*sync.Mutex)
}

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
	// Serialize the whole read-modify-write against any other writer of the SAME
	// ledger file so prev_hash → hash → append is atomic and the chain cannot fork.
	lock := lockForLedger(s.path)
	lock.Lock()
	defer lock.Unlock()

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
	err = forEachLedgerLine(f, func(line []byte) error {
		var entry contract.HarnessLedgerEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return fmt.Errorf("unmarshal ledger entry: %w", err)
		}
		last = entry.Hash
		return nil
	})
	if err != nil {
		return "", err
	}
	return last, nil
}

// forEachLedgerLine streams the JSONL file line-by-line with NO fixed line-size
// cap. bufio.Scanner has a 64 KiB default token limit that silently bricks the
// whole chain on one oversized entry (a gate report with large stdout/violations
// can exceed it); bufio.Reader.ReadBytes grows unbounded, so the audit layer is
// not brittle to entry size. Empty lines are skipped.
func forEachLedgerLine(r io.Reader, fn func(line []byte) error) error {
	reader := bufio.NewReader(r)
	for {
		line, err := reader.ReadBytes('\n')
		trimmed := line
		// Drop the trailing newline (ReadBytes includes it except possibly at EOF).
		if n := len(trimmed); n > 0 && trimmed[n-1] == '\n' {
			trimmed = trimmed[:n-1]
		}
		if len(trimmed) > 0 {
			if ferr := fn(trimmed); ferr != nil {
				return ferr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("read ledger: %w", err)
		}
	}
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
	err = forEachLedgerLine(f, func(line []byte) error {
		var entry contract.HarnessLedgerEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return fmt.Errorf("unmarshal ledger entry: %w", err)
		}
		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// Path returns the configured ledger file path.
func (s *LedgerFileSync) Path() string { return s.path }
