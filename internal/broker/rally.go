// Package broker provides the HTTP/SSE server and route handlers for the rallish broker.
//
// This file implements the rally-mode baton-passing endpoints.
package broker

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/jazz1x/rallish/pkg/contract"
)

// nameRe validates participant names per PRD §6.
var nameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,16}$`)

// rallyStore holds all active rally sessions keyed by session ID.
// It is protected by a single mutex; all state mutations must hold that lock.
type rallyStore struct {
	mu       sync.Mutex
	sessions map[string]*rallySession
}

// rallySession is the broker-internal state for a single rally session.
type rallySession struct {
	id           string
	participants []string        // ordered list
	repo         string
	task         string
	holder       string          // name of current baton holder
	turnN        int
	status       contract.RallyState
	lastSeen     map[string]int64
	joined       map[string]int64
	history      []contract.BatonHandoff
	createdAt    int64

	// streams maps participant name → channel that receives BatonEvents.
	// Nil channel means the participant is not currently connected.
	streams map[string]chan contract.BatonEvent

	// staleThreshold is how many milliseconds without a heartbeat before a
	// participant is considered stale. Defaults to 30 000 ms; overrideable
	// for tests.
	staleThreshold int64
}

// newRallyStore creates an empty store.
func newRallyStore() *rallyStore {
	return &rallyStore{sessions: make(map[string]*rallySession)}
}

// nextParticipant returns the next participant after `current` using round-robin
// over the ordered participants list. Returns "" if none found (shouldn't happen).
func (rs *rallySession) nextParticipant(current string, preferredNext string) string {
	if preferredNext != "" {
		for _, p := range rs.participants {
			if p == preferredNext {
				return p
			}
		}
	}
	n := len(rs.participants)
	for i, p := range rs.participants {
		if p == current {
			return rs.participants[(i+1)%n]
		}
	}
	// current not found — return first participant.
	return rs.participants[0]
}

// generateRallyID creates a session ID of the form rly_<unixmillis>_<rand4hex>.
func generateRallyID() (string, error) {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("rly_%d_%s", time.Now().UnixMilli(), hex.EncodeToString(b)), nil
}

// toPublic converts a rallySession to the public contract type for wire serialisation.
// Caller must hold the store lock.
func (rs *rallySession) toPublic(nowMS int64) contract.RallySession {
	lastSeen := make(map[string]int64, len(rs.lastSeen))
	for k, v := range rs.lastSeen {
		lastSeen[k] = v
	}

	history := make([]contract.BatonHandoff, len(rs.history))
	copy(history, rs.history)

	parts := make([]string, len(rs.participants))
	copy(parts, rs.participants)

	// Compute stale flags and append to LastSeen (already a copy).
	_ = nowMS // stale is advisory and exposed via the status response separately.

	return contract.RallySession{
		ID:           rs.id,
		Participants: parts,
		Repo:         rs.repo,
		Task:         rs.task,
		Holder:       rs.holder,
		TurnN:        rs.turnN,
		Status:       rs.status,
		LastSeen:     lastSeen,
		History:      history,
		CreatedAt:    rs.createdAt,
	}
}

// ---- server integration ----

// rallies is the process-global store. Kept separate from Server.mu to avoid
// nesting locks; all rally mutations use rallies.mu directly.
var rallies = newRallyStore()

// registerRallyRoutes wires the /rally/* routes into the broker mux.
func (s *Server) registerRallyRoutes() {
	s.mux.HandleFunc("POST /rally/sessions", s.handleRallyCreate)
	s.mux.HandleFunc("GET /rally/sessions/{id}/baton", s.handleRallyBaton)
	s.mux.HandleFunc("POST /rally/sessions/{id}/done", s.handleRallyDone)
	s.mux.HandleFunc("GET /rally/sessions/{id}", s.handleRallyStatus)
}

// requireJSON returns true and writes 415 if the Content-Type is not application/json.
func requireJSON(w http.ResponseWriter, r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	if ct != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return false
	}
	return true
}

// handleRallyCreate handles POST /rally/sessions.
func (s *Server) handleRallyCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := slog.With("handler", "rally_create")

	if !requireJSON(w, r) {
		return
	}

	var req contract.NewRallyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.ErrorContext(ctx, "decode request", "error", err)
		http.Error(w, fmt.Sprintf("bad request: %v", err), http.StatusBadRequest)
		return
	}

	// Validate participants.
	if len(req.Participants) < 2 {
		http.Error(w, "at least 2 participants required", http.StatusBadRequest)
		return
	}
	seen := make(map[string]bool)
	for _, name := range req.Participants {
		if !nameRe.MatchString(name) {
			http.Error(w, fmt.Sprintf("invalid participant name %q: must match ^[a-zA-Z0-9_-]{1,16}$", name), http.StatusBadRequest)
			return
		}
		if seen[name] {
			http.Error(w, fmt.Sprintf("duplicate participant name %q", name), http.StatusBadRequest)
			return
		}
		seen[name] = true
	}

	id, err := generateRallyID()
	if err != nil {
		logger.ErrorContext(ctx, "generate id", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	nowMS := time.Now().UnixMilli()
	sess := &rallySession{
		id:             id,
		participants:   req.Participants,
		repo:           req.Repo,
		task:           req.Task,
		status:         contract.RallyStateIdle,
		lastSeen:       make(map[string]int64),
		joined:         make(map[string]int64),
		history:        []contract.BatonHandoff{},
		createdAt:      nowMS,
		streams:        make(map[string]chan contract.BatonEvent),
		staleThreshold: 30_000,
	}

	rallies.mu.Lock()
	rallies.sessions[id] = sess
	rallies.mu.Unlock()

	logger.InfoContext(ctx, "rally_created", "session_id", id, "participants", req.Participants)

	pub := sess.toPublic(nowMS)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(pub); err != nil {
		logger.ErrorContext(ctx, "encode response", "error", err)
	}
}

// handleRallyBaton handles GET /rally/sessions/{id}/baton?as=<name> (SSE).
func (s *Server) handleRallyBaton(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := slog.With("handler", "rally_baton")

	id := r.PathValue("id")
	as := r.URL.Query().Get("as")
	if !nameRe.MatchString(as) {
		http.Error(w, fmt.Sprintf("invalid participant name %q: must match ^[a-zA-Z0-9_-]{1,16}$", as), http.StatusBadRequest)
		return
	}

	rallies.mu.Lock()
	sess, ok := rallies.sessions[id]
	if !ok {
		rallies.mu.Unlock()
		http.Error(w, "rally session not found", http.StatusNotFound)
		return
	}

	// Validate participant membership.
	isMember := false
	for _, p := range sess.participants {
		if p == as {
			isMember = true
			break
		}
	}
	if !isMember {
		rallies.mu.Unlock()
		http.Error(w, fmt.Sprintf("participant %q is not in this session", as), http.StatusForbidden)
		return
	}

	// Register stream channel (buffer=1 so sender never blocks).
	ch := make(chan contract.BatonEvent, 1)
	sess.streams[as] = ch
	nowMS := time.Now().UnixMilli()
	if sess.joined[as] == 0 {
		sess.joined[as] = nowMS
	}
	sess.lastSeen[as] = nowMS

	// If idle and this is the first participant in the ordered list, give them the baton.
	var immediateEvent *contract.BatonEvent
	if sess.status == contract.RallyStateIdle && sess.participants[0] == as {
		sess.turnN++
		sess.holder = as
		sess.status = contract.RallyTurnState(as)
		evt := contract.BatonEvent{TurnN: sess.turnN, From: ""}
		immediateEvent = &evt
	} else if sess.status == contract.RallyTurnState(as) {
		// Session already in this participant's turn (reconnect or late-join).
		// Derive "from"/"note" from the last history entry; sess.holder is
		// already `as` so we cannot read it there.
		var from, note string
		if n := len(sess.history); n > 0 {
			from = sess.history[n-1].From
			note = sess.history[n-1].Note
		}
		evt := contract.BatonEvent{TurnN: sess.turnN, From: from, Note: note}
		immediateEvent = &evt
	}
	rallies.mu.Unlock()

	// Flush SSE headers immediately.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	if err := rc.Flush(); err != nil {
		logger.ErrorContext(ctx, "initial flush", "error", err)
		return
	}

	// Deliver immediate baton event if applicable.
	if immediateEvent != nil {
		if err := writeSSEEvent(w, rc, *immediateEvent); err != nil {
			logger.ErrorContext(ctx, "write immediate baton event", "error", err)
			cleanupStream(id, as)
			return
		}
		// Drain channel if the sender also sent the event.
		select {
		case <-ch:
		default:
		}
	}

	// Heartbeat ticker — every 15 s.
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	logger.InfoContext(ctx, "rally_baton_waiting", "session_id", id, "participant", as)

	for {
		select {
		case <-ctx.Done():
			logger.InfoContext(ctx, "rally_baton_disconnect", "session_id", id, "participant", as)
			cleanupStream(id, as)
			return

		case evt, more := <-ch:
			if !more {
				// Channel closed (session interrupted): send terminal close event.
				_ = rc.SetWriteDeadline(time.Now().Add(2 * time.Second))
				_, _ = fmt.Fprintf(w, "data: {\"closed\":true}\n\n")
				_ = rc.SetWriteDeadline(time.Time{})
				_ = rc.Flush()
				cleanupStream(id, as)
				return
			}
			if evt.Closed {
				// Explicit close sentinel sent before channel close.
				_ = rc.SetWriteDeadline(time.Now().Add(2 * time.Second))
				_, _ = fmt.Fprintf(w, "data: {\"closed\":true}\n\n")
				_ = rc.SetWriteDeadline(time.Time{})
				_ = rc.Flush()
				cleanupStream(id, as)
				return
			}
			if err := writeSSEEvent(w, rc, evt); err != nil {
				logger.ErrorContext(ctx, "write baton event", "error", err)
				cleanupStream(id, as)
				return
			}
			// After delivering the baton, this SSE stream's job is done for this turn.
			// We keep the connection open so the participant can receive future turns too.

		case <-ticker.C:
			// Send heartbeat ping comment.
			_ = rc.SetWriteDeadline(time.Now().Add(2 * time.Second))
			if _, err := fmt.Fprintf(w, ": ping\n\n"); err != nil {
				logger.ErrorContext(ctx, "write heartbeat", "error", err)
				_ = rc.SetWriteDeadline(time.Time{})
				cleanupStream(id, as)
				return
			}
			_ = rc.SetWriteDeadline(time.Time{})
			if err := rc.Flush(); err != nil {
				logger.ErrorContext(ctx, "flush heartbeat", "error", err)
				cleanupStream(id, as)
				return
			}
			// Update last_seen.
			rallies.mu.Lock()
			if s2, ok2 := rallies.sessions[id]; ok2 {
				s2.lastSeen[as] = time.Now().UnixMilli()
			}
			rallies.mu.Unlock()
		}
	}
}

// writeSSEEvent writes a single BatonEvent as an SSE data line.
func writeSSEEvent(w http.ResponseWriter, rc *http.ResponseController, evt contract.BatonEvent) error {
	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	_ = rc.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	_ = rc.SetWriteDeadline(time.Time{})
	if err != nil {
		return err
	}
	return rc.Flush()
}

// cleanupStream removes the SSE channel registration for a participant.
func cleanupStream(sessionID, participant string) {
	rallies.mu.Lock()
	defer rallies.mu.Unlock()
	if sess, ok := rallies.sessions[sessionID]; ok {
		delete(sess.streams, participant)
	}
}

// handleRallyDone handles POST /rally/sessions/{id}/done?as=<name>.
func (s *Server) handleRallyDone(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := slog.With("handler", "rally_done")

	if !requireJSON(w, r) {
		return
	}

	id := r.PathValue("id")
	as := r.URL.Query().Get("as")
	if !nameRe.MatchString(as) {
		http.Error(w, fmt.Sprintf("invalid participant name %q: must match ^[a-zA-Z0-9_-]{1,16}$", as), http.StatusBadRequest)
		return
	}

	var req contract.DoneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.ErrorContext(ctx, "decode request", "error", err)
		http.Error(w, fmt.Sprintf("bad request: %v", err), http.StatusBadRequest)
		return
	}

	// Validate optional handoff_to name.
	if req.HandoffTo != "" && !nameRe.MatchString(req.HandoffTo) {
		http.Error(w, fmt.Sprintf("invalid handoff_to name %q: must match ^[a-zA-Z0-9_-]{1,16}$", req.HandoffTo), http.StatusBadRequest)
		return
	}

	rallies.mu.Lock()
	sess, ok := rallies.sessions[id]
	if !ok {
		rallies.mu.Unlock()
		http.Error(w, "rally session not found", http.StatusNotFound)
		return
	}

	// Only the current holder may call done.
	if sess.holder != as {
		rallies.mu.Unlock()
		http.Error(w, fmt.Sprintf("participant %q is not the current baton holder (holder: %q)", as, sess.holder), http.StatusConflict)
		return
	}

	// Determine next participant.
	next := sess.nextParticipant(as, req.HandoffTo)
	nowMS := time.Now().UnixMilli()

	// Record handoff in history.
	sess.turnN++
	handoff := contract.BatonHandoff{
		From:  as,
		To:    next,
		TurnN: sess.turnN,
		Note:  req.Note,
		At:    nowMS,
	}
	sess.history = append(sess.history, handoff)
	sess.holder = next
	sess.status = contract.RallyTurnState(next)

	// Deliver baton event to next participant's stream if connected.
	evt := contract.BatonEvent{
		TurnN: sess.turnN,
		From:  as,
		Note:  req.Note,
	}
	nextCh := sess.streams[next]
	rallies.mu.Unlock()

	// Send non-blocking to avoid deadlock if the channel is full or participant absent.
	if nextCh != nil {
		select {
		case nextCh <- evt:
		default:
			logger.WarnContext(ctx, "rally_baton_channel_full", "session_id", id, "next", next)
		}
	}

	logger.InfoContext(ctx, "rally_done",
		"session_id", id,
		"from", as,
		"to", next,
		"turn_n", sess.turnN,
	)

	rallies.mu.Lock()
	pub := sess.toPublic(nowMS)
	rallies.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(pub); err != nil {
		logger.ErrorContext(ctx, "encode response", "error", err)
	}
}

// handleRallyStatus handles GET /rally/sessions/{id}.
func (s *Server) handleRallyStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := slog.With("handler", "rally_status")

	id := r.PathValue("id")

	rallies.mu.Lock()
	sess, ok := rallies.sessions[id]
	if !ok {
		rallies.mu.Unlock()
		http.Error(w, "rally session not found", http.StatusNotFound)
		return
	}
	nowMS := time.Now().UnixMilli()
	pub := sess.toPublic(nowMS)
	rallies.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(pub); err != nil {
		logger.ErrorContext(ctx, "encode response", "error", err)
	}
}

// IsParticipantStale reports whether a participant's last_seen timestamp
// is older than staleThreshold milliseconds.
// Exported for test access; caller must hold no locks.
func IsParticipantStale(sessionID, name string, nowMS int64) bool {
	rallies.mu.Lock()
	defer rallies.mu.Unlock()
	sess, ok := rallies.sessions[sessionID]
	if !ok {
		return false
	}
	ls, hasSeen := sess.lastSeen[name]
	if !hasSeen {
		return false
	}
	return nowMS-ls > sess.staleThreshold
}

// SetRallyStaleThreshold overrides the stale threshold for a session (test helper).
func SetRallyStaleThreshold(sessionID string, thresholdMS int64) {
	rallies.mu.Lock()
	defer rallies.mu.Unlock()
	if sess, ok := rallies.sessions[sessionID]; ok {
		sess.staleThreshold = thresholdMS
	}
}

// CloseAllRallies marks every active rally as interrupted and sends a final
// {"closed":true} event to every connected SSE stream, then closes those
// streams. Idempotent.
func CloseAllRallies() {
	rallies.mu.Lock()
	defer rallies.mu.Unlock()
	for _, sess := range rallies.sessions {
		if sess.status == contract.RallyStateInterrupted {
			continue
		}
		sess.status = contract.RallyStateInterrupted
		for name, ch := range sess.streams {
			if ch == nil {
				continue
			}
			// Non-blocking send of the sentinel event; if the buffer is full we
			// still close the channel so the reader unblocks.
			select {
			case ch <- contract.BatonEvent{Closed: true}:
			default:
			}
			close(ch)
			delete(sess.streams, name)
		}
	}
}
