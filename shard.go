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

	return item.copy(nil) // nil means makes new *Item's.
}

// getInto records a hit and fills dst with a snapshot of the item. Caller must hold s.mu for reading.
func (s *shard) getInto(key string, now time.Time, dst *Item) bool {
	item := s.items[key]
	if item == nil {
		s.misses.Add(1)
		return false
	}

	s.hits.Add(1)
	item.hits.Add(1)
	item.last.Store(now.UnixNano())
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

	s.hits.Add(1)
	item.hits.Add(1)
	item.last.Store(now.UnixNano())

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

// save stores an item. Caller must hold sh.mu (write lock).
func (s *shard) save(key string, data any, opts Options, now time.Time, replace bool) *Item {
	item := s.items[key] // Get existing item out of the map.

	if replace { // Replace the existing item (update request).
		if item == nil {
			s.misses.Add(1)
			s.saves.Add(1)
			s.items[key] = placeItem(&Item{}, data, opts, now)

			return nil
		}

		s.updates.Add(1)
		s.hits.Add(1)
		item.hits.Add(1)
		item.last.Store(now.UnixNano())
		out := item.copy(nil) // get stats before updating the item. nil means makes new *Item
		placeItem(item, data, opts, now)

		return out
	}

	if item == nil {
		s.saves.Add(1)
		s.items[key] = placeItem(&Item{}, data, opts, now)

		return nil
	}

	s.updates.Add(1)

	return placeItem(item, data, opts, now)
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
