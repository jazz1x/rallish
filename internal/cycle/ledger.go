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

	"github.com/jazz1x/rallish/internal/safepath"
	"github.com/jazz1x/rallish/pkg/contract"
)

// ledgerLock serializes the read-modify-write of the hash chain PER LEDGER FILE
// (keyed by absolute path), not per LedgerFileSync instance. The broker creates a
// fresh LedgerFileSync on every appendLedger call, and an async orchestrate
// goroutine can race a step handler writing the SAME cycle ledger; an
// instance-level mutex would not see the other instance. Without this, two
// concurrent Append calls both read the same on-disk tail as PrevHash and fork
// the chain (VerifyChain then fails). A process-wide registry keyed by path is
// the in-process serialization point for a given ledger.
//
// It also caches the tail hash so appending does not re-scan the whole file
// (the `lastHash` O(n) risk in performance-spec.md §7).
//
// Scope and lifetime (intentional): the guarantee is IN-PROCESS only. The
// registry holds one entry per distinct ledger path for the lifetime of the
// process and never reclaims entries — a halted cycle may still be appended to
// (revival re-reads/writes the same path), so there is no point at which a given
// path is provably append-free and safe to Delete. Entries are tiny (one path
// string + a *sync.Mutex + a hash string) and cycle creation is low-frequency,
// so the resulting growth is bounded by the number of cycles a daemon ever runs
// (kilobytes over a long lifetime), not a hot path. Cross-process serialization
// is NOT provided: an in-memory mutex cannot coordinate a daemon and a separate
// `cycle run --once` writing the same ledger — that relies on
// single-active-writer-per-cycle across processes, and would need an OS file
// lock (flock) on the path if ever required.
type ledgerLock struct {
	mu       sync.Mutex
	lastHash string
}

var ledgerLocks sync.Map // map[string]*ledgerLock

func lockForLedger(path string) (*ledgerLock, error) {
	abs, err := safepath.Clean(path)
	if err != nil {
		abs = path // best-effort key; a relative path still locks consistently within a process
	}
	if v, ok := ledgerLocks.Load(abs); ok {
		return v.(*ledgerLock), nil
	}
	state := &ledgerLock{lastHash: contract.LedgerGenesisHash}
	last, err := readLastHash(abs)
	if err != nil {
		return nil, err
	}
	state.lastHash = last
	v, loaded := ledgerLocks.LoadOrStore(abs, state)
	if loaded {
		return v.(*ledgerLock), nil
	}
	return state, nil
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
	lock, err := lockForLedger(s.path)
	if err != nil {
		return err
	}
	lock.mu.Lock()
	defer lock.mu.Unlock()

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("mkdir ledger: %w", err)
	}

	prevHash := lock.lastHash
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
	lock.lastHash = hash
	return nil
}

// readLastHash returns the Hash of the final entry in the file at path, or
// LedgerGenesisHash when the ledger does not exist or is empty. It is used once
// per ledger path to initialize the process-wide tail-hash cache.
func readLastHash(path string) (string, error) {
	cleanPath, err := safepath.Clean(path)
	if err != nil {
		return "", fmt.Errorf("clean ledger path: %w", err)
	}
	f, err := os.Open(cleanPath) // #nosec G304 -- path is cleaned by safepath.Clean
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
