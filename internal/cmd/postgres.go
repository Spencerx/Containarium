package cmd

import "os"

// getPostgresConnString returns the PostgreSQL connection string from the
// environment or falls back to the default in-cluster address.
func getPostgresConnString() string {
	if url := os.Getenv("CONTAINARIUM_POSTGRES_URL"); url != "" {
		return url
	}
	return "postgres://containarium:containarium@10.100.0.2:5432/containarium?sslmode=disable"
}
