// Package broker provides the HTTP/SSE server and route handlers for the rallish broker.
//
// This file implements the rally-mode baton-passing endpoints.
package broker

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jazz1x/rallish/pkg/contract"
)

// rallyStore holds all active rally sessions keyed by session ID.
// It is protected by a single mutex; all state mutations must hold that lock.
type rallyStore struct {
	mu       sync.Mutex
	sessions map[contract.SessionID]*rallySession
}

// rallySession is the broker-internal state for a single rally session.
type rallySession struct {
	id           contract.SessionID
	participants []contract.ParticipantName // ordered list
	repo         string
	task         string
	pattern      contract.Pattern
	holder       contract.ParticipantName // name of current baton holder
	turnN        int
	status       contract.RallyState
	lastSeen     map[contract.ParticipantName]int64
	history      []contract.BatonHandoff
	createdAt    int64

	// streams maps participant name → channel that receives BatonEvents.
	// Nil channel means the participant is not currently connected.
	streams map[contract.ParticipantName]chan contract.BatonEvent

	// staleThreshold is how many milliseconds without a heartbeat before a
	// participant is considered stale. Defaults to 30 000 ms; overrideable
	// for tests.
	staleThreshold int64
}

// newRallyStore creates an empty store.
func newRallyStore() *rallyStore {
	return &rallyStore{sessions: make(map[contract.SessionID]*rallySession)}
}

// nextParticipant returns the next participant after `current` using round-robin
// over the ordered participants list. Returns "" if none found (shouldn't happen).
//
// Note: when handoff_to is supplied repeatedly to the same participant, that
// participant receives the baton every turn — starvation of others is a known
// property of explicit handoffs and is accepted by design.
func (rs *rallySession) nextParticipant(current contract.ParticipantName, preferredNext contract.ParticipantName) contract.ParticipantName {
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
func generateRallyID() (contract.SessionID, error) {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return contract.SessionID(fmt.Sprintf("rly_%d_%s", time.Now().UnixMilli(), hex.EncodeToString(b))), nil
}

// toPublic converts a rallySession to the public contract type for wire serialisation.
// Caller must hold the store lock.
func (rs *rallySession) toPublic(nowMS int64) contract.RallySession {
	lastSeen := make(map[string]int64, len(rs.lastSeen))
	for k, v := range rs.lastSeen {
		lastSeen[k.String()] = v
	}

	history := make([]contract.BatonHandoff, len(rs.history))
	copy(history, rs.history)

	parts := make([]string, len(rs.participants))
	for i, p := range rs.participants {
		parts[i] = p.String()
	}

	// Compute stale flags and append to LastSeen (already a copy).
	_ = nowMS // stale is advisory and exposed via the status response separately.

	return contract.RallySession{
		ID:           rs.id.String(),
		Participants: parts,
		Repo:         rs.repo,
		Task:         rs.task,
		Holder:       rs.holder.String(),
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

// writeRallyError maps a typed contract error to the appropriate HTTP status
// and writes the error message body. All rally handler error paths funnel here.
func writeRallyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, contract.ErrSessionNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, contract.ErrNotHolder):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, contract.ErrSessionInterrupted):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, contract.ErrAlreadyConnected):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, contract.ErrInvalidName),
		errors.Is(err, contract.ErrInvalidSessionID),
		errors.Is(err, contract.ErrInvalidPattern),
		errors.Is(err, contract.ErrDuplicateName),
		errors.Is(err, contract.ErrTooFewParticipants),
		errors.Is(err, contract.ErrSelfHandoff),
		errors.Is(err, contract.ErrHandoffToNotMember),
		errors.Is(err, contract.ErrFirstHolderNotMember):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// createRallySession validates and creates a new rally session, returning the
// public snapshot. It is used by both the HTTP handler and the MCP tool handler.
func createRallySession(req contract.NewRallyRequest) (contract.RallySession, error) {
	// Validate participants — must be ≥2, all valid names, no duplicates.
	if len(req.Participants) < 2 {
		return contract.RallySession{}, fmt.Errorf("need at least 2 participants: %w", contract.ErrTooFewParticipants)
	}
	seen := make(map[contract.ParticipantName]struct{})
	names := make([]contract.ParticipantName, 0, len(req.Participants))
	for _, raw := range req.Participants {
		pn, err := contract.NewParticipantName(raw)
		if err != nil {
			return contract.RallySession{}, err
		}
		if _, dup := seen[pn]; dup {
			return contract.RallySession{}, fmt.Errorf("participant %q: %w", raw, contract.ErrDuplicateName)
		}
		seen[pn] = struct{}{}
		names = append(names, pn)
	}

	// Validate first_holder if provided.
	var firstHolder contract.ParticipantName
	if req.FirstHolder != "" {
		fh, err := contract.NewParticipantName(req.FirstHolder)
		if err != nil {
			return contract.RallySession{}, err
		}
		if _, ok := seen[fh]; !ok {
			return contract.RallySession{}, fmt.Errorf("first_holder %q: %w", req.FirstHolder, contract.ErrFirstHolderNotMember)
		}
		firstHolder = fh
	}

	// Parse pattern from task prefix (advisory; stored internally only).
	pattern, _, _ := contract.ParsePattern(req.Task)

	id, err := generateRallyID()
	if err != nil {
		return contract.RallySession{}, fmt.Errorf("generate id: %w", err)
	}

	nowMS := time.Now().UnixMilli()
	sess := &rallySession{
		id:             id,
		participants:   names,
		repo:           req.Repo,
		task:           req.Task,
		pattern:        pattern,
		status:         contract.RallyStateIdle,
		lastSeen:       make(map[contract.ParticipantName]int64),
		history:        []contract.BatonHandoff{},
		createdAt:      nowMS,
		streams:        make(map[contract.ParticipantName]chan contract.BatonEvent),
		staleThreshold: 30_000,
	}

	// Apply first_holder pre-assignment if provided.
	if firstHolder != "" {
		sess.holder = firstHolder
		sess.status = contract.RallyTurnState(firstHolder.String())
		sess.turnN = 1
		sess.lastSeen[firstHolder] = nowMS
	}

	rallies.mu.Lock()
	rallies.sessions[id] = sess
	rallies.mu.Unlock()

	return sess.toPublic(nowMS), nil
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

	pub, err := createRallySession(req)
	if err != nil {
		writeRallyError(w, err)
		return
	}

	logger.InfoContext(ctx, "rally_created", "session_id", pub.ID, "participants", req.Participants)

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

	rawID := r.PathValue("id")
	id, err := contract.NewSessionID(rawID)
	if err != nil {
		writeRallyError(w, err)
		return
	}

	asRaw := r.URL.Query().Get("as")
	as, err := contract.NewParticipantName(asRaw)
	if err != nil {
		writeRallyError(w, err)
		return
	}

	rallies.mu.Lock()
	sess, ok := rallies.sessions[id]
	if !ok {
		rallies.mu.Unlock()
		writeRallyError(w, fmt.Errorf("session %q: %w", rawID, contract.ErrSessionNotFound))
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

	// If the session has already been interrupted, send the terminal close
	// sentinel immediately so the client does not hang.
	if sess.status == contract.RallyStateInterrupted {
		rallies.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		rc := http.NewResponseController(w)
		if err := rc.Flush(); err != nil {
			logger.ErrorContext(ctx, "initial flush", "error", err)
			return
		}
		if err := writeCloseSentinel(w, rc); err != nil {
			logger.ErrorContext(ctx, "write close sentinel", "error", err)
		}
		return
	}

	// Reject duplicate joins: a second live stream for the same participant
	// would orphan the first one and corrupt cleanupStream.
	if existingCh := sess.streams[as]; existingCh != nil {
		rallies.mu.Unlock()
		writeRallyError(w, fmt.Errorf("participant %q: %w", as, contract.ErrAlreadyConnected))
		return
	}

	// Register stream channel (buffer=1 so sender never blocks).
	ch := make(chan contract.BatonEvent, 1)
	sess.streams[as] = ch
	nowMS := time.Now().UnixMilli()
	sess.lastSeen[as] = nowMS

	// If idle and this is the first participant in the ordered list, give them the baton.
	var immediateEvent *contract.BatonEvent
	if sess.status == contract.RallyStateIdle && sess.participants[0] == as {
		sess.turnN++
		sess.holder = as
		sess.status = contract.RallyTurnState(as.String())
		evt := contract.BatonEvent{TurnN: sess.turnN, From: ""}
		immediateEvent = &evt
	} else if sess.status == contract.RallyTurnState(as.String()) {
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

	// Queue the immediate event on the channel so it is delivered by the same
	// SSE loop as regular handoff events. This removes a special-case write
	// that raced with CloseAllRallies.
	if immediateEvent != nil {
		ch <- *immediateEvent
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
				if err := writeCloseSentinel(w, rc); err != nil {
					logger.ErrorContext(ctx, "write close sentinel", "error", err)
				}
				cleanupStream(id, as)
				return
			}
			if evt.Closed {
				// Explicit close sentinel sent before channel close.
				if err := writeCloseSentinel(w, rc); err != nil {
					logger.ErrorContext(ctx, "write close sentinel", "error", err)
				}
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
	if err := rc.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return err
	}
	defer func() { _ = rc.SetWriteDeadline(time.Time{}) }()
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	return rc.Flush()
}

// writeCloseSentinel writes the terminal {"closed":true} SSE event.
func writeCloseSentinel(w http.ResponseWriter, rc *http.ResponseController) error {
	if err := rc.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return err
	}
	defer func() { _ = rc.SetWriteDeadline(time.Time{}) }()
	if _, err := fmt.Fprintf(w, "data: {\"closed\":true}\n\n"); err != nil {
		return err
	}
	return rc.Flush()
}

// cleanupStream removes the SSE channel registration for a participant.
func cleanupStream(sessionID contract.SessionID, participant contract.ParticipantName) {
	rallies.mu.Lock()
	defer rallies.mu.Unlock()
	if sess, ok := rallies.sessions[sessionID]; ok {
		delete(sess.streams, participant)
	}
}

// advanceRallyBaton validates and performs a baton handoff, returning the
// updated public snapshot. It is used by both the HTTP handler and the MCP tool
// handler.
func advanceRallyBaton(id contract.SessionID, as contract.ParticipantName, req contract.DoneRequest) (contract.RallySession, error) {
	// Validate optional handoff_to name.
	var handoffTo contract.ParticipantName
	if req.HandoffTo != "" {
		ht, herr := contract.NewParticipantName(req.HandoffTo)
		if herr != nil {
			return contract.RallySession{}, herr
		}
		// Reject self-handoff: passing the baton to yourself is a no-op and
		// almost certainly a client bug.
		if ht == as {
			return contract.RallySession{}, fmt.Errorf("participant %q: %w", as, contract.ErrSelfHandoff)
		}
		handoffTo = ht
	}

	rallies.mu.Lock()
	sess, ok := rallies.sessions[id]
	if !ok {
		rallies.mu.Unlock()
		return contract.RallySession{}, fmt.Errorf("session %q: %w", id, contract.ErrSessionNotFound)
	}

	// Reject operations on an already-interrupted session.
	if sess.status == contract.RallyStateInterrupted {
		rallies.mu.Unlock()
		return contract.RallySession{}, fmt.Errorf("session %q: %w", id, contract.ErrSessionInterrupted)
	}

	// Only the current holder may call done.
	if sess.holder != as {
		rallies.mu.Unlock()
		return contract.RallySession{}, fmt.Errorf("participant %q is not the current baton holder (holder: %q): %w",
			as, sess.holder, contract.ErrNotHolder)
	}

	// Validate handoff_to membership if provided.
	if handoffTo != "" {
		member := false
		for _, p := range sess.participants {
			if p == handoffTo {
				member = true
				break
			}
		}
		if !member {
			rallies.mu.Unlock()
			return contract.RallySession{}, fmt.Errorf("handoff_to %q: %w", handoffTo, contract.ErrHandoffToNotMember)
		}
	}

	// Determine next participant.
	next := sess.nextParticipant(as, handoffTo)
	nowMS := time.Now().UnixMilli()

	// Record handoff in history.
	sess.turnN++
	handoff := contract.BatonHandoff{
		From:  as.String(),
		To:    next.String(),
		TurnN: sess.turnN,
		Note:  req.Note,
		At:    nowMS,
	}
	sess.history = append(sess.history, handoff)
	sess.holder = next
	sess.status = contract.RallyTurnState(next.String())

	// Deliver baton event to next participant's stream if connected.
	// This send is performed while holding the mutex so that CloseAllRallies
	// cannot close the channel underneath us (send-on-closed-channel panic).
	evt := contract.BatonEvent{
		TurnN: sess.turnN,
		From:  as.String(),
		Note:  req.Note,
	}
	if nextCh := sess.streams[next]; nextCh != nil {
		select {
		case nextCh <- evt:
		default:
		}
	}

	pub := sess.toPublic(nowMS)
	rallies.mu.Unlock()
	return pub, nil
}

// handleRallyDone handles POST /rally/sessions/{id}/done?as=<name>.
func (s *Server) handleRallyDone(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := slog.With("handler", "rally_done")

	if !requireJSON(w, r) {
		return
	}

	rawID := r.PathValue("id")
	id, err := contract.NewSessionID(rawID)
	if err != nil {
		writeRallyError(w, err)
		return
	}

	asRaw := r.URL.Query().Get("as")
	as, err := contract.NewParticipantName(asRaw)
	if err != nil {
		writeRallyError(w, err)
		return
	}

	var req contract.DoneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.ErrorContext(ctx, "decode request", "error", err)
		http.Error(w, fmt.Sprintf("bad request: %v", err), http.StatusBadRequest)
		return
	}

	pub, err := advanceRallyBaton(id, as, req)
	if err != nil {
		writeRallyError(w, err)
		return
	}

	logger.InfoContext(ctx, "rally_done",
		"session_id", pub.ID,
		"from", as,
		"to", pub.Holder,
		"turn_n", pub.TurnN,
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(pub); err != nil {
		logger.ErrorContext(ctx, "encode response", "error", err)
	}
}

// getRallySession returns the public snapshot for a rally session. It is used
// by both the HTTP handler and the MCP tool handler.
func getRallySession(id contract.SessionID) (contract.RallySession, error) {
	rallies.mu.Lock()
	sess, ok := rallies.sessions[id]
	if !ok {
		rallies.mu.Unlock()
		return contract.RallySession{}, fmt.Errorf("session %q: %w", id, contract.ErrSessionNotFound)
	}
	nowMS := time.Now().UnixMilli()
	pub := sess.toPublic(nowMS)
	rallies.mu.Unlock()
	return pub, nil
}

// handleRallyStatus handles GET /rally/sessions/{id}.
func (s *Server) handleRallyStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := slog.With("handler", "rally_status")

	rawID := r.PathValue("id")
	id, err := contract.NewSessionID(rawID)
	if err != nil {
		writeRallyError(w, err)
		return
	}

	pub, err := getRallySession(id)
	if err != nil {
		writeRallyError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(pub); err != nil {
		logger.ErrorContext(ctx, "encode response", "error", err)
	}
}

// isParticipantStale reports whether a participant's last_seen timestamp
// is older than staleThreshold milliseconds.
// Caller must hold no locks.
func isParticipantStale(sessionID contract.SessionID, name contract.ParticipantName, nowMS int64) bool {
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

// setRallyStaleThreshold overrides the stale threshold for a session (test helper).
func setRallyStaleThreshold(sessionID contract.SessionID, thresholdMS int64) {
	rallies.mu.Lock()
	defer rallies.mu.Unlock()
	if sess, ok := rallies.sessions[sessionID]; ok {
		sess.staleThreshold = thresholdMS
	}
}

// interruptRallySession marks a single rally session as interrupted and sends
// a final {"closed":true} event to every connected stream, then closes those
// streams. It returns the public snapshot of the interrupted session.
func interruptRallySession(id contract.SessionID) (contract.RallySession, error) {
	rallies.mu.Lock()
	sess, ok := rallies.sessions[id]
	if !ok {
		rallies.mu.Unlock()
		return contract.RallySession{}, fmt.Errorf("session %q: %w", id, contract.ErrSessionNotFound)
	}

	var toClose []chan contract.BatonEvent
	if sess.status != contract.RallyStateInterrupted {
		sess.status = contract.RallyStateInterrupted
		sess.holder = ""
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
			delete(sess.streams, name)
			toClose = append(toClose, ch)
		}
	}
	nowMS := time.Now().UnixMilli()
	pub := sess.toPublic(nowMS)
	rallies.mu.Unlock()

	for _, ch := range toClose {
		close(ch)
	}
	return pub, nil
}

// CloseAllRallies marks every active rally as interrupted and sends a final
// {"closed":true} event to every connected SSE stream, then closes those
// streams. Idempotent.
func CloseAllRallies() {
	rallies.mu.Lock()
	var toClose []chan contract.BatonEvent
	for _, sess := range rallies.sessions {
		if sess.status == contract.RallyStateInterrupted {
			continue
		}
		sess.status = contract.RallyStateInterrupted
		sess.holder = ""
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
			delete(sess.streams, name)
			toClose = append(toClose, ch)
		}
	}
	rallies.mu.Unlock()
	for _, ch := range toClose {
		close(ch)
	}
}
