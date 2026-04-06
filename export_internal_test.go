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
	shardInst := c.shardFor(key)
	shardInst.mu.Lock()
	defer shardInst.mu.Unlock()

	optsCopy := opts
	it := &Item{Data: "test", Time: lastAccess, Last: lastAccess, opts: &optsCopy}
	it.last.Store(lastAccess.UnixNano())
	it.hits.Store(0)

	shardInst.items[key] = it
}

// HasKey reports whether a key currently exists in the cache map.
func (c *Cache) HasKey(key string) bool {
	shardInst := c.shardFor(key)

	shardInst.mu.RLock()
	defer shardInst.mu.RUnlock()

	_, ok := shardInst.items[key]

	return ok
}

// RunPrune calls the internal prune function with the provided reference time.
func (c *Cache) RunPrune(from time.Time) {
	pruneStart := time.Now()

	c.shardPools.Range(func(_, value any) bool {
		shard, ok := value.(*shard)
		if !ok {
			panic("cache: internal error: bad shard type in pool")
		}

		shard.mu.Lock()
		shard.prune(&from, c.conf)
		shard.mu.Unlock()

		return true
	})
	c.pruneRuns.Add(1)
	c.pruningNanos.Add(uint64(time.Since(pruneStart))) //nolint:gosec // duration is non-negative and bounded.
}

// PruneCounts returns the cumulative prune-run count and pruned-item count.
func (c *Cache) PruneCounts() (int64, int64) {
	var pruned int64

	c.shardPools.Range(func(_, value any) bool {
		shard, ok := value.(*shard)
		if !ok {
			panic("cache: internal error: bad shard type in pool")
		}

		pruned += shard.pruned.Load()

		return true
	})

	return c.pruneRuns.Load(), pruned
}
