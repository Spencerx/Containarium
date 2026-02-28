package gateway

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// UserKeys holds a username and their authorized_keys content.
type UserKeys struct {
	Username       string `json:"username"`
	AuthorizedKeys string `json:"authorized_keys"`
}

// KeysResponse is the JSON response from the /authorized-keys endpoint.
type KeysResponse struct {
	Keys []UserKeys `json:"keys"`
}

// SentinelKeyRequest is the JSON body for POST /authorized-keys/sentinel.
type SentinelKeyRequest struct {
	PublicKey string `json:"public_key"`
}

// ServeAuthorizedKeys returns an HTTP handler that walks /home/*/.ssh/authorized_keys
// and returns all users' authorized keys as JSON. This allows the sentinel to sync
// SSH keys for sshpiper configuration.
func ServeAuthorizedKeys() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		entries, err := os.ReadDir("/home")
		if err != nil {
			log.Printf("[keys] failed to read /home: %v", err)
			http.Error(w, `{"error":"failed to read home directories"}`, http.StatusInternalServerError)
			return
		}

		var keys []UserKeys
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			username := entry.Name()
			akPath := filepath.Join("/home", username, ".ssh", "authorized_keys")
			data, err := os.ReadFile(akPath)
			if err != nil {
				continue // no authorized_keys file, skip
			}
			content := strings.TrimSpace(string(data))
			if content == "" {
				continue
			}
			keys = append(keys, UserKeys{
				Username:       username,
				AuthorizedKeys: content,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(KeysResponse{Keys: keys})
	}
}

// ServeSentinelKey returns an HTTP handler that accepts a POST with the sentinel's
// public key and appends it to all jump server users' authorized_keys files.
// This allows sshpiper on the sentinel to authenticate to the spot VM.
func ServeSentinelKey() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		var req SentinelKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
			return
		}

		pubKey := strings.TrimSpace(req.PublicKey)
		if pubKey == "" {
			http.Error(w, `{"error":"public_key is required"}`, http.StatusBadRequest)
			return
		}

		entries, err := os.ReadDir("/home")
		if err != nil {
			log.Printf("[keys] failed to read /home: %v", err)
			http.Error(w, `{"error":"failed to read home directories"}`, http.StatusInternalServerError)
			return
		}

		var updated int
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			username := entry.Name()
			sshDir := filepath.Join("/home", username, ".ssh")
			if _, err := os.Stat(sshDir); os.IsNotExist(err) {
				continue // no .ssh directory, skip
			}

			akPath := filepath.Join(sshDir, "authorized_keys")
			// Read existing content to check for duplicates
			existing, _ := os.ReadFile(akPath)
			if strings.Contains(string(existing), pubKey) {
				updated++
				continue // key already present
			}

			// Append the sentinel key
			f, err := os.OpenFile(akPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
			if err != nil {
				log.Printf("[keys] failed to open %s: %v", akPath, err)
				continue
			}
			fmt.Fprintf(f, "\n# sshpiper sentinel upstream key\n%s\n", pubKey)
			f.Close()

			// Fix ownership — the file should be owned by the user
			// We use Lchown-compatible approach: look up the dir owner
			info, err := os.Stat(sshDir)
			if err == nil {
				if stat, ok := fileOwner(info); ok {
					os.Chown(akPath, stat.uid, stat.gid)
				}
			}

			updated++
			log.Printf("[keys] added sentinel key to %s", akPath)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"updated": updated})
	}
}
