package main

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"time"
)

// This example demonstrates an unbounded cache that leaks memory
// by keeping all cached objects forever without any eviction policy.

type CachedObject struct {
	Key       string
	Data      []byte // 5 KB of data
	Timestamp time.Time
}

var (
	// Unbounded cache - grows forever
	cache = make(map[string]*CachedObject)
)

func main() {
	// Start pprof server
	go func() {
		fmt.Println("pprof server running on http://localhost:6060")
		fmt.Println("Collect heap profile: curl http://localhost:6060/debug/pprof/heap > heap.pprof")
		fmt.Println("View profile: go tool pprof -http=:8081 heap.pprof")
		fmt.Println()
		if err := http.ListenAndServe("localhost:6060", nil); err != nil {
			fmt.Printf("pprof server error: %v\n", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("[START] Heap Alloc: %d MB, Objects cached: %d\n",
		m.Alloc/1024/1024, len(cache))

	// Simulate continuous caching without eviction
	go continuouslyCacheObjects()

	// Monitor memory every 2 seconds
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	duration := 10 * time.Second
	start := time.Now()

	for time.Since(start) < duration {
		<-ticker.C
		runtime.ReadMemStats(&m)
		fmt.Printf("[AFTER %v] Heap Alloc: %d MB, Objects cached: %d\n",
			time.Since(start).Round(time.Second),
			m.Alloc/1024/1024,
			len(cache))
	}

	fmt.Println("\nLeak demonstrated. Cache grows unbounded.")
	fmt.Println("Press Ctrl+C to stop")

	// Keep running for profiling
	select {}
}

func continuouslyCacheObjects() {
	counter := 0
	ticker := time.NewTicker(200 * time.Microsecond) // 5000 objects per second
	defer ticker.Stop()

	for range ticker.C {
		counter++
		key := fmt.Sprintf("key_%d", counter)

		// Create object with 5 KB of data
		obj := &CachedObject{
			Key:       key,
			Data:      make([]byte, 5*1024),
			Timestamp: time.Now(),
		}

		// Store in cache - never removed!
		cache[key] = obj

		// Fill with some data to prevent optimization
		for i := range obj.Data {
			obj.Data[i] = byte(i % 256)
		}
	}
}

