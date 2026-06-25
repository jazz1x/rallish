// Package session manages conversation state, jsonl append-only logs, and log replay.
package session

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jazz1x/rallish/pkg/contract"
)

// Clock abstracts time for testability.
type Clock interface {
	Now() time.Time
}

// Session represents the in-memory state of a rallish session.
type Session struct {
	ID         string        `json:"id"`
	PresetName string        `json:"preset_name"`
	Status     string        `json:"status"`
	CreatedAt  time.Time     `json:"created_at"`
	TurnCount  int           `json:"turn_count"`
	Task       contract.Task `json:"task"`
}

// TurnRecord is a single entry in the jsonl log.
type TurnRecord struct {
	Turn int                   `json:"turn"`
	Time time.Time             `json:"time"`
	Req  contract.TurnRequest  `json:"req"`
	Resp contract.TurnResponse `json:"resp"`
}

// Store owns jsonl append-only logs and in-memory session state.
type Store struct {
	dir    string
	clock  Clock
	nextID atomic.Int64

	mu       sync.Mutex
	sessions map[string]*sessionEntry
}

type sessionEntry struct {
	session Session
	mu      sync.Mutex
}

// NewStore creates a new Store under dir.
func NewStore(dir string, clock Clock) (*Store, error) {
	if clock == nil {
		return nil, errors.New("clock is required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create session dir: %w", err)
	}
	return &Store{
		dir:      dir,
		clock:    clock,
		sessions: make(map[string]*sessionEntry),
	}, nil
}

// Create creates a new session directory, writes meta.json, and returns the Session.
func (s *Store) Create(ctx context.Context, preset contract.Preset, task contract.Task) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	id := s.generateID()
	if err := validateSessionID(id); err != nil {
		return Session{}, fmt.Errorf("invalid session id: %w", err)
	}

	sessionDir := filepath.Join(s.dir, id)
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		return Session{}, fmt.Errorf("create session dir: %w", err)
	}

	session := Session{
		ID:         id,
		PresetName: preset.Name,
		Status:     "active",
		CreatedAt:  s.clock.Now(),
		TurnCount:  0,
		Task:       task,
	}

	metaPath := filepath.Join(sessionDir, "meta.json")
	metaFile, err := os.OpenFile(metaPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // path validated as subpath of store dir
	if err != nil {
		return Session{}, fmt.Errorf("create meta.json: %w", err)
	}
	if err := json.NewEncoder(metaFile).Encode(session); err != nil {
		_ = metaFile.Close()
		return Session{}, fmt.Errorf("encode meta.json: %w", err)
	}
	if err := metaFile.Close(); err != nil {
		return Session{}, fmt.Errorf("close meta.json: %w", err)
	}

	logPath := filepath.Join(sessionDir, "log.jsonl")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // path validated as subpath of store dir
	if err != nil {
		return Session{}, fmt.Errorf("create log.jsonl: %w", err)
	}
	_ = logFile.Close()

	s.mu.Lock()
	s.sessions[id] = &sessionEntry{session: session}
	s.mu.Unlock()

	return session, nil
}

// Append appends one line to log.jsonl. Safe for concurrent calls on different sessions.
func (s *Store) Append(ctx context.Context, sessionID string, req contract.TurnRequest, resp contract.TurnResponse) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateSessionID(sessionID); err != nil {
		return fmt.Errorf("invalid session id: %w", err)
	}

	s.mu.Lock()
	entry, ok := s.sessions[sessionID]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()

	sessionDir := filepath.Join(s.dir, sessionID)
	logPath := filepath.Join(sessionDir, "log.jsonl")

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // path validated as subpath of store dir
	if err != nil {
		return fmt.Errorf("open log.jsonl: %w", err)
	}
	defer func() { _ = f.Close() }()

	record := TurnRecord{
		Turn: req.Turn,
		Time: s.clock.Now(),
		Req:  req,
		Resp: resp,
	}

	w := bufio.NewWriter(f)
	if err := json.NewEncoder(w).Encode(record); err != nil {
		return fmt.Errorf("encode turn record: %w", err)
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("flush log.jsonl: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("fsync log.jsonl: %w", err)
	}

	entry.session.TurnCount++
	return nil
}

// Get returns the current Session state.
func (s *Store) Get(ctx context.Context, sessionID string) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	if err := validateSessionID(sessionID); err != nil {
		return Session{}, fmt.Errorf("invalid session id: %w", err)
	}

	s.mu.Lock()
	entry, ok := s.sessions[sessionID]
	s.mu.Unlock()
	if !ok {
		return Session{}, fmt.Errorf("session %s not found", sessionID)
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()
	return entry.session, nil
}

// SessionDir returns the directory where the session's files are persisted.
func (s *Store) SessionDir(sessionID string) string {
	return filepath.Join(s.dir, sessionID)
}

// Replay replays log.jsonl into a slice of TurnRecords.
func (s *Store) Replay(ctx context.Context, sessionID string) ([]TurnRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateSessionID(sessionID); err != nil {
		return nil, fmt.Errorf("invalid session id: %w", err)
	}

	s.mu.Lock()
	_, ok := s.sessions[sessionID]
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	sessionDir := filepath.Join(s.dir, sessionID)
	logPath := filepath.Join(sessionDir, "log.jsonl")

	f, err := os.Open(logPath) //nolint:gosec // path validated as subpath of store dir
	if err != nil {
		return nil, fmt.Errorf("open log.jsonl: %w", err)
	}
	defer func() { _ = f.Close() }()

	// bufio.Reader (not Scanner): a TurnRecord carries unbounded agent output, so a
	// single JSONL line routinely exceeds Scanner's 64 KiB default token cap, which
	// would silently truncate the replay mid-log. ReadBytes grows without a fixed cap.
	var records []TurnRecord
	reader := bufio.NewReader(f)
	for {
		line, readErr := reader.ReadBytes('\n')
		if trimmed := bytesTrimNewline(line); len(trimmed) > 0 {
			var rec TurnRecord
			if err := json.Unmarshal(trimmed, &rec); err != nil {
				return nil, fmt.Errorf("decode turn record: %w", err)
			}
			records = append(records, rec)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, fmt.Errorf("read log.jsonl: %w", readErr)
		}
	}
	return records, nil
}

// bytesTrimNewline drops a single trailing '\n' (and any '\r') from a line read
// by bufio.Reader.ReadBytes, which includes the delimiter.
func bytesTrimNewline(b []byte) []byte {
	n := len(b)
	if n > 0 && b[n-1] == '\n' {
		b = b[:n-1]
		n--
	}
	if n > 0 && b[n-1] == '\r' {
		b = b[:n-1]
	}
	return b
}

func (s *Store) generateID() string {
	return fmt.Sprintf("%d-%d", s.clock.Now().UnixNano(), s.nextID.Add(1))
}

func validateSessionID(id string) error {
	if id == "" {
		return errors.New("session ID is required")
	}
	if strings.Contains(id, "..") || strings.Contains(id, "/") || strings.Contains(id, "\\") {
		return errors.New("invalid session ID")
	}
	return nil
}
