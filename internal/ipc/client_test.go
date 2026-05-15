package ipc

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHTTPClientOverSocket(t *testing.T) {
	dir := t.TempDir()
	s := Socket{Path: filepath.Join(dir, "http.sock")}

	ln, err := s.Listen()
	require.NoError(t, err)
	defer ln.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()

	client := HTTPClientOverSocket(s.Path)
	resp, err := client.Get("http://rallish.local/")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(body, &result)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"ok": true}, result)
}
