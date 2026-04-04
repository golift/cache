package cache_test

import (
	"testing"
	"time"

	"golift.io/cache"
)

// TestPruneInternal_MaxUnused verifies that items idle longer than MaxUnused
// are removed and that recently-accessed items are kept.
func TestPruneInternal_MaxUnused(t *testing.T) {
	t.Parallel()

	cfg := cache.Config{
		MaxUnused:  100 * time.Millisecond,
		PruneAfter: cache.Forever, // keep PruneAfter out of the picture
	}

	testCache := cache.NewTestCache(cfg)

	past := time.Now().Add(-200 * time.Millisecond)
	testCache.AddTestItem("old", past, cache.Options{})
	testCache.AddTestItem("fresh", time.Now(), cache.Options{})

	testCache.RunPrune(time.Now())

	if testCache.HasKey("old") {
		t.Error("expected 'old' (idle > MaxUnused) to be pruned")
	}

	if !testCache.HasKey("fresh") {
		t.Error("expected 'fresh' (recently accessed) to survive")
	}

	runs, items := testCache.PruneCounts()

	if runs != 1 {
		t.Errorf("prune runs: want 1, got %d", runs)
	}

	if items != 1 {
		t.Errorf("pruned items: want 1, got %d", items)
	}
}

// TestPruneInternal_PruneAfter verifies that Prune=true items idle longer
// than PruneAfter are removed, while non-prunable items with the same age
// are kept (when MaxUnused=Forever).
func TestPruneInternal_PruneAfter(t *testing.T) {
	t.Parallel()

	cfg := cache.Config{
		MaxUnused:  cache.Forever, // disable MaxUnused eviction
		PruneAfter: 100 * time.Millisecond,
	}

	testCache := cache.NewTestCache(cfg)

	past := time.Now().Add(-200 * time.Millisecond)

	// prunable and long-idle → must be pruned
	testCache.AddTestItem("prunable-old", past, cache.Options{Prune: true})
	// non-prunable and long-idle → MaxUnused=Forever, must survive
	testCache.AddTestItem("plain-old", past, cache.Options{Prune: false})
	// prunable but fresh → must survive
	testCache.AddTestItem("prunable-fresh", time.Now(), cache.Options{Prune: true})

	testCache.RunPrune(time.Now())

	if testCache.HasKey("prunable-old") {
		t.Error("expected 'prunable-old' to be pruned (Prune=true, idle > PruneAfter)")
	}

	if !testCache.HasKey("plain-old") {
		t.Error("expected 'plain-old' to survive (Prune=false, MaxUnused=Forever)")
	}

	if !testCache.HasKey("prunable-fresh") {
		t.Error("expected 'prunable-fresh' to survive (recently accessed)")
	}
}

// TestPruneInternal_Expire verifies that items whose Expire deadline has
// passed are removed regardless of their Prune flag or access recency.
func TestPruneInternal_Expire(t *testing.T) {
	t.Parallel()

	cfg := cache.Config{
		MaxUnused:  cache.Forever, // disable MaxUnused eviction
		PruneAfter: cache.Forever, // disable PruneAfter eviction
	}

	testCache := cache.NewTestCache(cfg)

	now := time.Now()
	past := now.Add(-time.Millisecond)
	future := now.Add(time.Hour)

	testCache.AddTestItem("expired", now, cache.Options{Expire: past})
	testCache.AddTestItem("not-yet", now, cache.Options{Expire: future})
	testCache.AddTestItem("no-expire", now, cache.Options{})

	testCache.RunPrune(now)

	if testCache.HasKey("expired") {
		t.Error("expected 'expired' to be pruned (Expire in the past)")
	}

	if !testCache.HasKey("not-yet") {
		t.Error("expected 'not-yet' to survive (Expire in the future)")
	}

	if !testCache.HasKey("no-expire") {
		t.Error("expected 'no-expire' to survive (zero Expire time = never)")
	}

	_, items := testCache.PruneCounts()
	if items != 1 {
		t.Errorf("pruned items: want 1, got %d", items)
	}
}

// TestPruneInternal_Empty verifies that pruning an empty cache increments
// Prunes but not Pruned, and does not panic.
func TestPruneInternal_Empty(t *testing.T) {
	t.Parallel()

	tc := cache.NewTestCache(cache.Config{MaxUnused: time.Millisecond})
	tc.RunPrune(time.Now())

	runs, items := tc.PruneCounts()

	if runs != 1 {
		t.Errorf("prune runs: want 1, got %d", runs)
	}

	if items != 0 {
		t.Errorf("pruned items: want 0, got %d", items)
	}
}

// TestPruneInternal_MultipleRuns verifies that repeated prune calls
// accumulate Prunes and Pruned counters correctly.
func TestPruneInternal_MultipleRuns(t *testing.T) {
	t.Parallel()

	testCache := cache.NewTestCache(cache.Config{
		MaxUnused:  100 * time.Millisecond,
		PruneAfter: cache.Forever,
	})

	past := time.Now().Add(-200 * time.Millisecond)
	testCache.AddTestItem("a", past, cache.Options{})
	testCache.AddTestItem("b", past, cache.Options{})

	from := time.Now()
	testCache.RunPrune(from) // first run: prunes a and b
	testCache.RunPrune(from) // second run: cache is empty

	runs, items := testCache.PruneCounts()

	if runs != 2 {
		t.Errorf("prune runs: want 2, got %d", runs)
	}

	if items != 2 {
		t.Errorf("pruned items: want 2, got %d", items)
	}
}
