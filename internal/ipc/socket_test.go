package ipc

import (
	"context"
	"io"
	"net/http"
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
		defer conn.Close()

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

func TestHTTPClientOverSocket(t *testing.T) {
	dir := t.TempDir()
	s := Socket{Path: filepath.Join(dir, "http.sock")}

	ln, err := s.Listen()
	require.NoError(t, err)
	defer ln.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("pong"))
	})

	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()

	client := HTTPClientOverSocket(s.Path)
	resp, err := client.Get("http://unix/ping")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "pong", string(body))
}
