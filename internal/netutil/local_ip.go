package netutil

import (
	"net"
	"strings"
)

var dialUDP = func(network, address string) (net.Conn, error) {
	return net.Dial(network, address)
}

func UsableLocalIP(value string) string {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil || ip.IsLoopback() {
		return ""
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		return ipv4.String()
	}
	return ip.String()
}

func PreferredLocalIPv4(preferred string) string {
	if ip := UsableLocalIP(preferred); ip != "" {
		return ip
	}
	if ip := preferredInterfaceIPv4(); ip != "" {
		return ip
	}
	return routedLocalIPv4()
}

func BestAvailableIPv4(addrs []net.IP) string {
	bestIP := ""
	bestScore := -1
	for _, ip := range addrs {
		ipv4 := ip.To4()
		if ipv4 == nil || ipv4.IsLoopback() {
			continue
		}
		score := ipv4Score(ipv4)
		if score > bestScore {
			bestScore = score
			bestIP = ipv4.String()
		}
	}
	return bestIP
}

func preferredInterfaceIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	bestIP := ""
	bestScore := -1
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch value := addr.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			ipv4 := ip.To4()
			if ipv4 == nil || ipv4.IsLoopback() {
				continue
			}
			score := interfaceScore(iface, ipv4)
			if score > bestScore {
				bestScore = score
				bestIP = ipv4.String()
			}
		}
	}
	return bestIP
}

func routedLocalIPv4() string {
	conn, err := dialUDP("udp4", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()

	udpAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return ""
	}
	return UsableLocalIP(udpAddr.IP.String())
}

func interfaceScore(iface net.Interface, ip net.IP) int {
	score := ipv4Score(ip)
	if iface.Flags&net.FlagBroadcast != 0 {
		score += 10
	}
	if iface.Flags&net.FlagPointToPoint != 0 {
		score -= 40
	}

	name := strings.ToLower(iface.Name)
	switch {
	case strings.HasPrefix(name, "en"),
		strings.HasPrefix(name, "eth"),
		strings.HasPrefix(name, "wl"),
		strings.HasPrefix(name, "wlan"),
		strings.HasPrefix(name, "wifi"):
		score += 20
	case strings.HasPrefix(name, "utun"),
		strings.HasPrefix(name, "tun"),
		strings.HasPrefix(name, "tap"),
		strings.HasPrefix(name, "bridge"),
		strings.HasPrefix(name, "docker"),
		strings.HasPrefix(name, "veth"),
		strings.HasPrefix(name, "awdl"),
		strings.HasPrefix(name, "llw"):
		score -= 30
	}
	return score
}

func ipv4Score(ip net.IP) int {
	if score, ok := rfc1918Score(ip); ok {
		return score
	}
	if ip.IsLinkLocalUnicast() {
		return 40
	}
	return 10
}

func rfc1918Score(ip net.IP) (int, bool) {
	ipv4 := ip.To4()
	if ipv4 == nil {
		return 0, false
	}
	switch {
	case ipv4[0] == 192 && ipv4[1] == 168:
		return 120, true
	case ipv4[0] == 172 && ipv4[1] >= 16 && ipv4[1] <= 31:
		return 110, true
	case ipv4[0] == 10:
		return 100, true
	default:
		return 0, false
	}
}
