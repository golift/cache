package cache

import (
	"sync"
	"sync/atomic"
	"time"
)

// shard is one partition of a Cache. Keys are assigned by hash(key) % N.
type shard struct {
	mu    sync.RWMutex
	items map[string]*Item
	// Counters are atomics so Get can use RLock while increments stay correct.
	hits    atomic.Int64
	misses  atomic.Int64
	saves   atomic.Int64
	updates atomic.Int64
	deletes atomic.Int64
	delmiss atomic.Int64
	pruned  atomic.Int64
}

// get returns a copy of an item. Caller must hold sh.mu for reading (RLock) or writing (Lock).
func (s *shard) get(key string, now time.Time) *Item {
	item := s.items[key]
	if item == nil {
		s.misses.Add(1)
		return nil
	}

	s.hits.Add(1)
	item.hits.Add(1)
	item.last.Store(now.UnixNano())

	return item.copy()
}

// save stores an item. Caller must hold sh.mu (write lock).
func (s *shard) save(key string, data any, opts Options, now time.Time, replace bool) *Item {
	var item *Item

	if replace {
		item = s.get(key, now) // Apply stats to this Update() request.
	} else {
		item = s.items[key] // Avoid hit/miss stats on regular Save().
	}

	if item != nil {
		s.updates.Add(1)
	} else {
		s.saves.Add(1)
	}

	optsCopy := opts
	// Create a new item and return the old/previously stored item directly.
	s.items[key] = &Item{Data: data, Time: now, Last: now, opts: &optsCopy}
	s.items[key].last.Store(now.UnixNano())
	s.items[key].hits.Store(0)
	// replace=true (Update): item is a snapshot from get. replace=false: item is the prior *Item if any (not copied).
	return item
}

// delete removes a key. Caller must hold sh.mu (write lock).
func (s *shard) delete(key string) *Item {
	item := s.items[key]
	if item == nil {
		s.delmiss.Add(1)
		return nil
	}

	item.opts = nil

	s.deletes.Add(1)
	delete(s.items, key)
	// item is not copied, and no longer in cache.
	return item
}

// prune removes eligible keys. Caller must hold sh.mu (write lock).
func (s *shard) prune(from *time.Time, conf *Config) {
	for key, item := range s.items {
		lastTime := time.Unix(0, item.last.Load())
		if last := from.Sub(lastTime); last > conf.MaxUnused ||
			(item.opts.Prune && last > conf.PruneAfter) ||
			(!item.opts.Expire.IsZero() && from.After(item.opts.Expire)) {
			s.pruned.Add(1)
			delete(s.items, key)
		}
	}
}

// clean clears all items in this shard. Caller must hold sh.mu (write lock).
func (s *shard) clean() {
	for k := range s.items {
		s.items[k].opts = nil
		s.items[k].Data = nil
		s.items[k] = nil
		delete(s.items, k)
	}

	s.items = nil
}
