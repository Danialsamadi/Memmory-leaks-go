package main

import (
	"container/list"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"sync"
	"time"
)

// This example demonstrates a proper LRU cache with size limits
// that prevents memory leaks through automatic eviction.

type CachedObject struct {
	Key       string
	Data      []byte
	Timestamp time.Time
}

// LRUCache implements a simple LRU cache with size limit
type LRUCache struct {
	mu       sync.Mutex
	capacity int
	cache    map[string]*list.Element
	lruList  *list.List
}

type entry struct {
	key   string
	value *CachedObject
}

func NewLRUCache(capacity int) *LRUCache {
	return &LRUCache{
		capacity: capacity,
		cache:    make(map[string]*list.Element),
		lruList:  list.New(),
	}
}

func (c *LRUCache) Set(key string, value *CachedObject) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If key exists, update and move to front
	if elem, ok := c.cache[key]; ok {
		c.lruList.MoveToFront(elem)
		elem.Value.(*entry).value = value
		return
	}

	// Add new entry
	elem := c.lruList.PushFront(&entry{key, value})
	c.cache[key] = elem

	// Evict oldest if over capacity
	if c.lruList.Len() > c.capacity {
		oldest := c.lruList.Back()
		if oldest != nil {
			c.lruList.Remove(oldest)
			delete(c.cache, oldest.Value.(*entry).key)
		}
	}
}

func (c *LRUCache) Get(key string) (*CachedObject, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.cache[key]; ok {
		c.lruList.MoveToFront(elem)
		return elem.Value.(*entry).value, true
	}
	return nil, false
}

func (c *LRUCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lruList.Len()
}

var (
	// LRU cache with max 1000 items
	cache *LRUCache
)

func main() {
	// Initialize LRU cache with max 1000 items
	cache = NewLRUCache(1000)
	
	// Start pprof server
	go func() {
		fmt.Println("pprof server running on http://localhost:6060")
		fmt.Println("Collect heap profile: curl http://localhost:6060/debug/pprof/heap > heap_fixed.pprof")
		fmt.Println("Compare with leaky: go tool pprof -base=heap.pprof heap_fixed.pprof")
		fmt.Println()
		if err := http.ListenAndServe("localhost:6060", nil); err != nil {
			fmt.Printf("pprof server error: %v\n", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("[START] Heap Alloc: %d MB, Objects cached: %d\n",
		m.Alloc/1024/1024, cache.Len())

	// Simulate continuous caching with LRU eviction
	go continuouslyCacheObjects()

	// Monitor memory every 2 seconds
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	duration := 10 * time.Second
	start := time.Now()

	for time.Since(start) < duration {
		<-ticker.C
		runtime.ReadMemStats(&m)
		fmt.Printf("[AFTER %v] Heap Alloc: %d MB, Objects cached: %d (max: 1000)\n",
			time.Since(start).Round(time.Second),
			m.Alloc/1024/1024,
			cache.Len())
	}

	fmt.Println("\nMemory stabilized. Cache stays at max capacity.")
	fmt.Println("Old items automatically evicted.")
	fmt.Printf("Final cache size: %d objects\n", cache.Len())
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

		// Fill with some data
		for i := range obj.Data {
			obj.Data[i] = byte(i % 256)
		}

		// Store in LRU cache - old items automatically evicted
		cache.Set(key, obj)
	}
}

