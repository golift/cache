package cache

func normalizeShardCount(requested int) int {
	switch {
	case requested <= 1:
		return 1
	case requested >= maxShards:
		return maxShards
	default:
		return requested
	}
}

func (c *Cache) initShards() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.initShardsLocked()
}

func (c *Cache) initShardsLocked() {
	n := normalizeShardCount(c.conf.Shards)
	c.shardCount = uint32(n) //nolint:gosec // n is clamped to maxShards (65536).

	for i := range int(c.shardCount) {
		c.shardPools.Store(uint32(i), &shard{items: make(map[string]*Item)})
	}
}

// ensureShardMaps initializes shard maps on startup. Caller must hold c.mu (write lock).
func (c *Cache) ensureShardMaps() {
	if c.shardCount == 0 { // no shards requested, so use the default of 1.
		c.initShardsLocked()
		return
	}

	c.shardPools.Range(func(_, value any) bool {
		shard, ok := value.(*shard)
		if !ok {
			panic("cache: internal error: bad shard type in pool")
		}

		shard.mu.Lock()

		if shard.items == nil {
			shard.items = make(map[string]*Item)
		}

		shard.mu.Unlock()

		return true
	})
}

// shardFor returns the shard that owns key. Each shard is a separate map+mutex pair, so
// unrelated keys can be read/written concurrently without blocking each other.
func (c *Cache) shardFor(key string) *shard {
	var idx uint32

	if c.shardCount <= 1 {
		idx = 0 // No partitioning: one map holds everything, always index 0.
	} else {
		// Turn the string key into a pseudo-random 64-bit integer (FNV-1a). Same key
		// always gets the same hash; different keys usually get different hashes, which
		// spreads load across shards. This is standard cheap string hashing, not crypto.
		const (
			offset64 = uint64(14695981039346656037)
			prime64  = uint64(1099511628211)
		)

		hash := offset64
		for i := range len(key) {
			hash ^= uint64(key[i])
			hash *= prime64
		}

		// Map that hash onto a shard slot in [0, shardCount). The remainder is which
		// pool index we use; keys are mixed among shards but stay stable over time.
		idx = uint32(hash % uint64(c.shardCount)) //nolint:gosec // remainder is < shardCount (<= maxShards).
	}

	// At startup we stored shard 0..shardCount-1 in shardPools; Load is the hot-path lookup.
	value, loaded := c.shardPools.Load(idx)
	if !loaded {
		panic("cache: internal error: missing shard")
	}

	// sync.Map returns interface{}; assert to *shard (only type we ever Store).
	shardInst, ok := value.(*shard)
	if !ok {
		panic("cache: internal error: bad shard type")
	}

	return shardInst
}

// clean clears all items in all shards. Caller must hold c.mu (write lock).
func (c *Cache) clean() {
	c.shardPools.Range(func(_, value any) bool {
		shard, ok := value.(*shard)
		if !ok {
			panic("cache: internal error: bad shard type in pool")
		}

		shard.mu.Lock()
		shard.clean()
		shard.mu.Unlock()

		return true
	})
}
