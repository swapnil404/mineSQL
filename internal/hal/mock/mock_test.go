package mock

import (
	"context"
	"testing"

	"github.com/swapnil404/minesql/internal/hal"
)

func TestStorageReadWrite(t *testing.T) {
	ctx := context.Background()
	s := NewStorage()

	data, err := s.ReadBlock(ctx, 1, 2, 3)
	if err != nil {
		t.Fatalf("read empty: %v", err)
	}
	if data != nil {
		t.Errorf("expected nil for empty block, got %v", data)
	}

	if err := s.WriteBlock(ctx, 1, 2, 3, []byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, err = s.ReadBlock(ctx, 1, 2, 3)
	if err != nil {
		t.Fatalf("read after write: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("expected 'hello', got %q", string(data))
	}
}

func TestStorageDelete(t *testing.T) {
	ctx := context.Background()
	s := NewStorage()

	s.WriteBlock(ctx, 10, 20, 30, []byte("test"))
	if err := s.WriteBlock(ctx, 10, 20, 30, nil); err != nil {
		t.Fatalf("delete write: %v", err)
	}

	data, _ := s.ReadBlock(ctx, 10, 20, 30)
	if data != nil {
		t.Errorf("expected nil after delete, got %v", data)
	}
}

func TestStorageBatchRead(t *testing.T) {
	ctx := context.Background()
	s := NewStorage()

	positions := []hal.BlockPos{
		{X: 0, Y: 0, Z: 0},
		{X: 1, Y: 1, Z: 1},
		{X: 2, Y: 2, Z: 2},
	}

	s.WriteBlock(ctx, 0, 0, 0, []byte("a"))
	s.WriteBlock(ctx, 2, 2, 2, []byte("c"))

	results, err := s.BatchRead(ctx, positions)
	if err != nil {
		t.Fatalf("batch read: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if string(results[0]) != "a" {
		t.Errorf("pos 0: expected 'a', got %q", string(results[0]))
	}
	if results[1] != nil {
		t.Errorf("pos 1: expected nil, got %v", results[1])
	}
	if string(results[2]) != "c" {
		t.Errorf("pos 2: expected 'c', got %q", string(results[2]))
	}
}

func TestStorageForceLoadChunk(t *testing.T) {
	ctx := context.Background()
	s := NewStorage()

	if err := s.ForceLoadChunk(ctx, 0, 0); err != nil {
		t.Errorf("force load should never fail: %v", err)
	}
}

func TestStorageImplementsInterface(t *testing.T) {
	var _ hal.Storage = NewStorage()
}
