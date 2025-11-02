package main

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"time"
)

// This example demonstrates a classic goroutine leak where goroutines
// are spawned to send on a channel, but there's no receiver.
// Each goroutine blocks forever, causing them to accumulate.

func main() {
	// Start pprof server for profiling
	go func() {
		fmt.Println("pprof server running on http://localhost:6060")
		fmt.Println("Collect goroutine profile with: curl http://localhost:6060/debug/pprof/goroutine > goroutine_fixedEX.pprof")
		fmt.Println("View profile with: go tool pprof -http=:8081 goroutine_fixedEX.pprof")
		fmt.Println()
		if err := http.ListenAndServe("localhost:6060", nil); err != nil {
			fmt.Printf("pprof server error: %v\n", err)
		}
	}()

	// Give pprof server time to start
	time.Sleep(100 * time.Millisecond)

	fmt.Printf("[START] Goroutines: %d\n", runtime.NumGoroutine())

	// Simulate a leaky pattern - spawning goroutines that never terminate
	go leakGoroutines()

	// Monitor goroutine count every 2 seconds
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	duration := 10 * time.Second
	start := time.Now()

	for time.Since(start) < duration {
		<-ticker.C
		fmt.Printf("[AFTER %v] Goroutines: %d\n", time.Since(start).Round(time.Second), runtime.NumGoroutine())
	}

	fmt.Println("\nLeak demonstrated. Goroutines continue to accumulate.")
	fmt.Println("Press Ctrl+C to stop")

	// Keep running so you can collect profiles
	select {}
}

// leakGoroutines spawns goroutines that will block forever
func leakGoroutines() {
	// Create an unbuffered channel
	ch := make(chan int)

	// Spawn goroutines every 20ms (50 per second)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		// Each goroutine tries to send on the channel
		// Since there's no receiver, they all block forever
		go func() {
			result := doWork()
			ch <- result // THIS BLOCKS FOREVER - no one reads from ch
		}()
	}
}

// doWork simulates some work being done
func doWork() int {
	// Simulate work
	time.Sleep(10 * time.Millisecond)
	return 42
}
