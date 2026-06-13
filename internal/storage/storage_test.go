package storage

import (
	"context"
	"fmt"
	"testing"

	"github.com/swapnil404/minesql/internal/hal"
	"github.com/swapnil404/minesql/internal/hal/mock"
)

func newTestStorage(t *testing.T) *Storage {
	t.Helper()
	h := mock.NewStorage()
	s := NewStorage(h)
	if err := s.LoadCatalog(context.Background()); err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	return s
}

func TestCreateTable(t *testing.T) {
	ctx := context.Background()
	s := newTestStorage(t)

	cols := []ColumnDef{
		{Name: "name", Ordinal: 0, Type: "TEXT"},
		{Name: "kills", Ordinal: 1, Type: "INT"},
	}

	if err := s.CreateTable(ctx, "players", cols); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}

	meta, err := s.GetTable(ctx, "players")
	if err != nil {
		t.Fatalf("GetTable: %v", err)
	}
	if meta.Name != "players" {
		t.Errorf("expected name 'players', got %q", meta.Name)
	}
	if meta.ID != 1 {
		t.Errorf("expected ID 1, got %d", meta.ID)
	}
	if meta.YLevel != tableY {
		t.Errorf("expected YLevel %d, got %d", tableY, meta.YLevel)
	}
	if meta.ZStart != 0 {
		t.Errorf("expected ZStart 0, got %d", meta.ZStart)
	}
	if len(meta.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(meta.Columns))
	}
}

func TestCreateTableDuplicate(t *testing.T) {
	ctx := context.Background()
	s := newTestStorage(t)

	cols := []ColumnDef{{Name: "x", Ordinal: 0, Type: "INT"}}
	if err := s.CreateTable(ctx, "dup", cols); err != nil {
		t.Fatalf("first CreateTable: %v", err)
	}
	if err := s.CreateTable(ctx, "dup", cols); err == nil {
		t.Fatal("expected error on duplicate table")
	}
}

func TestGetTableNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStorage(t)

	_, err := s.GetTable(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent table")
	}
}

func TestInsertRow(t *testing.T) {
	ctx := context.Background()
	s := newTestStorage(t)

	cols := []ColumnDef{
		{Name: "name", Ordinal: 0, Type: "TEXT"},
		{Name: "kills", Ordinal: 1, Type: "INT"},
	}
	if err := s.CreateTable(ctx, "players", cols); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}

	meta, _ := s.GetTable(ctx, "players")
	values := map[string]interface{}{
		"name":  "swapnil",
		"kills": 42,
	}

	pos, err := s.InsertRow(ctx, meta, values, 10)
	if err != nil {
		t.Fatalf("InsertRow: %v", err)
	}

	if pos.Y != tableY {
		t.Errorf("expected Y=%d, got %d", tableY, pos.Y)
	}
	if pos.Z != meta.ZStart {
		t.Errorf("expected Z=%d (first row), got %d", meta.ZStart, pos.Z)
	}
	if pos.X != 0 {
		t.Errorf("expected X=0, got %d", pos.X)
	}
}

func TestInsertRowStripLayout(t *testing.T) {
	ctx := context.Background()
	h := mock.NewStorage()
	s := NewStorage(h)
	if err := s.LoadCatalog(ctx); err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	cols := []ColumnDef{
		{Name: "val_int", Ordinal: 0, Type: "INT"},
		{Name: "val_text", Ordinal: 1, Type: "TEXT"},
	}
	if err := s.CreateTable(ctx, "test_strip", cols); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}

	meta, _ := s.GetTable(ctx, "test_strip")
	values := map[string]interface{}{
		"val_int":  int32(42),
		"val_text": "hello",
	}

	pos, err := s.InsertRow(ctx, meta, values, 7)
	if err != nil {
		t.Fatalf("InsertRow: %v", err)
	}

	expectedZ := meta.ZStart
	if pos.Z != expectedZ {
		t.Errorf("expected Z=%d, got %d", expectedZ, pos.Z)
	}
	if pos.X != 0 {
		t.Errorf("expected X=0, got %d", pos.X)
	}
	if pos.Y != tableY {
		t.Errorf("expected Y=%d, got %d", tableY, pos.Y)
	}

	// Verify xmin banners
	b0, _ := h.ReadBlock(ctx, 0, tableY, pos.Z)
	b1, _ := h.ReadBlock(ctx, 1, tableY, pos.Z)
	if b0 == nil || b1 == nil {
		t.Fatal("xmin banners not written")
	}
	xmin, err := DecodeInt64(string(b0), string(b1))
	if err != nil {
		t.Fatalf("decode xmin: %v", err)
	}
	if xmin != 7 {
		t.Errorf("expected xmin=7, got %d", xmin)
	}

	// Verify xmax banners are null
	bX0, _ := h.ReadBlock(ctx, 2, tableY, pos.Z)
	bX1, _ := h.ReadBlock(ctx, 3, tableY, pos.Z)
	if !IsNull(string(bX0), string(bX1)) {
		t.Error("expected xmax null sentinel")
	}

	// Verify INT column
	bInt, _ := h.ReadBlock(ctx, 4, tableY, pos.Z)
	decInt, err := DecodeInt32(string(bInt))
	if err != nil {
		t.Fatalf("decode INT: %v", err)
	}
	if decInt != 42 {
		t.Errorf("expected INT=42, got %d", decInt)
	}

	// Verify TEXT column
	bText, _ := h.ReadBlock(ctx, 5, tableY, pos.Z)
	lines := decodeSignData(bText)
	decoded := DecodeText(lines)
	if decoded != "hello" {
		t.Errorf("expected TEXT='hello', got %q", decoded)
	}
}

func TestSeqScan(t *testing.T) {
	ctx := context.Background()
	s := newTestStorage(t)

	cols := []ColumnDef{
		{Name: "name", Ordinal: 0, Type: "TEXT"},
		{Name: "score", Ordinal: 1, Type: "INT"},
	}
	if err := s.CreateTable(ctx, "players", cols); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}

	meta, _ := s.GetTable(ctx, "players")

	s.InsertRow(ctx, meta, map[string]interface{}{"name": "alice", "score": int32(100)}, 5)
	s.InsertRow(ctx, meta, map[string]interface{}{"name": "bob", "score": int32(200)}, 5)

	ch, err := s.SeqScan(ctx, meta, 10)
	if err != nil {
		t.Fatalf("SeqScan: %v", err)
	}

	var rows []Row
	for result := range ch {
		if result.Err != nil {
			t.Fatalf("SeqScan error: %v", result.Err)
		}
		rows = append(rows, result.Row)
	}

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	for _, row := range rows {
		name, ok := row["c0"].(string)
		if !ok {
			t.Errorf("expected c0 to be string, got %T", row["c0"])
		}
		if name != "alice" && name != "bob" {
			t.Errorf("unexpected name: %v", name)
		}
		score, ok := row["c1"].(int32)
		if !ok {
			t.Errorf("expected c1 to be int32, got %T", row["c1"])
		}
		if score != 100 && score != 200 {
			t.Errorf("unexpected score: %v", score)
		}
	}
}

func TestSeqScanMVCC(t *testing.T) {
	ctx := context.Background()
	s := newTestStorage(t)

	cols := []ColumnDef{{Name: "val", Ordinal: 0, Type: "INT"}}
	if err := s.CreateTable(ctx, "data", cols); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}

	meta, _ := s.GetTable(ctx, "data")

	s.InsertRow(ctx, meta, map[string]interface{}{"val": int32(1)}, 5)
	s.InsertRow(ctx, meta, map[string]interface{}{"val": int32(2)}, 15)

	ch, err := s.SeqScan(ctx, meta, 10)
	if err != nil {
		t.Fatalf("SeqScan: %v", err)
	}

	var rows []Row
	for result := range ch {
		if result.Err != nil {
			t.Fatalf("SeqScan error: %v", result.Err)
		}
		rows = append(rows, result.Row)
	}

	if len(rows) != 1 {
		t.Fatalf("expected 1 visible row, got %d", len(rows))
	}

	val, ok := rows[0]["c0"].(int32)
	if !ok {
		t.Fatalf("expected c0 to be int32, got %T", rows[0]["c0"])
	}
	if val != 1 {
		t.Errorf("expected val=1, got %d", val)
	}
}

func TestSeqScanBigintAndBool(t *testing.T) {
	ctx := context.Background()
	s := newTestStorage(t)

	cols := []ColumnDef{
		{Name: "big", Ordinal: 0, Type: "BIGINT"},
		{Name: "flag", Ordinal: 1, Type: "BOOLEAN"},
	}
	if err := s.CreateTable(ctx, "test_multi", cols); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}

	meta, _ := s.GetTable(ctx, "test_multi")

	s.InsertRow(ctx, meta, map[string]interface{}{"big": int64(9223372036854775807), "flag": true}, 1)

	ch, err := s.SeqScan(ctx, meta, 10)
	if err != nil {
		t.Fatalf("SeqScan: %v", err)
	}

	var rows []Row
	for result := range ch {
		if result.Err != nil {
			t.Fatalf("SeqScan error: %v", result.Err)
		}
		rows = append(rows, result.Row)
	}

	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}

	big, ok := rows[0]["c0"].(int64)
	if !ok {
		t.Fatalf("expected c0 to be int64, got %T", rows[0]["c0"])
	}
	if big != 9223372036854775807 {
		t.Errorf("expected big=%d, got %d", int64(9223372036854775807), big)
	}

	flag, ok := rows[0]["c1"].(bool)
	if !ok {
		t.Fatalf("expected c1 to be bool, got %T", rows[0]["c1"])
	}
	if !flag {
		t.Error("expected flag=true")
	}
}

func TestMarkDeleted(t *testing.T) {
	ctx := context.Background()
	s := newTestStorage(t)

	cols := []ColumnDef{{Name: "val", Ordinal: 0, Type: "INT"}}
	if err := s.CreateTable(ctx, "data", cols); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}

	meta, _ := s.GetTable(ctx, "data")

	pos, err := s.InsertRow(ctx, meta, map[string]interface{}{"val": int32(99)}, 5)
	if err != nil {
		t.Fatalf("InsertRow: %v", err)
	}

	if err := s.MarkDeleted(ctx, pos, 10); err != nil {
		t.Fatalf("MarkDeleted: %v", err)
	}

	// Verify xmax banners updated
	bX0, _ := s.hal.ReadBlock(ctx, 2, pos.Y, pos.Z)
	bX1, _ := s.hal.ReadBlock(ctx, 3, pos.Y, pos.Z)
	xmax, err := DecodeInt64(string(bX0), string(bX1))
	if err != nil {
		t.Fatalf("decode xmax: %v", err)
	}
	if xmax != 10 {
		t.Errorf("expected xmax=10, got %d", xmax)
	}

	// Row should still be visible to txid 8 (xmax=10 > 8)
	ch, err := s.SeqScan(ctx, meta, 8)
	if err != nil {
		t.Fatalf("SeqScan: %v", err)
	}

	rows := collectRows(ch)
	if len(rows) != 1 {
		t.Fatalf("expected 1 visible row at txid=8, got %d", len(rows))
	}

	// Row should NOT be visible to txid 15 (xmax=10 <= 15)
	ch, err = s.SeqScan(ctx, meta, 15)
	if err != nil {
		t.Fatalf("SeqScan: %v", err)
	}

	rows = collectRows(ch)
	if len(rows) != 0 {
		t.Fatalf("expected 0 visible rows at txid=15, got %d", len(rows))
	}
}

func TestMarkDeletedNonExistent(t *testing.T) {
	ctx := context.Background()
	s := newTestStorage(t)

	pos := hal.BlockPos{X: 0, Y: tableY, Z: 999}
	err := s.MarkDeleted(ctx, pos, 10)
	if err == nil {
		t.Fatal("expected error for non-existent row")
	}
}

func TestStripWidth(t *testing.T) {
	tests := []struct {
		cols []ColumnDef
		want int
	}{
		{[]ColumnDef{}, 4},
		{[]ColumnDef{{Type: "INT"}}, 5},
		{[]ColumnDef{{Type: "BIGINT"}}, 6},
		{[]ColumnDef{{Type: "BOOLEAN"}}, 5},
		{[]ColumnDef{{Type: "TEXT"}}, 5},
		{[]ColumnDef{{Type: "INT"}, {Type: "TEXT"}}, 6},
		{[]ColumnDef{{Type: "BIGINT"}, {Type: "BIGINT"}}, 8},
		{[]ColumnDef{{Type: "INT"}, {Type: "BOOLEAN"}, {Type: "TEXT"}, {Type: "TEXT"}}, 8},
	}

	for _, tt := range tests {
		got := stripWidth(tt.cols)
		if got != tt.want {
			t.Errorf("stripWidth(%v): got %d, want %d", tt.cols, got, tt.want)
		}
	}
}

func TestZStart(t *testing.T) {
	ctx := context.Background()
	s := newTestStorage(t)

	cols := []ColumnDef{{Name: "x", Ordinal: 0, Type: "INT"}}

	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("t%d", i)
		if err := s.CreateTable(ctx, name, cols); err != nil {
			t.Fatalf("CreateTable %q: %v", name, err)
		}
		meta, _ := s.GetTable(ctx, name)
		expectedZ := (meta.ID - 1) * tableZSpacing
		if meta.ZStart != expectedZ {
			t.Errorf("table %q (ID=%d): expected ZStart=%d, got %d", name, meta.ID, expectedZ, meta.ZStart)
		}
	}
}

func TestRowCountIncrements(t *testing.T) {
	ctx := context.Background()
	s := newTestStorage(t)

	cols := []ColumnDef{{Name: "val", Ordinal: 0, Type: "INT"}}
	s.CreateTable(ctx, "test", cols)
	meta, _ := s.GetTable(ctx, "test")

	if meta.RowCount != 0 {
		t.Errorf("expected RowCount=0, got %d", meta.RowCount)
	}

	for i := 0; i < 5; i++ {
		_, err := s.InsertRow(ctx, meta, map[string]interface{}{"val": int32(i)}, int64(i+1))
		if err != nil {
			t.Fatalf("InsertRow %d: %v", i, err)
		}
	}

	s.mu.Lock()
	rc := meta.RowCount
	s.mu.Unlock()
	if rc != 5 {
		t.Errorf("expected RowCount=5, got %d", rc)
	}

	ch, err := s.SeqScan(ctx, meta, 100)
	if err != nil {
		t.Fatalf("SeqScan: %v", err)
	}
	rows := collectRows(ch)
	if len(rows) != 5 {
		t.Errorf("expected 5 rows, got %d", len(rows))
	}
}

func collectRows(ch <-chan ScanResult) []Row {
	var rows []Row
	for r := range ch {
		if r.Err == nil {
			rows = append(rows, r.Row)
		}
	}
	return rows
}
