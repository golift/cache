package cache

import "time"

// req is our request (input channel data).
type req struct {
	key  string
	get  bool // get request.
	stat bool // return stats.
	list bool // return cache.
	data any  // input data for a save op.
	opts *Options
}

func (c *Cache) start() {
	if c.cache == nil {
		c.cache = make(map[string]*Item)
	}

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
		c.cache[k].opts = nil
		c.cache[k].Data = nil
		c.cache[k] = nil
		delete(c.cache, k)
	}

	c.cache = nil
}

// processRequests readies and starts the main go routine for the cache.
func (c *Cache) processRequests() {
	defer close(c.res) // close response channel when request channel closes.

	pruner := &time.Ticker{}
	if c.conf.PruneInterval > 0 {
		pruner = time.NewTicker(c.conf.PruneInterval)
		defer pruner.Stop()
	}

	timer := time.NewTicker(c.conf.RequestAccuracy)
	defer timer.Stop()

	// this only returns when Stop() is called.
	c.processor(time.Now(), pruner, timer)
}

// processor is the single go routine in this module for request processing.
//
//nolint:cyclop // processor has to be "complicated" to do the job.
func (c *Cache) processor(now time.Time, pruner, timer *time.Ticker) {
	for {
		select {
		case now = <-timer.C: // usually 1 second to 1 minute, max 1 hour.
			// Update `now` with a ticker to avoid slow time.Now() calls during request processing.
		case req, ok := <-c.req:
			switch {
			case !ok:
				return
			case req.data != nil:
				c.res <- c.save(req, now, req.get)
			case req.get:
				c.res <- c.get(req.key, now)
			case req.list:
				c.res <- c.list()
			case req.stat:
				c.res <- &Item{Data: c.stats, Hits: int64(len(c.cache))}
			default:
				c.res <- c.delete(req.key)
			}
		case now = <-pruner.C: // usually a few minutes (ticker).
			c.prune(&now)
			c.stats.Pruning.Duration += time.Since(now)
		}
	}
}

// prune (optionally) runs at an interval inside tha main thread.
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

func (c *Cache) save(req *req, now time.Time, replace bool) *Item {
	var item *Item

	if replace {
		item = c.get(req.key, now) // Apply stats to this Update() request.
	} else {
		item = c.cache[req.key] // Avoid hit/miss stats on regular Save().
	}

	if item != nil {
		c.stats.Updates++
	} else {
		c.stats.Saves++
	}

	// Update the item in the cache with the provided value.
	c.cache[req.key] = &Item{Data: req.data, Time: now, Last: now, opts: req.opts}

	return item // Not a copy, but also no longer in cache.
}

func (c *Cache) list() *Item {
	items := make(map[string]*Item)
	for key, item := range c.cache {
		items[key] = item.copy()
	}

	return &Item{Data: items}
}

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
