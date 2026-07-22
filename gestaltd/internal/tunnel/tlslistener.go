package tunnel

import (
	"crypto/tls"
	"net"
	"time"
)

// handshakeTimeout bounds the inner mTLS handshake on the Host side so a
// stalled peer cannot park the accept loop indefinitely.
const handshakeTimeout = 15 * time.Second

// tlsListener wraps a net.Listener so Accept completes the TLS handshake
// before returning the connection. Handshake failures (including rejected
// client SPKIs and timeouts) are swallowed and the accept loop continues, so
// gRPC Serve and similar loops do not treat a single bad connection as fatal.
type tlsListener struct {
	net.Listener
	tlsCfg *tls.Config
}

func (l *tlsListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		tlsConn := tls.Server(conn, l.tlsCfg)
		_ = tlsConn.SetDeadline(time.Now().Add(handshakeTimeout))
		if err := tlsConn.Handshake(); err != nil {
			_ = conn.Close()
			continue
		}
		_ = tlsConn.SetDeadline(time.Time{})
		return tlsConn, nil
	}
}

var _ net.Listener = (*tlsListener)(nil)
