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
	if c.cache == nil {
		c.cache = make(map[string]*Item)
	}

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
			c.mu.Lock()
			c.prune(&now)
			c.stats.Pruning.Duration += time.Since(now)
			c.mu.Unlock()
		}
	}
}

// clean it up and free some memory.
func (c *Cache) clean() {
	for k := range c.cache {
		c.cache[k].opts = nil
		c.cache[k].Data = nil
		c.cache[k] = nil
		delete(c.cache, k)
	}

	c.cache = nil
}

// prune (optionally) runs at an interval inside the main thread.
// Caller must hold c.mu (write lock).
func (c *Cache) prune(from *time.Time) {
	c.stats.Prunes++

	for key, item := range c.cache {
		if last := from.Sub(item.Last); last > c.conf.MaxUnused ||
			(item.opts.Prune && last > c.conf.PruneAfter) ||
			(!item.opts.Expire.IsZero() && from.After(item.opts.Expire)) {
			c.stats.Pruned++
			delete(c.cache, key)
		}
	}
}

// get returns a copy of an item. Caller must hold c.mu (write lock).
func (c *Cache) get(key string, now time.Time) *Item {
	if item := c.cache[key]; item != nil {
		c.stats.Hits++
		item.Hits++
		item.Last = now

		return item.copy()
	}

	c.stats.Misses++

	return nil
}

// save stores an item. Caller must hold c.mu (write lock).
func (c *Cache) save(key string, data any, opts Options, now time.Time, replace bool) *Item {
	var item *Item

	if replace {
		item = c.get(key, now) // Apply stats to this Update() request.
	} else {
		item = c.cache[key] // Avoid hit/miss stats on regular Save().
	}

	if item != nil {
		c.stats.Updates++
	} else {
		c.stats.Saves++
	}

	optsCopy := opts
	c.cache[key] = &Item{Data: data, Time: now, Last: now, opts: &optsCopy}

	return item // Not a copy, but also no longer in cache.
}

// listCopy returns a shallow copy of all items. Caller must hold c.mu (read or write lock).
func (c *Cache) listCopy() map[string]*Item {
	items := make(map[string]*Item)
	for key, item := range c.cache {
		items[key] = item.copy()
	}

	return items
}

// delete removes a key. Caller must hold c.mu (write lock).
func (c *Cache) delete(key string) *Item {
	item := c.cache[key]
	if item == nil {
		c.stats.DelMiss++
		return nil
	}

	// item isn't used, but future proof this and avoid leaking
	// this pointer in case item is returned out of the module.
	item.opts = nil
	c.stats.Deletes++
	delete(c.cache, key)

	return item // not copied.
}

// copy an item so it can be returned to the caller.
// Do not call this with a nil Item.
func (i *Item) copy() *Item {
	return &Item{
		Data: i.Data,
		Time: i.Time,
		Last: i.Last,
		Hits: i.Hits,
	}
}
