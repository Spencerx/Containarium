package sentinel

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

const defaultBinaryPath = "/usr/local/bin/containarium"

// StatusJSON is the JSON response for the /status endpoint.
type StatusJSON struct {
	State          string `json:"state"`
	SpotIP         string `json:"spot_ip"`
	PreemptCount   int    `json:"preempt_count"`
	OutageDuration string `json:"outage_duration,omitempty"`
	LastPreemption string `json:"last_preemption,omitempty"`
	CertSyncCount  int    `json:"cert_sync_count"`
	CertLastSync   string `json:"cert_last_sync,omitempty"`
	CertSyncError  string `json:"cert_sync_error,omitempty"`
}

// StartBinaryServer starts an HTTP server that serves the containarium binary
// for the spot VM to download, plus a /status JSON endpoint.
// This runs on an internal-only port (default 8888).
func StartBinaryServer(port int, manager *Manager) (stop func(), err error) {
	binaryPath := defaultBinaryPath

	// Verify binary exists
	if _, err := os.Stat(binaryPath); err != nil {
		return nil, fmt.Errorf("binary not found at %s: %w", binaryPath, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/containarium", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename=containarium")
		http.ServeFile(w, r, binaryPath)
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Status JSON endpoint — always available
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		status := StatusJSON{
			State:         string(manager.CurrentState()),
			SpotIP:        manager.SpotIP(),
			PreemptCount:  manager.PreemptCount(),
			CertSyncCount: manager.certStore.SyncedCount(),
		}

		if od := manager.OutageDuration(); od > 0 {
			status.OutageDuration = od.Round(time.Second).String()
		}
		if lp := manager.LastPreemption(); !lp.IsZero() {
			status.LastPreemption = lp.Format(time.RFC3339)
		}
		if ls := manager.certStore.LastSync(); !ls.IsZero() {
			status.CertLastSync = ls.Format(time.RFC3339)
		}
		if err := manager.certStore.LastSyncErr(); err != nil {
			status.CertSyncError = err.Error()
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	})

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second, // large file transfer
	}

	go func() {
		log.Printf("[sentinel] binary server listening on :%d (serving %s)", port, binaryPath)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("[sentinel] binary server error: %v", err)
		}
	}()

	return func() { srv.Close() }, nil
}
