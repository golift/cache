package cache

import (
	"time"
)

const (
	// Defaults.
	maxUnusedAge  = 25 * time.Hour
	pruneInterval = 3 * time.Minute
	expireAfter   = 18 * time.Minute
)

// checkPruneSettings runs once on startup.
func (c *Cache) checkPruneSettings() {
	if c.conf.PruneInterval == 0 {
		return
	}

	if c.conf.PruneInterval < time.Second {
		panic("cache prune interval must be 1 second or greater, or 0 to disable pruning")
	}

	if c.conf.PruneAfter == 0 {
		c.conf.PruneAfter = expireAfter
	}

	if c.conf.MaxUnused == 0 {
		c.conf.MaxUnused = maxUnusedAge
	}
}

// pruneRequests runs a go routine that sends prune requests at an interval.
func (c *Cache) pruneRequests() *time.Ticker {
	ticker := time.NewTicker(pruneInterval)

	go func() {
		for tick := range ticker.C {
			c.req <- &req{del: true, time: tick}
			<-c.res
		}
	}()

	return ticker
}

// pruneItems runs at an interval inside tha main thread.
func (c *Cache) pruneItems(before time.Time) *res {
	c.stats.pruneRn++

	for key, item := range c.cache {
		last := before.Sub(item.Last)
		if last > c.conf.MaxUnused || (item.opts.Prune && last > c.conf.PruneAfter) {
			c.cache[key].Data = nil
			c.cache[key] = nil
			delete(c.cache, key)
			c.stats.pruned++
		}
	}

	return &res{}
}
