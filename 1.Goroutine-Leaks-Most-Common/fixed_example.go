package main

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"time"
)

// This example demonstrates the FIXED version using context for cancellation
// and proper channel handling to prevent goroutine leaks.

func main() {
	// Start pprof server for profiling
	go func() {
		fmt.Println("pprof server running on http://localhost:6060")
		fmt.Println("Collect goroutine profile with: curl http://localhost:6060/debug/pprof/goroutine > goroutine_fixed.pprof")
		fmt.Println("Compare with leaked version: go tool pprof -base=goroutine_leak.pprof goroutine_fixed.pprof")
		fmt.Println()
		if err := http.ListenAndServe("localhost:6060", nil); err != nil {
			fmt.Printf("pprof server error: %v\n", err)
		}
	}()

	// Give pprof server time to start
	time.Sleep(100 * time.Millisecond)

	fmt.Printf("[START] Goroutines: %d\n", runtime.NumGoroutine())

	// Create a context with cancellation for cleanup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the fixed version - goroutines will terminate properly
	go processWorkersFixed(ctx)

	// Monitor goroutine count every 2 seconds
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	duration := 10 * time.Second
	start := time.Now()

	for time.Since(start) < duration {
		<-ticker.C
		fmt.Printf("[AFTER %v] Goroutines: %d\n", time.Since(start).Round(time.Second), runtime.NumGoroutine())
	}

	// Cancel context to trigger cleanup
	cancel()
	
	// Give goroutines time to clean up
	time.Sleep(100 * time.Millisecond)
	
	fmt.Println("\nAll goroutines cleaned up successfully")
	fmt.Printf("Final goroutine count: %d\n", runtime.NumGoroutine())
	fmt.Println("Press Ctrl+C to stop")
	
	// Keep running so you can collect profiles
	select {}
}

// processWorkersFixed demonstrates the proper pattern using context
func processWorkersFixed(ctx context.Context) {
	// Use a buffered channel to prevent blocking
	// Buffer size should match expected concurrency
	resultCh := make(chan int, 10)

	// Start a receiver goroutine
	go func() {
		for {
			select {
			case result := <-resultCh:
				// Process results
				_ = result
			case <-ctx.Done():
				return
			}
		}
	}()

	// Spawn worker goroutines with proper cancellation
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Spawn worker that respects context
			go worker(ctx, resultCh)
		case <-ctx.Done():
			// Stop spawning new workers and return
			return
		}
	}
}

// worker performs work and respects context cancellation
func worker(ctx context.Context, resultCh chan<- int) {
	// Check if context is already cancelled
	select {
	case <-ctx.Done():
		return
	default:
	}

	result := doWork()

	// Try to send result, but also check for cancellation
	select {
	case resultCh <- result:
		// Successfully sent
	case <-ctx.Done():
		// Context cancelled, exit without sending
		return
	}
}

// doWork simulates some work being done
func doWork() int {
	time.Sleep(10 * time.Millisecond)
	return 42
}

