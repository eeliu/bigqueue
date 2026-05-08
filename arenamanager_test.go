package bigqueue

import (
	"testing"
)

func TestSetArenaPath(t *testing.T) {
	am := &arenaManager{
		dir: "/tmp/testqueue",
	}

	testCases := []struct {
		aid      int
		expected string
	}{
		{0, "/tmp/testqueue/0_arena.dat"},
		{1, "/tmp/testqueue/1_arena.dat"},
		{123, "/tmp/testqueue/123_arena.dat"},
	}

	for _, tc := range testCases {
		pathResult := am.setArenaPath(tc.aid)
		if pathResult != tc.expected {
			t.Errorf("expected %s, got %s", tc.expected, pathResult)
		}
	}

	// Test with a different directory
	am.dir = "relative/path"
	expected := "relative/path/10_arena.dat"
	if res := am.setArenaPath(10); res != expected {
		t.Errorf("expected %s, got %s", expected, res)
	}
}

func TestSetArenaPath_Reuse(t *testing.T) {
	am := &arenaManager{
		dir: "/tmp",
	}

	// First call
	p1 := am.setArenaPath(1)

	// Second call with larger ID to ensure slice growth/reuse doesn't corrupt previous logic
	p2 := am.setArenaPath(1000000)

	expected1 := "/tmp/1_arena.dat"
	expected2 := "/tmp/1000000_arena.dat"

	if p1 == p2 {
		t.Log("Note: string() conversion creates a copy, so p1 and p2 should have different underlying memory in Go even if am.fullPath is reused.")
	}

	if p1 != expected1 {
		t.Errorf("first path corrupted: got %s", p1)
	}
	if p2 != expected2 {
		t.Errorf("second path incorrect: got %s", p2)
	}
}
