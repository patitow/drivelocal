package main

import (
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
)

func advertisedIPv4Addresses() []string {
	if env := strings.TrimSpace(os.Getenv("HOST_LAN_IP")); env != "" {
		seen := map[string]bool{}
		var out []string
		for _, part := range strings.Split(env, ",") {
			ip := strings.TrimSpace(part)
			parsed := net.ParseIP(ip)
			if parsed == nil || parsed.To4() == nil || seen[ip] {
				continue
			}
			seen[ip] = true
			out = append(out, ip)
		}
		if len(out) > 0 {
			return out
		}
	}
	return lanIPv4Addresses()
}

func lanIPv4Addresses() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		if !isAdvertisableIPv4(ipnet.IP) {
			continue
		}
		ip := ipnet.IP.To4().String()
		if seen[ip] {
			continue
		}
		seen[ip] = true
		out = append(out, ip)
	}
	sort.Slice(out, func(i, j int) bool {
		pi, pj := ipv4Preference(net.ParseIP(out[i])), ipv4Preference(net.ParseIP(out[j]))
		if pi != pj {
			return pi < pj
		}
		return out[i] < out[j]
	})
	return out
}

func isAdvertisableIPv4(ip net.IP) bool {
	if ip == nil {
		return false
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	if ip4.IsLoopback() || ip4.IsUnspecified() || ip4.IsLinkLocalUnicast() || ip4.IsMulticast() {
		return false
	}
	if !ip4.IsPrivate() {
		return false
	}
	// Docker / WSL NAT ranges — they are private but not useful as a LAN URL.
	if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
		return false
	}
	return true
}

func ipv4Preference(ip net.IP) int {
	ip4 := ip.To4()
	if ip4 == nil {
		return 9
	}
	if ip4[0] == 192 && ip4[1] == 168 {
		return 0
	}
	if ip4[0] == 10 {
		return 1
	}
	return 2
}

func advertisedPort() interface{} {
	if n, err := strconv.Atoi(PORT); err == nil {
		return n
	}
	return PORT
}
