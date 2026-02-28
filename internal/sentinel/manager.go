package sentinel

import (
	"context"
	"log"
	"sync/atomic"
	"time"
)

// State represents the current sentinel mode.
type State string

const (
	StateProxy       State = "proxy"
	StateMaintenance State = "maintenance"
)

// Config holds the sentinel configuration.
type Config struct {
	HealthPort         int
	CheckInterval      time.Duration
	HTTPPort           int
	HTTPSPort          int
	ForwardedPorts     []int
	HealthyThreshold   int
	UnhealthyThreshold int
	BinaryPort         int           // port to serve containarium binary for spot VM downloads (0 = disabled)
	RecoveryTimeout    time.Duration // warn if recovery takes longer than this (0 = no warning)
	CertSyncInterval   time.Duration // interval for syncing TLS certs from backend (0 = default 6h)
	KeySyncInterval    time.Duration // interval for syncing SSH keys from backend (0 = default 2m)
}

// Manager is the core sentinel orchestrator.
// It health-checks the backend spot VM and switches between proxy and maintenance modes.
type Manager struct {
	config   Config
	provider CloudProvider

	state atomic.Value // holds State
	spotIP string

	stopMaintenance func() // stops the HTTP/HTTPS maintenance servers
	certStore       *CertStore
	keyStore        *KeyStore

	// Recovery tracking
	outageStart    time.Time // when the current outage began
	lastPreemption time.Time // when the last preemption event was detected
	preemptCount   int       // total preemption events observed
}

// NewManager creates a new sentinel manager.
func NewManager(config Config, provider CloudProvider) *Manager {
	m := &Manager{
		config:    config,
		provider:  provider,
		certStore: NewCertStore(),
		keyStore:  NewKeyStore(),
	}
	m.state.Store(StateMaintenance)
	return m
}

// Run is the main loop. It starts in maintenance mode and switches between
// proxy and maintenance based on TCP health checks. Blocks until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) error {
	log.Printf("[sentinel] starting (check-interval=%s, health-port=%d, forwarded-ports=%v)",
		m.config.CheckInterval, m.config.HealthPort, m.config.ForwardedPorts)

	// Start binary server if configured
	if m.config.BinaryPort > 0 {
		stopBinary, err := StartBinaryServer(m.config.BinaryPort, m)
		if err != nil {
			log.Printf("[sentinel] warning: binary server not started: %v", err)
		} else {
			defer stopBinary()
		}
	}

	// Resolve backend IP
	ip, err := m.provider.GetInstanceIP(ctx)
	if err != nil {
		return err
	}
	m.spotIP = ip
	log.Printf("[sentinel] backend IP: %s", m.spotIP)

	// Start cert sync loop
	certSyncInterval := m.config.CertSyncInterval
	if certSyncInterval == 0 {
		certSyncInterval = 6 * time.Hour
	}
	go m.certStore.RunSyncLoop(ctx, m.spotIP, m.config.HealthPort, certSyncInterval)

	// Start key sync loop (for sshpiper SSH proxy configuration)
	keySyncInterval := m.config.KeySyncInterval
	if keySyncInterval == 0 {
		keySyncInterval = 2 * time.Minute
	}
	go m.keyStore.RunSyncLoop(ctx, m.spotIP, m.config.HealthPort, keySyncInterval)

	// Start in maintenance mode
	if err := m.switchToMaintenance(); err != nil {
		return err
	}

	// Start event watcher if provider supports it
	eventCh := make(chan VMEvent, 10)
	if watcher, ok := m.provider.(EventWatcher); ok {
		go func() {
			log.Printf("[sentinel] event watcher started (polling GCP operations every 10s)")
			if err := watcher.WatchEvents(ctx, eventCh); err != nil {
				log.Printf("[sentinel] event watcher error: %v", err)
			}
		}()
	}

	ticker := time.NewTicker(m.config.CheckInterval)
	defer ticker.Stop()

	var healthyCount int
	var unhealthyCount int

	for {
		select {
		case <-ctx.Done():
			log.Printf("[sentinel] shutting down...")
			m.cleanup()
			return nil

		case event := <-eventCh:
			m.handleEvent(ctx, event, &healthyCount, &unhealthyCount)

		case <-ticker.C:
			healthy := CheckHealth(m.spotIP, m.config.HealthPort, 5*time.Second)

			if healthy {
				unhealthyCount = 0
				healthyCount++
				if healthyCount >= m.config.HealthyThreshold && m.currentState() == StateMaintenance {
					recoveryDuration := time.Since(m.outageStart)
					log.Printf("[sentinel] backend healthy (%d consecutive checks), switching to proxy (recovery took %s)",
						healthyCount, recoveryDuration.Round(time.Second))
					if err := m.switchToProxy(); err != nil {
						log.Printf("[sentinel] failed to switch to proxy: %v", err)
					}
				}
			} else {
				healthyCount = 0
				unhealthyCount++
				if unhealthyCount >= m.config.UnhealthyThreshold && m.currentState() == StateProxy {
					log.Printf("[sentinel] backend unhealthy (%d consecutive checks), switching to maintenance", unhealthyCount)
					m.outageStart = time.Now()
					if err := m.switchToMaintenance(); err != nil {
						log.Printf("[sentinel] failed to switch to maintenance: %v", err)
					}
					m.diagnoseAndRecover(ctx)
				} else if unhealthyCount >= m.config.UnhealthyThreshold && m.currentState() == StateMaintenance {
					// Check recovery timeout
					m.checkRecoveryTimeout()
					// Periodically try to recover
					if unhealthyCount%4 == 0 {
						m.diagnoseAndRecover(ctx)
					}
				}
			}
		}
	}
}

func (m *Manager) currentState() State {
	return m.state.Load().(State)
}

func (m *Manager) switchToProxy() error {
	// Stop maintenance HTTP servers (ports needed for iptables)
	if m.stopMaintenance != nil {
		m.stopMaintenance()
		m.stopMaintenance = nil
	}

	// Immediate cert sync — spot VM is now healthy, grab fresh certs
	if err := m.certStore.Sync(m.spotIP, m.config.HealthPort); err != nil {
		log.Printf("[sentinel] cert sync on proxy switch failed: %v", err)
	} else {
		log.Printf("[sentinel] cert sync on proxy switch: %d certs", m.certStore.SyncedCount())
	}

	// Immediate key sync — update sshpiper config with fresh keys
	if err := m.keyStore.Sync(m.spotIP, m.config.HealthPort); err != nil {
		log.Printf("[sentinel] key sync on proxy switch failed: %v", err)
	} else {
		if err := m.keyStore.PushSentinelKey(m.spotIP, m.config.HealthPort); err != nil {
			log.Printf("[sentinel] push sentinel key on proxy switch failed: %v", err)
		}
		if err := m.keyStore.Apply(); err != nil {
			log.Printf("[sentinel] key apply on proxy switch failed: %v", err)
		} else {
			log.Printf("[sentinel] key sync on proxy switch: %d users", m.keyStore.SyncedCount())
			m.keyStore.RestartSSHPiper()
		}
	}

	// Enable iptables forwarding
	if err := enableForwarding(m.spotIP, m.config.ForwardedPorts); err != nil {
		return err
	}

	m.state.Store(StateProxy)
	log.Printf("[sentinel] mode: PROXY → forwarding to %s", m.spotIP)
	return nil
}

func (m *Manager) switchToMaintenance() error {
	// Disable iptables forwarding
	if err := disableForwarding(); err != nil {
		log.Printf("[sentinel] warning: failed to disable forwarding: %v", err)
	}

	// Start maintenance HTTP servers (if not already running)
	if m.stopMaintenance == nil {
		stop, err := startMaintenanceServers(m.config.HTTPPort, m.config.HTTPSPort, m.certStore, m)
		if err != nil {
			return err
		}
		m.stopMaintenance = stop
	}

	m.state.Store(StateMaintenance)
	log.Printf("[sentinel] mode: MAINTENANCE → serving maintenance page")
	return nil
}

// handleEvent processes a VM lifecycle event from the event watcher.
// Preemption events trigger an immediate switch to maintenance mode (fast-path).
func (m *Manager) handleEvent(ctx context.Context, event VMEvent, healthyCount, unhealthyCount *int) {
	switch event.Type {
	case EventPreempted:
		m.preemptCount++
		m.lastPreemption = event.Timestamp
		log.Printf("[sentinel] EVENT: PREEMPTION detected at %s (total: %d) — %s",
			event.Timestamp.Format(time.RFC3339), m.preemptCount, event.Detail)

		// Immediate switch — don't wait for health check threshold
		if m.currentState() == StateProxy {
			*healthyCount = 0
			*unhealthyCount = 0
			m.outageStart = event.Timestamp
			if err := m.switchToMaintenance(); err != nil {
				log.Printf("[sentinel] failed to switch to maintenance: %v", err)
			}
			m.diagnoseAndRecover(ctx)
		}

	case EventStopped:
		log.Printf("[sentinel] EVENT: VM stopped at %s — %s", event.Timestamp.Format(time.RFC3339), event.Detail)
		if m.currentState() == StateProxy {
			*healthyCount = 0
			*unhealthyCount = 0
			m.outageStart = event.Timestamp
			if err := m.switchToMaintenance(); err != nil {
				log.Printf("[sentinel] failed to switch to maintenance: %v", err)
			}
			m.diagnoseAndRecover(ctx)
		}

	case EventStarted:
		log.Printf("[sentinel] EVENT: VM started at %s — %s", event.Timestamp.Format(time.RFC3339), event.Detail)

	case EventTerminated:
		log.Printf("[sentinel] EVENT: VM terminated at %s — %s", event.Timestamp.Format(time.RFC3339), event.Detail)
		if m.currentState() == StateProxy {
			*healthyCount = 0
			*unhealthyCount = 0
			m.outageStart = event.Timestamp
			if err := m.switchToMaintenance(); err != nil {
				log.Printf("[sentinel] failed to switch to maintenance: %v", err)
			}
			m.diagnoseAndRecover(ctx)
		}
	}
}

// checkRecoveryTimeout logs a warning if recovery is taking too long.
func (m *Manager) checkRecoveryTimeout() {
	if m.config.RecoveryTimeout <= 0 || m.outageStart.IsZero() {
		return
	}
	elapsed := time.Since(m.outageStart)
	if elapsed > m.config.RecoveryTimeout {
		log.Printf("[sentinel] WARNING: recovery has taken %s (threshold: %s) — manual intervention may be needed",
			elapsed.Round(time.Second), m.config.RecoveryTimeout)
	}
}

// diagnoseAndRecover queries the cloud provider for VM status and attempts recovery.
func (m *Manager) diagnoseAndRecover(ctx context.Context) {
	status, err := m.provider.GetInstanceStatus(ctx)
	if err != nil {
		log.Printf("[sentinel] failed to get instance status: %v", err)
		return
	}

	log.Printf("[sentinel] backend VM status: %s", status)

	switch status {
	case StatusStopped, StatusTerminated:
		log.Printf("[sentinel] attempting to start backend VM...")
		if err := m.provider.StartInstance(ctx); err != nil {
			log.Printf("[sentinel] failed to start VM: %v", err)
		} else {
			log.Printf("[sentinel] start command sent, waiting for VM to boot...")
		}
	case StatusProvisioning:
		log.Printf("[sentinel] VM is provisioning, waiting for it to become ready...")
	case StatusRunning:
		log.Printf("[sentinel] VM reports running but health check failed — possible app-level issue")
	default:
		log.Printf("[sentinel] VM in unknown state: %s", status)
	}
}

func (m *Manager) cleanup() {
	if m.stopMaintenance != nil {
		m.stopMaintenance()
		m.stopMaintenance = nil
	}
	disableForwarding()
	log.Printf("[sentinel] cleanup complete")
}

// --- Exported state getters ---

// CurrentState returns the current sentinel mode (proxy or maintenance).
func (m *Manager) CurrentState() State {
	return m.currentState()
}

// SpotIP returns the backend spot VM IP address.
func (m *Manager) SpotIP() string {
	return m.spotIP
}

// OutageDuration returns the duration of the current outage, or 0 if not in maintenance.
func (m *Manager) OutageDuration() time.Duration {
	if m.currentState() != StateMaintenance || m.outageStart.IsZero() {
		return 0
	}
	return time.Since(m.outageStart)
}

// PreemptCount returns the total number of preemption events observed.
func (m *Manager) PreemptCount() int {
	return m.preemptCount
}

// LastPreemption returns the timestamp of the last preemption event.
func (m *Manager) LastPreemption() time.Time {
	return m.lastPreemption
}
