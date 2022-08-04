package cache

import (
	"time"

	"github.com/hako/durafmt"
)

// stats is embedded in the main cache struct.
// All access is gated and happens inside a single routine.
type stats struct {
	hit     int64
	missed  int64
	saved   int64
	updated int64
	deleted int64
	delmiss int64
	pruned  int64
	pruneRn int64
	pruneTm time.Duration
}

// GetStats returns the stats as a map.
func (c *Cache) GetStats() map[string]any {
	return c.GetExpStats().(map[string]any)
}

// GetExpStats returns the stats as a map[string]interface{} inside of an interface{} so expvar can consume it.
// Use it in your app like this:
//     myCache := cache.New(cache.Config{})
//     expvar.Publish("Cache", expvar.Func(myCache.GetExpStats))
//     /* or put it inside your own expvar map. */
//     myMap := expvar.NewMap("myMap")
//     myMap.Set("Cache", expvar.Func(myCache.GetExpStats))
func (c *Cache) GetExpStats() any {
	c.req <- &req{key: getStats}
	ret := <-c.res

	return ret.item.Data
}

func (c *Cache) getStats() *res {
	data := map[string]any{
		"Get":     c.stats.hit + c.stats.missed,
		"Hits":    c.stats.hit,
		"Misses":  c.stats.missed,
		"Saves":   c.stats.saved,
		"Updates": c.stats.updated,
		"Deletes": c.stats.deleted,
		"DelMiss": c.stats.delmiss,
		"Size":    len(c.cache),
	}

	if c.conf.PruneInterval != 0 {
		data["Pruned"] = c.stats.pruned
		data["Prune Runs"] = c.stats.pruneRn
		data["Prune Time"] = durafmt.Parse(c.stats.pruneTm).LimitFirstN(2).String()
	}

	return &res{item: &Item{Data: data}}
}
