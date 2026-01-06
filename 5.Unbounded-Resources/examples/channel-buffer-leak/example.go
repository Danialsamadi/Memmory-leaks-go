package main

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"sync/atomic"
	"time"
)

// This example demonstrates how excessively large channel buffers
// hide backpressure problems and can lead to memory exhaustion.

type Event struct {
	ID        int64
	Timestamp time.Time
	Data      [1024]byte // 1KB payload
}

var (
	eventsQueued    int64
	eventsProcessed int64
)

// EventProcessor with dangerously large buffer
type EventProcessor struct {
	// BUG: 1 million event buffer = 1GB memory!
	events chan Event
}

func NewEventProcessor() *EventProcessor {
	return &EventProcessor{
		// BUG: Huge buffer hides backpressure
		// 1M events × 1KB = 1GB of memory
		events: make(chan Event, 1_000_000),
	}
}

func (p *EventProcessor) Queue(e Event) {
	p.events <- e // Never blocks until 1M events!
	atomic.AddInt64(&eventsQueued, 1)
}

func (p *EventProcessor) Process() {
	for e := range p.events {
		// Simulate slow processing
		time.Sleep(10 * time.Millisecond)
		_ = e.ID
		atomic.AddInt64(&eventsProcessed, 1)
	}
}

func main() {
	// Start pprof server
	go func() {
		fmt.Println("pprof server running on http://localhost:6060")
		if err := http.ListenAndServe("localhost:6060", nil); err != nil {
			fmt.Printf("pprof server error: %v\n", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	processor := NewEventProcessor()

	// Start slow processor (100 events/second)
	go processor.Process()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("[START] Heap Alloc: %d MB, Events queued: 0\n", m.Alloc/1024/1024)
	fmt.Println("Simulating event burst: 10,000 events/second")
	fmt.Println("Processing rate: 100 events/second")
	fmt.Println()

	// Simulate burst of events (much faster than processing)
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
		pending := queued - processed

		fmt.Printf("[AFTER %v] Heap: %d MB  |  Queued: %d  |  Processed: %d  |  Pending: %d\n",
			time.Since(start).Round(time.Second),
			m.Alloc/1024/1024,
			queued,
			processed,
			pending)

		if pending > 10000 {
			fmt.Println("\nWARNING: Event backlog growing!")
			fmt.Println("Large buffer hides the problem - no backpressure signal.")
		}
	}

	runtime.ReadMemStats(&m)
	fmt.Printf("\nFinal state: %d MB heap, %d events pending\n",
		m.Alloc/1024/1024,
		atomic.LoadInt64(&eventsQueued)-atomic.LoadInt64(&eventsProcessed))
	fmt.Println("The large buffer consumed memory without providing feedback.")
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
		p.Queue(event)
	}
}
