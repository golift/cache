package cache

import "time"

// NewTestCache creates a *Cache with its internal map initialized but without
// starting the background goroutine. Only used from white-box tests.
func NewTestCache(conf Config) *Cache {
	tc := newCache(&conf)
	tc.cache = make(map[string]*Item)

	return tc
}

// AddTestItem inserts an item directly into the cache map so tests can set
// an arbitrary Last timestamp.
func (c *Cache) AddTestItem(key string, lastAccess time.Time, opts Options) {
	c.mu.Lock()
	defer c.mu.Unlock()

	optsCopy := opts
	c.cache[key] = &Item{Data: "test", Time: lastAccess, Last: lastAccess, opts: &optsCopy}
}

// HasKey reports whether a key currently exists in the cache map.
func (c *Cache) HasKey(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	_, ok := c.cache[key]

	return ok
}

// RunPrune calls the internal prune function with the provided reference time.
func (c *Cache) RunPrune(from time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.prune(&from)
}

// PruneCounts returns the cumulative prune-run count and pruned-item count.
func (c *Cache) PruneCounts() (int64, int64) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.stats.Prunes, c.stats.Pruned
}
