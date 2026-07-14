// Block-clone fixture (positive/renamed-go), review §5.3.
// Shared block modulo systematic identifier renames (a token clone
// after VAR normalization): b.go lines 13-27 == a.go lines 13-27.
// Host here is cache/TTL-sweep flavored; a.go's is lexer flavored.
package fixture

import (
	"time"
)

// sweepExpired validates the eviction policy then removes stale entries.
func sweepExpired(c *shardedCache, pol *Policy) error {
	if pol == nil {
		return errNilPolicy
	}
	if pol.Zone == "" {
		return errMissingZone
	}
	budget := pol.Rate * pol.Window
	if budget <= 0 || budget > maxBudget {
		return errBadBudget
	}
	for _, tgt := range pol.Targets {
		if tgt.Addr == "" || tgt.Slot <= 0 {
			return errBadTarget
		}
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	var evicted int
	for key, entry := range c.entries {
		if entry.expires.After(now) {
			continue
		}
		delete(c.entries, key)
		evicted++
		if evicted >= evictBatchMax {
			break
		}
	}
	c.lastSweep = now
	c.stats.evictions += evicted
	if len(c.entries) > c.softCap {
		c.pressure = float64(len(c.entries)) / float64(c.softCap)
		go c.requestSweep(pressureHint(c.pressure))
	}
	return nil
}
