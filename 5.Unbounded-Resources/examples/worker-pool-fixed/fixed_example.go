package main

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"sync/atomic"
	"time"
)

// This example demonstrates a properly bounded worker pool that
// limits concurrent goroutines and provides backpressure when overloaded.

var (
	tasksSubmitted int64
	tasksCompleted int64
	tasksRejected  int64
)

// WorkerPool implements a fixed-size pool of workers
type WorkerPool struct {
	tasks    chan func()
	workers  int
	shutdown chan struct{}
}

// NewWorkerPool creates a pool with fixed worker count and queue size
func NewWorkerPool(workerCount, queueSize int) *WorkerPool {
	pool := &WorkerPool{
		tasks:    make(chan func(), queueSize),
		workers:  workerCount,
		shutdown: make(chan struct{}),
	}

	// Start fixed number of workers
	for i := 0; i < workerCount; i++ {
		go pool.worker(i)
	}

	return pool
}

// worker processes tasks from the queue
func (p *WorkerPool) worker(id int) {
	for {
		select {
		case task := <-p.tasks:
			task()
		case <-p.shutdown:
			return
		}
	}
}

// Submit adds a task to the pool, returns false if queue is full
func (p *WorkerPool) Submit(task func()) bool {
	select {
	case p.tasks <- task:
		return true
	default:
		// Queue full - apply backpressure
		return false
	}
}

// Close shuts down the worker pool
func (p *WorkerPool) Close() {
	close(p.shutdown)
}

func main() {
	// Start pprof server
	go func() {
		fmt.Println("pprof server running on http://localhost:6061")
		if err := http.ListenAndServe("localhost:6061", nil); err != nil {
			fmt.Printf("pprof server error: %v\n", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	// Create bounded worker pool: 100 workers, 500 queue size
	pool := NewWorkerPool(100, 500)
	defer pool.Close()

	initialGoroutines := runtime.NumGoroutine()
	fmt.Printf("[START] Goroutines: %d (100 workers + overhead)\n", initialGoroutines)
	fmt.Println("Simulating traffic spike: 1000 tasks/second")
	fmt.Println()

	// Simulate incoming tasks at high rate
	go simulateTrafficSpike(pool)

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
		rejected := atomic.LoadInt64(&tasksRejected)

		fmt.Printf("[AFTER %v] Goroutines: %d  |  Submitted: %d  |  Completed: %d  |  Rejected: %d\n",
			time.Since(start).Round(time.Second),
			goroutines,
			submitted,
			completed,
			rejected)

		if goroutines <= initialGoroutines+10 {
			fmt.Println("Goroutines stable! Worker pool bounded at 100.")
		}
	}

	fmt.Println("\nNo leak! Goroutine count remained stable.")
	fmt.Printf("Final goroutine count: %d\n", runtime.NumGoroutine())
	fmt.Printf("Total tasks: submitted=%d, completed=%d, rejected=%d\n",
		atomic.LoadInt64(&tasksSubmitted),
		atomic.LoadInt64(&tasksCompleted),
		atomic.LoadInt64(&tasksRejected))
	fmt.Println("Press Ctrl+C to stop")

	select {}
}

// simulateTrafficSpike creates tasks at a high rate
func simulateTrafficSpike(pool *WorkerPool) {
	ticker := time.NewTicker(1 * time.Millisecond) // 1000 tasks/second
	defer ticker.Stop()

	for range ticker.C {
		// FIX: Submit to bounded pool
		// Returns false if pool is full (backpressure)
		task := func() {
			processTaskCorrectly()
		}

		if pool.Submit(task) {
			atomic.AddInt64(&tasksSubmitted, 1)
		} else {
			atomic.AddInt64(&tasksRejected, 1)
		}
	}
}

// processTaskCorrectly simulates a slow task that takes 5 seconds
func processTaskCorrectly() {
	time.Sleep(5 * time.Second)
	atomic.AddInt64(&tasksCompleted, 1)
}
