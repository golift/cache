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

			c.shardPools.Range(func(_, value any) bool {
				shard, ok := value.(*shard)
				if !ok {
					panic("cache: internal error: bad shard type in pool")
				}

				shard.mu.Lock()
				shard.prune(&now, c.conf)
				shard.mu.Unlock()

				return true
			})
			c.pruneRuns.Add(1)
			// One nanosecond to avoid 0 duration when the prune pass is extremely fast and causes tests to fail on Windows.
			c.pruningNanos.Add(uint64(time.Since(pruneStart) + 1)) //nolint:gosec // duration is non-negative and bounded.
		}
	}
}

// copy an item so it can be returned to the caller.
// Do not call this with a nil Item.
func (i *Item) copy() *Item {
	return &Item{
		Data: i.Data,
		Time: i.Time,
		Last: time.Unix(0, i.last.Load()),
		Hits: i.hits.Load(),
	}
}
