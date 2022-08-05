package cache_test

import (
	"fmt"

	"golift.io/cache"
)

func ExampleNew() {
	users := cache.New(cache.Config{})
	defer users.Stop(true) // stop the processor go routine.

	// This example just saves a string.
	// The input is an interface{} so you can save more than strings.
	users.Save("admin", "Super Dooper", cache.Options{})
	users.Save("luser", "Under Dawggy", cache.Options{})

	user := users.Get("luser")
	fmt.Println("User Name:", user.Data)

	users.Delete("luser")

	stats := users.Stats()
	fmt.Println("Gets:", stats.Gets)
	fmt.Println("Hits:", stats.Hits)
	fmt.Println("Miss:", stats.Misses)
	fmt.Println("Save:", stats.Saves)
	fmt.Println("Del:", stats.Deletes)
	fmt.Println("Size:", stats.Size)
	// Output:
	// User Name: Under Dawggy
	// Gets: 1
	// Hits: 1
	// Miss: 0
	// Save: 2
	// Del: 1
	// Size: 1
}
