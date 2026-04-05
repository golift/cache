package cache

import (
	"sync"
	"time"
)

// shard is one partition of a Cache. Keys are assigned by hash(key) % N.
type shard struct {
	mu    sync.RWMutex
	items map[string]*Item
	stats Stats
}

// get returns a copy of an item. Caller must hold sh.mu (write lock).
func (sh *shard) get(key string, now time.Time) *Item {
	if item := sh.items[key]; item != nil {
		sh.stats.Hits++
		item.Hits++
		item.Last = now

		return item.copy()
	}

	sh.stats.Misses++

	return nil
}

// save stores an item. Caller must hold sh.mu (write lock).
func (sh *shard) save(key string, data any, opts Options, now time.Time, replace bool) *Item {
	var item *Item

	if replace {
		item = sh.get(key, now) // Apply stats to this Update() request.
	} else {
		item = sh.items[key] // Avoid hit/miss stats on regular Save().
	}

	if item != nil {
		sh.stats.Updates++
	} else {
		sh.stats.Saves++
	}

	optsCopy := opts
	sh.items[key] = &Item{Data: data, Time: now, Last: now, opts: &optsCopy}

	return item // Not a copy, but also no longer in cache.
}

// delete removes a key. Caller must hold sh.mu (write lock).
func (sh *shard) delete(key string) *Item {
	item := sh.items[key]
	if item == nil {
		sh.stats.DelMiss++

		return nil
	}

	item.opts = nil
	sh.stats.Deletes++
	delete(sh.items, key)

	return item // not copied.
}

// prune removes eligible keys. Caller must hold sh.mu (write lock).
func (sh *shard) prune(from *time.Time, conf *Config) {
	for key, item := range sh.items {
		if last := from.Sub(item.Last); last > conf.MaxUnused ||
			(item.opts.Prune && last > conf.PruneAfter) ||
			(!item.opts.Expire.IsZero() && from.After(item.opts.Expire)) {
			sh.stats.Pruned++
			delete(sh.items, key)
		}
	}
}

// clean clears all items in this shard. Caller must hold sh.mu (write lock).
func (sh *shard) clean() {
	for k := range sh.items {
		sh.items[k].opts = nil
		sh.items[k].Data = nil
		sh.items[k] = nil
		delete(sh.items, k)
	}

	sh.items = nil
}
