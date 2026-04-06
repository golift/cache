package cache

import (
	"context"
	"time"
)

func (c *Cache) areWeRunning() {
	if !c.running.Load() {
		panic("cache: operation on cache that is not running")
	}
}

// cachedNow returns time.Now() when RequestAccuracy is zero (default). When polling
// is enabled, it returns the last tick from the background goroutine.
func (c *Cache) cachedNow() time.Time {
	if c.conf.RequestAccuracy == 0 {
		return time.Now()
	}

	return *c.now.Load()
}

func (c *Cache) start(ctx context.Context) {
	c.stopMu.Lock()
	defer c.stopMu.Unlock()

	if c.running.Load() {
		return
	}

	c.mu.Lock()
	c.ensureShardMaps()
	c.mu.Unlock()

	ctx, c.cancel = context.WithCancel(ctx)
	c.wg.Go(func() { c.backgroundLoop(ctx) })
	c.running.Store(true)
}

func (c *Cache) backgroundLoop(ctx context.Context) {
	if c.conf.RequestAccuracy == 0 && c.conf.PruneInterval == 0 {
		return
	}

	now := time.Now()
	update := now
	c.now.Store(&update)

	timer := &time.Ticker{}
	if c.conf.RequestAccuracy > 0 {
		timer = time.NewTicker(c.conf.RequestAccuracy)
		defer timer.Stop()
	}

	pruner := &time.Ticker{}
	if c.conf.PruneInterval > 0 {
		pruner = time.NewTicker(c.conf.PruneInterval)
		defer pruner.Stop()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case update := <-timer.C:
			c.now.Store(&update)
		case now = <-pruner.C:
			pruneStart := time.Now()

			for _, shard := range c.shards {
				shard.mu.Lock()
				shard.prune(&now, c.conf)
				shard.mu.Unlock()
			}

			c.pruneRuns.Add(1)
			// One nanosecond to avoid 0 duration when the prune pass is extremely fast and causes tests to fail on Windows.
			c.pruningNanos.Add(uint64(time.Since(pruneStart) + 1)) //nolint:gosec // duration is non-negative and bounded.
		}
	}
}

// copy an item so it can be returned to the caller.
// If dst is nil, a new Item is allocated and returned.
// Otherwise, the fields are copied into the existing Item.
// Do not call this with a nil Item.
func (src *Item) copy(dst *Item) *Item {
	if dst == nil {
		return &Item{
			Data: src.Data,
			Time: src.Time,
			Last: time.Unix(0, src.last.Load()),
			Hits: src.hits.Load(),
		}
	}

	dst.Data = src.Data
	dst.Time = src.Time
	dst.Last = time.Unix(0, src.last.Load())
	dst.Hits = src.hits.Load()

	return dst
}
