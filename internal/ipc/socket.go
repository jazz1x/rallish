//go:build !windows

package ipc

import (
	"context"
	"fmt"
	"net"
	"os"
)

// Socket represents a Unix domain socket path.
type Socket struct {
	Path string
}

// Listen creates a Unix domain socket listener at s.Path.
func (s *Socket) Listen() (net.Listener, error) {
	ln, err := net.Listen("unix", s.Path)
	if err != nil {
		return nil, fmt.Errorf("ipc listen on %s: %w", s.Path, err)
	}
	return ln, nil
}

// Dial connects to the Unix domain socket at s.Path.
func (s *Socket) Dial(ctx context.Context) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", s.Path)
	if err != nil {
		return nil, fmt.Errorf("ipc dial %s: %w", s.Path, err)
	}
	return conn, nil
}

// Remove deletes the socket file if it exists.
// It is idempotent: no error is returned when the file does not exist.
func (s *Socket) Remove() error {
	err := os.Remove(s.Path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("ipc remove %s: %w", s.Path, err)
	}
	return nil
}
