package bigqueue

import (
	"bytes"
	"testing"
)

// TestDefaultMaxMessageSize verifies that messages larger than the default
// 1 MiB limit are rejected with ErrMessageTooLarge.
func TestDefaultMaxMessageSize(t *testing.T) {
	dir := t.TempDir()
	bq, err := NewMmapQueue(dir, SetArenaSize(4096))
	if err != nil {
		t.Fatalf("NewMmapQueue: %v", err)
	}
	defer bq.Close()

	// A message just over the default 1 MiB limit.
	msg := bytes.Repeat([]byte("a"), cDefaultMaxMessageSize+1)
	if err := bq.Enqueue(msg); err != ErrMessageTooLarge {
		t.Fatalf("Enqueue oversized message: got err=%v, want ErrMessageTooLarge", err)
	}

	// A message exactly at the limit is allowed.
	msg = bytes.Repeat([]byte("a"), cDefaultMaxMessageSize)
	if err := bq.Enqueue(msg); err != nil {
		t.Fatalf("Enqueue message at limit: %v", err)
	}
}

// TestSetMaxMessageSize verifies the configurable limit is enforced.
func TestSetMaxMessageSize(t *testing.T) {
	dir := t.TempDir()
	bq, err := NewMmapQueue(dir, SetArenaSize(4096), SetMaxMessageSize(10))
	if err != nil {
		t.Fatalf("NewMmapQueue: %v", err)
	}
	defer bq.Close()

	if err := bq.Enqueue([]byte("1234567890")); err != nil { // exactly 10 bytes
		t.Fatalf("Enqueue 10-byte message: %v", err)
	}
	if err := bq.Enqueue([]byte("12345678901")); err != ErrMessageTooLarge { // 11 bytes
		t.Fatalf("Enqueue 11-byte message: got err=%v, want ErrMessageTooLarge", err)
	}
}

// TestSetMaxMessageSizeStringAndTag verifies the limit applies to all enqueue
// variants.
func TestSetMaxMessageSizeStringAndTag(t *testing.T) {
	dir := t.TempDir()
	bq, err := NewMmapQueue(dir, SetArenaSize(4096), SetMaxMessageSize(5))
	if err != nil {
		t.Fatalf("NewMmapQueue: %v", err)
	}
	defer bq.Close()

	if err := bq.EnqueueString("hello"); err != nil { // exactly 5 bytes
		t.Fatalf("EnqueueString 5-byte message: %v", err)
	}
	if err := bq.EnqueueString("hello!"); err != ErrMessageTooLarge { // 6 bytes
		t.Fatalf("EnqueueString 6-byte message: got err=%v, want ErrMessageTooLarge", err)
	}

	if err := bq.EnqueueWithTag([]byte("hello"), []byte("t")); err != nil { // 5 bytes payload
		t.Fatalf("EnqueueWithTag 5-byte message: %v", err)
	}
	if err := bq.EnqueueWithTag([]byte("hello!"), []byte("t")); err != ErrMessageTooLarge { // 6 bytes
		t.Fatalf("EnqueueWithTag 6-byte message: got err=%v, want ErrMessageTooLarge", err)
	}
}

// TestSetMaxMessageSizeInvalid verifies that invalid values are rejected.
func TestSetMaxMessageSizeInvalid(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewMmapQueue(dir, SetMaxMessageSize(0)); err != ErrInvalidMaxMessageSize {
		t.Fatalf("SetMaxMessageSize(0): got err=%v, want ErrInvalidMaxMessageSize", err)
	}
	if _, err := NewMmapQueue(dir, SetMaxMessageSize(-1)); err != ErrInvalidMaxMessageSize {
		t.Fatalf("SetMaxMessageSize(-1): got err=%v, want ErrInvalidMaxMessageSize", err)
	}
}
