package main

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"sync/atomic"
	"time"
)

// This example demonstrates unbounded goroutine creation where
// every task spawns a new goroutine without any limits.
// Under load, this causes exponential goroutine growth and eventual OOM.

var (
	tasksSubmitted int64
	tasksCompleted int64
)

func main() {
	// Start pprof server
	go func() {
		fmt.Println("pprof server running on http://localhost:6060")
		fmt.Println("Collect goroutine profile: curl http://localhost:6060/debug/pprof/goroutine > goroutine.pprof")
		fmt.Println()
		if err := http.ListenAndServe("localhost:6060", nil); err != nil {
			fmt.Printf("pprof server error: %v\n", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	fmt.Printf("[START] Goroutines: %d\n", runtime.NumGoroutine())
	fmt.Println("Simulating traffic spike: 1000 tasks/second")
	fmt.Println()

	// Simulate incoming tasks at high rate
	go simulateTrafficSpike()

	// Monitor goroutine count
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	duration := 10 * time.Second
	start := time.Now()

	for time.Since(start) < duration {
		<-ticker.C
		goroutines := runtime.NumGoroutine()
		submitted := atomic.LoadInt64(&tasksSubmitted)
		completed := atomic.LoadInt64(&tasksCompleted)

		fmt.Printf("[AFTER %v] Goroutines: %d  |  Tasks submitted: %d  |  Completed: %d\n",
			time.Since(start).Round(time.Second),
			goroutines,
			submitted,
			completed)

		if goroutines > 1000 {
			fmt.Println("\nWARNING: Unbounded goroutine growth detected!")
			fmt.Println("Each task creates a new goroutine without limits.")
		}
	}

	fmt.Println("\nLeak demonstrated. Goroutines grow without bound.")
	fmt.Printf("Final goroutine count: %d\n", runtime.NumGoroutine())
	fmt.Println("Press Ctrl+C to stop")

	// Keep running for profiling
	select {}
}

// simulateTrafficSpike creates tasks at a high rate
func simulateTrafficSpike() {
	ticker := time.NewTicker(1 * time.Millisecond) // 1000 tasks/second
	defer ticker.Stop()

	for range ticker.C {
		// BUG: Every task spawns a new goroutine!
		// No limit on concurrent goroutines
		go processTaskBadly()
		atomic.AddInt64(&tasksSubmitted, 1)
	}
}

// processTaskBadly simulates a slow task that takes 5 seconds
func processTaskBadly() {
	// Simulate work that takes time
	time.Sleep(5 * time.Second)
	atomic.AddInt64(&tasksCompleted, 1)
}
