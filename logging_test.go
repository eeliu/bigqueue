package bigqueue

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
)

// slogAdapter wraps slog.Logger to implement Logger interface for testing.
type slogAdapter struct {
	*slog.Logger
}

func (s *slogAdapter) Debugf(format string, args ...any) {
	s.Logger.Debug(fmt.Sprintf(format, args...))
}

func (s *slogAdapter) Infof(format string, args ...any) {
	s.Logger.Info(fmt.Sprintf(format, args...))
}

func (s *slogAdapter) Warnf(format string, args ...any) {
	s.Logger.Warn(fmt.Sprintf(format, args...))
}

func (s *slogAdapter) Errorf(format string, args ...any) {
	s.Logger.Error(fmt.Sprintf(format, args...))
}

func TestLoggingIntegration(t *testing.T) {
	testDir := t.TempDir()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Test case: Create queue and verify initialization logs
	bq, err := NewMmapQueue(testDir, SetLogger(&slogAdapter{logger}), SetArenaSize(4096))
	if err != nil {
		t.Fatalf("failed to create mmap queue: %v", err)
	}
	defer bq.Close()

	output := buf.String()
	if !strings.Contains(strings.ToLower(output), "initializ") {
		t.Errorf("expected initialization log not found in: %s", output)
	}

	// Trigger activity
	_, _ = bq.NewConsumer("test-consumer")
	_ = bq.Enqueue([]byte("test data"))
	_ = bq.Flush()

	if buf.Len() == 0 {
		t.Errorf("expected logs, but output buffer is empty")
	}
}

func TestLoggingGC(t *testing.T) {
	testDir := os.TempDir() + "/bq-log-gc-test"
	os.RemoveAll(testDir)
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}
	defer os.RemoveAll(testDir)

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	arenaSize := 4096
	bq, err := NewMmapQueue(testDir, SetLogger(&slogAdapter{logger}), SetArenaSize(arenaSize), SetMaxArenasToKeep(1))
	if err != nil {
		t.Fatalf("failed to create mmap queue: %v", err)
	}

	for i := 0; i < 5; i++ {
		_ = bq.Enqueue(make([]byte, arenaSize))
	}

	consumer, _ := bq.NewConsumer("c1")
	for i := 0; i < 5; i++ {
		_, _ = consumer.Dequeue()
	}

	buf.Reset()
	bq.GC()

	if buf.Len() == 0 {
		t.Log("Note: GC triggered, check if logs are produced if maxArenasToKeep > 0")
	}

	bq.Close()
}

type mockHandler struct {
	lastLevel slog.Level
}

func (h *mockHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *mockHandler) Handle(_ context.Context, r slog.Record) error {
	h.lastLevel = r.Level
	return nil
}
func (h *mockHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *mockHandler) WithGroup(_ string) slog.Handler      { return h }

func TestConfigLoggingMethods(t *testing.T) {
	handler := &mockHandler{}
	logger := slog.New(handler)
	conf := newConfig()
	opt := SetLogger(&slogAdapter{logger})
	opt(conf)

	conf.Debugf("debug msg")
	if handler.lastLevel != slog.LevelDebug {
		t.Errorf("expected Debug level, got %v", handler.lastLevel)
	}

	conf.Infof("info msg")
	if handler.lastLevel != slog.LevelInfo {
		t.Errorf("expected Info level, got %v", handler.lastLevel)
	}

	conf.Warnf("warn msg")
	if handler.lastLevel != slog.LevelWarn {
		t.Errorf("expected Warn level, got %v", handler.lastLevel)
	}

	conf.Errorf("error msg")
	if handler.lastLevel != slog.LevelError {
		t.Errorf("expected Error level, got %v", handler.lastLevel)
	}

	conf.logger = nil
	conf.Debugf("no panic")
}
