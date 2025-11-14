package main

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"time"
)

// This demonstrates the slice reslicing memory trap where small slices
// keep large underlying arrays alive, preventing garbage collection.

type FileHeader struct {
	Name   string
	Header []byte // Only 1 KB needed
}

var (
	headers []FileHeader
)

func main() {
	go func() {
		fmt.Println("pprof server: http://localhost:6060")
		http.ListenAndServe("localhost:6060", nil)
	}()

	time.Sleep(100 * time.Millisecond)

	fmt.Println("Processing 100 files (10 MB each)...")

	// Process 100 files, keeping only headers
	for i := 0; i < 100; i++ {
		header := processFileBadly(i)
		headers = append(headers, header)
	}

	// Force GC
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	fmt.Printf("\n[AFTER Processing] Heap Alloc: %d MB\n", m.Alloc/1024/1024)
	fmt.Printf("Kept only headers (1 KB each × 100 = 0.1 MB expected)\n")
	fmt.Printf("But full arrays still in memory! (~1000 MB leaked)\n")
	fmt.Println("\nPress Ctrl+C to stop")

	// Keep running for profiling
	select {}
}

func processFileBadly(fileNum int) FileHeader {
	// Simulate reading 10 MB file
	fileData := make([]byte, 10*1024*1024) // 10 MB

	// Fill with data to prevent optimization
	for i := range fileData {
		fileData[i] = byte(i % 256)
	}

	// Extract header (first 1 KB)
	// BUG: This creates a slice that references the entire 10 MB array!
	header := fileData[:1024]

	return FileHeader{
		Name:   fmt.Sprintf("file_%d.dat", fileNum),
		Header: header, // Keeps entire 10 MB array alive
	}
}
