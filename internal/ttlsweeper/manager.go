package ttlsweeper

import (
	"context"
	"log"
	"sync"
	"time"
)

// DefaultInterval is the tick cadence. Once a minute keeps the
// deletion latency tight (max ~1m past TTL plus graceMargin) without
// flooding incusd with list calls — the typical Containarium daemon
// owns dozens of containers, not thousands.
const DefaultInterval = 60 * time.Second

// IncusClient is the narrow slice of the daemon's container source
// the sweeper actually needs. The wiring PR adapts the real
// *incus.Client (or whatever persists TTL — Incus user.* config,
// SQLite, etc.) into ContainerView records on the way through.
//
// Kept as a one-method interface so the daemon-side implementation
// has zero coupling beyond "produce a slice of view records on
// demand"; tests use a literal fake without mockgen.
type IncusClient interface {
	ListContainers() ([]ContainerView, error)
}

// Deleter is the action seam. The wiring PR provides an
// implementation that invokes the existing DeleteContainer plumbing
// with the right audit-event banner. The sweeper itself does not
// know how a container gets deleted — only that it should be.
//
// reason is a short human-readable string (e.g. "ttl_expired
// 2026-05-23T13:00:00Z") that the implementation may log and/or
// surface in the audit trail. Errors are logged and the loop
// continues — one bad container should not poison the rest of the
// tick.
type Deleter interface {
	DeleteContainer(ctx context.Context, name string, reason string) error
}

// Manager owns the tick loop. One Manager per daemon. Mirrors the
// shape of internal/autosleep/manager.Manager so the two ticker
// goroutines have the same lifecycle story (Start spawns, Stop
// signals + waits, both idempotent).
type Manager struct {
	incus    IncusClient
	deleter  Deleter
	interval time.Duration
	clock    func() time.Time

	stopCh   chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

// Options bundles the optional knobs. Production callers should pass
// zero values for Interval/Clock to get DefaultInterval and time.Now.
type Options struct {
	Interval time.Duration
	Clock    func() time.Time
}

// NewManager constructs a manager. Neither IncusClient nor Deleter
// may be nil — the sweeper has nothing useful to do without both, and
// silently degrading would hide a wiring bug in the daemon's startup.
// The follow-up wiring PR is expected to construct exactly one of
// these in main and Start it alongside the other ticker managers.
func NewManager(inc IncusClient, deleter Deleter, opts Options) *Manager {
	if inc == nil {
		panic("ttlsweeper: nil IncusClient")
	}
	if deleter == nil {
		panic("ttlsweeper: nil Deleter")
	}
	if opts.Interval <= 0 {
		opts.Interval = DefaultInterval
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	return &Manager{
		incus:    inc,
		deleter:  deleter,
		interval: opts.Interval,
		clock:    opts.Clock,
		stopCh:   make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Start spawns the tick loop. Returns immediately. The loop exits on
// either ctx cancellation or Stop being called, whichever comes
// first.
func (m *Manager) Start(ctx context.Context) {
	go m.run(ctx)
	log.Printf("[ttlsweeper] ticker started (interval=%s)", m.interval)
}

// Stop signals the loop to exit and waits for it to finish.
// Idempotent — safe to call from multiple goroutines or multiple
// times.
func (m *Manager) Stop() {
	m.stopOnce.Do(func() {
		close(m.stopCh)
	})
	<-m.done
}

func (m *Manager) run(ctx context.Context) {
	defer close(m.done)

	t := time.NewTicker(m.interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		case <-t.C:
			m.tick(ctx)
		}
	}
}

// tick evaluates one round: list containers, ask Decide which are
// expired, delete each one. Errors are logged and the loop continues
// — a single misbehaving container shouldn't halt the ticker for
// everyone else (mirrors the autosleep manager's per-container
// failure isolation).
func (m *Manager) tick(ctx context.Context) {
	containers, err := m.incus.ListContainers()
	if err != nil {
		log.Printf("[ttlsweeper] list containers: %v", err)
		return
	}
	now := m.clock()
	expired := Decide(containers, now)
	if len(expired) == 0 {
		return
	}

	// Build a lookup of name → expiry so the audit message can
	// include the exact TTL we acted on. Tiny — the typical daemon
	// has a few dozen containers, so the O(N) build cost is moot.
	expiryByName := make(map[string]time.Time, len(containers))
	for _, c := range containers {
		if c.TTLExpiresAt != nil {
			expiryByName[c.Name] = *c.TTLExpiresAt
		}
	}

	for _, name := range expired {
		reason := "ttl_expired"
		if exp, ok := expiryByName[name]; ok {
			reason = "ttl_expired at " + exp.UTC().Format(time.RFC3339)
		}
		if err := m.deleter.DeleteContainer(ctx, name, reason); err != nil {
			log.Printf("[ttlsweeper] delete %s: %v", name, err)
			continue
		}
		log.Printf("[ttlsweeper] deleted name=%s reason=%q", name, reason)
	}
}
