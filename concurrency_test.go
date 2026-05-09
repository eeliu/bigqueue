package bigqueue

import (
	"fmt"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"
)

func TestConcurrency_MapAccess(t *testing.T) {
	testDir := "test_concurrency_map"
	os.MkdirAll(testDir, 0755)
	defer os.RemoveAll(testDir)

	// Set memory limit and GC threshold to increase frequency of gc() and ensureEnoughMem triggers
	bq, err := NewMmapQueue(testDir, SetArenaSize(4096), SetMaxInMemArenas(3), SetMaxArenasToKeep(2))
	if err != nil {
		t.Fatalf("failed to create bigqueue: %v", err)
	}
	defer bq.Close()

	var wg sync.WaitGroup
	numOps := 200

	// Start multiple goroutines for concurrent Enqueue, Dequeue and GC
	wg.Add(3)

	// Writer
	go func() {
		defer wg.Done()
		for i := 0; i < numOps; i++ {
			msg := []byte(fmt.Sprintf("message-%d", i))
			if err := bq.Enqueue(msg); err != nil {
				t.Errorf("enqueue failed: %v", err)
			}
			time.Sleep(time.Duration(rand.Intn(5)) * time.Millisecond)
		}
	}()

	// Reader
	go func() {
		defer wg.Done()
		for i := 0; i < numOps; i++ {
			_, err := bq.Dequeue()
			if err != nil && err != ErrEmptyQueue {
				t.Errorf("dequeue failed: %v", err)
			}
			time.Sleep(time.Duration(rand.Intn(5)) * time.Millisecond)
		}
	}()

	// GC Trigger
	go func() {
		defer wg.Done()
		for i := 0; i < numOps/10; i++ {
			bq.GC()
			time.Sleep(10 * time.Millisecond)
		}
	}()

	wg.Wait()
}
