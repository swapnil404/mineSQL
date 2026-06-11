package hal

import "context"

const (
	BlockTypeBarrel  byte = 0x00
	BlockTypeBanner  byte = 0x01
	BlockTypeSign    byte = 0x02
	BlockTypeLectern byte = 0x03
)

type BlockPos struct {
	X, Y, Z int
}

type BlockWrite struct {
	Pos       BlockPos
	BlockType byte
	Data      []byte
}

type Storage interface {
	ReadBlock(ctx context.Context, x, y, z int) ([]byte, error)
	WriteBlock(ctx context.Context, x, y, z int, blockType byte, data []byte) error
	BatchRead(ctx context.Context, positions []BlockPos) ([][]byte, error)
	BatchWrite(ctx context.Context, writes []BlockWrite) error
	ForceLoadChunk(ctx context.Context, chunkX, chunkZ int) error
	IsChunkLoaded(ctx context.Context, chunkX, chunkZ int) (bool, error)
}
