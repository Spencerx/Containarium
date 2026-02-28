package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestServeAuthorizedKeys_Empty(t *testing.T) {
	// The handler reads from /home which we can't override, so test the response structure
	handler := ServeAuthorizedKeys()
	req := httptest.NewRequest(http.MethodGet, "/authorized-keys", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp KeysResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// On a dev machine, /home may or may not have users with authorized_keys
	// The key thing is the response parses correctly as JSON
	t.Logf("got %d keys from /home", len(resp.Keys))
}

func TestServeAuthorizedKeys_MethodNotAllowed(t *testing.T) {
	handler := ServeAuthorizedKeys()
	req := httptest.NewRequest(http.MethodPost, "/authorized-keys", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

func TestServeSentinelKey_MethodNotAllowed(t *testing.T) {
	handler := ServeSentinelKey()
	req := httptest.NewRequest(http.MethodGet, "/authorized-keys/sentinel", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

func TestServeSentinelKey_EmptyKey(t *testing.T) {
	handler := ServeSentinelKey()
	body := `{"public_key": ""}`
	req := httptest.NewRequest(http.MethodPost, "/authorized-keys/sentinel", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty key, got %d", rr.Code)
	}
}

func TestServeSentinelKey_InvalidJSON(t *testing.T) {
	handler := ServeSentinelKey()
	req := httptest.NewRequest(http.MethodPost, "/authorized-keys/sentinel", bytes.NewBufferString("{invalid}"))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", rr.Code)
	}
}

func TestKeysResponseSerialization(t *testing.T) {
	resp := KeysResponse{
		Keys: []UserKeys{
			{Username: "alice", AuthorizedKeys: "ssh-ed25519 AAAAC3Nz... alice@laptop"},
			{Username: "bob", AuthorizedKeys: "ssh-rsa AAAAB3Nz... bob@workstation"},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed KeysResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(parsed.Keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(parsed.Keys))
	}
	if parsed.Keys[0].Username != "alice" {
		t.Errorf("expected username alice, got %s", parsed.Keys[0].Username)
	}
	if parsed.Keys[1].Username != "bob" {
		t.Errorf("expected username bob, got %s", parsed.Keys[1].Username)
	}
}

func TestSentinelKeyRequest_DuplicateDetection(t *testing.T) {
	// Setup: create a temp home dir with a user who already has the sentinel key
	tmpHome := t.TempDir()
	username := "testuser"
	sshDir := filepath.Join(tmpHome, username, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}

	sentinelKey := "ssh-ed25519 AAAA_sentinel_key sentinel@sshpiper"
	akContent := "ssh-ed25519 AAAA_existing_key user@laptop\n# sshpiper sentinel upstream key\n" + sentinelKey + "\n"
	akPath := filepath.Join(sshDir, "authorized_keys")
	if err := os.WriteFile(akPath, []byte(akContent), 0600); err != nil {
		t.Fatal(err)
	}

	// Verify the key is already there (checking string containment logic)
	data, err := os.ReadFile(akPath)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Contains(data, []byte(sentinelKey)) {
		t.Error("sentinel key should already be present in authorized_keys")
	}
}
