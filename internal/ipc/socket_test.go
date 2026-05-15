package ipc

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSocketListenDial(t *testing.T) {
	dir := t.TempDir()
	s := Socket{Path: filepath.Join(dir, "test.sock")}

	ln, err := s.Listen()
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	echoDone := make(chan struct{})
	go func() {
		defer close(echoDone)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		buf := make([]byte, 5)
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		_, _ = conn.Write(buf[:n])
	}()

	conn, err := s.Dial(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	_, err = conn.Write([]byte("hello"))
	require.NoError(t, err)

	got := make([]byte, 5)
	_, err = io.ReadFull(conn, got)
	require.NoError(t, err)
	require.Equal(t, "hello", string(got))

	require.NoError(t, conn.Close())
	require.NoError(t, ln.Close())
	<-echoDone
}

func TestSocketRemove(t *testing.T) {
	dir := t.TempDir()
	s := Socket{Path: filepath.Join(dir, "test.sock")}

	// Removing a non-existent file should not error.
	err := s.Remove()
	require.NoError(t, err)

	// Create the file.
	f, err := os.Create(s.Path)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	_, err = os.Stat(s.Path)
	require.NoError(t, err)

	// Remove should delete the existing file.
	err = s.Remove()
	require.NoError(t, err)

	_, err = os.Stat(s.Path)
	require.True(t, os.IsNotExist(err), "expected file to be removed")
}

