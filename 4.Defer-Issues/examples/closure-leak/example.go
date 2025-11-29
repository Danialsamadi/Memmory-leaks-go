package main

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"sync"
)

// Connection simulates a closeable resource (database connection, file handle, etc.)
type Connection struct {
	ID      int
	Address string
	closed  bool
	mu      sync.Mutex
}

func (c *Connection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		fmt.Printf("  WARNING: Connection %d at %s already closed!\n", c.ID, c.Address)
		return nil
	}
	c.closed = true
	fmt.Printf("  Closing connection %d at %s\n", c.ID, c.Address)
	return nil
}

func main() {
	// Start pprof server for analysis
	go func() {
		http.ListenAndServe("localhost:6060", nil)
	}()

	fmt.Println("=== Closure Variable Capture Bug Demo ===\n")
	fmt.Println("Creating 5 connections with closure capture bug...")
	fmt.Println()

	demonstrateClosureBug()

	fmt.Println("\n=== Analysis ===")
	fmt.Println("BUG: All defers captured the same variable 'connPtr' by reference.")
	fmt.Println("When defers execute, 'connPtr' holds its final value (connection 4).")
	fmt.Println("Result: Connection 4 closed 5 times, connections 0-3 never closed!")
}

// demonstrateClosureBug shows the incorrect closure capture pattern
func demonstrateClosureBug() {
	connections := make([]*Connection, 5)

	fmt.Println("--- Opening connections ---")
	for i := 0; i < 5; i++ {
		connections[i] = &Connection{
			ID:      i,
			Address: fmt.Sprintf("0x%x", 0xc000010200+i*8),
		}
		fmt.Printf("Connection %d: opened (address: %s)\n", i, connections[i].Address)
	}

	fmt.Println("\n--- Setting up defers with closure bug ---")

	// BUG: All closures capture the same 'connPtr' variable by reference
	// When the defers execute, they all see the FINAL value of 'connPtr'
	// This pattern demonstrates the bug even in Go 1.22+
	var connPtr *Connection
	for i := 0; i < len(connections); i++ {
		connPtr = connections[i] // Update the shared pointer
		defer func() {
			// BUG: 'connPtr' is captured by reference, not by value!
			// All 5 closures will close the same connection (the last one)
			fmt.Printf("  Defer executing: attempting to close connection %d at %s\n",
				connPtr.ID, connPtr.Address)
			connPtr.Close()
		}()
	}

	fmt.Println("Defers registered. Variable 'connPtr' now points to last connection.")
	fmt.Println()
	fmt.Println("--- Function returning, executing defers (LIFO order) ---")

	// When this function returns, all 5 defers execute
	// But they all captured the same 'connPtr' variable which now points to connections[4]
}
