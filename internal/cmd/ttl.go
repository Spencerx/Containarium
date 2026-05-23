package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TODO(ttl-server-side): The TTL verbs below are CLI-only for now. The
// server-side enforcement (so a box actually auto-deletes when its TTL
// expires) still needs to land. Specifically:
//
//   1. Add a `SetContainerTTL` (and `GetContainerTTL`) RPC to
//      proto/containarium/v1/container.proto, with a `google.api.http`
//      annotation so grpc-gateway exposes a REST shim (PUT/GET
//      /v1/containers/{username}/ttl), and regenerate via `make proto`.
//   2. Persist `expires_at` (and `ttl_set_at`) on the container row in
//      the daemon's container metadata store.
//   3. Add a sweeper goroutine in the daemon's main loop that wakes on
//      a short tick (e.g. every 30s) and force-deletes any container
//      whose `expires_at` is in the past.
//   4. Wire `internal/client/{grpc,http}.go` so SetContainerTTL /
//      GetContainerTTL hit the new endpoint; then swap the
//      synthetic-Unimplemented stub in ttlClientStub below for the
//      real client call.
//
// Until that lands, this verb is wired up so that callers (notably
// the containarium-run GitHub Action's "keep on failure" feature)
// can call `containarium ttl set <box> 1h` and get a clean,
// non-fatal "server does not support TTL yet" message rather than a
// confusing error. The Action can then proceed; the box just won't
// auto-delete and will need manual cleanup. Once the server-side
// lands, the same CLI call will Just Work — no Action change needed.

// maxTTL caps the duration a caller can request. 168h (7 days) is the
// upper bound: long enough to cover a long weekend debug session on a
// failed CI run, short enough that a forgotten box won't bleed cloud
// budget for a month. Reject anything longer with InvalidArgument so
// callers see a clear error rather than a silently clamped value.
const maxTTL = 168 * time.Hour

var ttlCmd = &cobra.Command{
	Use:   "ttl",
	Short: "Manage container time-to-live (auto-delete) settings",
	Long: `Manage the TTL (time-to-live) on a container. When a TTL is set,
the container is scheduled for automatic deletion after the given
duration. Useful for ephemeral/CI boxes that should not leak when a
test run fails and the box is kept open for debugging.

Examples:
  # Keep this box alive for 1 hour, then auto-delete
  containarium ttl set alice 1h

  # Update an existing TTL to 30 minutes from now
  containarium ttl set alice 30m

  # Inspect the current TTL
  containarium ttl get alice

  # Clear the TTL (box will never auto-delete)
  containarium ttl unset alice

Maximum allowed TTL is 168h (7 days).`,
}

var ttlSetCmd = &cobra.Command{
	Use:   "set <username> <duration>",
	Short: "Set or update the TTL on a container",
	Long: `Set the TTL on a container. The container will be auto-deleted
after the given duration. Duration uses Go's time.ParseDuration
format ("30m", "1h", "24h", etc.). Maximum allowed: 168h (7 days).

If a TTL is already set, this overrides it (the timer restarts
from now). To clear an existing TTL, use 'containarium ttl unset'.

Examples:
  containarium ttl set alice 1h
  containarium ttl set ci-bob-pr123 30m
  containarium ttl set debug-box 24h`,
	Args: cobra.ExactArgs(2),
	RunE: runTTLSet,
}

var ttlGetCmd = &cobra.Command{
	Use:   "get <username>",
	Short: "Show the current TTL on a container",
	Long: `Print the TTL setting and the remaining time before
auto-delete. Prints "no TTL set" if the container has no TTL.

Examples:
  containarium ttl get alice`,
	Args: cobra.ExactArgs(1),
	RunE: runTTLGet,
}

var ttlUnsetCmd = &cobra.Command{
	Use:   "unset <username>",
	Short: "Clear the TTL on a container (it will never auto-delete)",
	Args:  cobra.ExactArgs(1),
	RunE:  runTTLUnset,
}

func init() {
	rootCmd.AddCommand(ttlCmd)
	ttlCmd.AddCommand(ttlSetCmd)
	ttlCmd.AddCommand(ttlGetCmd)
	ttlCmd.AddCommand(ttlUnsetCmd)
}

// parseTTL parses and validates a Go-format duration string against
// the maxTTL cap. Returns InvalidArgument-flavoured errors so callers
// can distinguish "your input was wrong" from "the server choked".
func parseTTL(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("ttl duration is required")
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w (expected Go duration format like '30m', '1h', '24h')", s, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("ttl duration must be positive, got %s", s)
	}
	if d > maxTTL {
		return 0, fmt.Errorf("ttl duration %s exceeds maximum of %s (7 days); use a smaller value or delete the container manually", s, maxTTL)
	}
	return d, nil
}

func runTTLSet(cmd *cobra.Command, args []string) error {
	username := args[0]
	durStr := args[1]

	d, err := parseTTL(durStr)
	if err != nil {
		return err
	}

	if serverAddr == "" {
		return fmt.Errorf("--server is required (ttl scheduling is a server-side feature)")
	}

	expiresAt := time.Now().Add(d).UTC()
	if verbose {
		fmt.Printf("Setting TTL on %s: %s (expires at %s)\n", username, d, expiresAt.Format(time.RFC3339))
	}

	err = ttlClientSet(username, d)
	if isUnimplemented(err) {
		fmt.Printf("⚠ TTL not yet supported by this server (containarium daemon does not implement SetContainerTTL); the box will not auto-delete.\n")
		fmt.Printf("  Requested: ttl=%s, would have expired at %s.\n", d, expiresAt.Format(time.RFC3339))
		fmt.Printf("  See the TODO at the top of internal/cmd/ttl.go for the server-side gap.\n")
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to set TTL: %w", err)
	}

	fmt.Printf("✓ TTL set on %s: expires in %s (at %s)\n", username, d, expiresAt.Format(time.RFC3339))
	return nil
}

func runTTLGet(cmd *cobra.Command, args []string) error {
	username := args[0]

	if serverAddr == "" {
		return fmt.Errorf("--server is required (ttl scheduling is a server-side feature)")
	}

	expiresAt, ok, err := ttlClientGet(username)
	if isUnimplemented(err) {
		fmt.Printf("⚠ TTL not yet supported by this server (containarium daemon does not implement GetContainerTTL).\n")
		fmt.Printf("  See the TODO at the top of internal/cmd/ttl.go for the server-side gap.\n")
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get TTL: %w", err)
	}

	if !ok {
		fmt.Printf("no TTL set on %s\n", username)
		return nil
	}

	remaining := time.Until(expiresAt).Round(time.Second)
	if remaining <= 0 {
		fmt.Printf("TTL on %s has expired (was due at %s); awaiting sweeper.\n", username, expiresAt.UTC().Format(time.RFC3339))
		return nil
	}
	fmt.Printf("expires in %s (at %s)\n", remaining, expiresAt.UTC().Format(time.RFC3339))
	return nil
}

func runTTLUnset(cmd *cobra.Command, args []string) error {
	username := args[0]

	if serverAddr == "" {
		return fmt.Errorf("--server is required (ttl scheduling is a server-side feature)")
	}

	err := ttlClientUnset(username)
	if isUnimplemented(err) {
		fmt.Printf("⚠ TTL not yet supported by this server (containarium daemon does not implement ClearContainerTTL); nothing to clear.\n")
		fmt.Printf("  See the TODO at the top of internal/cmd/ttl.go for the server-side gap.\n")
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to clear TTL: %w", err)
	}

	fmt.Printf("✓ TTL cleared on %s\n", username)
	return nil
}

// isUnimplemented returns true if err carries the gRPC Unimplemented
// status code. We treat that as a soft failure (print a warning,
// exit 0) rather than a hard error, so callers like the
// containarium-run Action can wire `containarium ttl set` into their
// failure path today without breaking when running against an older
// daemon.
func isUnimplemented(err error) bool {
	if err == nil {
		return false
	}
	if s, ok := status.FromError(err); ok && s.Code() == codes.Unimplemented {
		return true
	}
	return false
}

// ttlClientSet / ttlClientGet / ttlClientUnset are the call sites that
// will become thin wrappers over `internal/client/{grpc,http}.go`
// methods once the server-side RPCs ship. Until then they synthesize
// an Unimplemented status so the handler's friendly-warning path
// exercises end-to-end.
//
// When the real client methods land, replace the body of each with
// the equivalent of:
//
//   if httpMode { return httpClient.SetContainerTTL(username, d) }
//   return grpcClient.SetContainerTTL(username, d)
//
// and the rest of the file stays as-is.
func ttlClientSet(username string, d time.Duration) error {
	return status.Errorf(codes.Unimplemented, "SetContainerTTL not implemented by this server")
}

func ttlClientGet(username string) (time.Time, bool, error) {
	return time.Time{}, false, status.Errorf(codes.Unimplemented, "GetContainerTTL not implemented by this server")
}

func ttlClientUnset(username string) error {
	return status.Errorf(codes.Unimplemented, "ClearContainerTTL not implemented by this server")
}
