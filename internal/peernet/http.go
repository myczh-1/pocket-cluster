package peernet

import "net/http"

// NewHTTPClient returns an HTTP client for peer-to-peer node traffic.
//
// Node addresses are local-network endpoints learned from mDNS or join
// responses. They must be dialed directly: environment HTTP proxies commonly
// return gateway errors for RFC1918/link-local addresses and break sync.
func NewHTTPClient() *http.Client {
	return &http.Client{Transport: NewTransport()}
}

// NewTransport clones Go's default transport but disables environment proxy
// resolution so peer-to-peer requests never leave the local network path.
func NewTransport() *http.Transport {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Transport{}
	}
	transport := base.Clone()
	transport.Proxy = nil
	return transport
}
