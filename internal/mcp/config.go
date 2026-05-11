package mcp

import (
	"os"
	"strconv"
)

// Config holds configuration for the MCP server
type Config struct {
	// ServerURL is the base URL of the Containarium REST API
	// Example: http://localhost:8080 or https://containarium.example.com
	ServerURL string

	// JWTToken is the JWT token for authentication
	JWTToken string

	// SentinelHost is the public SSH endpoint for the deployment.
	// When set, create_container's response includes a ready-to-paste
	// `ssh -i <key> <user>@<sentinel-host>` command — agents don't have
	// to figure out hostnames or modify ~/.ssh/config. Empty means
	// the response falls back to a placeholder.
	// Example: sentinel.example.com or 34.42.156.100
	SentinelHost string

	// Debug enables debug logging
	Debug bool
}

// LoadConfig loads configuration from environment variables
func LoadConfig() *Config {
	debug := false
	if debugStr := os.Getenv("CONTAINARIUM_DEBUG"); debugStr != "" {
		debug, _ = strconv.ParseBool(debugStr)
	}

	return &Config{
		ServerURL:    os.Getenv("CONTAINARIUM_SERVER_URL"),
		JWTToken:     os.Getenv("CONTAINARIUM_JWT_TOKEN"),
		SentinelHost: os.Getenv("CONTAINARIUM_SENTINEL_HOST"),
		Debug:        debug,
	}
}
