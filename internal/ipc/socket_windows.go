//go:build windows

package ipc

import (
	"context"
	"errors"
	"net"
	"net/http"
)

// ErrUnsupported is returned by all ipc operations on Windows. The CLI/daemon
// must use the TCP loopback fallback instead.
var ErrUnsupported = errors.New("ipc: unix domain sockets are not supported on windows; using tcp fallback")

// Socket is a no-op stub on Windows so the rest of the codebase compiles.
type Socket struct {
	Path string
}

// Listen always fails on Windows.
func (s *Socket) Listen() (net.Listener, error) { return nil, ErrUnsupported }

// Dial always fails on Windows.
func (s *Socket) Dial(_ context.Context) (net.Conn, error) { return nil, ErrUnsupported }

// Remove is a no-op on Windows.
func (s *Socket) Remove() error { return nil }

// HTTPClientOverSocket returns a client whose RoundTripper immediately errors,
// preserving the type signature used by the CLI on Unix platforms.
func HTTPClientOverSocket(_ string) *http.Client {
	return &http.Client{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, ErrUnsupported
		}),
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
