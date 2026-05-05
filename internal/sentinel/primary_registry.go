package sentinel

import (
	"sync"
	"time"
)

// PrimaryTTL is the maximum age of a Primary registration before it's considered
// stale and may be evicted. Primaries are expected to heartbeat every PrimaryTTL/3.
const PrimaryTTL = 90 * time.Second

// Primary represents a registered primary daemon serving one pool.
type Primary struct {
	Pool          Pool      `json:"pool"`
	Hostname      string    `json:"hostname"` // primary's own subdomain (e.g. containarium-prod.kafeido.app)
	Aliases       []string  `json:"aliases,omitempty"` // additional hostnames the primary's Caddy routes (e.g. api.kafeido.app, voice.kafeido.app)
	IP            string    `json:"ip"`       // primary's reachable IP (typically internal VPC IP)
	Port          int       `json:"port"`     // HTTPS port on the primary (typically 443)
	BackendID     string    `json:"backend_id,omitempty"`
	RegisteredAt  time.Time `json:"registered_at"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
}

// PrimaryRegistry tracks pool → primary mappings populated by daemon
// self-registration. Entries that don't heartbeat within PrimaryTTL are
// evicted on read.
type PrimaryRegistry struct {
	mu        sync.RWMutex
	primaries map[Pool]*Primary
	now       func() time.Time
}

// NewPrimaryRegistry creates an empty registry.
func NewPrimaryRegistry() *PrimaryRegistry {
	return &PrimaryRegistry{
		primaries: make(map[Pool]*Primary),
		now:       time.Now,
	}
}

// Register inserts or updates a primary. Pool must be non-empty (one primary
// per pool). The RegisteredAt timestamp is preserved on update.
func (r *PrimaryRegistry) Register(p Primary) *Primary {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	if existing, ok := r.primaries[p.Pool]; ok {
		// Update fields that can change, keep original RegisteredAt
		existing.Hostname = p.Hostname
		existing.Aliases = p.Aliases
		existing.IP = p.IP
		existing.Port = p.Port
		existing.BackendID = p.BackendID
		existing.LastHeartbeat = now
		return existing
	}

	stored := p
	stored.RegisteredAt = now
	stored.LastHeartbeat = now
	r.primaries[p.Pool] = &stored
	return &stored
}

// Heartbeat refreshes the LastHeartbeat timestamp for a pool. Returns the
// updated primary, or nil if the pool isn't registered.
func (r *PrimaryRegistry) Heartbeat(pool Pool) *Primary {
	r.mu.Lock()
	defer r.mu.Unlock()

	p, ok := r.primaries[pool]
	if !ok {
		return nil
	}
	p.LastHeartbeat = r.now()
	return p
}

// Unregister removes a primary by pool name. Returns true if it existed.
func (r *PrimaryRegistry) Unregister(pool Pool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.primaries[pool]
	delete(r.primaries, pool)
	return ok
}

// UnregisterByBackendID removes any primary entry whose BackendID matches.
// Used when a tunnel-registered primary disconnects. Returns the number of
// entries removed.
func (r *PrimaryRegistry) UnregisterByBackendID(backendID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	removed := 0
	for pool, p := range r.primaries {
		if p.BackendID == backendID {
			delete(r.primaries, pool)
			removed++
		}
	}
	return removed
}

// LookupByPool returns the primary serving the given pool, or nil. Stale
// entries (last heartbeat older than PrimaryTTL) are treated as absent.
func (r *PrimaryRegistry) LookupByPool(pool Pool) *Primary {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.primaries[pool]
	if !ok {
		return nil
	}
	if r.now().Sub(p.LastHeartbeat) > PrimaryTTL {
		return nil
	}
	return p
}

// LookupByHostname returns the primary registered for the given public
// hostname, matching either the Hostname or any of the Aliases. Stale
// entries are skipped. Used by the SNI router.
func (r *PrimaryRegistry) LookupByHostname(hostname string) *Primary {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cutoff := r.now().Add(-PrimaryTTL)
	for _, p := range r.primaries {
		if p.LastHeartbeat.Before(cutoff) {
			continue
		}
		if p.Hostname == hostname {
			return p
		}
		for _, a := range p.Aliases {
			if a == hostname {
				return p
			}
		}
	}
	return nil
}

// All returns a snapshot of registered primaries. Stale entries are excluded
// and evicted from the underlying map.
func (r *PrimaryRegistry) All() []*Primary {
	r.mu.Lock()
	defer r.mu.Unlock()
	cutoff := r.now().Add(-PrimaryTTL)
	out := make([]*Primary, 0, len(r.primaries))
	for pool, p := range r.primaries {
		if p.LastHeartbeat.Before(cutoff) {
			delete(r.primaries, pool)
			continue
		}
		copy := *p
		out = append(out, &copy)
	}
	return out
}
