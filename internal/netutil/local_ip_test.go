package netutil

import (
	"net"
	"testing"
	"time"
)

func TestBestAvailableIPv4PrefersSiteLANOverVPN(t *testing.T) {
	t.Parallel()

	addrs := []net.IP{
		net.ParseIP("10.7.7.2"),
		net.ParseIP("192.168.31.102"),
	}

	if got := BestAvailableIPv4(addrs); got != "192.168.31.102" {
		t.Fatalf("BestAvailableIPv4() = %q, want %q", got, "192.168.31.102")
	}
}

func TestPreferredLocalIPv4UsesExplicitValue(t *testing.T) {
	t.Parallel()

	if got := PreferredLocalIPv4("192.168.31.102"); got != "192.168.31.102" {
		t.Fatalf("PreferredLocalIPv4() = %q, want %q", got, "192.168.31.102")
	}
}

func TestPreferredLocalIPv4FallsBackToRouteProbe(t *testing.T) {
	t.Parallel()

	originalDialUDP := dialUDP
	dialUDP = func(network, address string) (net.Conn, error) {
		return &fakeConn{local: &net.UDPAddr{IP: net.ParseIP("192.168.31.55"), Port: 54321}}, nil
	}
	defer func() {
		dialUDP = originalDialUDP
	}()

	if got := routedLocalIPv4(); got != "192.168.31.55" {
		t.Fatalf("routedLocalIPv4() = %q, want %q", got, "192.168.31.55")
	}
}

type fakeConn struct {
	local net.Addr
}

func (f *fakeConn) Read(_ []byte) (int, error)         { return 0, nil }
func (f *fakeConn) Write(b []byte) (int, error)        { return len(b), nil }
func (f *fakeConn) Close() error                       { return nil }
func (f *fakeConn) LocalAddr() net.Addr                { return f.local }
func (f *fakeConn) RemoteAddr() net.Addr               { return &net.UDPAddr{} }
func (f *fakeConn) SetDeadline(_ time.Time) error      { return nil }
func (f *fakeConn) SetReadDeadline(_ time.Time) error  { return nil }
func (f *fakeConn) SetWriteDeadline(_ time.Time) error { return nil }
