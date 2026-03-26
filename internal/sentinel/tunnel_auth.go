package sentinel

import (
	"encoding/json"
	"fmt"
	"io"
)

// TunnelHandshake is sent by the spot (tunnel client) to the sentinel (tunnel server)
// immediately after the TCP connection is established.
type TunnelHandshake struct {
	Token  string `json:"token"`
	SpotID string `json:"spot_id"`
	Ports  []int  `json:"ports"`
}

// TunnelHandshakeResponse is sent by the sentinel back to the spot after
// validating the handshake.
type TunnelHandshakeResponse struct {
	OK         bool   `json:"ok"`
	AssignedIP string `json:"assigned_ip,omitempty"`
	Error      string `json:"error,omitempty"`
}

// readHandshake reads and decodes a TunnelHandshake from the connection.
func readHandshake(r io.Reader) (*TunnelHandshake, error) {
	var hs TunnelHandshake
	dec := json.NewDecoder(r)
	if err := dec.Decode(&hs); err != nil {
		return nil, fmt.Errorf("decode handshake: %w", err)
	}
	return &hs, nil
}

// writeHandshake encodes and writes a TunnelHandshake to the connection.
func writeHandshake(w io.Writer, hs *TunnelHandshake) error {
	return json.NewEncoder(w).Encode(hs)
}

// readHandshakeResponse reads and decodes a TunnelHandshakeResponse.
func readHandshakeResponse(r io.Reader) (*TunnelHandshakeResponse, error) {
	var resp TunnelHandshakeResponse
	dec := json.NewDecoder(r)
	if err := dec.Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode handshake response: %w", err)
	}
	return &resp, nil
}

// writeHandshakeResponse encodes and writes a TunnelHandshakeResponse.
func writeHandshakeResponse(w io.Writer, resp *TunnelHandshakeResponse) error {
	return json.NewEncoder(w).Encode(resp)
}

// validateHandshake checks the handshake token and required fields.
func validateHandshake(hs *TunnelHandshake, expectedToken string) error {
	if hs.Token != expectedToken {
		return fmt.Errorf("invalid token")
	}
	if hs.SpotID == "" {
		return fmt.Errorf("spot_id is required")
	}
	if len(hs.Ports) == 0 {
		return fmt.Errorf("at least one port is required")
	}
	return nil
}
