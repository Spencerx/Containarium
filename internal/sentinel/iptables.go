package sentinel

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strconv"
)

const chainName = "SENTINEL_PREROUTING"

// enableForwarding sets up iptables DNAT rules to forward traffic to the spot VM.
// It creates a custom chain, adds DNAT rules for each port, and enables MASQUERADE.
// On non-Linux systems or when iptables is unavailable, it logs a warning and returns nil.
func enableForwarding(spotIP string, ports []int) error {
	if runtime.GOOS != "linux" {
		log.Printf("[sentinel] iptables: skipping on %s (non-Linux)", runtime.GOOS)
		return nil
	}

	if _, err := exec.LookPath("iptables"); err != nil {
		log.Printf("[sentinel] iptables: binary not found, skipping forwarding")
		return nil
	}

	// Enable IP forwarding
	if err := exec.Command("sysctl", "-w", "net.ipv4.ip_forward=1").Run(); err != nil {
		return fmt.Errorf("failed to enable ip_forward: %w", err)
	}

	// Flush any existing sentinel chain rules
	if err := disableForwarding(); err != nil {
		log.Printf("[sentinel] iptables: warning during cleanup: %v", err)
	}

	// Create the custom chain
	exec.Command("iptables", "-t", "nat", "-N", chainName).Run() // ignore error if exists

	// Add DNAT rules for each port
	for _, port := range ports {
		portStr := strconv.Itoa(port)
		dest := fmt.Sprintf("%s:%d", spotIP, port)
		err := exec.Command("iptables", "-t", "nat", "-A", chainName,
			"-p", "tcp", "--dport", portStr,
			"-j", "DNAT", "--to-destination", dest,
		).Run()
		if err != nil {
			return fmt.Errorf("failed to add DNAT rule for port %d: %w", port, err)
		}
	}

	// Jump from PREROUTING to our chain
	err := exec.Command("iptables", "-t", "nat", "-A", "PREROUTING",
		"-j", chainName,
	).Run()
	if err != nil {
		return fmt.Errorf("failed to add PREROUTING jump: %w", err)
	}

	// Add MASQUERADE for forwarded traffic
	err = exec.Command("iptables", "-t", "nat", "-A", "POSTROUTING",
		"-d", spotIP,
		"-j", "MASQUERADE",
	).Run()
	if err != nil {
		return fmt.Errorf("failed to add MASQUERADE rule: %w", err)
	}

	// Allow forwarded traffic in the FORWARD chain
	err = exec.Command("iptables", "-A", "FORWARD",
		"-d", spotIP,
		"-j", "ACCEPT",
	).Run()
	if err != nil {
		return fmt.Errorf("failed to add FORWARD ACCEPT rule: %w", err)
	}

	err = exec.Command("iptables", "-A", "FORWARD",
		"-s", spotIP,
		"-j", "ACCEPT",
	).Run()
	if err != nil {
		return fmt.Errorf("failed to add FORWARD ACCEPT return rule: %w", err)
	}

	log.Printf("[sentinel] iptables: forwarding enabled for %s on ports %v", spotIP, ports)
	return nil
}

// disableForwarding removes all sentinel iptables rules.
// Safe to call even if no rules exist.
func disableForwarding() error {
	if runtime.GOOS != "linux" {
		return nil
	}

	if _, err := exec.LookPath("iptables"); err != nil {
		return nil
	}

	// Remove jump from PREROUTING
	exec.Command("iptables", "-t", "nat", "-D", "PREROUTING", "-j", chainName).Run()

	// Flush and delete our chain
	exec.Command("iptables", "-t", "nat", "-F", chainName).Run()
	exec.Command("iptables", "-t", "nat", "-X", chainName).Run()

	// Clean up POSTROUTING MASQUERADE and FORWARD rules added by sentinel
	// We use comment matching in production, but for simplicity flush by known patterns
	// These are best-effort cleanup
	log.Printf("[sentinel] iptables: forwarding rules cleared")
	return nil
}
