package cmd

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/footprintai/containarium/internal/app"
	"github.com/footprintai/containarium/internal/incus"
	"github.com/footprintai/containarium/internal/mtls"
	"github.com/footprintai/containarium/internal/network"
	"github.com/footprintai/containarium/internal/server"
	"github.com/spf13/cobra"
)

var (
	daemonAddress      string
	daemonPort         int
	daemonHTTPPort     int
	enableMTLS         bool
	enableREST         bool
	daemonCertsDir     string
	jwtSecret          string
	jwtSecretFile      string
	swaggerDir         string
	networkSubnet      string
	skipInfraInit      bool
	enableAppHosting   bool
	postgresConnString string
	baseDomain         string
	caddyAdminURL      string
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Run Containarium as a dual-protocol daemon service (gRPC + REST)",
	Long: `Start the Containarium daemon with both gRPC and REST/HTTP APIs for container management.

The daemon provides both gRPC and REST APIs for maximum flexibility:
  - gRPC API for high-performance programmatic access (mTLS authentication)
  - REST/HTTP API for web clients and easy integration (Bearer token authentication)
  - Interactive Swagger UI for API exploration and testing
  - OpenAPI spec generation for client code generation

Security:
  - gRPC: Uses mutual TLS (mTLS) for certificate-based authentication
  - REST: Uses Bearer tokens (JWT) for HTTP authentication
  - Both protocols can run simultaneously on different ports

Examples:
  # Run with both gRPC and REST APIs
  containarium daemon --mtls --rest --jwt-secret your-secret-key

  # Run gRPC only (original behavior)
  containarium daemon --mtls --rest=false

  # Run REST only (development)
  containarium daemon --rest --jwt-secret dev-secret

  # Use JWT secret from file
  containarium daemon --mtls --rest --jwt-secret-file /etc/containarium/jwt.secret

  # Custom ports
  containarium daemon --port 50051 --http-port 8080 --rest --jwt-secret secret

  # Run as systemd service (recommended for production)
  sudo systemctl start containarium`,
	RunE: runDaemon,
}

func init() {
	rootCmd.AddCommand(daemonCmd)

	// gRPC settings
	daemonCmd.Flags().StringVar(&daemonAddress, "address", "0.0.0.0", "Address to listen on")
	daemonCmd.Flags().IntVar(&daemonPort, "port", 50051, "gRPC port to listen on")
	daemonCmd.Flags().BoolVar(&enableMTLS, "mtls", false, "Enable mutual TLS authentication for gRPC (recommended)")
	daemonCmd.Flags().StringVar(&daemonCertsDir, "certs-dir", mtls.DefaultCertsDir, "Directory containing TLS certificates")

	// HTTP/REST settings
	daemonCmd.Flags().IntVar(&daemonHTTPPort, "http-port", 8080, "HTTP/REST port to listen on")
	daemonCmd.Flags().BoolVar(&enableREST, "rest", true, "Enable HTTP/REST API gateway")

	// Authentication settings
	daemonCmd.Flags().StringVar(&jwtSecret, "jwt-secret", "", "JWT secret key for REST API authentication")
	daemonCmd.Flags().StringVar(&jwtSecretFile, "jwt-secret-file", "", "Path to file containing JWT secret key")

	// Swagger/OpenAPI settings
	daemonCmd.Flags().StringVar(&swaggerDir, "swagger-dir", "api/swagger", "Directory containing Swagger/OpenAPI files")

	// Infrastructure settings
	daemonCmd.Flags().StringVar(&networkSubnet, "network-subnet", "10.100.0.1/24", "IPv4 subnet for container network (CIDR format, e.g., 10.100.0.1/24)")
	daemonCmd.Flags().BoolVar(&skipInfraInit, "skip-infra-init", false, "Skip automatic infrastructure initialization (storage, network, profile)")

	// App hosting settings
	daemonCmd.Flags().BoolVar(&enableAppHosting, "app-hosting", false, "Enable app hosting feature (requires PostgreSQL)")
	daemonCmd.Flags().StringVar(&postgresConnString, "postgres", "", "PostgreSQL connection string for app hosting (e.g., postgres://user:pass@host:5432/db)")
	daemonCmd.Flags().StringVar(&baseDomain, "base-domain", "containarium.dev", "Base domain for app subdomains (e.g., containarium.dev)")
	daemonCmd.Flags().StringVar(&caddyAdminURL, "caddy-admin-url", "", "Caddy admin API URL for reverse proxy configuration (leave empty for auto-setup with --app-hosting)")
}

func runDaemon(cmd *cobra.Command, args []string) error {
	// Check Incus version before starting daemon
	incusClient, err := incus.New()
	if err != nil {
		return fmt.Errorf("failed to connect to Incus: %w", err)
	}

	if warning, err := incusClient.CheckVersion(); err != nil {
		return fmt.Errorf("failed to check Incus version: %w", err)
	} else if warning != "" {
		// Print warning but continue - don't block daemon startup
		log.Printf("\n%s\n", warning)
	} else {
		// Version is OK, log it
		serverInfo, _ := incusClient.GetServerInfo()
		if serverInfo != nil {
			log.Printf("Incus version: %s (OK)", serverInfo.Environment.ServerVersion)
		}
	}

	// Initialize infrastructure (storage, network, profile) unless skipped
	if !skipInfraInit {
		log.Printf("Initializing infrastructure...")
		networkConfig := incus.NetworkConfig{
			Name:        "incusbr0",
			IPv4Address: networkSubnet,
			IPv4NAT:     true,
		}
		if err := incusClient.InitializeInfrastructure(networkConfig); err != nil {
			return fmt.Errorf("failed to initialize infrastructure: %w", err)
		}
		log.Printf("  Network: incusbr0 (%s)", networkSubnet)
		log.Printf("  Storage: default (dir)")
		log.Printf("  Profile: default (configured)")
	}

	// Always auto-detect Caddy container IP if no URL specified
	// This ensures port forwarding (80/443 → Caddy) and route sync work
	// even without --app-hosting (e.g., after VM recreation)
	if caddyAdminURL == "" {
		log.Printf("Auto-detecting Caddy container IP...")
		if caddyIP, err := incusClient.FindCaddyContainerIP(); err != nil {
			log.Printf("Warning: Could not auto-detect Caddy container: %v", err)
			log.Printf("  Features requiring Caddy (routes, TLS, port forwarding) will not work")
			log.Printf("  To fix: ensure a container with 'caddy' in its name is running,")
			log.Printf("  or specify --caddy-admin-url explicitly")
		} else {
			caddyAdminURL = fmt.Sprintf("http://%s:2019", caddyIP)
			log.Printf("  Detected Caddy at: %s", caddyAdminURL)

			// Set up port forwarding from host to Caddy for Let's Encrypt and HTTPS
			if network.CheckIPTablesAvailable() {
				portForwarder := network.NewPortForwarder(caddyIP)
				if err := portForwarder.SetupPortForwarding(); err != nil {
					log.Printf("Warning: Failed to setup port forwarding: %v", err)
					log.Printf("  External HTTPS for app domains may not work")
					log.Printf("  You may need to manually configure iptables - see docs/CADDY-SETUP.md")
				}
			} else {
				log.Printf("Warning: iptables not available, skipping port forwarding setup")
				log.Printf("  External HTTPS for app domains may not work without manual configuration")
			}
		}
	}

	// Auto-detect PostgreSQL container IP if no --postgres flag specified
	if postgresConnString == "" {
		if pgIP, err := incusClient.GetContainerIP(server.CorePostgresContainer); err == nil && pgIP != "" {
			postgresConnString = fmt.Sprintf(
				"postgres://%s:%s@%s:%d/%s?sslmode=disable",
				server.DefaultPostgresUser, server.DefaultPostgresPassword,
				pgIP, server.DefaultPostgresPort, server.DefaultPostgresDB)
			log.Printf("Detected PostgreSQL at: %s", pgIP)
		} else {
			log.Printf("PostgreSQL auto-detect: container %s not found or has no IP", server.CorePostgresContainer)
		}
	}

	// Load persisted daemon config from PostgreSQL (values saved by previous runs).
	// CLI flags that were explicitly set always override DB values.
	var daemonConfigStore *app.DaemonConfigStore
	if postgresConnString != "" {
		pool, poolErr := pgxpool.New(context.Background(), postgresConnString)
		if poolErr == nil {
			if pingErr := pool.Ping(context.Background()); pingErr == nil {
				cs, csErr := app.NewDaemonConfigStore(context.Background(), pool)
				if csErr == nil {
					daemonConfigStore = cs
					savedConfig, loadErr := cs.GetAll(context.Background())
					if loadErr == nil && len(savedConfig) > 0 {
						log.Printf("Loaded daemon config from PostgreSQL (%d keys)", len(savedConfig))
						if !cmd.Flags().Changed("base-domain") {
							if v, ok := savedConfig["base_domain"]; ok {
								baseDomain = v
								log.Printf("  base-domain = %s (from DB)", v)
							}
						}
						if !cmd.Flags().Changed("http-port") {
							if v, ok := savedConfig["http_port"]; ok {
								if parsed, err := strconv.Atoi(v); err == nil {
									daemonHTTPPort = parsed
									log.Printf("  http-port = %d (from DB)", parsed)
								}
							}
						}
						if !cmd.Flags().Changed("port") {
							if v, ok := savedConfig["grpc_port"]; ok {
								if parsed, err := strconv.Atoi(v); err == nil {
									daemonPort = parsed
									log.Printf("  port = %d (from DB)", parsed)
								}
							}
						}
						if !cmd.Flags().Changed("address") {
							if v, ok := savedConfig["listen_address"]; ok {
								daemonAddress = v
								log.Printf("  address = %s (from DB)", v)
							}
						}
						if !cmd.Flags().Changed("mtls") {
							if v, ok := savedConfig["enable_mtls"]; ok {
								if parsed, err := strconv.ParseBool(v); err == nil {
									enableMTLS = parsed
									log.Printf("  mtls = %v (from DB)", parsed)
								}
							}
						}
						if !cmd.Flags().Changed("rest") {
							if v, ok := savedConfig["enable_rest"]; ok {
								if parsed, err := strconv.ParseBool(v); err == nil {
									enableREST = parsed
									log.Printf("  rest = %v (from DB)", parsed)
								}
							}
						}
						if !cmd.Flags().Changed("app-hosting") {
							if v, ok := savedConfig["enable_app_hosting"]; ok {
								if parsed, err := strconv.ParseBool(v); err == nil {
									enableAppHosting = parsed
									log.Printf("  app-hosting = %v (from DB)", parsed)
								}
							}
						}
					} else if loadErr != nil {
						log.Printf("Warning: Failed to load daemon config from DB: %v", loadErr)
					}
				} else {
					log.Printf("Warning: Failed to create daemon config store: %v", csErr)
					pool.Close()
				}
			} else {
				log.Printf("Warning: Failed to ping PostgreSQL for config: %v", pingErr)
				pool.Close()
			}
		} else {
			log.Printf("Warning: Failed to connect to PostgreSQL for config: %v", poolErr)
		}
	}

	// Save recovery config to persistent storage (for disaster recovery)
	if err := saveRecoveryConfigToPersistentStorage(networkSubnet, baseDomain, caddyAdminURL, jwtSecretFile, enableAppHosting); err != nil {
		// Log warning but don't fail - recovery config is optional
		log.Printf("Warning: Failed to save recovery config: %v", err)
	}

	// Load or generate JWT secret if REST is enabled
	var finalJWTSecret string
	var isRandomSecret bool
	if enableREST {
		// Priority 1: Environment variable (silent - production use)
		if envSecret := os.Getenv("CONTAINARIUM_JWT_SECRET"); envSecret != "" {
			finalJWTSecret = envSecret
			log.Printf("Using JWT secret from CONTAINARIUM_JWT_SECRET environment variable")
		} else if jwtSecretFile != "" {
			// Priority 2: Secret file (production use)
			secretBytes, err := os.ReadFile(jwtSecretFile)
			if err != nil {
				return fmt.Errorf("failed to read JWT secret file %s: %w", jwtSecretFile, err)
			}
			finalJWTSecret = strings.TrimSpace(string(secretBytes))
			log.Printf("Loaded JWT secret from file: %s", jwtSecretFile)
		} else if jwtSecret != "" {
			// Priority 3: Command-line flag (testing use)
			finalJWTSecret = jwtSecret
			log.Printf("Using JWT secret from --jwt-secret flag")
		} else {
			// Priority 4: Generate random (development use)
			finalJWTSecret = generateRandomSecret()
			isRandomSecret = true
		}
	}

	// Create dual server config
	config := &server.DualServerConfig{
		GRPCAddress:        daemonAddress,
		GRPCPort:           daemonPort,
		EnableMTLS:         enableMTLS,
		CertsDir:           daemonCertsDir,
		HTTPPort:           daemonHTTPPort,
		EnableREST:         enableREST,
		JWTSecret:          finalJWTSecret,
		SwaggerDir:         swaggerDir,
		EnableAppHosting:   enableAppHosting,
		PostgresConnString: postgresConnString,
		BaseDomain:         baseDomain,
		CaddyAdminURL:      caddyAdminURL,
		HostIP:             hostIPFromCIDR(networkSubnet),
		DaemonConfigStore:  daemonConfigStore,
	}

	// Create dual server
	dualServer, err := server.NewDualServer(config)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("\nReceived shutdown signal")
		cancel()
	}()

	// Start servers
	log.Printf("Containarium daemon starting...")
	log.Printf("  gRPC: %s:%d", daemonAddress, daemonPort)
	if enableMTLS {
		log.Printf("  gRPC authentication: mTLS (certificate-based)")
	} else {
		log.Printf("  gRPC authentication: INSECURE (no authentication)")
	}
	if enableREST {
		log.Printf("  HTTP/REST: %s:%d", daemonAddress, daemonHTTPPort)
		log.Printf("  REST authentication: Bearer tokens (JWT)")
		log.Printf("  Swagger UI: http://localhost:%d/swagger-ui/", daemonHTTPPort)
		log.Printf("  OpenAPI spec: http://localhost:%d/swagger.json", daemonHTTPPort)

		// If using random secret, print it prominently for easy token generation
		if isRandomSecret {
			log.Printf("")
			log.Printf("═══════════════════════════════════════════════════════════════")
			log.Printf("  🔐 JWT Secret (Auto-Generated)")
			log.Printf("═══════════════════════════════════════════════════════════════")
			log.Printf("")
			log.Printf("  %s", finalJWTSecret)
			log.Printf("")
			log.Printf("⚠️  This secret is random and temporary!")
			log.Printf("   • Tokens will be invalid after daemon restart")
			log.Printf("   • For production, use one of these methods:")
			log.Printf("     - Environment: export CONTAINARIUM_JWT_SECRET='your-secret'")
			log.Printf("     - File: --jwt-secret-file /etc/containarium/jwt.secret")
			log.Printf("     - Flag: --jwt-secret 'your-secret'")
			log.Printf("")
			log.Printf("Generate a token:")
			log.Printf("  containarium token generate --username admin --secret '%s'", finalJWTSecret)
			log.Printf("")
			log.Printf("═══════════════════════════════════════════════════════════════")
			log.Printf("")
		}
	}
	log.Printf("Press Ctrl+C to stop")

	return dualServer.Start(ctx)
}

// generateRandomSecret generates a random secret for development mode
func generateRandomSecret() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

// hostIPFromCIDR extracts the host IP address from a CIDR notation string.
// e.g., "10.100.0.1/24" → "10.100.0.1"
func hostIPFromCIDR(cidr string) string {
	if idx := strings.Index(cidr, "/"); idx > 0 {
		return cidr[:idx]
	}
	return cidr
}

// saveRecoveryConfigToPersistentStorage saves recovery config to persistent disk
// This enables auto-recovery after instance recreation
func saveRecoveryConfigToPersistentStorage(networkCIDR, baseDomain, caddyAdminURL, jwtSecretFile string, appHosting bool) error {
	// Only save if the persistent storage path exists
	persistentDir := "/mnt/incus-data"
	if _, err := os.Stat(persistentDir); os.IsNotExist(err) {
		// Persistent storage not mounted, skip
		return nil
	}

	// Try to get ZFS source from current storage pool
	zfsSource := ""
	cmd := exec.Command("incus", "storage", "get", "default", "source")
	output, err := cmd.Output()
	if err == nil {
		zfsSource = strings.TrimSpace(string(output))
	}

	config := &RecoveryConfig{
		NetworkName:     "incusbr0",
		NetworkCIDR:     networkCIDR,
		StoragePoolName: "default",
		StorageDriver:   "zfs",
		ZFSSource:       zfsSource,
		DaemonFlags: DaemonConfig{
			Address:       daemonAddress,
			Port:          daemonPort,
			HTTPPort:      daemonHTTPPort,
			BaseDomain:    baseDomain,
			CaddyAdminURL: caddyAdminURL,
			JWTSecretFile: jwtSecretFile,
			AppHosting:    appHosting,
			SkipInfraInit: true, // Always skip on recovery
		},
	}

	return SaveRecoveryConfig(config, DefaultRecoveryConfigPath)
}
