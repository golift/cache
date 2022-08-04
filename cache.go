package cache

import (
	"time"
)

type Config struct {
	// Prune enables the pruner routine.
	// @ recommend 3 minutes - 5 minutes
	PruneInterval time.Duration
	// PruneAfter causes the pruner routine to prune keys marked prunable
	// after they have not been used for this duration.
	// @default 18 minutes
	PruneAfter time.Duration
	// Keys not marked prunable are pruned by the pruner routine
	// after they have not been used for this duration.
	// @default 25 hours
	MaxUnused time.Duration
}

// Cache provides methods to get a user and delete a user from cache.
// If the user is not in cache it is fetched using the userinfo module.
type Cache struct {
	cache map[string]*Item
	req   chan *req
	res   chan *res
	conf  *Config
	stats stats
}

// Item is what's returned from a cache Get.
type Item struct {
	Data any       `json:"data"`
	Time time.Time `json:"created"`
	Last time.Time `json:"last_access"`
	Hits int64     `json:"hits"`
	opts *Options
}

// Options may be provided when saving a cached item.
type Options struct {
	// Setting Prune true will allow the pruning routine to prune this item.
	// Items are pruned when they have not been retreived in the PruneAfter duration.
	Prune bool
}

// New starts the cache routine and returns a struct to get data from the cache.
func New(config Config) *Cache {
	cache := &Cache{conf: &config}
	cache.checkPruneSettings()
	cache.start()

	return cache
}

// Starts sets up the cache and starts the go routine.
func (c *Cache) Start() {
	c.clean()
	c.start()
}

// Stop stops the go routine and closes the channels.
// If clean is true it will clean up memory usage.
// Pass clean if the app will continue to run.
func (c *Cache) Stop(clean bool) {
	c.stop()

	if clean {
		c.clean()
	}
}

// Get returns an item, or nil if it doesn't exist.
func (c *Cache) Get(requestKey string) *Item {
	c.req <- &req{key: requestKey, get: true}
	return (<-c.res).item
}

// Save saves an item, and returns true if it already existed.
// Setting prune to true marks the item prunable.
// If the pruner is enabled, this allows the key to be pruned after
// it hasn't been used in the pruneAfter duration.
func (c *Cache) Save(requestKey string, data any, opts Options) bool {
	c.req <- &req{key: requestKey, data: data, opts: &opts}
	return (<-c.res).exists
}

// Delete removes an item and returns true if it existed.
func (c *Cache) Delete(requestKey string) bool {
	c.req <- &req{key: requestKey, del: true}
	return (<-c.res).exists
}

// List retuns a copy of the in-memory cache.
func (c *Cache) List() map[string]*Item {
	c.req <- &req{key: getList}
	return (<-c.res).item.Data.(map[string]*Item)
}
