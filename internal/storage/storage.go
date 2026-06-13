package storage

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/swapnil404/minesql/internal/hal"
	"github.com/swapnil404/minesql/internal/wal"
)

const (
	catalogY       = 10
	tableY         = 64
	tableZSpacing  = 10000
	systemTxID     = 1
	maxCatalogZ    = 1024
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
	ID         int
	Name       string
	YLevel     int
	ZStart     int
	StripWidth int
	Columns    []ColumnDef
	RowCount   int
}

type Storage struct {
	hal            hal.Storage
	wal            *wal.WAL
	mu             sync.Mutex
	tables         map[string]*TableMeta
	nextCatalogZ   int
	nextTableID    int
}

type catalogEntry struct {
	TableID int         `json:"table_id"`
	Name    string      `json:"name"`
	YLevel  int         `json:"y_level"`
	Columns []ColumnDef `json:"columns"`
}

func NewStorage(h hal.Storage, w *wal.WAL) *Storage {
	s := &Storage{
		hal:         h,
		wal:         w,
		tables:      make(map[string]*TableMeta),
		nextTableID: 1,
	}
	return s
}

func (s *Storage) LoadCatalog(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	maxTableID := 0
	lastZ := 0

	for z := 0; z < maxCatalogZ; z++ {
		data, err := s.hal.ReadBlock(ctx, 0, catalogY, z)
		if err != nil {
			return fmt.Errorf("load catalog: read Z=%d: %w", z, err)
		}
		if len(data) == 0 {
			lastZ = z
			break
		}

		row, err := deserializeRow(data)
		if err != nil {
			continue
		}

		entry, err := parseCatalogRow(row)
		if err != nil {
			continue
		}

		zStart := (entry.TableID - 1) * tableZSpacing
		stripW := stripWidth(entry.Columns)

		meta := &TableMeta{
			ID:         entry.TableID,
			Name:       entry.Name,
			YLevel:     tableY,
			ZStart:     zStart,
			StripWidth: stripW,
			Columns:    entry.Columns,
		}
		s.tables[entry.Name] = meta
		if entry.TableID > maxTableID {
			maxTableID = entry.TableID
		}
		if z+1 > lastZ {
			lastZ = z + 1
		}
	}

	s.nextCatalogZ = lastZ
	if maxTableID >= s.nextTableID {
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
	zStart := (id - 1) * tableZSpacing
	stripW := stripWidth(cols)

	entry := catalogEntry{
		TableID: id,
		Name:    name,
		YLevel:  tableY,
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

	catalogZ := s.nextCatalogZ
	s.nextCatalogZ++

	rowJSON, err := serializeRow(catalogRow)
	if err != nil {
		return fmt.Errorf("storage: serialize catalog row: %w", err)
	}

	if err := s.hal.WriteBlock(ctx, 0, catalogY, catalogZ, hal.BlockTypeBarrel, rowJSON); err != nil {
		return fmt.Errorf("storage: write catalog block: %w", err)
	}

	s.tables[name] = &TableMeta{
		ID:         id,
		Name:       name,
		YLevel:     tableY,
		ZStart:     zStart,
		StripWidth: stripW,
		Columns:    cols,
	}
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
	z := table.ZStart + table.RowCount
	table.RowCount++
	s.mu.Unlock()

	chunkX := 0
	chunkZ := z / 16

	if err := s.hal.ForceLoadChunk(ctx, chunkX, chunkZ); err != nil {
		return hal.BlockPos{}, fmt.Errorf("storage: forceload chunk: %w", err)
	}

	var writes []hal.BlockWrite
	stripY := table.YLevel

	xmin0, xmin1 := EncodeInt64(txid)
	xmax0, xmax1 := EncodeNull()

	writes = append(writes,
		hal.BlockWrite{Pos: hal.BlockPos{X: 0, Y: stripY, Z: z}, BlockType: hal.BlockTypeBanner, Data: []byte(xmin0)},
		hal.BlockWrite{Pos: hal.BlockPos{X: 1, Y: stripY, Z: z}, BlockType: hal.BlockTypeBanner, Data: []byte(xmin1)},
		hal.BlockWrite{Pos: hal.BlockPos{X: 2, Y: stripY, Z: z}, BlockType: hal.BlockTypeBanner, Data: []byte(xmax0)},
		hal.BlockWrite{Pos: hal.BlockPos{X: 3, Y: stripY, Z: z}, BlockType: hal.BlockTypeBanner, Data: []byte(xmax1)},
	)

	offset := 4
	for _, col := range table.Columns {
		val := getColumnValue(values, col)
		switch col.Type {
		case "INT":
			enc := EncodeInt32(toInt32(val))
			writes = append(writes, hal.BlockWrite{
				Pos: hal.BlockPos{X: offset, Y: stripY, Z: z}, BlockType: hal.BlockTypeBanner, Data: []byte(enc),
			})
			offset++
		case "BIGINT":
			s1, s2 := EncodeInt64(toInt64(val))
			writes = append(writes,
				hal.BlockWrite{Pos: hal.BlockPos{X: offset, Y: stripY, Z: z}, BlockType: hal.BlockTypeBanner, Data: []byte(s1)},
				hal.BlockWrite{Pos: hal.BlockPos{X: offset + 1, Y: stripY, Z: z}, BlockType: hal.BlockTypeBanner, Data: []byte(s2)},
			)
			offset += 2
		case "BOOLEAN":
			enc := EncodeBool(toBool(val))
			writes = append(writes, hal.BlockWrite{
				Pos: hal.BlockPos{X: offset, Y: stripY, Z: z}, BlockType: hal.BlockTypeBanner, Data: []byte(enc),
			})
			offset++
		}
	}

	for _, col := range table.Columns {
		if col.Type != "TEXT" {
			continue
		}
		val := getColumnValue(values, col)
		lines := EncodeText(toString(val))
		writes = append(writes, hal.BlockWrite{
			Pos: hal.BlockPos{X: offset, Y: stripY, Z: z}, BlockType: hal.BlockTypeSign, Data: encodeSignData(lines),
		})
		offset++
	}

	newValueJSON, err := json.Marshal(values)
	if err != nil {
		return hal.BlockPos{}, fmt.Errorf("storage: marshal values for WAL: %w", err)
	}

	lsn, err := s.wal.Append(ctx, txid, "INSERT", table.ID, 0, stripY, z, string(newValueJSON))
	if err != nil {
		return hal.BlockPos{}, fmt.Errorf("storage: wal append: %w", err)
	}

	if err := s.hal.BatchWrite(ctx, writes); err != nil {
		return hal.BlockPos{}, fmt.Errorf("storage: batch write: %w", err)
	}

	if err := s.wal.Commit(ctx, lsn); err != nil {
		return hal.BlockPos{}, fmt.Errorf("storage: wal commit: %w", err)
	}

	return hal.BlockPos{X: 0, Y: stripY, Z: z}, nil
}

func (s *Storage) SeqScan(ctx context.Context, table *TableMeta, txid int64) (<-chan ScanResult, error) {
	s.mu.Lock()
	rowCount := table.RowCount
	s.mu.Unlock()

	stripW := table.StripWidth

	ch := make(chan ScanResult, 100)

	go func() {
		defer close(ch)

		if rowCount == 0 {
			return
		}

		log.Printf("[SeqScan DEBUG] table=%q Y=%d Z=[%d..%d] stripWidth=%d rowCount=%d",
			table.Name, table.YLevel, table.ZStart, table.ZStart+rowCount-1, stripW, rowCount)

		for ri := 0; ri < rowCount; ri++ {
			select {
			case <-ctx.Done():
				return
			default:
			}

			z := table.ZStart + ri
			chunkX := 0
			chunkZ := z / 16

			if err := s.hal.ForceLoadChunk(ctx, chunkX, chunkZ); err != nil {
				ch <- ScanResult{Err: fmt.Errorf("storage: forceload chunk (%d,%d): %w", chunkX, chunkZ, err)}
				return
			}

			positions := make([]hal.BlockPos, stripW)
			for ox := 0; ox < stripW; ox++ {
				positions[ox] = hal.BlockPos{X: ox, Y: table.YLevel, Z: z}
			}

			data, err := s.hal.BatchRead(ctx, positions)
			if err != nil {
				ch <- ScanResult{Err: fmt.Errorf("storage: batch read row Z=%d: %w", z, err)}
				return
			}

			nonEmpty := 0
			var firstNonEmpty []byte
			for _, d := range data {
				if len(d) > 0 {
					if nonEmpty == 0 {
						firstNonEmpty = d
					}
					nonEmpty++
				}
			}
			if len(firstNonEmpty) > 24 {
				firstNonEmpty = firstNonEmpty[:24]
			}
			log.Printf("[SeqScan DEBUG] Z=%d stripPositions=%d responses=%d nonEmpty=%d firstHex=%q",
				z, len(positions), len(data), nonEmpty, hex.EncodeToString(firstNonEmpty))

			if isEmptyStrip(data) {
				continue
			}

			row, err := decodeStrip(data, table)
			if err != nil {
				continue
			}

			if isVisible(row, txid) {
				row["_x"] = 0
				row["_y"] = table.YLevel
				row["_z"] = z
				ch <- ScanResult{Row: row}
			}
		}
	}()

	return ch, nil
}

func (s *Storage) MarkDeleted(ctx context.Context, pos hal.BlockPos, txid int64) error {
	xmax0Banner, err := s.hal.ReadBlock(ctx, 2, pos.Y, pos.Z)
	if err != nil {
		return fmt.Errorf("storage: read xmax banner 0 for delete: %w", err)
	}
	xmax1Banner, err := s.hal.ReadBlock(ctx, 3, pos.Y, pos.Z)
	if err != nil {
		return fmt.Errorf("storage: read xmax banner 1 for delete: %w", err)
	}

	if xmax0Banner == nil || xmax1Banner == nil {
		return fmt.Errorf("storage: no row at position (%d,%d,%d)", pos.X, pos.Y, pos.Z)
	}

	if dep := string(xmax0Banner); dep == "" {
		return fmt.Errorf("storage: empty xmax at position (%d,%d,%d)", pos.X, pos.Y, pos.Z)
	}

	tableID := (pos.Z / tableZSpacing) + 1
	newValue := fmt.Sprintf("%d", txid)

	lsn, err := s.wal.Append(ctx, txid, "UPDATE_XMAX", tableID, pos.X, pos.Y, pos.Z, newValue)
	if err != nil {
		return fmt.Errorf("storage: wal append: %w", err)
	}

	s1New, s2New := EncodeInt64(txid)

	writes := []hal.BlockWrite{
		{Pos: hal.BlockPos{X: 2, Y: pos.Y, Z: pos.Z}, BlockType: hal.BlockTypeBanner, Data: []byte(s1New)},
		{Pos: hal.BlockPos{X: 3, Y: pos.Y, Z: pos.Z}, BlockType: hal.BlockTypeBanner, Data: []byte(s2New)},
	}

	if err := s.hal.BatchWrite(ctx, writes); err != nil {
		return fmt.Errorf("storage: write delete marker: %w", err)
	}

	if err := s.wal.Commit(ctx, lsn); err != nil {
		return fmt.Errorf("storage: wal commit: %w", err)
	}

	return nil
}

func stripWidth(columns []ColumnDef) int {
	w := 4
	for _, c := range columns {
		switch c.Type {
		case "INT", "BOOLEAN":
			w++
		case "BIGINT":
			w += 2
		case "TEXT":
			w++
		}
	}
	return w
}

func isEmptyStrip(data [][]byte) bool {
	for _, d := range data {
		if d != nil && len(d) > 0 {
			return false
		}
	}
	return true
}

func decodeStrip(data [][]byte, table *TableMeta) (Row, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("storage: strip too short: %d blocks", len(data))
	}

	xmin, err := DecodeInt64(string(data[0]), string(data[1]))
	if err != nil {
		return nil, fmt.Errorf("storage: decode xmin: %w", err)
	}

	row := Row{
		"xmin": xmin,
	}

	if IsNull(string(data[2]), string(data[3])) {
		row["xmax"] = nil
	} else {
		xmax, err := DecodeInt64(string(data[2]), string(data[3]))
		if err != nil {
			return nil, fmt.Errorf("storage: decode xmax: %w", err)
		}
		row["xmax"] = xmax
	}

	bannerOffset := 4
	signData := data[bannerOffset:]

	numBanners := 0
	numSigns := 0
	for _, c := range table.Columns {
		switch c.Type {
		case "INT", "BOOLEAN":
			numBanners++
		case "BIGINT":
			numBanners += 2
		case "TEXT":
			numSigns++
		}
	}

	bannerIdx := 0
	signIdx := 0

	for _, col := range table.Columns {
		key := fmt.Sprintf("c%d", col.Ordinal)
		switch col.Type {
		case "INT":
			if bannerIdx < len(signData) && len(signData[bannerIdx]) > 0 {
				dec, err := DecodeInt32(string(signData[bannerIdx]))
				if err == nil {
					row[key] = dec
				}
			}
			bannerIdx++
		case "BIGINT":
			if bannerIdx+1 < len(signData) && len(signData[bannerIdx]) > 0 {
				dec, err := DecodeInt64(string(signData[bannerIdx]), string(signData[bannerIdx+1]))
				if err == nil {
					row[key] = dec
				}
			}
			bannerIdx += 2
		case "BOOLEAN":
			if bannerIdx < len(signData) && len(signData[bannerIdx]) > 0 {
				dec, err := DecodeBool(string(signData[bannerIdx]))
				if err == nil {
					row[key] = dec
				}
			}
			bannerIdx++
		case "TEXT":
			signStart := numBanners + signIdx
			if signStart < len(signData) && len(signData[signStart]) > 0 {
				lines := decodeSignData(signData[signStart])
				row[key] = DecodeText(lines)
			}
			signIdx++
		}
	}

	return row, nil
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
	v, ok := row[key]
	if !ok {
		return 0
	}
	switch v := v.(type) {
	case int64:
		return v
	case int32:
		return int64(v)
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	}
	return 0
}

func rowInt(v interface{}) int {
	switch v := v.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case int32:
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

func getColumnValue(values map[string]interface{}, col ColumnDef) interface{} {
	if v, ok := values[col.Name]; ok {
		return v
	}
	key := fmt.Sprintf("c%d", col.Ordinal)
	return values[key]
}

func toInt32(v interface{}) int32 {
	switch v := v.(type) {
	case int32:
		return v
	case int:
		return int32(v)
	case int64:
		return int32(v)
	case float64:
		return int32(v)
	case json.Number:
		n, _ := v.Int64()
		return int32(n)
	}
	return 0
}

func toInt64(v interface{}) int64 {
	switch v := v.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	}
	return 0
}

func toBool(v interface{}) bool {
	switch v := v.(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "1"
	case float64:
		return v != 0
	case int:
		return v != 0
	case int64:
		return v != 0
	case json.Number:
		n, _ := v.Int64()
		return n != 0
	}
	return false
}

func toString(v interface{}) string {
	switch v := v.(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%v", v)
	case int:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	case bool:
		return fmt.Sprintf("%v", v)
	}
	return ""
}

func encodeSignData(lines [4]string) []byte {
	return []byte(lines[0] + "\x00" + lines[1] + "\x00" + lines[2] + "\x00" + lines[3])
}

func decodeSignData(data []byte) [4]string {
	s := string(data)
	parts := strings.SplitN(s, "\x00", 4)
	var lines [4]string
	for i, p := range parts {
		if i < 4 {
			lines[i] = p
		}
	}
	return lines
}
