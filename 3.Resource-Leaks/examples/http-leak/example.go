package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"time"
)

// APIGateway simulates a service that makes HTTP requests to external APIs
// BUG: HTTP response bodies are not closed, leaking connections
type APIGateway struct {
	requestsMade int
	mockServer   *http.Server
}

func main() {
	// Start pprof server
	go func() {
		log.Println("pprof server running on http://localhost:6060")
		log.Fatal(http.ListenAndServe("localhost:6060", nil))
	}()

	gateway := &APIGateway{}
	
	// Start a mock HTTP server to make requests against
	gateway.startMockServer()
	time.Sleep(100 * time.Millisecond) // Let server start
	
	// Print initial state
	fmt.Printf("[START] Goroutines: %d\n", runtime.NumGoroutine())
	
	// Simulate continuous API calls
	ticker := time.NewTicker(40 * time.Millisecond) // 25 requests/second
	defer ticker.Stop()
	
	startTime := time.Now()
	reportInterval := 2 * time.Second
	lastReport := startTime
	
	for {
		select {
		case <-ticker.C:
			// BUG: fetchDataBadly leaks HTTP connections
			if _, err := gateway.fetchDataBadly(); err != nil {
				log.Printf("Error fetching data: %v", err)
			}
			
			// Report every 2 seconds
			if time.Since(lastReport) >= reportInterval {
				goroutines := runtime.NumGoroutine()
				elapsed := time.Since(startTime).Seconds()
				fmt.Printf("[AFTER %.0fs] Goroutines: %d  |  Requests made: %d\n", 
					elapsed, goroutines, gateway.requestsMade)
				
				if goroutines > 20 {
					fmt.Println("\n⚠️  WARNING: Connection leak detected!")
					fmt.Println("Many goroutines stuck in HTTP read/write")
					fmt.Println("pprof server running on http://localhost:6060")
					fmt.Println("Run: curl http://localhost:6060/debug/pprof/goroutine > goroutine.pprof")
				}
				
				lastReport = time.Now()
			}
		}
	}
}

// fetchDataBadly makes an HTTP request but NEVER closes the response body
func (gw *APIGateway) fetchDataBadly() ([]byte, error) {
	// BUG: Using default HTTP client with no timeouts
	resp, err := http.Get("http://localhost:8080/api/data")
	if err != nil {
		return nil, err
	}
	
	// BUG: Response body is never closed!
	// This keeps the HTTP connection open indefinitely
	
	// Check status
	if resp.StatusCode != 200 {
		// BUG: Early return without closing body
		return nil, fmt.Errorf("bad status: %d", resp.StatusCode)
	}
	
	// Read body
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		// BUG: Another early return without closing body
		return nil, err
	}
	
	gw.requestsMade++
	
	// Response body never closed - connection leaks!
	return data, nil
}

// startMockServer creates a simple HTTP server for testing
func (gw *APIGateway) startMockServer() {
	mux := http.NewServeMux()
	
	mux.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		// Simulate some processing time
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"ok","data":"test-%d"}`, gw.requestsMade)
	})
	
	gw.mockServer = &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
	
	go func() {
		if err := gw.mockServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("Mock server error: %v", err)
		}
	}()
}

