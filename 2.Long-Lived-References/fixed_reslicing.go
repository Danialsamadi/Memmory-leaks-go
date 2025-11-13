package main

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"time"
)

// This demonstrates the proper way to handle slice reslicing by copying
// data when you want to release the underlying array.

type FileHeader struct {
	Name   string
	Header []byte
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
		header := processFileCorrectly(i)
		headers = append(headers, header)
	}

	// Force GC
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	
	fmt.Printf("\n[AFTER Processing] Heap Alloc: %d MB\n", m.Alloc/1024/1024)
	fmt.Printf("Kept only headers (1 KB each × 100 = 0.1 MB)\n")
	fmt.Printf("Headers properly copied, arrays freed by GC\n")
	fmt.Println("\nPress Ctrl+C to stop")

	select {}
}

func processFileCorrectly(fileNum int) FileHeader {
	// Simulate reading 10 MB file
	fileData := make([]byte, 10*1024*1024) // 10 MB
	
	// Fill with data
	for i := range fileData {
		fileData[i] = byte(i % 256)
	}

	// Extract and COPY header to new slice
	// This allows fileData to be garbage collected
	header := make([]byte, 1024)
	copy(header, fileData[:1024])

	// fileData can now be GC'd because no references remain

	return FileHeader{
		Name:   fmt.Sprintf("file_%d.dat", fileNum),
		Header: header, // Only 1 KB, independent of fileData
	}
}

