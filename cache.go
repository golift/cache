// Package cache provides a simple in-memory, globally accessible data cache.
package cache

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Config provides the input options for a new cache.
// All the fields are optional.
type Config struct {
	// PruneInterval enables the pruner routine and controls
	// how often it checks every cache key and deletes those eligible.
	// This must be enabled to use Expire time on Items.
	// If you don't want other prunes to happen,
	// set really long durations for PruneAfter and/or MaxUnused.
	// @recommend 3 minutes - 5 minutes
	PruneInterval time.Duration
	// PruneAfter causes the pruner routine to prune keys marked prunable
	// after they have not been used for this duration.
	// Pass cache.Forever to never prune items marked prunable.
	// @default 18 minutes
	PruneAfter time.Duration
	// Keys not marked prunable are pruned by the pruner routine
	// after they have not been used for this duration.
	// Pass cache.Forever to avoid expiring non-prunable items.
	// @default 25 hours
	MaxUnused time.Duration
	// RequestAccuracy controls whether the cache polls time in the background.
	// Zero (the default) means each Get/Save path calls time.Now() directly.
	// Values from 1 up to minimumAccuracy (100ms) are treated like 100ms.
	// Above minimumAccuracy, a ticker updates a shared clock at this interval
	// so hot paths can avoid calling time.Now() every time (capped at 1 hour).
	RequestAccuracy time.Duration
	// Shards is the number of independent backing maps (hash partitions).
	// 0 or 1 means a single shard (default). Values greater than 1 spread keys
	// across shards using a fast deterministic hash of the key modulo Shards.
	// The maximum enforced value is maxShards.
	Shards int
}

// Cache provides methods to get, save and delete a key (with data) from cache.
type Cache struct {
	mu         sync.RWMutex
	stopMu     sync.Mutex // serializes Start/Stop (avoids races with wg.Add/Wait).
	shardCount uint32
	shards     []*shard // index i holds partition i; length is shardCount.
	running    atomic.Bool
	conf       *Config
	// now is updated by the background goroutine when RequestAccuracy is above
	// minimumAccuracy; otherwise cachedNow uses time.Now() per request.
	now atomic.Pointer[time.Time]

	// Global prune cycle counters (one increment per full prune pass over all shards).
	pruneRuns    atomic.Int64
	pruningNanos atomic.Uint64

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Item is what's returned from a cache Get.
//   - Data is the input data. Type-check it back to what it should be.
//   - Time is when the item was saved (or updated) in cache.
//   - Last is the time when the last cache get for this item occurred.
//   - Hits is the number of cache gets for this key.
//
// Items held inside the cache use atomics for Hits/last access so Get can use a read lock;
// values returned from Get and List are snapshots with plain fields.
// GetInto copies those snapshot fields into caller memory.
type Item struct {
	Data any       `json:"data"`
	Time time.Time `json:"created"`
	Last time.Time `json:"lastAccess"` // Copied from 'last' on read.
	Hits int64     `json:"hits"`       // Copied from 'hits' on read.

	opts Options // prune/eviction settings (by value to avoid heap alloc per Save).
	hits atomic.Int64
	last atomic.Int64 // unix nano time.
}

// Options are optional, and may be provided when saving a cached item.
type Options struct {
	// Setting Prune true will allow the pruning routine to prune this item.
	// Items are pruned when they have not been retrieved in the PruneAfter duration.
	Prune bool
	// You may set a specific eviction time for an item. This only works if the
	// pruner is running. The item will be removed from cache after this date/time.
	// This works independently from setting Prune to true, and follows different logic.
	// Not setting this, or setting it to zero time will never expire the item.
	Expire time.Time
}

// Defaults.
const (
	defaultMaxUnused = 25 * time.Hour         // Use cache.Forever to avoid expiring unused items.
	minimumPruneDur  = time.Second            // Not optimized for sub-second caches. (set PruneInterval)
	defaultPruneDur  = 18 * time.Minute       // 18m is probably not what you want. (set PruneAfter if 0)
	minimumAccuracy  = 100 * time.Millisecond // Below or equal: no polled clock; use time.Now() per request.
	maximumAccuracy  = time.Hour              // Good for slow-use cache.
	maxShards        = 65536                  // If you need more, make another cache pool.
)

const (
	// Forever represents the maximum Go Duration.
	// You may pass this value to Config.MaxUnused to avoid expiring non-prunable items.
	Forever time.Duration = 1<<63 - 1
)

// New starts the cache routine and returns a struct to get data from the cache.
// You do not need to call Start() after calling New(); it's already started.
func New(config Config) *Cache {
	return newWithContext(context.Background(), config)
}

// NewWithContext starts the cache routine and returns a struct to get data from the cache.
// You do not need to call Start() after calling New(); it's already started.
// If the context is cancelled or times out the cache processor exits.
func NewWithContext(ctx context.Context, config Config) *Cache {
	return newWithContext(ctx, config)
}

func newWithContext(ctx context.Context, config Config) *Cache {
	cache := newCache(&config)
	cache.start(ctx)

	return cache
}

// newCache runs once from New() and turns a *Config into a *Cache you can Start().
func newCache(conf *Config) *Cache {
	if conf.RequestAccuracy != 0 && conf.RequestAccuracy < minimumAccuracy {
		conf.RequestAccuracy = minimumAccuracy
	} else if conf.RequestAccuracy > maximumAccuracy {
		conf.RequestAccuracy = maximumAccuracy
	}

	if conf.PruneInterval != 0 && conf.PruneInterval < minimumPruneDur {
		conf.PruneInterval = minimumPruneDur
	}

	// If prune interval is 0, PruneAfter does not control anything.
	if conf.PruneAfter == 0 {
		conf.PruneAfter = defaultPruneDur
	}

	// If prune interval is 0, MaxUnused does not control anything.
	if conf.MaxUnused == 0 {
		conf.MaxUnused = defaultMaxUnused
	}

	cache := &Cache{conf: conf}
	cache.initShards()
	now := time.Now()
	// Initialize this here so cacheNow() doesn't panic.
	cache.now.Store(&now)

	return cache
}

// Start sets up the cache and starts the go routine using a Background context.
// Call this only if you already called Stop() and wish to turn it back on.
// Setting clean will clear the existing cache before restarting.
func (c *Cache) Start(clean bool) {
	c.startWithContext(context.Background(), clean)
}

// StartWithContext sets up the cache and starts the go routine with a context.
// Call this only if you already called Stop() and wish to turn it back on.
// Setting clean will clear the existing cache before restarting.
// If the context is cancelled or times out the cache processor exits.
func (c *Cache) StartWithContext(ctx context.Context, clean bool) {
	c.startWithContext(ctx, clean)
}

// Stop stops the cache processor.
// If clean is true it will clean up memory usage and delete the cache.
// Pass clean if the app will continue to run, and you don't need to re-use the cache data.
func (c *Cache) Stop(clean bool) {
	c.stopMu.Lock() // serializes Start/Stop (avoids races with wg.Add/Wait).
	if !c.running.Load() {
		c.stopMu.Unlock()
		return
	}

	c.cancel()
	c.wg.Wait()
	c.running.Store(false)
	c.stopMu.Unlock()

	if clean {
		c.mu.Lock()
		c.clean()
		c.mu.Unlock()
	}
}

// Get returns a pointer to a copy of an item, or nil if it doesn't exist.
// This library will not read or write to the item after it's returned.
// Calling this procedure after calling Stop() or cancelling the context produces a panic.
func (c *Cache) Get(requestKey string) *Item {
	c.areWeRunning()

	shard := c.shardFor(requestKey)

	shard.mu.RLock()
	defer shard.mu.RUnlock()

	return shard.get(requestKey, c.cachedNow())
}

// GetInto fills dst with the same snapshot fields that Get would return (Data, Time, Last, Hits).
// It returns false if the key is missing and does not modify dst in that case.
// It panics if dst is nil.
//
// Reuse dst across calls to avoid allocating a new *Item on each hit. Data is still the same
// shallow reference as in the cache; do not mutate it in place if other goroutines may read
// the cached value.
//
// Calling this procedure after calling Stop() or cancelling the context produces a panic.
func (c *Cache) GetInto(requestKey string, dst *Item) bool {
	if dst == nil {
		panic("cache: GetInto with nil *Item")
	}

	c.areWeRunning()

	shard := c.shardFor(requestKey)

	shard.mu.RLock()
	defer shard.mu.RUnlock()

	return shard.getInto(requestKey, c.cachedNow(), dst)
}

// GetRaw returns the stored value (Item.Data) for the key. The second result is false if the key
// is missing. If the key exists and Data is nil, it returns (nil, true).
// It updates hit/miss statistics the same way as Get.
//
// The returned value is the same reference held in the cache; after this method returns, other
// goroutines may replace or remove the entry or mutate mutable values pointed to by Data.
// For a detached copy use Get; to fill caller-owned memory without allocating a new *Item use GetInto.
//
// Calling this procedure after calling Stop() or cancelling the context produces a panic.
func (c *Cache) GetRaw(requestKey string) (any, bool) {
	c.areWeRunning()

	shard := c.shardFor(requestKey)

	shard.mu.RLock()
	defer shard.mu.RUnlock()

	return shard.getRaw(requestKey, c.cachedNow())
}

// Save saves an item, and returns true if it already existed (got updated).
// This procedure does NOT update hit/miss stats like cache.Get() does.
// Calling this procedure after calling Stop() or cancelling the context produces a panic.
func (c *Cache) Save(requestKey string, data any, opts Options) bool {
	c.areWeRunning()

	now := c.cachedNow()

	shard := c.shardFor(requestKey)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	return shard.save(requestKey, data, opts, now)
}

// Update saves an item, and returns a copy of the previously saved item.
// If you do not need the previous item, use Save() instead (no prior snapshot allocation).
// This procedure updates hit/miss stats like cache.Get() does.
// Check the item for nil to determine if it existed prior to this call.
// Calling this procedure after calling Stop() or cancelling the context produces a panic.
func (c *Cache) Update(requestKey string, data any, opts Options) *Item {
	c.areWeRunning()

	now := c.cachedNow()

	shard := c.shardFor(requestKey)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	return shard.update(requestKey, data, opts, now)
}

// Delete removes an item and returns true if it existed.
// Calling this procedure after calling Stop() or cancelling the context produces a panic.
func (c *Cache) Delete(requestKey string) bool {
	c.areWeRunning()

	shard := c.shardFor(requestKey)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	return shard.delete(requestKey) != nil
}

// List returns a copy of the in-memory cache. The map list will never be nil.
// This library will not read or write to the map after it's returned.
// The map Items will also never be nil, and because they are copies,
// this library will not read or write to them after they're returned.
// This method will double the memory footprint until release, and garbage collection runs.
// If the data stored in cache is large and not pointers, then you may
// not want to call this method much, or at all.
// Calling this procedure after calling Stop() or cancelling the context produces a panic.
func (c *Cache) List() map[string]*Item {
	c.mu.RLock()
	defer c.mu.RUnlock()

	c.areWeRunning()

	out := make(map[string]*Item)

	for _, shard := range c.shards {
		shard.mu.RLock()

		for key, item := range shard.items {
			out[key] = item.copy(nil) // nil means makes new *Items's.
		}

		shard.mu.RUnlock()
	}

	return out
}

func (c *Cache) startWithContext(ctx context.Context, clean bool) {
	c.mu.Lock()
	if c.running.Load() {
		c.mu.Unlock()
		return // already running, nothing to start.
	}

	if clean {
		c.clean()
	}

	c.mu.Unlock()
	c.start(ctx)
}
