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

	// 设置内存限制和 GC 阈值以增加 gc() 和 ensureEnoughMem 触发频率
	bq, err := NewMmapQueue(testDir, SetArenaSize(4096), SetMaxInMemArenas(3), SetMaxArenasToKeep(2))
	if err != nil {
		t.Fatalf("failed to create bigqueue: %v", err)
	}
	defer bq.Close()

	var wg sync.WaitGroup
	numOps := 200

	// 启动多个协程同时进行 Enqueue, Dequeue 和 GC
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
