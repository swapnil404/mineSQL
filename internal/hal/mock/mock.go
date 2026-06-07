package mock

import (
	"context"
	"fmt"
	"sync"

	"github.com/swapnil404/minesql/internal/hal"
)

type Storage struct {
	mu    sync.RWMutex
	store map[string][]byte
}

func NewStorage() *Storage {
	return &Storage{
		store: make(map[string][]byte),
	}
}

func (s *Storage) ReadBlock(ctx context.Context, x, y, z int) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := posKey(x, y, z)
	data, ok := s.store[key]
	if !ok {
		return nil, nil
	}
	result := make([]byte, len(data))
	copy(result, data)
	return result, nil
}

func (s *Storage) WriteBlock(ctx context.Context, x, y, z int, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := posKey(x, y, z)
	if data == nil || len(data) == 0 {
		delete(s.store, key)
		return nil
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	s.store[key] = cp
	return nil
}

func (s *Storage) BatchRead(ctx context.Context, positions []hal.BlockPos) ([][]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([][]byte, len(positions))
	for i, p := range positions {
		key := posKey(p.X, p.Y, p.Z)
		data, ok := s.store[key]
		if ok {
			cp := make([]byte, len(data))
			copy(cp, data)
			results[i] = cp
		}
	}
	return results, nil
}

func (s *Storage) IsChunkLoaded(ctx context.Context, chunkX, chunkZ int) (bool, error) {
	return true, nil
}

func (s *Storage) BatchWrite(ctx context.Context, writes []hal.BlockWrite) error {
	for _, w := range writes {
		if err := s.WriteBlock(ctx, w.Pos.X, w.Pos.Y, w.Pos.Z, w.Data); err != nil {
			return err
		}
	}
	return nil
}

func (s *Storage) ForceLoadChunk(ctx context.Context, chunkX, chunkZ int) error {
	return nil
}

func posKey(x, y, z int) string {
	return fmt.Sprintf("%d,%d,%d", x, y, z)
}
