package cache

import "time"

// Stats contains the exported cache statistics.
type Stats struct {
	Size    int64    // derived. Count of items in cache.
	Gets    int64    // derived. Cache gets issued.
	Hits    int64    // Gets for cached keys.
	Misses  int64    // Gets for missing keys.
	Saves   int64    // Saves for a new key.
	Updates int64    // Saves that casued an update.
	Deletes int64    // Delete hits.
	DelMiss int64    // Delete misses.
	Pruned  int64    // Total items pruned.
	Prunes  int64    // Number of times pruner has run.
	Pruning Duration // How much time has been spent pruning.
}

// Duration is used to format time duration(s) in stats output.
type Duration struct {
	time.Duration
}

// Stats returns the cache statistics.
func (c *Cache) Stats() *Stats {
	c.req <- &req{key: getStats, info: true}
	ret := <-c.res
	stats := ret.Data.(Stats)
	stats.Gets = stats.Hits + stats.Misses
	stats.Size = ret.Hits

	return &stats
}

// ExpStats returns the stats  inside of an interface{} so expvar can consume it.
// Use it in your app like this:
//     myCache := cache.New(cache.Config{})
//     expvar.Publish("Cache", expvar.Func(myCache.ExpStats))
//     /* or put it inside your own expvar map. */
//     myMap := expvar.NewMap("myMap")
//     myMap.Set("Cache", expvar.Func(myCache.ExpStats))
func (c *Cache) ExpStats() any {
	return c.Stats()
}

// getStats is run from within the main processor routine.
// Returns a copy of the running statistics
func (c *Cache) getStats() *Item {
	return &Item{Data: c.stats, Hits: int64(len(c.cache))}
}

// MarshalJSON turns a Duration into a string for json.
func (d *Duration) MarshalJSON() ([]byte, error) {
	return []byte(`"` + d.String() + `"`), nil
}
