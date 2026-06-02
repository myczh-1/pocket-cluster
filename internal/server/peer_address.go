package server

import (
	"net"

	"github.com/pocketcluster/agent/internal/types"
)

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

func mergeAddresses(values ...string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		addr := normalizeNodeAddress(value)
		if addr == "" {
			continue
		}
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		out = append(out, addr)
	}
	return out
}

func mergeAddressCandidates(existing []string, values ...string) []string {
	all := make([]string, 0, len(existing)+len(values))
	all = append(all, existing...)
	all = append(all, values...)
	return mergeAddresses(all...)
}

func nodeDialAddresses(n types.Node) []string {
	values := make([]string, 0, len(n.AddressCandidates)+2)
	values = append(values, n.LastWorkingAddress)
	values = append(values, n.AddressCandidates...)
	values = append(values, n.Address)
	return mergeAddresses(values...)
}
