package ipc

import (
	"context"
	"net"
	"net/http"
)

// HTTPClientOverSocket returns an *http.Client whose transport dials the
// Unix domain socket at socketPath regardless of the requested address.
func HTTPClientOverSocket(socketPath string) *http.Client {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		},
	}
	return &http.Client{Transport: tr}
}
