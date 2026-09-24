package bigqueue

import (
	"errors"
	"os"
	"path"
	"strconv"
	"testing"

	"github.com/grandecola/mmap"
)

type mockMetadata struct {
	headAid, headOff int
	tailAid, tailOff int
	consumers        map[string]int64
	consumerHeads    map[int64][2]int
	flushErr         error
	flushCount       int
}

func (m *mockMetadata) getTail() (int, int)  { return m.tailAid, m.tailOff }
func (m *mockMetadata) getHead() (int, int)  { return m.headAid, m.headOff }
func (m *mockMetadata) putHead(aid, off int) { m.headAid, m.headOff = aid, off }
func (m *mockMetadata) flush() error {
	m.flushCount++
	return m.flushErr
}
func (m *mockMetadata) getConsumerHead(addr int64) (int, int) {
	h := m.consumerHeads[addr]
	return h[0], h[1]
}
func (m *mockMetadata) getConsumers() map[string]int64 {
	consumers := make(map[string]int64, len(m.consumers))
	for name, base := range m.consumers {
		consumers[name] = base
	}
	return consumers
}

func TestArenaManagerGC_FlushError(t *testing.T) {
	testDir := t.TempDir()
	conf := newConfig()
	conf.maxArenasToKeep = 1
	conf.arenaSize = 4096

	// Mock metadata that fails to flush
	mockMD := &mockMetadata{
		consumers: map[string]int64{
			"c1": 1,
		},
		consumerHeads: map[int64][2]int{
			1: {3, 100},
		},
		flushErr: errors.New("flush failed"),
	}

	am := &arenaManager{
		dir:    testDir,
		conf:   conf,
		md:     mockMD,
		arenas: make(map[int]*mmap.File),
	}

	// Create arena files
	for i := 0; i < 5; i++ {
		path := am.getArenaPath(i)
		if err := os.WriteFile(path, make([]byte, conf.arenaSize), 0644); err != nil {
			t.Fatalf("failed to create arena file %d: %v", i, err)
		}
	}

	// GC should stop for this round if flush fails.
	am.gc()

	// Files should remain untouched because flush failed.
	for i := 0; i < 5; i++ {
		path := am.getArenaPath(i)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("arena file %d should still exist when flush fails", i)
		}
	}

	// Global head should still be updated in memory
	if mockMD.headAid != 3 || mockMD.headOff != 100 {
		t.Errorf("global head should be updated in memory to (3, 100), got (%d, %d)", mockMD.headAid, mockMD.headOff)
	}
}

func TestArenaManagerGC(t *testing.T) {
	testDir := t.TempDir()
	conf := newConfig()
	conf.maxArenasToKeep = 1
	conf.arenaSize = 4096

	md, err := newMetadata(testDir, conf.arenaSize)
	if err != nil {
		t.Fatalf("failed to create metadata: %v", err)
	}
	defer md.close()

	am, err := newArenaManager(testDir, conf, md)
	if err != nil {
		t.Fatalf("failed to create arena manager: %v", err)
	}
	defer am.close()

	// Create some arena files
	for i := 0; i < 5; i++ {
		path := am.getArenaPath(i)
		if err := os.WriteFile(path, make([]byte, conf.arenaSize), 0644); err != nil {
			t.Fatalf("failed to create arena file %d: %v", i, err)
		}
	}

	// Add a consumer and set its head to arena 3
	c1Base, err := md.getConsumer("c1")
	if err != nil {
		t.Fatalf("failed to add consumer c1: %v", err)
	}
	md.putConsumerHead(c1Base, 3, 100)

	// Call GC
	am.gc()

	// Check if arenas 0 and 1 are deleted (3 - 1 = 2 is the limitAid, so < 2 should be deleted)
	for i := 0; i < 2; i++ {
		path := am.getArenaPath(i)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("arena file %d should be deleted", i)
		}
	}

	// Arenas 2, 3, 4 should still exist
	for i := 2; i <= 4; i++ {
		path := am.getArenaPath(i)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("arena file %d should exist", i)
		}
	}

	// Check global head update
	headAid, headOff := md.getHead()
	if headAid != 3 || headOff != 100 {
		t.Errorf("global head should be (3, 100), got (%d, %d)", headAid, headOff)
	}
}

func TestArenaManagerGC_MultipleConsumers(t *testing.T) {
	testDir := t.TempDir()
	conf := newConfig()
	conf.maxArenasToKeep = 1
	conf.arenaSize = 4096

	md, err := newMetadata(testDir, conf.arenaSize)
	if err != nil {
		t.Fatalf("failed to create metadata: %v", err)
	}
	defer md.close()

	am, err := newArenaManager(testDir, conf, md)
	if err != nil {
		t.Fatalf("failed to create arena manager: %v", err)
	}
	defer am.close()

	// Create some arena files
	for i := 0; i < 5; i++ {
		path := am.getArenaPath(i)
		if err := os.WriteFile(path, make([]byte, conf.arenaSize), 0644); err != nil {
			t.Fatalf("failed to create arena file %d: %v", i, err)
		}
	}

	// Add consumers
	c1Base, _ := md.getConsumer("c1")
	md.putConsumerHead(c1Base, 3, 100)
	c2Base, _ := md.getConsumer("c2")
	md.putConsumerHead(c2Base, 2, 50)

	// minHeadAid should be 2
	// limitAid = 2 - 1 = 1.
	// deletes only 0.

	am.gc()

	if _, err := os.Stat(am.getArenaPath(0)); !os.IsNotExist(err) {
		t.Errorf("arena file 0 should be deleted")
	}

	for i := 1; i <= 4; i++ {
		path := am.getArenaPath(i)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("arena file %d should exist", i)
		}
	}

	headAid, headOff := md.getHead()
	if headAid != 2 || headOff != 50 {
		t.Errorf("global head should be (2, 50), got (%d, %d)", headAid, headOff)
	}
}

func TestArenaManagerGC_NoConsumers(t *testing.T) {
	testDir := t.TempDir()
	conf := newConfig()
	conf.maxArenasToKeep = 1
	conf.arenaSize = 4096

	md, err := newMetadata(testDir, conf.arenaSize)
	if err != nil {
		t.Fatalf("failed to create metadata: %v", err)
	}
	defer md.close()

	am, err := newArenaManager(testDir, conf, md)
	if err != nil {
		t.Fatalf("failed to create arena manager: %v", err)
	}
	defer am.close()

	// Create some arena files
	for i := 0; i < 5; i++ {
		path := am.getArenaPath(i)
		_ = os.WriteFile(path, make([]byte, conf.arenaSize), 0644)
	}

	// GC with no consumers should do nothing
	am.gc()

	for i := 0; i < 5; i++ {
		path := am.getArenaPath(i)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("arena file %d should not be deleted when no consumers exist", i)
		}
	}
}

func TestArenaManagerEviction(t *testing.T) {
	testDir := t.TempDir()
	conf := newConfig()
	conf.maxInMemArenas = 3
	conf.arenaSize = 4096

	md, err := newMetadata(testDir, conf.arenaSize)
	if err != nil {
		t.Fatalf("failed to create metadata: %v", err)
	}
	defer md.close()

	// Set head to 0, tail to 4 (simulating 5 arenas)
	md.putHead(0, 0)
	md.putTail(4, 0)

	am, err := newArenaManager(testDir, conf, md) // loads arena 4
	if err != nil {
		t.Fatalf("failed to create arena manager: %v", err)
	}
	defer am.close()

	// Manually load arenas 0 and 1 via loadOrGetArena
	// This ensures ensureEnoughMem is called which handles the limit
	for i := 0; i < 2; i++ {
		if _, err := am.loadOrGetArena(i); err != nil {
			t.Fatalf("failed to load arena %d: %v", i, err)
		}
	}

	if am.inMem != 3 {
		t.Errorf("expected 3 arenas in memory, got %d", am.inMem)
	}

	// loadOrGetArena(2) should trigger eviction of arena 1
	// Arena 0 is head, Arena 4 is tail.
	// Arena 1 is the only candidate for eviction besides 0 and 4.
	_, err = am.loadOrGetArena(2)
	if err != nil {
		t.Fatalf("failed to load or get arena 2: %v", err)
	}

	if am.inMem > 3 {
		t.Errorf("expected at most 3 arenas in memory after eviction, got %d", am.inMem)
	}

	// Verify head and tail arenas are still in memory
	if _, ok := am.arenas[0]; !ok {
		t.Errorf("head arena 0 should still be in memory")
	}
	if _, ok := am.arenas[4]; !ok {
		t.Errorf("tail arena 4 should still be in memory")
	}
	// Arena 2 should be in memory
	if _, ok := am.arenas[2]; !ok {
		t.Errorf("arena 2 should be in memory")
	}
}

func TestArenaManagerGC_IncrementalScan(t *testing.T) {
	testDir := t.TempDir()
	conf := newConfig()
	conf.maxArenasToKeep = 1
	conf.arenaSize = 4096

	mockMD := &mockMetadata{
		consumers: map[string]int64{
			"c1": 1,
		},
		consumerHeads: map[int64][2]int{
			1: {3, 100},
		},
	}

	am := &arenaManager{
		dir:    testDir,
		conf:   conf,
		md:     mockMD,
		arenas: make(map[int]*mmap.File),
	}

	for i := 0; i < 5; i++ {
		path := am.getArenaPath(i)
		if err := os.WriteFile(path, make([]byte, conf.arenaSize), 0644); err != nil {
			t.Fatalf("failed to create arena file %d: %v", i, err)
		}
	}

	// First GC: head moves (0,0) -> (3,100), so one flush; limitAid = 2,
	// arenas 0 and 1 are deleted and the watermark advances to 2.
	am.gc()
	if mockMD.flushCount != 1 {
		t.Fatalf("expected 1 flush after first gc, got %d", mockMD.flushCount)
	}
	if am.lastGCLimitAid != 2 {
		t.Fatalf("expected lastGCLimitAid=2, got %d", am.lastGCLimitAid)
	}
	for i := 0; i < 2; i++ {
		if _, err := os.Stat(am.getArenaPath(i)); !os.IsNotExist(err) {
			t.Errorf("arena file %d should be deleted", i)
		}
	}

	// Second GC with unchanged consumer head: no flush, no scan, watermark
	// unchanged. This is the steady-state O(1) path.
	am.gc()
	if mockMD.flushCount != 1 {
		t.Errorf("expected no additional flush when head does not move, got %d", mockMD.flushCount)
	}
	if am.lastGCLimitAid != 2 {
		t.Errorf("expected lastGCLimitAid to stay 2, got %d", am.lastGCLimitAid)
	}

	// Consumer advances to arena 5: head moves -> one more flush;
	// limitAid = 4, only the incremental range [2,4) is scanned.
	mockMD.consumerHeads[1] = [2]int{5, 0}
	am.gc()
	if mockMD.flushCount != 2 {
		t.Errorf("expected 2 flushes after consumer advanced, got %d", mockMD.flushCount)
	}
	if am.lastGCLimitAid != 4 {
		t.Errorf("expected lastGCLimitAid=4, got %d", am.lastGCLimitAid)
	}
	for i := 0; i < 4; i++ {
		if _, err := os.Stat(am.getArenaPath(i)); !os.IsNotExist(err) {
			t.Errorf("arena file %d should be deleted", i)
		}
	}
	if _, err := os.Stat(am.getArenaPath(4)); os.IsNotExist(err) {
		t.Errorf("arena file 4 should exist")
	}
}

func TestArenaManagerGC_RestartCleansOrphans(t *testing.T) {
	testDir := t.TempDir()
	conf := newConfig()
	conf.maxArenasToKeep = 1
	conf.arenaSize = 4096

	md, err := newMetadata(testDir, conf.arenaSize)
	if err != nil {
		t.Fatalf("failed to create metadata: %v", err)
	}
	defer md.close()

	// Consumer head at arena 3, so limitAid = 2 on the first gc.
	c1Base, err := md.getConsumer("c1")
	if err != nil {
		t.Fatalf("failed to add consumer c1: %v", err)
	}
	md.putConsumerHead(c1Base, 3, 100)

	// Simulate orphan files left behind by failed deletions in a previous
	// run (arena files below limitAid that still exist on disk).
	for i := 0; i < 2; i++ {
		path := path.Join(testDir, strconv.Itoa(i)+cArenaFileSuffix)
		if err := os.WriteFile(path, make([]byte, conf.arenaSize), 0644); err != nil {
			t.Fatalf("failed to create orphan arena file %d: %v", i, err)
		}
	}

	// newArenaManager runs gc once with a zero watermark, which performs a
	// full scan from aid 0 and cleans up the orphans.
	am, err := newArenaManager(testDir, conf, md)
	if err != nil {
		t.Fatalf("failed to create arena manager: %v", err)
	}
	defer am.close()

	for i := 0; i < 2; i++ {
		if _, err := os.Stat(am.getArenaPath(i)); !os.IsNotExist(err) {
			t.Errorf("orphan arena file %d should be deleted on startup gc", i)
		}
	}
	if am.lastGCLimitAid != 2 {
		t.Errorf("expected lastGCLimitAid=2 after startup gc, got %d", am.lastGCLimitAid)
	}
}

func TestLoadOrGetArena_GCOnlyOnTailExpansion(t *testing.T) {
	testDir := t.TempDir()
	conf := newConfig()
	conf.maxArenasToKeep = 1
	conf.arenaSize = 4096

	md, err := newMetadata(testDir, conf.arenaSize)
	if err != nil {
		t.Fatalf("failed to create metadata: %v", err)
	}
	defer md.close()

	// tail at arena 2; consumer head at arena 2 so limitAid = 1.
	md.putTail(2, 0)
	c1Base, err := md.getConsumer("c1")
	if err != nil {
		t.Fatalf("failed to add consumer c1: %v", err)
	}
	md.putConsumerHead(c1Base, 2, 0)

	am, err := newArenaManager(testDir, conf, md)
	if err != nil {
		t.Fatalf("failed to create arena manager: %v", err)
	}
	defer am.close()

	// startup gc already ran and advanced the watermark.
	startupWatermark := am.lastGCLimitAid

	// Loading an existing arena (read path, aid <= tail) must not run gc.
	if _, err := am.loadOrGetArena(1); err != nil {
		t.Fatalf("failed to load arena 1: %v", err)
	}
	if am.lastGCLimitAid != startupWatermark {
		t.Errorf("read-path load must not trigger gc; watermark changed from %d to %d",
			startupWatermark, am.lastGCLimitAid)
	}

	// Loading a new arena beyond the tail (write path) triggers gc.
	if _, err := am.loadOrGetArena(3); err != nil {
		t.Fatalf("failed to load arena 3: %v", err)
	}
	// consumer head is still at arena 2, limitAid = 1 <= watermark, so gc
	// ran but had nothing new to delete; the important part is it did not
	// panic and the queue is still functional.
	if _, ok := am.arenas[3]; !ok {
		t.Errorf("arena 3 should be loaded in memory")
	}
}
