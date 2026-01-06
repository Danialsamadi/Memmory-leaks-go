package main

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"sync/atomic"
	"time"
)

// This example demonstrates proper channel sizing with backpressure
// and timeout handling for event processing.

type Event struct {
	ID        int64
	Timestamp time.Time
	Data      [1024]byte // 1KB payload
}

var (
	eventsQueued    int64
	eventsProcessed int64
	eventsDropped   int64
)

// EventProcessor with properly sized buffer and backpressure
type EventProcessor struct {
	events chan Event
}

func NewEventProcessor() *EventProcessor {
	return &EventProcessor{
		// FIX: Reasonable buffer size (1000 events = 1MB)
		// Provides some buffering without hiding problems
		events: make(chan Event, 1000),
	}
}

// Queue attempts to queue an event with timeout
// Returns false if queue is full (backpressure signal)
func (p *EventProcessor) Queue(ctx context.Context, e Event) bool {
	select {
	case p.events <- e:
		atomic.AddInt64(&eventsQueued, 1)
		return true
	case <-ctx.Done():
		atomic.AddInt64(&eventsDropped, 1)
		return false
	default:
		// Queue full - signal backpressure
		atomic.AddInt64(&eventsDropped, 1)
		return false
	}
}

// QueueWithTimeout queues with a deadline
func (p *EventProcessor) QueueWithTimeout(e Event, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	select {
	case p.events <- e:
		atomic.AddInt64(&eventsQueued, 1)
		return true
	case <-ctx.Done():
		atomic.AddInt64(&eventsDropped, 1)
		return false
	}
}

func (p *EventProcessor) Process() {
	for e := range p.events {
		// Simulate processing
		time.Sleep(10 * time.Millisecond)
		_ = e.ID
		atomic.AddInt64(&eventsProcessed, 1)
	}
}

func (p *EventProcessor) Close() {
	close(p.events)
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

	processor := NewEventProcessor()
	defer processor.Close()

	// Start processor (100 events/second)
	go processor.Process()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("[START] Heap Alloc: %d MB, Buffer size: 1000 events\n", m.Alloc/1024/1024)
	fmt.Println("Simulating event burst: 10,000 events/second")
	fmt.Println("Processing rate: 100 events/second")
	fmt.Println("Excess events will be dropped (backpressure)")
	fmt.Println()

	// Simulate burst of events
	go simulateEventBurst(processor)

	// Monitor memory and queue
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	duration := 10 * time.Second
	start := time.Now()

	for time.Since(start) < duration {
		<-ticker.C
		runtime.ReadMemStats(&m)
		queued := atomic.LoadInt64(&eventsQueued)
		processed := atomic.LoadInt64(&eventsProcessed)
		dropped := atomic.LoadInt64(&eventsDropped)
		pending := queued - processed

		fmt.Printf("[AFTER %v] Heap: %d MB  |  Queued: %d  |  Processed: %d  |  Dropped: %d  |  Pending: %d\n",
			time.Since(start).Round(time.Second),
			m.Alloc/1024/1024,
			queued,
			processed,
			dropped,
			pending)

		if pending <= 1000 {
			fmt.Println("Buffer bounded! Backpressure working.")
		}
	}

	runtime.ReadMemStats(&m)
	fmt.Printf("\nFinal state: %d MB heap\n", m.Alloc/1024/1024)
	fmt.Printf("Events: queued=%d, processed=%d, dropped=%d\n",
		atomic.LoadInt64(&eventsQueued),
		atomic.LoadInt64(&eventsProcessed),
		atomic.LoadInt64(&eventsDropped))
	fmt.Println("Backpressure prevented memory exhaustion.")
	fmt.Println("Press Ctrl+C to stop")

	select {}
}

// simulateEventBurst sends events much faster than they can be processed
func simulateEventBurst(p *EventProcessor) {
	ticker := time.NewTicker(100 * time.Microsecond) // 10,000 events/second
	defer ticker.Stop()

	var id int64
	for range ticker.C {
		id++
		event := Event{
			ID:        id,
			Timestamp: time.Now(),
		}
		// Fill with data
		for i := range event.Data {
			event.Data[i] = byte(i % 256)
		}

		// FIX: Use non-blocking queue with backpressure
		// Events are dropped when buffer is full
		p.Queue(context.Background(), event)
	}
}
