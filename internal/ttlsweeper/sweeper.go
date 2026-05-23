package ttlsweeper

import "time"

// ContainerView is the slice of container state the sweeper actually
// reads. Decoupled from incus.ContainerInfo (and from the generated
// pb.Container) so this package has zero dependency on either — easier
// to test, and the daemon-wiring follow-up can adapt whichever source
// it picks (Incus user.* config keys, SQLite row, etc.) at the call
// site without touching the decision logic.
type ContainerView struct {
	// Name is the Incus container name (e.g. "alice-container").
	// Echoed back in Decide's returned slice and used by the wiring
	// PR's Deleter as the delete key.
	Name string

	// TTLExpiresAt is the wall-clock time at which the container
	// should be auto-deleted. nil = no TTL set (container persists
	// indefinitely). Pointer-not-zero-value so we can distinguish
	// "unset" from "set to the zero time" — important because Decide
	// must treat unset as "skip" rather than "delete immediately".
	TTLExpiresAt *time.Time
}

// graceMargin is subtracted from the comparison clock before checking
// whether ttl_expires_at has elapsed. Protects against minor clock
// skew between the daemon host and whatever set the TTL (the GHA
// runner, a developer's laptop, etc.). 30s is generous compared to
// typical NTP skew (sub-second) and comfortably less than 1% of the
// smallest realistic TTL window — see the package doc for the
// reasoning. Exported as a package constant rather than a tunable so
// the same value shows up in tests and operator-facing docs.
const graceMargin = 30 * time.Second

// Decide returns the names of containers whose TTL has elapsed
// (accounting for graceMargin). Pure function — no IO, no clock, no
// locks. Safe to call from a goroutine; safe to call repeatedly with
// the same inputs (idempotent, deterministic).
//
// Rules:
//   - TTLExpiresAt == nil → skip (no TTL set).
//   - TTLExpiresAt.After(now - graceMargin) → skip (not yet expired).
//   - Otherwise → include in the returned slice.
//
// The "at or before" comparison (`.After()` returning false on equal)
// means a TTL exactly at now-graceMargin is treated as expired, which
// matches an operator's intuition for "TTL of 0 means delete on the
// next sweep".
func Decide(containers []ContainerView, now time.Time) []string {
	cutoff := now.Add(-graceMargin)
	var expired []string
	for _, c := range containers {
		if c.TTLExpiresAt == nil {
			continue
		}
		if c.TTLExpiresAt.After(cutoff) {
			continue
		}
		expired = append(expired, c.Name)
	}
	return expired
}
