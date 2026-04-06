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

// bumpLookupHit records a successful lookup (same counters as a cache Get hit). Caller must hold s.mu.
func (s *shard) bumpLookupHit(item *Item, now time.Time) {
	s.hits.Add(1)
	item.hits.Add(1)
	item.last.Store(now.UnixNano())
}

// get returns a copy of an item. Caller must hold sh.mu for reading (RLock) or writing (Lock).
func (s *shard) get(key string, now time.Time) *Item {
	item := s.items[key]
	if item == nil {
		s.misses.Add(1)
		return nil
	}

	s.bumpLookupHit(item, now)

	return item.copy(nil) // nil means makes new *Item's.
}

// getInto records a hit and fills dst with a snapshot of the item. Caller must hold s.mu for reading.
func (s *shard) getInto(key string, now time.Time, dst *Item) bool {
	item := s.items[key]
	if item == nil {
		s.misses.Add(1)
		return false
	}

	s.bumpLookupHit(item, now)
	item.copy(dst) // copy the cached item into the caller's *Item.

	return true
}

// getRaw records a hit and returns the stored Data for the key. Caller must hold s.mu for reading.
func (s *shard) getRaw(key string, now time.Time) (any, bool) {
	item := s.items[key]
	if item == nil {
		s.misses.Add(1)
		return nil, false
	}

	s.bumpLookupHit(item, now)

	return item.Data, true
}

func placeItem(item *Item, data any, opts Options, now time.Time) *Item {
	item.Data = data
	item.Time = now
	item.Last = now
	item.opts = opts
	item.last.Store(now.UnixNano())
	item.hits.Store(0)

	return item // send it back out for chaining.
}

// save inserts or overwrites without changing get hit/miss counters. Returns true if the key
// already existed. Caller must hold s.mu (write lock).
func (s *shard) save(key string, data any, opts Options, now time.Time) bool {
	item := s.items[key]
	if item == nil {
		s.saves.Add(1)
		s.items[key] = placeItem(&Item{}, data, opts, now)

		return false
	}

	s.updates.Add(1)
	placeItem(item, data, opts, now)

	return true
}

// update replaces the value for key. For a new key it counts a get miss plus a save and returns nil.
// For an existing key it applies the same lookup accounting as get(), returns a detached copy of
// the entry as it was after that accounting, then stores the new value. Caller must hold s.mu.
func (s *shard) update(key string, data any, opts Options, now time.Time) *Item {
	item := s.get(key, now)
	if item == nil {
		s.saves.Add(1)                                     // record stats for new item.
		s.items[key] = placeItem(&Item{}, data, opts, now) // add new item to the shard.

		return nil
	}

	s.updates.Add(1)                         // record stats for update.
	placeItem(s.items[key], data, opts, now) // update the item in the shard.

	return item
}

// delete removes a key. Caller must hold sh.mu (write lock).
func (s *shard) delete(key string) *Item {
	item := s.items[key]
	if item == nil {
		s.delmiss.Add(1)
		return nil
	}

	item.opts = Options{}

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
		s.items[k].opts = Options{}
		s.items[k].Data = nil
		s.items[k] = nil
		delete(s.items, k)
	}

	s.items = nil
}
