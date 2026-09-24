package bigqueue

import (
	"testing"
)

// TestCloseIsIdempotent verifies that calling Close multiple times does not
// panic and returns a consistent result.
func TestCloseIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	bq, err := NewMmapQueue(dir, SetArenaSize(4096))
	if err != nil {
		t.Fatalf("NewMmapQueue: %v", err)
	}

	if err := bq.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	// Second (and third) close must be safe and return the same result.
	if err := bq.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if err := bq.Close(); err != nil {
		t.Fatalf("third Close: %v", err)
	}
}

// TestGetConsumersReturnsDefensiveCopy verifies that mutating the map returned
// by getConsumers does not corrupt the internal consumer index.
func TestGetConsumersReturnsDefensiveCopy(t *testing.T) {
	dir := t.TempDir()
	md, err := newMetadata(dir, 4096)
	if err != nil {
		t.Fatalf("newMetadata: %v", err)
	}
	defer md.close()

	base, err := md.getConsumer("c1")
	if err != nil {
		t.Fatalf("getConsumer: %v", err)
	}

	consumers := md.getConsumers()
	// Mutating the returned map must not affect the internal index.
	consumers["c1"] = 99999
	delete(consumers, "c1")

	if md.co["c1"] != base {
		t.Errorf("internal consumer index was mutated: co[c1]=%d, want %d", md.co["c1"], base)
	}
}

// TestDequeueRejectsCorruptLength verifies that a corrupt length prefix is
// rejected with ErrInvalidMessageLength instead of causing an unbounded
// allocation (OOM) or returning garbage data.
func TestDequeueRejectsCorruptLength(t *testing.T) {
	dir := t.TempDir()
	bq, err := NewMmapQueue(dir, SetArenaSize(4096))
	if err != nil {
		t.Fatalf("NewMmapQueue: %v", err)
	}
	defer bq.Close()

	if err := bq.Enqueue([]byte("hello")); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Corrupt the length prefix to a value far larger than the message and
	// larger than the safety ceiling.
	headAid, headOff := bq.md.getConsumerHead(bq.dc)
	aa, err := bq.am.loadOrGetArena(headAid)
	if err != nil {
		t.Fatalf("loadOrGetArena: %v", err)
	}
	aa.WriteUint64At(uint64(1<<40), int64(headOff)) // ~1 TiB

	_, err = bq.Dequeue()
	if err != ErrInvalidMessageLength {
		t.Fatalf("Dequeue with corrupt length: got err=%v, want ErrInvalidMessageLength", err)
	}
}
