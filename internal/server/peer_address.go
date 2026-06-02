package server

import "net"

func addressFromRemote(remoteAddr, advertisedAddr string) string {
	remoteHost, _, err := net.SplitHostPort(remoteAddr)
	if err != nil || remoteHost == "" {
		return normalizeNodeAddress(advertisedAddr)
	}
	_, advertisedPort, err := net.SplitHostPort(normalizeNodeAddress(advertisedAddr))
	if err != nil || advertisedPort == "" {
		return normalizeNodeAddress(advertisedAddr)
	}
	return net.JoinHostPort(remoteHost, advertisedPort)
}
