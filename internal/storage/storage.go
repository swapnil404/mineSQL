package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/swapnil404/minesql/internal/hal"
)

const (
	catalogY      = 64
	tableYStep    = 64
	slotsPerChunk = 256
	chunkSize     = 16
	systemTxID    = 1
)

type Row map[string]interface{}

type ScanResult struct {
	Row Row
	Err error
}

type ColumnDef struct {
	Name    string
	Ordinal int
	Type    string
}

type TableMeta struct {
	ID      int
	Name    string
	YLevel  int
	Columns []ColumnDef
}

type Storage struct {
	hal             hal.Storage
	mu              sync.Mutex
	tables          map[string]*TableMeta
	nextSlot        map[int]int
	catalogNextSlot int
	nextTableID     int
}

type catalogEntry struct {
	TableID int         `json:"table_id"`
	Name    string      `json:"name"`
	YLevel  int         `json:"y_level"`
	Columns []ColumnDef `json:"columns"`
}

func NewStorage(h hal.Storage) *Storage {
	s := &Storage{
		hal:             h,
		tables:          make(map[string]*TableMeta),
		nextSlot:        make(map[int]int),
		catalogNextSlot: 1,
		nextTableID:     1,
	}
	return s
}

func (s *Storage) LoadCatalog(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	maxTableID := 0
	seen := 0

	for slot := 1; slot < slotsPerChunk; slot++ {
		_, _, wx, wz := slotToWorld(slot, catalogY)
		data, err := s.hal.ReadBlock(ctx, wx, catalogY, wz)
		if err != nil {
			return fmt.Errorf("load catalog: read slot %d: %w", slot, err)
		}
		if len(data) == 0 {
			continue
		}

		row, err := deserializeRow(data)
		if err != nil {
			continue
		}

		entry, err := parseCatalogRow(row)
		if err != nil {
			continue
		}

		meta := &TableMeta{
			ID:      entry.TableID,
			Name:    entry.Name,
			YLevel:  entry.YLevel,
			Columns: entry.Columns,
		}
		s.tables[entry.Name] = meta
		if entry.TableID > maxTableID {
			maxTableID = entry.TableID
		}
		seen++
	}

	s.catalogNextSlot = seen + 1
	if maxTableID > 0 {
		s.nextTableID = maxTableID + 1
	}

	return nil
}

func (s *Storage) CreateTable(ctx context.Context, name string, cols []ColumnDef) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.tables[name]; ok {
		return fmt.Errorf("storage: table %q already exists", name)
	}

	id := s.nextTableID
	yLevel := catalogY + id*tableYStep

	entry := catalogEntry{
		TableID: id,
		Name:    name,
		YLevel:  yLevel,
		Columns: cols,
	}
	colsJSON, err := json.Marshal(entry.Columns)
	if err != nil {
		return fmt.Errorf("storage: marshal columns: %w", err)
	}

	catalogRow := Row{
		"xmin": systemTxID,
		"xmax": nil,
		"c0":   entry.TableID,
		"c1":   entry.Name,
		"c2":   entry.YLevel,
		"c3":   string(colsJSON),
	}

	catalogSlot := s.catalogNextSlot
	s.catalogNextSlot++

	_, _, wx, wz := slotToWorld(catalogSlot, catalogY)

	rowJSON, err := serializeRow(catalogRow)
	if err != nil {
		return fmt.Errorf("storage: serialize catalog row: %w", err)
	}

	if err := s.hal.WriteBlock(ctx, wx, catalogY, wz, rowJSON); err != nil {
		return fmt.Errorf("storage: write catalog block: %w", err)
	}

	s.tables[name] = &TableMeta{
		ID:      id,
		Name:    name,
		YLevel:  yLevel,
		Columns: cols,
	}
	s.nextSlot[id] = 0
	s.nextTableID++

	return nil
}

func (s *Storage) GetTable(ctx context.Context, name string) (*TableMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tables[name]
	if !ok {
		return nil, fmt.Errorf("storage: table %q not found", name)
	}
	return t, nil
}

func (s *Storage) InsertRow(ctx context.Context, table *TableMeta, values map[string]interface{}, txid int64) (hal.BlockPos, error) {
	s.mu.Lock()
	slot := s.nextSlot[table.ID]
	s.nextSlot[table.ID]++
	s.mu.Unlock()

	chunkX, chunkZ, wx, wz := slotToWorld(slot, table.YLevel)

	if err := s.hal.ForceLoadChunk(ctx, chunkX, chunkZ); err != nil {
		return hal.BlockPos{}, fmt.Errorf("storage: forceload chunk: %w", err)
	}

	row := Row{
		"xmin": txid,
		"xmax": nil,
	}
	for _, col := range table.Columns {
		key := fmt.Sprintf("c%d", col.Ordinal)
		val, ok := values[col.Name]
		if !ok {
			val = values[key]
		}
		row[key] = val
	}

	rowJSON, err := serializeRow(row)
	if err != nil {
		return hal.BlockPos{}, fmt.Errorf("storage: serialize row: %w", err)
	}

	if err := s.hal.WriteBlock(ctx, wx, table.YLevel, wz, rowJSON); err != nil {
		return hal.BlockPos{}, fmt.Errorf("storage: write block: %w", err)
	}

	return hal.BlockPos{X: wx, Y: table.YLevel, Z: wz}, nil
}

func (s *Storage) SeqScan(ctx context.Context, table *TableMeta, txid int64) (<-chan ScanResult, error) {
	s.mu.Lock()
	maxSlot := s.nextSlot[table.ID]
	s.mu.Unlock()

	ch := make(chan ScanResult, 100)

	go func() {
		defer close(ch)

		chunkCount := (maxSlot + slotsPerChunk - 1) / slotsPerChunk
		if chunkCount == 0 {
			return
		}

		for ci := 0; ci < chunkCount; ci++ {
			select {
			case <-ctx.Done():
				return
			default:
			}

			chunkX := ci
			chunkZ := 0

			if err := s.hal.ForceLoadChunk(ctx, chunkX, chunkZ); err != nil {
				ch <- ScanResult{Err: fmt.Errorf("storage: forceload chunk (%d,%d): %w", chunkX, chunkZ, err)}
				return
			}

			positions := make([]hal.BlockPos, slotsPerChunk)
			for si := 0; si < slotsPerChunk; si++ {
				xOff := si % chunkSize
				zOff := si / chunkSize
				positions[si] = hal.BlockPos{
					X: chunkX*chunkSize + xOff,
					Y: table.YLevel,
					Z: zOff,
				}
			}

			data, err := s.hal.BatchRead(ctx, positions)
			if err != nil {
				ch <- ScanResult{Err: fmt.Errorf("storage: batch read chunk (%d,%d) at y=%d: %w", chunkX, chunkZ, table.YLevel, err)}
				return
			}

			for i, d := range data {
				if len(d) == 0 {
					continue
				}

				select {
				case <-ctx.Done():
					return
				default:
				}

				row, err := deserializeRow(d)
				if err != nil {
					continue
				}
				if isVisible(row, txid) {
					row["_x"] = positions[i].X
					row["_y"] = positions[i].Y
					row["_z"] = positions[i].Z
					ch <- ScanResult{Row: row}
				}
			}
		}
	}()

	return ch, nil
}

func (s *Storage) MarkDeleted(ctx context.Context, pos hal.BlockPos, txid int64) error {
	data, err := s.hal.ReadBlock(ctx, pos.X, pos.Y, pos.Z)
	if err != nil {
		return fmt.Errorf("storage: read block for delete: %w", err)
	}
	if len(data) == 0 {
		return fmt.Errorf("storage: no row at position (%d,%d,%d)", pos.X, pos.Y, pos.Z)
	}

	row, err := deserializeRow(data)
	if err != nil {
		return fmt.Errorf("storage: deserialize row for delete: %w", err)
	}

	row["xmax"] = txid

	rowJSON, err := serializeRow(row)
	if err != nil {
		return fmt.Errorf("storage: serialize row for delete: %w", err)
	}

	if err := s.hal.WriteBlock(ctx, pos.X, pos.Y, pos.Z, rowJSON); err != nil {
		return fmt.Errorf("storage: write delete marker: %w", err)
	}

	return nil
}

func slotToWorld(slot int, yLevel int) (chunkX, chunkZ, worldX, worldZ int) {
	chunkIndex := slot / slotsPerChunk
	slotInChunk := slot % slotsPerChunk
	xOff := slotInChunk % chunkSize
	zOff := slotInChunk / chunkSize
	return chunkIndex, 0, chunkIndex*chunkSize + xOff, zOff
}

func serializeRow(row Row) ([]byte, error) {
	return json.Marshal(row)
}

func deserializeRow(data []byte) (Row, error) {
	row := make(Row)
	if err := json.Unmarshal(data, &row); err != nil {
		return nil, err
	}
	return row, nil
}

func parseCatalogRow(row Row) (*catalogEntry, error) {
	entry := &catalogEntry{}

	if v, ok := row["c0"]; ok {
		entry.TableID = rowInt(v)
	}
	if v, ok := row["c1"]; ok {
		entry.Name = rowStr(v)
	}
	if v, ok := row["c2"]; ok {
		entry.YLevel = rowInt(v)
	}
	if v, ok := row["c3"]; ok {
		colsStr := rowStr(v)
		if colsStr != "" {
			if err := json.Unmarshal([]byte(colsStr), &entry.Columns); err != nil {
				return nil, fmt.Errorf("parse catalog columns: %w", err)
			}
		}
	}

	if entry.Name == "" {
		return nil, fmt.Errorf("storage: invalid catalog row")
	}

	return entry, nil
}

func isVisible(row Row, txid int64) bool {
	xmin := rowInt64(row, "xmin")
	if xmin > txid {
		return false
	}

	if row["xmax"] == nil {
		return true
	}

	xmax := rowInt64(row, "xmax")
	return xmax > txid
}

func rowInt64(row Row, key string) int64 {
	return int64(rowInt(row[key]))
}

func rowInt(v interface{}) int {
	switch v := v.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	}
	return 0
}

func rowStr(v interface{}) string {
	switch v := v.(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%v", v)
	}
	return ""
}
