package sentinel

import (
	"net"
	"strconv"
	"time"
)

// CheckHealth performs a TCP health check against the given address and port.
// Returns true if a TCP connection can be established within the timeout.
// This is the primary health check — cloud-agnostic and free (no API calls).
func CheckHealth(ip string, port int, timeout time.Duration) bool {
	addr := net.JoinHostPort(ip, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
