package cache

import "time"

// Config provides the input options for a new cache.
// All the fields are optional.
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
	// RequestAccuracy can be set between 100 milliseconds and 1 minute.
	// This sets the ticker interval that updates our time.Now() variable.
	// Generally, the default of 1 second should be fine for most apps.
	// If accuracy about when an item was saved isn't important, you can raise
	// this to a few seconds quite safely and the cache will use fewer cpu cycles.
	RequestAccuracy time.Duration
}

// Cache provides methods to get a user and delete a user from cache.
// If the user is not in cache it is fetched using the userinfo module.
type Cache struct {
	cache map[string]*Item
	req   chan *req
	res   chan *Item
	conf  *Config
	stats Stats
}

// Item is what's returned from a cache Get.
// - Data is the input data. Type-check it back to what it should be.
// - Time is when the item was saved (or updated) in cache.
// - Last is the time when the last cache get for this item occurred.
// - Hits is the number of cache gets for this key.
type Item struct {
	Data any       `json:"data"`
	Time time.Time `json:"created"`
	Last time.Time `json:"lastAccess"`
	Hits int64     `json:"hits"`
	opts *Options
}

// Options are optional, and may be provided when saving a cached item.
type Options struct {
	// Setting Prune true will allow the pruning routine to prune this item.
	// Items are pruned when they have not been retreived in the PruneAfter duration.
	Prune bool
	// You may set a specific evication time for an item. This only works if the
	// pruner is running. The item will be removed from cache after this date/time.
	// This works independently from setting Prune to true, and follows different logic.
	// Not setting this, or setting it to zero time will never expire the item.
	Expire time.Time
}

// Defaults.
const (
	maxUnusedAge = 25 * time.Hour
	expireAfter  = 18 * time.Minute
	minAccuracy  = 100 * time.Millisecond // don't do this.
	maxAccuracy  = time.Minute            // good for slow-use cache.
	accuracy     = time.Second            // 1-5s is fine for most things.
	maxExpire    = 1 << 62                // (almost) max go time. https://stackoverflow.com/questions/25065055
)

// New starts the cache routine and returns a struct to get data from the cache.
// You do not need to call Start() after calling New(); it's already started.
func New(config Config) *Cache {
	cache := &Cache{conf: &config}
	cache.checkPruneSettings()
	cache.start()

	return cache
}

// checkPruneSettings runs once on startup.
func (c *Cache) checkPruneSettings() {
	switch {
	case c.conf.RequestAccuracy == 0:
		c.conf.RequestAccuracy = accuracy
	case c.conf.RequestAccuracy < minAccuracy:
		c.conf.RequestAccuracy = minAccuracy
	case c.conf.RequestAccuracy > maxAccuracy:
		c.conf.RequestAccuracy = maxAccuracy
	}

	if c.conf.PruneInterval == 0 {
		return
	}

	if c.conf.PruneInterval < time.Second {
		c.conf.PruneInterval = time.Second
	}

	if c.conf.PruneAfter == 0 {
		c.conf.PruneAfter = expireAfter
	}

	if c.conf.MaxUnused == 0 {
		c.conf.MaxUnused = maxUnusedAge
	}
}

// Starts sets up the cache and starts the go routine.
// Call this only if you already called Stop() and wish to turn it back on.
// Setting clean will clear the existing cache before restarting.
func (c *Cache) Start(clean bool) {
	if c.req != nil {
		return // already running, nothing to start.
	}

	if clean {
		c.clean()
	}

	c.start()
}

// Stop stops the go routine and closes the channels.
// If clean is true it will clean up memory usage.
// Pass clean if the app will continue to run,
// and you don't need to re-use the cache data.
func (c *Cache) Stop(clean bool) {
	c.stop()

	if clean {
		c.clean()
	}
}

// Get returns a pointer to a copy of an item, or nil if it doesn't exist.
// Because it's a copy, concurrent access is OK.
func (c *Cache) Get(requestKey string) *Item {
	c.req <- &req{key: requestKey, get: true}
	return <-c.res
}

// Save saves an item, and returns true if it already existed.
func (c *Cache) Save(requestKey string, data any, opts Options) bool {
	if opts.Expire.IsZero() {
		opts.Expire = time.Unix(maxExpire, 0)
	}

	c.req <- &req{key: requestKey, data: data, opts: &opts}

	return <-c.res != nil
}

// Delete removes an item and returns true if it existed.
func (c *Cache) Delete(requestKey string) bool {
	c.req <- &req{key: requestKey}
	return <-c.res != nil
}

// List returns a copy of the in-memory cache.
// This will never be nil, and concurrent access is OK.
// The map Items will also never be nil.
// Because all items are copies, concurrent access to Itms is also OK.
// This method will double the memory footprint until release, and garbage collection runs.
// If the data stored in cache is large and not pointers, then you may
// not want to call this method much, or at all.
// If the data stored in cache is not pointers, this method could double the memory footprint.
func (c *Cache) List() map[string]*Item {
	c.req <- &req{list: true}
	items, _ := (<-c.res).Data.(map[string]*Item)

	return items
}
