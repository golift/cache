# cache

[![Go Reference](https://pkg.go.dev/badge/golift.io/cache.svg)](https://pkg.go.dev/golift.io/cache)
[![Go Report Card](https://goreportcard.com/badge/golift.io/cache)](https://goreportcard.com/report/golift.io/cache)
[![MIT License](http://img.shields.io/:license-mit-blue.svg)](https://github.com/golift/cache/blob/master/LICENSE)
[![discord](https://badgen.net/badge/icon/Discord?color=0011ff&label&icon=https://simpleicons.now.sh/discord/eee "GoLift Discord")](https://golift.io/discord)

This module provides a small in-memory key/value cache for Go.

- **Concurrency:** A `sync.RWMutex` protects the map and write-heavy operations (`Get`, `Save`, `Update`, `Delete`).
  Read-mostly work such as `List` and `Stats` uses a read lock when possible.
- **Background work:** One goroutine refreshes a shared clock on `RequestAccuracy` (so hot paths avoid `time.Now()`
  every time) and runs the optional pruner on `PruneInterval`.
- **Metrics:** Hit/miss and other counters are exposed for `expvar` or your own metrics pipeline.

Items can be marked prunable or not. Prunable entries are removed after they have not been retrieved within `PruneAfter`.
Non-prunable entries use a separate maximum idle duration (`MaxUnused`).

I wrote this to cache data from MySQL queries for an [nginx auth proxy](https://github.com/Notifiarr/mysql-auth-proxy).
It is also useful as a simple global in-process store.

## Example: basic use

```go
package main

import (
	"fmt"

	"golift.io/cache"
)

func main() {
	c := cache.New(cache.Config{})
	defer c.Stop(true)

	c.Save("user:42", "Ada", cache.Options{})

	item := c.Get("user:42")
	if item != nil {
		fmt.Println("value:", item.Data)
	}
}
```

## Example: pruning and stats

```go
import (
	"fmt"
	"time"

	"golift.io/cache"
)

func exampleStats() {
	c := cache.New(cache.Config{
		PruneInterval: 5 * time.Minute,
		PruneAfter:    10 * time.Minute,
		MaxUnused:     24 * time.Hour,
	})
	defer c.Stop(false)

	c.Save("session:1", "opaque-token", cache.Options{Prune: true})

	stats := c.Stats()
	fmt.Println("hits:", stats.Hits, "size:", stats.Size)
}
```

See also the package example in [cache_test.go](cache_test.go).
