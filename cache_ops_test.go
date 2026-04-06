package cache_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"golift.io/cache"
)

// originalData is used by several tests that save and retrieve the same string.
const originalData = "original"

// assertEqual is a tiny typed helper to avoid a testify dependency.
func assertEqual[T comparable](t *testing.T, name string, want, got T) {
	t.Helper()

	if want != got {
		t.Errorf("%s: want %v, got %v", name, want, got)
	}
}

// pruneConfig returns a Config suitable for prune-related integration tests.
// PruneInterval is set to the library minimum of 1 second; tests sleep for
// 2.5 seconds to allow at least one full prune cycle.
func pruneConfig() cache.Config {
	return cache.Config{
		RequestAccuracy: 100 * time.Millisecond,
		PruneInterval:   time.Second, // minimum enforced by newCache; sub-second values are clamped
		PruneAfter:      500 * time.Millisecond,
		MaxUnused:       500 * time.Millisecond,
	}
}

const pruneWait = 2500 * time.Millisecond

// TestShards_MultiplePoolsRoundTrip verifies Config.Shards spreads keys across
// independent maps while preserving aggregate size and List consistency.
func TestShards_MultiplePoolsRoundTrip(t *testing.T) {
	t.Parallel()

	store := cache.New(cache.Config{Shards: 8})
	defer store.Stop(true)

	for i := range 64 {
		key := fmt.Sprintf("k-%d", i)
		store.Save(key, i, cache.Options{})
	}

	stats := store.Stats()
	if stats.Size != 64 {
		t.Fatalf("Stats().Size: want 64, got %d", stats.Size)
	}

	if len(store.List()) != 64 {
		t.Fatalf("List len: want 64, got %d", len(store.List()))
	}
}

// TestConcurrentGetSameKey exercises many parallel Gets on one key (RLock + atomics).
func TestConcurrentGetSameKey(t *testing.T) {
	t.Parallel()

	store := cache.New(cache.Config{})
	defer store.Stop(true)

	const key = "shared"
	store.Save(key, "v", cache.Options{})

	const concurrentGets = 500

	var waitGroup sync.WaitGroup

	waitGroup.Add(concurrentGets)

	for range concurrentGets {
		go func() {
			defer waitGroup.Done()

			item := store.Get(key)
			if item == nil || item.Data != "v" {
				t.Error("expected cache hit with saved data")
			}
		}()
	}

	waitGroup.Wait()

	stats := store.Stats()
	if stats.Hits < concurrentGets {
		t.Fatalf("Stats().Hits: want >= %d, got %d", concurrentGets, stats.Hits)
	}
}

// TestConcurrentGetSameKey_Sharded is the same stress with multiple shards.
func TestConcurrentGetSameKey_Sharded(t *testing.T) {
	t.Parallel()

	store := cache.New(cache.Config{Shards: 16})
	defer store.Stop(true)

	const key = "only-one-key"
	store.Save(key, "v", cache.Options{})

	const concurrentGets = 500

	var waitGroup sync.WaitGroup

	waitGroup.Add(concurrentGets)

	for range concurrentGets {
		go func() {
			defer waitGroup.Done()

			item := store.Get(key)
			if item == nil || item.Data != "v" {
				t.Error("expected cache hit with saved data")
			}
		}()
	}

	waitGroup.Wait()

	stats := store.Stats()
	if stats.Hits < concurrentGets {
		t.Fatalf("Stats().Hits: want >= %d, got %d", concurrentGets, stats.Hits)
	}
}

// ---- Regression Tests -------------------------------------------------------

// TestBug1_NoPruneIntervalStop verifies that Stop() does not panic when the
// cache was created with PruneInterval == 0 (the default).
// The original bug called pruner.Stop() on an uninitialised &time.Ticker{}.
func TestBug1_NoPruneIntervalStop(t *testing.T) {
	t.Parallel()

	store := cache.New(cache.Config{})
	store.Stop(true) // must not panic
}

// TestBug2_ContextCancelThenStop verifies that calling Stop() after the
// context is cancelled does not panic from a double-close of store.req.
func TestBug2_ContextCancelThenStop(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	store := cache.NewWithContext(ctx, cache.Config{})

	cancel()
	time.Sleep(20 * time.Millisecond) // allow the goroutine to exit
	store.Stop(true)                  // must not panic
}

// TestBug3_StopStartRace verifies there is no data race on store.run when Stop
// and Start are called concurrently. Run with go test -race to detect races.
func TestBug3_StopStartRace(t *testing.T) {
	t.Parallel()

	const workers = 10

	store := cache.New(cache.Config{})
	done := make(chan struct{}, workers)

	for range workers {
		go func() {
			store.Stop(false)
			store.Start(false)

			done <- struct{}{}
		}()
	}

	for range workers {
		<-done
	}

	store.Stop(true)
}

// TestBug4_SaveNilData verifies that saving nil data stores the key rather
// than silently deleting it, which was the pre-fix behaviour.
func TestBug4_SaveNilData(t *testing.T) {
	t.Parallel()

	store := cache.New(cache.Config{})
	defer store.Stop(true)

	existed := store.Save("key", nil, cache.Options{})
	if existed {
		t.Fatal("Save: expected false (key is new)")
	}

	item := store.Get("key")
	if item == nil {
		t.Fatal("Get: expected item to exist after saving nil; got nil — key was deleted (regression)")
	}

	if item.Data != nil {
		t.Fatalf("Get: expected item.Data == nil; got %v", item.Data)
	}
}

// TestBug4_UpdateNilData mirrors Bug 4 for Update(), which shares the same
// dispatch path.
func TestBug4_UpdateNilData(t *testing.T) {
	t.Parallel()

	store := cache.New(cache.Config{})
	defer store.Stop(true)

	store.Update("key", nil, cache.Options{})

	item := store.Get("key")
	if item == nil {
		t.Fatal("Get: expected item to exist after Update with nil data; got nil — key was deleted (regression)")
	}
}

// ---- Lifecycle Tests --------------------------------------------------------

func TestStopIdempotent(t *testing.T) {
	t.Parallel()

	store := cache.New(cache.Config{})
	store.Stop(false) // first stop
	store.Stop(false) // must be a no-op, not panic
	store.Stop(true)  // third call still fine
}

func TestStartIdempotent(t *testing.T) {
	t.Parallel()

	store := cache.New(cache.Config{})
	defer store.Stop(true)

	store.Save("key", "value", cache.Options{})
	store.Start(false) // already running; must be a no-op

	item := store.Get("key")
	if item == nil {
		t.Fatal("cache contents should be unchanged after Start() when already running")
	}
}

func TestStopClean_True(t *testing.T) {
	t.Parallel()

	store := cache.New(cache.Config{})
	store.Save("key", "value", cache.Options{})
	store.Stop(true) // clean=true must clear the cache

	store.Start(false)
	defer store.Stop(true)

	if item := store.Get("key"); item != nil {
		t.Fatalf("expected nil after Stop(true) cleared cache, got %+v", item)
	}
}

func TestStopClean_False(t *testing.T) {
	t.Parallel()

	store := cache.New(cache.Config{})
	store.Save("key", "value", cache.Options{})
	store.Stop(false) // clean=false must retain the cache

	store.Start(false)
	defer store.Stop(true)

	item := store.Get("key")
	if item == nil {
		t.Fatal("expected cache to retain data after Stop(false)")
	}

	if item.Data != "value" {
		t.Fatalf("expected 'value', got %v", item.Data)
	}
}

func TestStartWithContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	store := cache.NewWithContext(ctx, cache.Config{})

	cancel()
	time.Sleep(20 * time.Millisecond) // allow goroutine to exit

	store.Stop(true) // must not panic or deadlock
}

// ---- Operation Tests --------------------------------------------------------

func TestGet(t *testing.T) {
	t.Parallel()

	store := cache.New(cache.Config{})
	defer store.Stop(true)

	if got := store.Get("missing"); got != nil {
		t.Fatalf("Get miss: expected nil, got %+v", got)
	}

	store.Save("k", "hello", cache.Options{})

	item := store.Get("k")
	if item == nil {
		t.Fatal("Get hit: expected item, got nil")
	}

	if item.Data != "hello" {
		t.Fatalf("Get hit: expected 'hello', got %v", item.Data)
	}

	if item.Hits != 1 {
		t.Fatalf("Get hit: expected Hits=1, got %d", item.Hits)
	}

	item2 := store.Get("k")
	if item2.Hits != 2 {
		t.Fatalf("Get hit #2: expected Hits=2, got %d", item2.Hits)
	}
}

func TestSave_New(t *testing.T) {
	t.Parallel()

	store := cache.New(cache.Config{})
	defer store.Stop(true)

	existed := store.Save("k", "v", cache.Options{})
	if existed {
		t.Fatal("Save new key: expected false")
	}

	stats := store.Stats()
	assertEqual(t, "Saves", int64(1), stats.Saves)
	assertEqual(t, "Updates", int64(0), stats.Updates)
}

func TestSave_Overwrite(t *testing.T) {
	t.Parallel()

	store := cache.New(cache.Config{})
	defer store.Stop(true)

	store.Save("k", "v1", cache.Options{})

	existed := store.Save("k", "v2", cache.Options{})
	if !existed {
		t.Fatal("Save overwrite: expected true (key existed)")
	}

	stats := store.Stats()
	assertEqual(t, "Saves", int64(1), stats.Saves)
	assertEqual(t, "Updates", int64(1), stats.Updates)
	// Save() must not count as a Get/hit.
	assertEqual(t, "Hits", int64(0), stats.Hits)

	item := store.Get("k")
	if item == nil || item.Data != "v2" {
		t.Fatalf("after overwrite expected 'v2', got %v", item)
	}
}

func TestUpdate_NewKey(t *testing.T) {
	t.Parallel()

	store := cache.New(cache.Config{})
	defer store.Stop(true)

	prev := store.Update("k", "new", cache.Options{})
	if prev != nil {
		t.Fatalf("Update new key: expected nil, got %+v", prev)
	}

	stats := store.Stats()
	assertEqual(t, "Saves", int64(1), stats.Saves)
}

func TestUpdate_ExistingKey(t *testing.T) {
	t.Parallel()

	store := cache.New(cache.Config{})
	defer store.Stop(true)

	store.Save("k", originalData, cache.Options{})

	prev := store.Update("k", "new", cache.Options{})
	if prev == nil {
		t.Fatal("Update existing key: expected previous item, got nil")
	}

	if prev.Data != originalData {
		t.Fatalf("Update existing key: expected previous Data=%q, got %v", originalData, prev.Data)
	}

	// Update counts the read of the old value as a cache hit.
	stats := store.Stats()
	assertEqual(t, "Updates", int64(1), stats.Updates)
	assertEqual(t, "Hits", int64(1), stats.Hits)

	item := store.Get("k")
	if item == nil || item.Data != "new" {
		t.Fatalf("after Update expected 'new', got %v", item)
	}
}

func TestDelete_Hit(t *testing.T) {
	t.Parallel()

	store := cache.New(cache.Config{})
	defer store.Stop(true)

	store.Save("k", "v", cache.Options{})

	deleted := store.Delete("k")
	if !deleted {
		t.Fatal("Delete existing key: expected true")
	}

	if item := store.Get("k"); item != nil {
		t.Fatal("expected nil after deletion")
	}

	stats := store.Stats()
	assertEqual(t, "Deletes", int64(1), stats.Deletes)
}

func TestDelete_Miss(t *testing.T) {
	t.Parallel()

	store := cache.New(cache.Config{})
	defer store.Stop(true)

	deleted := store.Delete("nonexistent")
	if deleted {
		t.Fatal("Delete missing key: expected false")
	}

	stats := store.Stats()
	assertEqual(t, "DelMiss", int64(1), stats.DelMiss)
}

func TestList_Empty(t *testing.T) {
	t.Parallel()

	store := cache.New(cache.Config{})
	defer store.Stop(true)

	items := store.List()
	if items == nil {
		t.Fatal("List() must never return nil (documented guarantee)")
	}

	if len(items) != 0 {
		t.Fatalf("expected empty map on fresh cache, got %d items", len(items))
	}
}

func TestList_Populated(t *testing.T) {
	t.Parallel()

	store := cache.New(cache.Config{})
	defer store.Stop(true)

	store.Save("a", 1, cache.Options{})
	store.Save("b", 2, cache.Options{})

	items := store.List()
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	if items["a"] == nil || items["b"] == nil {
		t.Fatal("expected both keys to appear in List()")
	}

	if items["a"].Data != 1 || items["b"].Data != 2 {
		t.Fatal("List items have wrong data")
	}
}

// ---- Stats Tests ------------------------------------------------------------

func TestStats_Accounting(t *testing.T) {
	t.Parallel()

	store := cache.New(cache.Config{})
	defer store.Stop(true)

	store.Save("a", 1, cache.Options{})   // Saves=1
	store.Save("b", 2, cache.Options{})   // Saves=2
	store.Save("a", 99, cache.Options{})  // Updates=1 (a exists); no hit recorded
	store.Get("a")                        // Hits=1
	store.Get("a")                        // Hits=2
	store.Get("missing")                  // Misses=1
	store.Update("b", 3, cache.Options{}) // Updates=2, Hits=3
	store.Delete("b")                     // Deletes=1
	store.Delete("gone")                  // DelMiss=1

	stats := store.Stats()
	assertEqual(t, "Saves", int64(2), stats.Saves)
	assertEqual(t, "Updates", int64(2), stats.Updates)
	assertEqual(t, "Hits", int64(3), stats.Hits)
	assertEqual(t, "Misses", int64(1), stats.Misses)
	assertEqual(t, "Gets (Hits+Misses)", int64(4), stats.Gets)
	assertEqual(t, "Deletes", int64(1), stats.Deletes)
	assertEqual(t, "DelMiss", int64(1), stats.DelMiss)
	assertEqual(t, "Size", int64(1), stats.Size) // only "a" remains
}

func TestExpStats(t *testing.T) {
	t.Parallel()

	store := cache.New(cache.Config{})
	defer store.Stop(true)

	got := store.ExpStats()
	if got == nil {
		t.Fatal("ExpStats() must not return nil")
	}

	if _, ok := got.(*cache.Stats); !ok {
		t.Fatalf("ExpStats() must return *cache.Stats; got %T", got)
	}
}

// ---- Prune Integration Tests ------------------------------------------------

func TestPrune_MaxUnused(t *testing.T) {
	t.Parallel()

	store := cache.New(pruneConfig())
	defer store.Stop(true)

	// Non-prunable key left unaccessed — must be evicted by MaxUnused.
	store.Save("k", "v", cache.Options{})
	time.Sleep(pruneWait)

	if item := store.Get("k"); item != nil {
		t.Fatal("expected key to be evicted by MaxUnused; it was not")
	}
}

func TestPrune_PruneAfter(t *testing.T) {
	t.Parallel()

	store := cache.New(pruneConfig())
	defer store.Stop(true)

	store.Save("k", "v", cache.Options{Prune: true})
	time.Sleep(pruneWait)

	if item := store.Get("k"); item != nil {
		t.Fatal("expected Prune=true key to be evicted after PruneAfter; it was not")
	}
}

func TestPrune_Expire(t *testing.T) {
	t.Parallel()

	cfg := pruneConfig()
	cfg.MaxUnused = cache.Forever // prevent MaxUnused from interfering

	store := cache.New(cfg)
	defer store.Stop(true)

	// Expire well before the 1-second pruner tick fires.
	store.Save("k", "v", cache.Options{Expire: time.Now().Add(100 * time.Millisecond)})
	time.Sleep(pruneWait)

	if item := store.Get("k"); item != nil {
		t.Fatal("expected key to be evicted after Expire time; it was not")
	}
}

func TestPrune_ActiveKeyKept(t *testing.T) {
	t.Parallel()

	// MaxUnused is 800 ms; access every 100 ms gives a comfortable 8× safety margin.
	cfg := cache.Config{
		RequestAccuracy: 100 * time.Millisecond,
		PruneInterval:   time.Second,
		MaxUnused:       800 * time.Millisecond,
	}

	store := cache.New(cfg)
	defer store.Stop(true)

	store.Save("active", "v", cache.Options{})

	// Keep accessing the key until just before we check it.
	ticker := time.NewTicker(100 * time.Millisecond)
	deadline := time.NewTimer(pruneWait - 200*time.Millisecond)

loop:
	for {
		select {
		case <-ticker.C:
			store.Get("active")
		case <-deadline.C:
			break loop
		}
	}

	ticker.Stop()

	if item := store.Get("active"); item == nil {
		t.Fatal("expected actively-accessed key to NOT be evicted")
	}
}

func TestPrune_Stats(t *testing.T) {
	t.Parallel()

	store := cache.New(pruneConfig())
	defer store.Stop(true)

	store.Save("k", "v", cache.Options{})
	time.Sleep(pruneWait)

	stats := store.Stats()

	if stats.Prunes == 0 {
		t.Fatal("expected Prunes > 0 after prune interval elapsed")
	}

	if stats.Pruned == 0 {
		t.Fatal("expected Pruned > 0 after eviction")
	}

	if stats.Pruning.Duration == 0 {
		t.Fatal("expected Pruning.Duration > 0 after pruning ran")
	}
}

func TestForever(t *testing.T) {
	t.Parallel()

	cfg := pruneConfig()
	cfg.MaxUnused = cache.Forever
	cfg.PruneAfter = cache.Forever

	store := cache.New(cfg)
	defer store.Stop(true)

	store.Save("k", "v", cache.Options{})
	time.Sleep(pruneWait)

	if item := store.Get("k"); item == nil {
		t.Fatal("expected key to survive when MaxUnused=Forever and PruneAfter=Forever")
	}
}

// ---- Isolation Tests --------------------------------------------------------

// TestItemIsolation verifies that mutating a Get-returned *Item does not
// affect a subsequent Get of the same key.
func TestItemIsolation(t *testing.T) {
	t.Parallel()

	store := cache.New(cache.Config{})
	defer store.Stop(true)

	store.Save("k", originalData, cache.Options{})

	item := store.Get("k")
	item.Data = "mutated" // modify the returned copy

	item2 := store.Get("k")
	if item2.Data != originalData {
		t.Fatalf("modifying a returned Item affected the cache; got %v, want %q", item2.Data, originalData)
	}
}

// TestListIsolation verifies that mutations to the map returned by List()
// do not affect the live cache.
func TestListIsolation(t *testing.T) {
	t.Parallel()

	store := cache.New(cache.Config{})
	defer store.Stop(true)

	store.Save("k", originalData, cache.Options{})

	items := store.List()
	items["k"].Data = "mutated" // modify item in the returned map
	delete(items, "k")          // remove from the returned map

	item := store.Get("k")
	if item == nil {
		t.Fatal("removing a key from the List() map must not affect the cache")
	}

	if item.Data != originalData {
		t.Fatalf("modifying a List() item affected the cache; got %v, want %q", item.Data, originalData)
	}
}

// ---- Concurrency Test -------------------------------------------------------

// TestConcurrentAccess hammers the cache from 20 goroutines simultaneously.
// Run with go test -race to surface any synchronisation issues.
func TestConcurrentAccess(t *testing.T) {
	t.Parallel()

	store := cache.New(cache.Config{})
	defer store.Stop(true)

	const workers = 20

	keys := []string{"a", "b", "c", "d", "e"}
	done := make(chan struct{}, workers)
	deadline := time.Now().Add(100 * time.Millisecond)

	for workerIdx := range workers {
		go func() {
			defer func() { done <- struct{}{} }()

			key := keys[workerIdx%len(keys)]

			for time.Now().Before(deadline) {
				switch workerIdx % 3 {
				case 0:
					store.Save(key, workerIdx, cache.Options{})
				case 1:
					store.Get(key)
				case 2:
					store.Delete(key)
				}
			}
		}()
	}

	for range workers {
		<-done
	}
}

// ---- Config / Duration Tests ------------------------------------------------

// TestConfigDefaults verifies that a zero Config{} is fully usable without
// any manual field initialisation.
func TestConfigDefaults(t *testing.T) {
	t.Parallel()

	store := cache.New(cache.Config{})
	defer store.Stop(true)

	store.Save("k", "v", cache.Options{})

	item := store.Get("k")
	if item == nil || item.Data != "v" {
		t.Fatal("cache not functional with all-default config")
	}
}

func TestDurationMarshalJSON(t *testing.T) {
	t.Parallel()

	dur := cache.Duration{Duration: 5 * time.Second}

	raw, err := json.Marshal(&dur)
	if err != nil {
		t.Fatalf("MarshalJSON error: %v", err)
	}

	got := string(raw)
	if got != `"5s"` {
		t.Fatalf("MarshalJSON: expected %q, got %q", `"5s"`, got)
	}
}
