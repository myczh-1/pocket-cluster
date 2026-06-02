package peernet

import "testing"

func TestNewTransportDisablesEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("http_proxy", "http://127.0.0.1:1")

	transport := NewTransport()
	if transport.Proxy != nil {
		t.Fatal("peer transport must not use environment proxies")
	}
}
