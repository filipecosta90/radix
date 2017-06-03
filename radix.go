// Package radix is a simple redis driver. It needs better docs
package radix

import (
	"bufio"
	"net"
	"time"

	"github.com/mediocregopher/radix.v2/resp"
)

// Client describes an entity which can carry out Actions, e.g. a connection
// pool for a single redis instance or the cluster client.
type Client interface {
	Do(Action) error

	// Once Close() is called all future method calls on the Client will return
	// an error
	Close() error
}

// Conn is a Client which synchronously reads/writes data on a network
// connection using the redis resp protocol.
//
// Encode and Decode may be called at the same time by two different
// go-routines, but each should only be called once at a time (i.e. two routines
// shouldn't call Encode at the same time, same with Decode).
//
// If either Encode or Decode encounter a net.Error the Conn will be
// automatically closed.
type Conn interface {
	Client

	Encode(resp.Marshaler) error
	Decode(resp.Unmarshaler) error

	// Returns the underlying network connection, as-is. Read, Write, and Close
	// should not be called on the returned Conn.
	NetConn() net.Conn
}

type connWrap struct {
	net.Conn
	brw *bufio.ReadWriter
}

// NewConn takes an existing net.Conn and wraps it to support the Conn interface
// of this package. The Read and Write methods on the original net.Conn should
// not be used after calling this method.
//
// In both the Encode and Decode methods of the returned Conn, if a net.Error is
// encountered the Conn will have Close called on it automatically.
func NewConn(conn net.Conn) Conn {
	return connWrap{
		Conn: conn,
		brw:  bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn)),
	}
}

func (cw connWrap) Do(a Action) error {
	return a.Run(cw)
}

func (cw connWrap) Encode(m resp.Marshaler) error {
	err := m.MarshalRESP(cw.brw)
	defer func() {
		if _, ok := err.(net.Error); ok {
			cw.Close()
		}
	}()

	if err != nil {
		return err
	}
	err = cw.brw.Flush()
	return err
}

func (cw connWrap) Decode(u resp.Unmarshaler) error {
	err := u.UnmarshalRESP(cw.brw.Reader)
	if _, ok := err.(net.Error); ok {
		cw.Close()
	}
	return err
}

func (cw connWrap) NetConn() net.Conn {
	return cw.Conn
}

// DialFunc is a function which returns an initialized, ready-to-be-used Conn.
// Functions like NewPool or NewCluster take in a DialFunc in order to allow for
// things like calls to AUTH on each new connection, setting timeouts, custom
// Conn implementations, etc...
type DialFunc func(network, addr string) (Conn, error)

// Dial creates a network connection using net.Dial and passes it into NewConn.
func Dial(network, addr string) (Conn, error) {
	c, err := net.Dial(network, addr)
	if err != nil {
		return nil, err
	}
	return NewConn(c), nil
}

type timeoutConn struct {
	net.Conn
	timeout time.Duration
}

func (tc *timeoutConn) setDeadline() {
	if tc.timeout > 0 {
		tc.Conn.SetDeadline(time.Now().Add(tc.timeout))
	}
}

func (tc *timeoutConn) Read(b []byte) (int, error) {
	tc.setDeadline()
	return tc.Conn.Read(b)
}

func (tc *timeoutConn) Write(b []byte) (int, error) {
	tc.setDeadline()
	return tc.Conn.Write(b)
}

// DialTimeout is like Dial, but the given timeout is used to set read/write
// deadlines on all reads/writes
func DialTimeout(network, addr string, timeout time.Duration) (Conn, error) {
	c, err := net.DialTimeout(network, addr, timeout)
	if err != nil {
		return nil, err
	}
	return NewConn(&timeoutConn{Conn: c, timeout: timeout}), nil
}
