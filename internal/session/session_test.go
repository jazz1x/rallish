package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jazz1x/hocketty/pkg/contract"
	"github.com/stretchr/testify/require"
)

type fakeClock struct {
	t time.Time
}

func (f *fakeClock) Now() time.Time { return f.t }

func TestStore_CreateAppendGetReplay(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	clock := &fakeClock{t: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}

	store, err := NewStore(dir, clock)
	require.NoError(t, err)

	preset := contract.Preset{Name: "test-preset"}
	task := contract.Task{Title: "test task", Body: "do something", RepoRoot: "/tmp"}

	// Create
	sess, err := store.Create(ctx, preset, task)
	require.NoError(t, err)
	require.NotEmpty(t, sess.ID)
	require.Equal(t, "test-preset", sess.PresetName)
	require.Equal(t, "active", sess.Status)
	require.Equal(t, task, sess.Task)
	require.Equal(t, 0, sess.TurnCount)

	// Verify meta.json exists
	metaPath := filepath.Join(dir, sess.ID, "meta.json")
	_, err = os.Stat(metaPath)
	require.NoError(t, err)

	// Verify log.jsonl exists
	logPath := filepath.Join(dir, sess.ID, "log.jsonl")
	_, err = os.Stat(logPath)
	require.NoError(t, err)

	// Get
	got, err := store.Get(ctx, sess.ID)
	require.NoError(t, err)
	require.Equal(t, sess, got)

	// Append
	req := contract.TurnRequest{Session: sess.ID, Turn: 1, Role: "planner"}
	resp := contract.TurnResponse{Done: false, Summary: "step 1"}
	err = store.Append(ctx, sess.ID, req, resp)
	require.NoError(t, err)

	// Get after append
	got, err = store.Get(ctx, sess.ID)
	require.NoError(t, err)
	require.Equal(t, 1, got.TurnCount)

	// Replay
	records, err := store.Replay(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, 1, records[0].Turn)
	require.Equal(t, req, records[0].Req)
	require.Equal(t, resp, records[0].Resp)
}

func TestStore_Get_NotFound(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := NewStore(dir, &fakeClock{})
	require.NoError(t, err)

	_, err = store.Get(ctx, "nonexistent")
	require.Error(t, err)
}

func TestStore_Append_NotFound(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := NewStore(dir, &fakeClock{})
	require.NoError(t, err)

	err = store.Append(ctx, "nonexistent", contract.TurnRequest{}, contract.TurnResponse{})
	require.Error(t, err)
}

func TestStore_Replay_NotFound(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := NewStore(dir, &fakeClock{})
	require.NoError(t, err)

	_, err = store.Replay(ctx, "nonexistent")
	require.Error(t, err)
}

func BenchmarkStoreAppend(b *testing.B) {
	ctx := context.Background()
	dir := b.TempDir()
	clock := &fakeClock{t: time.Now()}
	store, err := NewStore(dir, clock)
	require.NoError(b, err)

	preset := contract.Preset{Name: "bench"}
	task := contract.Task{Title: "bench"}
	sess, err := store.Create(ctx, preset, task)
	require.NoError(b, err)

	req := contract.TurnRequest{Session: sess.ID, Turn: 1, Role: "planner"}
	resp := contract.TurnResponse{Summary: "x"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := req
		r.Turn = i + 1
		if err := store.Append(ctx, sess.ID, r, resp); err != nil {
			b.Fatal(err)
		}
	}
}
