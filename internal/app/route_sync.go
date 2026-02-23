package app

import (
	"context"
	"log"
	"sync"
	"time"
)

// RouteSyncJob synchronizes routes from PostgreSQL (source of truth) to Caddy (runtime cache)
type RouteSyncJob struct {
	routeStore   *RouteStore
	proxyManager *ProxyManager
	interval     time.Duration

	mu       sync.Mutex
	running  bool
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// NewRouteSyncJob creates a new route sync job
func NewRouteSyncJob(routeStore *RouteStore, proxyManager *ProxyManager, interval time.Duration) *RouteSyncJob {
	if interval <= 0 {
		interval = 5 * time.Second // default 5 seconds
	}

	return &RouteSyncJob{
		routeStore:   routeStore,
		proxyManager: proxyManager,
		interval:     interval,
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}
}

// Start begins the background sync job
// It runs an immediate sync, then syncs at the configured interval
func (j *RouteSyncJob) Start(ctx context.Context) {
	j.mu.Lock()
	if j.running {
		j.mu.Unlock()
		return
	}
	j.running = true
	j.stopCh = make(chan struct{})
	j.doneCh = make(chan struct{})
	j.mu.Unlock()

	go j.run(ctx)
}

// Stop stops the background sync job
func (j *RouteSyncJob) Stop() {
	j.mu.Lock()
	if !j.running {
		j.mu.Unlock()
		return
	}
	j.mu.Unlock()

	close(j.stopCh)
	<-j.doneCh

	j.mu.Lock()
	j.running = false
	j.mu.Unlock()
}

// SyncNow triggers an immediate sync
func (j *RouteSyncJob) SyncNow(ctx context.Context) error {
	return j.sync(ctx)
}

// run is the main loop for the background sync job
func (j *RouteSyncJob) run(ctx context.Context) {
	defer close(j.doneCh)

	// Run initial sync immediately
	if err := j.sync(ctx); err != nil {
		log.Printf("[RouteSyncJob] Initial sync failed: %v", err)
	}

	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()

	for {
		select {
		case <-j.stopCh:
			log.Println("[RouteSyncJob] Stopping route sync job")
			return
		case <-ctx.Done():
			log.Println("[RouteSyncJob] Context cancelled, stopping route sync job")
			return
		case <-ticker.C:
			if err := j.sync(ctx); err != nil {
				log.Printf("[RouteSyncJob] Sync failed: %v", err)
			}
		}
	}
}

// sync performs the actual synchronization from PostgreSQL to Caddy
func (j *RouteSyncJob) sync(ctx context.Context) error {
	// Get routes from PostgreSQL (source of truth)
	dbRoutes, err := j.routeStore.List(ctx, true) // activeOnly = true
	if err != nil {
		return err
	}

	// Get current routes from Caddy
	caddyRoutes, err := j.proxyManager.ListRoutes()
	if err != nil {
		// If Caddy is unreachable, we'll retry on next interval
		return err
	}

	// Build maps for efficient diffing
	dbRouteMap := make(map[string]*RouteRecord)
	for _, r := range dbRoutes {
		dbRouteMap[r.FullDomain] = r
	}

	caddyRouteMap := make(map[string]Route)
	for _, r := range caddyRoutes {
		caddyRouteMap[r.FullDomain] = r
	}

	var added, removed, updated int

	// Find routes to add or update (in DB but not in Caddy, or different)
	for domain, dbRoute := range dbRouteMap {
		caddyRoute, exists := caddyRouteMap[domain]

		if !exists {
			// Route in DB but not in Caddy - add it
			if err := j.addRouteToCaddy(dbRoute); err != nil {
				log.Printf("[RouteSyncJob] Failed to add route %s: %v", domain, err)
				continue
			}
			added++
		} else {
			// Route exists in both - check if it needs update
			if j.needsUpdate(dbRoute, caddyRoute) {
				if err := j.updateRouteInCaddy(dbRoute); err != nil {
					log.Printf("[RouteSyncJob] Failed to update route %s: %v", domain, err)
					continue
				}
				updated++
			}
		}
	}

	// Find routes to remove (in Caddy but not in DB)
	for domain := range caddyRouteMap {
		if _, exists := dbRouteMap[domain]; !exists {
			// Route in Caddy but not in DB - remove it
			if err := j.proxyManager.RemoveRoute(domain); err != nil {
				log.Printf("[RouteSyncJob] Failed to remove route %s: %v", domain, err)
				continue
			}
			removed++
		}
	}

	// Only log if there were changes
	if added > 0 || removed > 0 || updated > 0 {
		log.Printf("[RouteSyncJob] Synced routes: +%d added, -%d removed, ~%d updated", added, removed, updated)
	}

	return nil
}

// needsUpdate checks if a route needs to be updated in Caddy
func (j *RouteSyncJob) needsUpdate(dbRoute *RouteRecord, caddyRoute Route) bool {
	// Check if target IP or port changed
	if dbRoute.TargetIP != caddyRoute.UpstreamIP {
		return true
	}
	if dbRoute.TargetPort != caddyRoute.UpstreamPort {
		return true
	}
	// Check if protocol changed
	if dbRoute.Protocol == "grpc" && caddyRoute.Protocol != RouteProtocolGRPC {
		return true
	}
	if dbRoute.Protocol == "http" && caddyRoute.Protocol != RouteProtocolHTTP {
		return true
	}
	return false
}

// addRouteToCaddy adds a route to Caddy based on the database record
func (j *RouteSyncJob) addRouteToCaddy(route *RouteRecord) error {
	if route.Protocol == "grpc" {
		return j.proxyManager.AddGRPCRoute(route.FullDomain, route.TargetIP, route.TargetPort)
	}
	return j.proxyManager.AddRoute(route.FullDomain, route.TargetIP, route.TargetPort)
}

// updateRouteInCaddy updates a route in Caddy based on the database record
func (j *RouteSyncJob) updateRouteInCaddy(route *RouteRecord) error {
	if route.Protocol == "grpc" {
		return j.proxyManager.UpdateGRPCRoute(route.FullDomain, route.TargetIP, route.TargetPort)
	}
	return j.proxyManager.UpdateRoute(route.FullDomain, route.TargetIP, route.TargetPort)
}
