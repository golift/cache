package cache

import (
	"time"
)

const (
	getList  = "getList"
	getStats = "getStats"
)

// req is our request (input channel).
type req struct {
	key  string
	del  bool
	get  bool
	data any
	time time.Time
	opts *Options
}

// res is our response (output channel).
type res struct {
	item   *Item
	exists bool
}

func (c *Cache) start() {
	c.cache = make(map[string]*Item)
	c.req = make(chan *req)
	c.res = make(chan *res)

	go c.processRequests()
}

func (c *Cache) stop() {
	close(c.req)
	<-c.res
	c.req = nil
	c.res = nil
}

// clean it up.
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

	if c.conf.PruneInterval > 0 {
		ticker := c.pruneRequests() // in prune.go
		defer ticker.Stop()
	}

	for req := range c.req {
		switch {
		case req.key == getStats && !req.get:
			c.res <- c.getStats() // in stats.go
		case req.key == getList && !req.get:
			c.res <- c.list()
		case req.del && !req.time.IsZero():
			c.res <- c.pruneItems(req.time) // in prune.go
			c.stats.pruneTm += time.Since(req.time)
		case req.data != nil:
			c.res <- c.save(req)
		case req.del:
			c.res <- c.delete(req.key)
		default:
			c.res <- c.get(req.key)
		}
	}
}

func (c *Cache) save(req *req) *res {
	_, exists := c.cache[req.key]
	if exists {
		c.stats.updated++
	} else {
		c.stats.saved++
	}

	now := time.Now()
	c.cache[req.key] = &Item{Data: req.data, Time: now, opts: req.opts, Last: now}

	return &res{item: c.cache[req.key], exists: exists}
}

func (c *Cache) get(key string) *res {
	item, exists := c.cache[key]
	if exists {
		item = item.copy(true) // make a copy, and update.
		c.stats.hit++
	} else {
		c.stats.missed++
	}

	return &res{item: item, exists: exists}
}

func (c *Cache) delete(key string) *res {
	_, exists := c.cache[key]
	if exists {
		c.stats.deleted++
		c.cache[key].Data = nil
		c.cache[key] = nil
		delete(c.cache, key)
	} else {
		c.stats.delmiss++
	}

	return &res{exists: exists}
}

func (c *Cache) list() *res {
	list := make(map[string]*Item)
	for key, item := range c.cache {
		list[key] = item.copy(false) // copy without updating.
	}

	return &res{item: &Item{Data: list}}
}

func (item *Item) copy(update bool) *Item {
	if update {
		item.Hits++
		item.Last = time.Now()
	}

	return &Item{
		// do not include item.options.
		Data: item.Data,
		Time: item.Time,
		Last: item.Last,
		Hits: item.Hits,
	}
}
