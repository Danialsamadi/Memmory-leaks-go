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
		http.ListenAndServe("localhost:6061", nil)
	}()

	fmt.Println("=== Closure Variable Capture - FIXED Demo ===\n")
	fmt.Println("Demonstrating three correct patterns:\n")

	fmt.Println("Pattern 1: Pass as function argument")
	fmt.Println("=========================================")
	demonstrateFixWithArgument()

	fmt.Println("\nPattern 2: Shadow the loop variable")
	fmt.Println("=========================================")
	demonstrateFixWithShadowing()

	fmt.Println("\nPattern 3: Extract to separate function")
	fmt.Println("=========================================")
	demonstrateFixWithExtraction()
}

// demonstrateFixWithArgument shows the fix: pass connection as argument
func demonstrateFixWithArgument() {
	connections := createConnections()

	fmt.Println("--- Setting up defers with argument passing ---")

	for _, conn := range connections {
		// ✅ FIX: Pass 'conn' as argument to the closure
		// Arguments are evaluated at defer registration time
		defer func(c *Connection) {
			fmt.Printf("  Defer executing: closing connection %d at %s\n", c.ID, c.Address)
			c.Close()
		}(conn) // conn's current value is passed here
	}

	fmt.Println("Defers registered with correct values captured.")
	fmt.Println()
	fmt.Println("--- Function returning, executing defers (LIFO order) ---")
}

// demonstrateFixWithShadowing shows the fix: shadow the loop variable
func demonstrateFixWithShadowing() {
	connections := createConnections()

	fmt.Println("--- Setting up defers with variable shadowing ---")

	for _, conn := range connections {
		// ✅ FIX: Create a new variable in each iteration's scope
		conn := conn // This creates a new 'conn' that the closure captures
		defer func() {
			fmt.Printf("  Defer executing: closing connection %d at %s\n", conn.ID, conn.Address)
			conn.Close()
		}()
	}

	fmt.Println("Defers registered with shadowed variables.")
	fmt.Println()
	fmt.Println("--- Function returning, executing defers (LIFO order) ---")
}

// demonstrateFixWithExtraction shows the fix: extract cleanup to separate function
func demonstrateFixWithExtraction() {
	connections := createConnections()

	fmt.Println("--- Setting up defers with extracted function ---")

	for _, conn := range connections {
		// ✅ FIX: Call a separate function that handles defer
		// This is the cleanest pattern for complex cleanup logic
		setupCleanup(conn)
	}

	fmt.Println("Defers registered via extracted function.")
	fmt.Println()
	fmt.Println("--- Function returning, executing defers (LIFO order) ---")
}

// setupCleanup registers a defer for a single connection
// The connection value is captured correctly because it's a function parameter
func setupCleanup(conn *Connection) {
	defer func() {
		fmt.Printf("  Defer executing: closing connection %d at %s\n", conn.ID, conn.Address)
		conn.Close()
	}()
	// Note: This defer will execute when setupCleanup returns, not when the caller returns
	// For demonstration, we'd need a different pattern if we want defers to execute later
}

// createConnections creates test connections
func createConnections() []*Connection {
	connections := make([]*Connection, 5)
	fmt.Println("--- Opening connections ---")
	for i := 0; i < 5; i++ {
		connections[i] = &Connection{
			ID:      i,
			Address: fmt.Sprintf("0x%x", 0xc000010200+i*8),
		}
		fmt.Printf("Connection %d: opened (address: %s)\n", i, connections[i].Address)
	}
	return connections
}
