package cache

import "time"

const (
	getList  = "getList"
	getStats = "getStats"
)

// req is our request (input channel).
type req struct {
	key  string
	del  bool // delete request.
	info bool // special info request.
	data any  // input data for a save op.
	opts *Options
}

func (c *Cache) start() {
	c.cache = make(map[string]*Item)
	c.req = make(chan *req)
	c.res = make(chan *Item)

	go c.processRequests()
}

func (c *Cache) stop() {
	close(c.req)
	<-c.res
	c.req = nil
	c.res = nil
}

// clean it up and free some memory.
func (c *Cache) clean() {
	for k := range c.cache {
		c.cache[k].Data = nil
		c.cache[k] = nil
		delete(c.cache, k)
	}

	c.cache = nil
}

// processRequests is the main running go routine for the cache.
func (c *Cache) processRequests() {
	defer close(c.res) // close response channel when request channel closes.

	ticker := &time.Ticker{}
	if c.conf.PruneInterval > 0 {
		ticker = time.NewTicker(c.conf.PruneInterval)
		defer ticker.Stop()
	}

	now := time.Now()
	second := time.NewTicker(time.Second)
	defer second.Stop()

	for {
		select {
		case now = <-ticker.C:
			c.prune(now) // in prune.go
			c.stats.Pruning.Duration += time.Since(now)
		case now = <-second.C:
			// Update now every second to avoid slow time.Now() calls during request processing.
		case req, ok := <-c.req:
			switch {
			case !ok:
				return
			case req.key == getStats && req.info:
				c.res <- c.getStats() // in stats.go
			case req.key == getList && req.info:
				c.res <- c.list()
			case req.data != nil:
				c.res <- c.save(req, now)
			case req.del:
				c.res <- c.delete(req.key)
			default:
				c.res <- c.get(req.key, now)
			}
		}
	}
}

// pruneItems runs at an interval inside tha main thread.
func (c *Cache) prune(before time.Time) {
	c.stats.Prunes++

	for key, item := range c.cache {
		last := before.Sub(item.Last)
		if last > c.conf.MaxUnused || (item.opts.Prune && last > c.conf.PruneAfter) {
			delete(c.cache, key)
			c.stats.Pruned++
		}
	}
}

func (c *Cache) save(req *req, now time.Time) *Item {
	item := c.cache[req.key]
	if item != nil {
		c.stats.Updates++
	} else {
		c.stats.Saves++
	}

	c.cache[req.key] = &Item{Data: req.data, Time: now, Last: now, opts: req.opts}

	return item // not copied.
}

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

func (c *Cache) delete(key string) *Item {
	item := c.cache[key]
	if item == nil {
		c.stats.DelMiss++
		return nil
	}

	c.stats.Deletes++
	delete(c.cache, key)

	return item // not copied.
}

func (c *Cache) list() *Item {
	list := make(map[string]*Item)
	for key, item := range c.cache {
		list[key] = item.copy()
	}

	return &Item{Data: list}
}

// copy an item so it can be returned to a user.
func (i *Item) copy() *Item {
	item := *i   // copy item.
	return &item // pointer to copy
}
