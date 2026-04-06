package cache

import (
	"time"
)

// NewTestCache creates a *Cache with its internal map initialized but without
// starting the background goroutine. Only used from white-box tests.
func NewTestCache(conf Config) *Cache {
	return newCache(&conf)
}

// AddTestItem inserts an item directly into the cache map so tests can set
// an arbitrary Last timestamp.
func (c *Cache) AddTestItem(key string, lastAccess time.Time, opts Options) {
	shard := c.shardFor(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	it := &Item{Data: "test", Time: lastAccess, Last: lastAccess, opts: opts}
	it.last.Store(lastAccess.UnixNano())
	it.hits.Store(0)

	shard.items[key] = it
}

// HasKey reports whether a key currently exists in the cache map.
func (c *Cache) HasKey(key string) bool {
	shard := c.shardFor(key)

	shard.mu.RLock()
	defer shard.mu.RUnlock()

	_, ok := shard.items[key]

	return ok
}

// RunPrune calls the internal prune function with the provided reference time.
func (c *Cache) RunPrune(from time.Time) {
	pruneStart := time.Now()

	for _, shard := range c.shards {
		shard.mu.Lock()
		shard.prune(&from, c.conf)
		shard.mu.Unlock()
	}

	c.pruneRuns.Add(1)
	// One nanosecond to avoid 0 duration when the prune pass is extremely fast and causes tests to fail on Windows.
	c.pruningNanos.Add(uint64(time.Since(pruneStart) + 1)) //nolint:gosec // duration is non-negative and bounded.
}

// PruneCounts returns the cumulative prune-run count and pruned-item count.
func (c *Cache) PruneCounts() (int64, int64) {
	var pruned int64

	for _, shard := range c.shards {
		pruned += shard.pruned.Load()
	}

	return c.pruneRuns.Load(), pruned
}
