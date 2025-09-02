package main

import (
	"fmt"
	"net"
)

// function to convert bytes to human readable format
func humanizeBytes(bytes uint64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.2f KB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%.2f MB", float64(bytes)/1024/1024)
}

// getLocalIPv4Addrs returns a list of IPv4 addresses for local interfaces
func getLocalIPv4Addrs() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return []string{"127.0.0.1"}
	}
	var result []string
	seen := map[string]bool{}
	result = append(result, "127.0.0.1")
	seen["127.0.0.1"] = true
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil || ip.IsLoopback() {
			continue
		}
		ip = ip.To4()
		if ip == nil {
			continue
		}
		s := ip.String()
		if !seen[s] {
			result = append(result, s)
			seen[s] = true
		}
	}
	return result
}
