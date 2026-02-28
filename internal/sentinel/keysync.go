package sentinel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/footprintai/containarium/internal/gateway"
)

const (
	sshpiperConfigDir  = "/etc/sshpiper"
	sshpiperConfigFile = "/etc/sshpiper/config.yaml"
	sshpiperUsersDir   = "/etc/sshpiper/users"
	sshpiperUpstreamKey = "/etc/sshpiper/upstream_key"
)

// KeyStore syncs SSH authorized keys from the backend and generates
// sshpiper YAML configuration. Follows the same pattern as CertStore.
type KeyStore struct {
	mu            sync.RWMutex
	users         []gateway.UserKeys // synced user keys
	lastSync      time.Time
	lastSyncErr   error
	syncedCount   int
	backendIP     string // cached for sshpiper config generation
	configChanged bool   // true if Apply() wrote new config (avoids unnecessary restarts)
}

// NewKeyStore creates a new KeyStore.
func NewKeyStore() *KeyStore {
	return &KeyStore{}
}

// Sync fetches authorized keys from the backend daemon's /authorized-keys endpoint.
func (ks *KeyStore) Sync(backendIP string, httpPort int) error {
	url := fmt.Sprintf("http://%s:%d/authorized-keys", backendIP, httpPort)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		ks.mu.Lock()
		ks.lastSyncErr = err
		ks.mu.Unlock()
		return fmt.Errorf("key sync GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("key sync: unexpected status %d from %s", resp.StatusCode, url)
		ks.mu.Lock()
		ks.lastSyncErr = err
		ks.mu.Unlock()
		return err
	}

	var keysResp gateway.KeysResponse
	if err := json.NewDecoder(resp.Body).Decode(&keysResp); err != nil {
		ks.mu.Lock()
		ks.lastSyncErr = err
		ks.mu.Unlock()
		return fmt.Errorf("key sync: decode response: %w", err)
	}

	ks.mu.Lock()
	ks.users = keysResp.Keys
	ks.lastSync = time.Now()
	ks.lastSyncErr = nil
	ks.syncedCount = len(keysResp.Keys)
	ks.backendIP = backendIP
	ks.mu.Unlock()

	return nil
}

// Apply writes the sshpiper YAML config and per-user authorized_keys files.
func (ks *KeyStore) Apply() error {
	ks.mu.RLock()
	users := ks.users
	backendIP := ks.backendIP
	ks.mu.RUnlock()

	if len(users) == 0 {
		return fmt.Errorf("no users to configure")
	}

	// Ensure directories exist
	if err := os.MkdirAll(sshpiperUsersDir, 0755); err != nil {
		return fmt.Errorf("failed to create sshpiper users dir: %w", err)
	}

	// Write per-user authorized_keys
	for _, u := range users {
		userDir := filepath.Join(sshpiperUsersDir, u.Username)
		if err := os.MkdirAll(userDir, 0755); err != nil {
			log.Printf("[keysync] failed to create dir for %s: %v", u.Username, err)
			continue
		}
		akPath := filepath.Join(userDir, "authorized_keys")
		if err := os.WriteFile(akPath, []byte(u.AuthorizedKeys+"\n"), 0600); err != nil {
			log.Printf("[keysync] failed to write authorized_keys for %s: %v", u.Username, err)
			continue
		}
	}

	// Generate sshpiper YAML config
	var buf bytes.Buffer
	buf.WriteString("version: \"1.0\"\npipes:\n")

	for _, u := range users {
		akPath := filepath.Join(sshpiperUsersDir, u.Username, "authorized_keys")
		fmt.Fprintf(&buf, "  - from:\n")
		fmt.Fprintf(&buf, "      - username: %q\n", u.Username)
		fmt.Fprintf(&buf, "        authorized_keys:\n")
		fmt.Fprintf(&buf, "          - %s\n", akPath)
		fmt.Fprintf(&buf, "    to:\n")
		fmt.Fprintf(&buf, "      host: %s:22\n", backendIP)
		fmt.Fprintf(&buf, "      username: %q\n", u.Username)
		fmt.Fprintf(&buf, "      ignore_hostkey: true\n")
		fmt.Fprintf(&buf, "      private_key: %s\n", sshpiperUpstreamKey)
	}

	newContent := buf.Bytes()

	// Compare with existing config — only write if changed to avoid unnecessary sshpiper restarts
	oldContent, _ := os.ReadFile(sshpiperConfigFile)
	if bytes.Equal(oldContent, newContent) {
		ks.mu.Lock()
		ks.configChanged = false
		ks.mu.Unlock()
		return nil
	}

	if err := os.WriteFile(sshpiperConfigFile, newContent, 0600); err != nil {
		return fmt.Errorf("failed to write sshpiper config: %w", err)
	}

	ks.mu.Lock()
	ks.configChanged = true
	ks.mu.Unlock()

	log.Printf("[keysync] sshpiper config updated: %d users", len(users))
	return nil
}

// PushSentinelKey sends the sentinel's upstream public key to the backend
// so it gets added to all jump server users' authorized_keys.
func (ks *KeyStore) PushSentinelKey(backendIP string, httpPort int) error {
	pubKeyPath := sshpiperUpstreamKey + ".pub"
	pubKey, err := os.ReadFile(pubKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read sentinel upstream public key %s: %w", pubKeyPath, err)
	}

	url := fmt.Sprintf("http://%s:%d/authorized-keys/sentinel", backendIP, httpPort)
	body, _ := json.Marshal(gateway.SentinelKeyRequest{PublicKey: strings.TrimSpace(string(pubKey))})

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("push sentinel key POST %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("push sentinel key: unexpected status %d", resp.StatusCode)
	}

	log.Printf("[keysync] sentinel upstream key pushed to backend")
	return nil
}

// RestartSSHPiper restarts the sshpiper systemd service to pick up config changes.
func (ks *KeyStore) RestartSSHPiper() error {
	if runtime.GOOS != "linux" {
		log.Printf("[keysync] sshpiper restart: skipping on %s", runtime.GOOS)
		return nil
	}
	if err := exec.Command("systemctl", "restart", "sshpiper").Run(); err != nil {
		return fmt.Errorf("failed to restart sshpiper: %w", err)
	}
	log.Printf("[keysync] sshpiper restarted")
	return nil
}

// RunSyncLoop periodically syncs keys from the backend and updates sshpiper config.
// Blocks until ctx is cancelled.
func (ks *KeyStore) RunSyncLoop(ctx context.Context, backendIP string, httpPort int, interval time.Duration) {
	log.Printf("[keysync] starting sync loop (backend=%s:%d, interval=%s)", backendIP, httpPort, interval)

	// Initial sync attempt
	ks.syncAndApply(backendIP, httpPort)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[keysync] sync loop stopped")
			return
		case <-ticker.C:
			ks.syncAndApply(backendIP, httpPort)
		}
	}
}

func (ks *KeyStore) syncAndApply(backendIP string, httpPort int) {
	if err := ks.Sync(backendIP, httpPort); err != nil {
		log.Printf("[keysync] sync failed: %v", err)
		return
	}

	ks.mu.RLock()
	count := ks.syncedCount
	ks.mu.RUnlock()
	log.Printf("[keysync] sync OK: %d users", count)

	// Push sentinel key to backend so sshpiper can authenticate upstream
	if err := ks.PushSentinelKey(backendIP, httpPort); err != nil {
		log.Printf("[keysync] push sentinel key failed: %v", err)
	}

	// Write config (only if changed) and restart sshpiper only when needed
	if err := ks.Apply(); err != nil {
		log.Printf("[keysync] apply failed: %v", err)
		return
	}

	ks.mu.RLock()
	changed := ks.configChanged
	ks.mu.RUnlock()

	if changed {
		if err := ks.RestartSSHPiper(); err != nil {
			log.Printf("[keysync] sshpiper restart failed: %v", err)
		}
	}
}

// SyncedCount returns the number of users currently synced.
func (ks *KeyStore) SyncedCount() int {
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	return ks.syncedCount
}

// LastSync returns the time of the last successful sync.
func (ks *KeyStore) LastSync() time.Time {
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	return ks.lastSync
}

// LastSyncErr returns the error from the last sync attempt, or nil.
func (ks *KeyStore) LastSyncErr() error {
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	return ks.lastSyncErr
}
