package cache_test

import (
	"fmt"

	"golift.io/cache"
)

func ExampleNew() {
	users := cache.New(cache.Config{})
	defer users.Stop(true)

	users.Save("admin", "Super Dooper", cache.Options{})
	users.Save("user", "Under Dawggy", cache.Options{})

	user := users.Get("user")
	fmt.Println("User Name:", user.Data)

	for k, v := range users.GetStats() {
		fmt.Println(k, ":", v)
	}
	// Unordered Output:
	// User Name: Under Dawggy
	// Hits : 1
	// Misses : 0
	// Saves : 2
	// Updates : 0
	// Deletes : 0
	// DelMiss : 0
	// Size : 2
	// Get : 1
}
