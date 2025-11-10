package main

import (
	"log/slog"
	"net"
)

// getPublicIP returns the first non-loopback IPv4 address
func getPublicIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		slog.Warn("Could not get interface addresses",
			"event", "interface_addr_error",
			"error", err.Error())
		return "0.0.0.0"
	}

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				return ipNet.IP.String()
			}
		}
	}

	slog.Warn("Could not find non-loopback IPv4 address, using 0.0.0.0",
		"event", "no_ipv4_address")
	return "0.0.0.0"
}
