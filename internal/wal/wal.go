package wal

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	"github.com/swapnil404/minesql/internal/hal"
)

const (
	walY          = 64
	bytesPerBanner = 6
	xormask       byte = 0x55
	pageSep       = "\n---\n"
)

type Entry struct {
	LSN      int64
	TXID     int64
	Status   string
	Op       string
	TableID  int
	TargetX  int
	TargetY  int
	TargetZ  int
	NewValue string
}

type WAL struct {
	hal     hal.Storage
	mu      sync.Mutex
	nextLSN int64
}

func NewWAL(h hal.Storage) *WAL {
	return &WAL{hal: h, nextLSN: 1}
}

func (w *WAL) NextLSN() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.nextLSN
}

func (w *WAL) Append(ctx context.Context, txid int64, op string, tableID int, x, y, z int, newValue string) (int64, error) {
	w.mu.Lock()
	lsn := w.nextLSN
	w.nextLSN++
	w.mu.Unlock()

	book := serializeEntry(lsn, txid, "PENDING", op, tableID, x, y, z, newValue)
	walZ := -int(lsn)

	chunkX := 0
	chunkZ := walZ / 16
	if err := w.hal.ForceLoadChunk(ctx, chunkX, chunkZ); err != nil {
		return 0, fmt.Errorf("wal: append LSN %d: forceload chunk: %w", lsn, err)
	}

	if err := w.hal.WriteBlock(ctx, 0, walY, walZ, hal.BlockTypeLectern, []byte(book)); err != nil {
		return 0, fmt.Errorf("wal: append LSN %d: %w", lsn, err)
	}

	return lsn, nil
}

func (w *WAL) Commit(ctx context.Context, lsn int64) error {
	walZ := -int(lsn)

	data, err := w.hal.ReadBlock(ctx, 0, walY, walZ)
	if err != nil {
		return fmt.Errorf("wal: commit LSN %d: read: %w", lsn, err)
	}
	if len(data) == 0 {
		return fmt.Errorf("wal: commit LSN %d: lectern not found at Z=%d", lsn, walZ)
	}

	entry, err := deserializeEntry(string(data))
	if err != nil {
		return fmt.Errorf("wal: commit LSN %d: parse: %w", lsn, err)
	}

	if entry.Status != "PENDING" {
		return fmt.Errorf("wal: commit LSN %d: entry already %s", lsn, entry.Status)
	}

	book := serializeEntry(entry.LSN, entry.TXID, "COMMITTED", entry.Op, entry.TableID, entry.TargetX, entry.TargetY, entry.TargetZ, entry.NewValue)

	if err := w.hal.WriteBlock(ctx, 0, walY, walZ, hal.BlockTypeLectern, []byte(book)); err != nil {
		return fmt.Errorf("wal: commit LSN %d: write: %w", lsn, err)
	}

	return nil
}

func (w *WAL) Recover(ctx context.Context) error {
	for z := -1; ; z-- {
		chunkX := 0
		chunkZ := z / 16
		if err := w.hal.ForceLoadChunk(ctx, chunkX, chunkZ); err != nil {
			return fmt.Errorf("wal: recover: forceload chunk (%d,%d): %w", chunkX, chunkZ, err)
		}

		data, err := w.hal.ReadBlock(ctx, 0, walY, z)
		if err != nil {
			return fmt.Errorf("wal: recover: read Z=%d: %w", z, err)
		}
		if len(data) == 0 {
			lsn := int64(-z)
			w.mu.Lock()
			if lsn >= w.nextLSN {
				w.nextLSN = lsn
			}
			w.mu.Unlock()
			return nil
		}

		entry, err := deserializeEntry(string(data))
		if err != nil {
			log.Printf("wal: recover: skip corrupt entry at Z=%d: %v", z, err)
			continue
		}

		if entry.Status == "COMMITTED" {
			lsn := int64(-z)
			w.mu.Lock()
			if lsn >= w.nextLSN {
				w.nextLSN = lsn + 1
			}
			w.mu.Unlock()
			continue
		}

		log.Printf("wal: recover: found PENDING entry LSN=%d op=%s table=%d target=(%d,%d,%d)",
			entry.LSN, entry.Op, entry.TableID, entry.TargetX, entry.TargetY, entry.TargetZ)

		replayed := false
		switch entry.Op {
		case "UPDATE_XMAX":
			replayed, err = w.replayUpdateXmax(ctx, entry)
		case "INSERT":
			replayed, err = w.replayInsert(ctx, entry)
		}

		if err != nil {
			log.Printf("wal: recover: LSN %d replay error: %v", entry.LSN, err)
			continue
		}

		if replayed {
			log.Printf("wal: recover: LSN %d replayed", entry.LSN)
		}

		book := serializeEntry(entry.LSN, entry.TXID, "COMMITTED", entry.Op, entry.TableID, entry.TargetX, entry.TargetY, entry.TargetZ, entry.NewValue)
		if err := w.hal.WriteBlock(ctx, 0, walY, z, hal.BlockTypeLectern, []byte(book)); err != nil {
			return fmt.Errorf("wal: recover: commit LSN %d: %w", entry.LSN, err)
		}

		lsn := int64(-z)
		w.mu.Lock()
		if lsn >= w.nextLSN {
			w.nextLSN = lsn + 1
		}
		w.mu.Unlock()
	}
}

func (w *WAL) replayUpdateXmax(ctx context.Context, entry *Entry) (bool, error) {
	targetXmax0, err := w.hal.ReadBlock(ctx, 2, entry.TargetY, entry.TargetZ)
	if err != nil {
		return false, fmt.Errorf("read xmax banner 0: %w", err)
	}
	targetXmax1, err := w.hal.ReadBlock(ctx, 3, entry.TargetY, entry.TargetZ)
	if err != nil {
		return false, fmt.Errorf("read xmax banner 1: %w", err)
	}

	expectedTxid, err := strconv.ParseInt(entry.NewValue, 10, 64)
	if err != nil {
		return false, fmt.Errorf("parse xmax txid from NewValue %q: %w", entry.NewValue, err)
	}

	currentXmax, err := decodeInt64(string(targetXmax0), string(targetXmax1))
	if err == nil && currentXmax == expectedTxid {
		return false, nil
	}

	s1, s2 := encodeInt64(expectedTxid)
	writes := []hal.BlockWrite{
		{Pos: hal.BlockPos{X: 2, Y: entry.TargetY, Z: entry.TargetZ}, BlockType: hal.BlockTypeBanner, Data: []byte(s1)},
		{Pos: hal.BlockPos{X: 3, Y: entry.TargetY, Z: entry.TargetZ}, BlockType: hal.BlockTypeBanner, Data: []byte(s2)},
	}
	if err := w.hal.BatchWrite(ctx, writes); err != nil {
		return false, fmt.Errorf("write xmax banners: %w", err)
	}
	return true, nil
}

func (w *WAL) replayInsert(ctx context.Context, entry *Entry) (bool, error) {
	targetBlock, err := w.hal.ReadBlock(ctx, entry.TargetX, entry.TargetY, entry.TargetZ)
	if err != nil {
		return false, fmt.Errorf("read target block: %w", err)
	}
	if len(targetBlock) == 0 {
		log.Printf("wal: recover: LSN %d INSERT target block empty, data may be lost", entry.LSN)
		return false, nil
	}
	return false, nil
}

func serializeEntry(lsn, txid int64, status, op string, tableID, x, y, z int, newValue string) string {
	pages := []string{
		fmt.Sprintf("LSN: %d\nTXID: %d\nSTATUS: %s", lsn, txid, status),
		fmt.Sprintf("OP: %s\nTABLE: %d", op, tableID),
		fmt.Sprintf("X: %d\nY: %d\nZ: %d", x, y, z),
	}

	remaining := newValue
	for len(remaining) > 0 {
		end := 200
		if end > len(remaining) {
			end = len(remaining)
		}
		pages = append(pages, remaining[:end])
		remaining = remaining[end:]
	}

	return strings.Join(pages, pageSep)
}

func deserializeEntry(book string) (*Entry, error) {
	pages := strings.Split(book, pageSep)
	if len(pages) < 4 {
		return nil, fmt.Errorf("wal: expected at least 4 pages, got %d", len(pages))
	}

	entry := &Entry{}

	lines := strings.Split(pages[0], "\n")
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "LSN: "):
			v, err := strconv.ParseInt(strings.TrimPrefix(line, "LSN: "), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse LSN: %w", err)
			}
			entry.LSN = v
		case strings.HasPrefix(line, "TXID: "):
			v, err := strconv.ParseInt(strings.TrimPrefix(line, "TXID: "), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse TXID: %w", err)
			}
			entry.TXID = v
		case strings.HasPrefix(line, "STATUS: "):
			entry.Status = strings.TrimPrefix(line, "STATUS: ")
		}
	}

	lines = strings.Split(pages[1], "\n")
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "OP: "):
			entry.Op = strings.TrimPrefix(line, "OP: ")
		case strings.HasPrefix(line, "TABLE: "):
			v, err := strconv.Atoi(strings.TrimPrefix(line, "TABLE: "))
			if err != nil {
				return nil, fmt.Errorf("parse TABLE: %w", err)
			}
			entry.TableID = v
		}
	}

	lines = strings.Split(pages[2], "\n")
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "X: "):
			v, err := strconv.Atoi(strings.TrimPrefix(line, "X: "))
			if err != nil {
				return nil, fmt.Errorf("parse X: %w", err)
			}
			entry.TargetX = v
		case strings.HasPrefix(line, "Y: "):
			v, err := strconv.Atoi(strings.TrimPrefix(line, "Y: "))
			if err != nil {
				return nil, fmt.Errorf("parse Y: %w", err)
			}
			entry.TargetY = v
		case strings.HasPrefix(line, "Z: "):
			v, err := strconv.Atoi(strings.TrimPrefix(line, "Z: "))
			if err != nil {
				return nil, fmt.Errorf("parse Z: %w", err)
			}
			entry.TargetZ = v
		}
	}

	if len(pages) > 4 {
		entry.NewValue = strings.Join(pages[3:], "")
	} else {
		entry.NewValue = pages[3]
	}

	return entry, nil
}

func encodeInt64(v int64) (string, string) {
	b := make([]byte, bytesPerBanner*2)
	binary.BigEndian.PutUint64(b[0:8], uint64(v))
	for i := range b {
		b[i] ^= xormask
	}
	return hex.EncodeToString(b[0:6]), hex.EncodeToString(b[6:12])
}

func decodeInt64(s1, s2 string) (int64, error) {
	b1, err := hex.DecodeString(s1)
	if err != nil {
		return 0, fmt.Errorf("invalid hex (banner 1): %w", err)
	}
	b2, err := hex.DecodeString(s2)
	if err != nil {
		return 0, fmt.Errorf("invalid hex (banner 2): %w", err)
	}
	if len(b1) != bytesPerBanner || len(b2) != bytesPerBanner {
		return 0, fmt.Errorf("expected %d bytes per banner", bytesPerBanner)
	}
	b := make([]byte, 8)
	copy(b[0:6], b1)
	copy(b[6:8], b2[0:2])
	for i := range b {
		b[i] ^= xormask
	}
	return int64(binary.BigEndian.Uint64(b)), nil
}
