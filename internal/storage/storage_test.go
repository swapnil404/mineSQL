package storage

import (
	"context"
	"testing"

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
	if meta.YLevel != 64+1*64 {
		t.Errorf("expected YLevel %d, got %d", 64+1*64, meta.YLevel)
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

	if pos.Y != meta.YLevel {
		t.Errorf("expected Y=%d, got %d", meta.YLevel, pos.Y)
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

	s.InsertRow(ctx, meta, map[string]interface{}{"name": "alice", "score": 100}, 5)
	s.InsertRow(ctx, meta, map[string]interface{}{"name": "bob", "score": 200}, 5)

	ch, err := s.SeqScan(ctx, meta, 10)
	if err != nil {
		t.Fatalf("SeqScan: %v", err)
	}

	var rows []Row
	for row := range ch {
		rows = append(rows, row)
	}

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	name0, _ := rows[0]["c0"].(string)
	name1, _ := rows[1]["c0"].(string)
	if name0 != "alice" && name0 != "bob" {
		t.Errorf("unexpected name: %v", name0)
	}
	if name1 != "alice" && name1 != "bob" {
		t.Errorf("unexpected name: %v", name1)
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

	// Insert with txid=5, should be visible to txid=10
	s.InsertRow(ctx, meta, map[string]interface{}{"val": 1}, 5)
	// Insert with txid=15, should NOT be visible to txid=10 (future insert)
	s.InsertRow(ctx, meta, map[string]interface{}{"val": 2}, 15)

	ch, err := s.SeqScan(ctx, meta, 10)
	if err != nil {
		t.Fatalf("SeqScan: %v", err)
	}

	var rows []Row
	for row := range ch {
		rows = append(rows, row)
	}

	if len(rows) != 1 {
		t.Fatalf("expected 1 visible row, got %d", len(rows))
	}

	val, _ := rows[0]["c0"].(float64)
	if val != 1 {
		t.Errorf("expected val=1, got %v", val)
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

	pos, err := s.InsertRow(ctx, meta, map[string]interface{}{"val": 99}, 5)
	if err != nil {
		t.Fatalf("InsertRow: %v", err)
	}

	if err := s.MarkDeleted(ctx, pos, 10); err != nil {
		t.Fatalf("MarkDeleted: %v", err)
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

func TestSlotToWorld(t *testing.T) {
	tests := []struct {
		slot       int
		y          int
		wantChunkX int
		wantChunkZ int
		wantWorldX int
		wantWorldZ int
	}{
		{0, 128, 0, 0, 0, 0},
		{15, 128, 0, 0, 15, 0},
		{16, 128, 0, 0, 0, 1},
		{255, 128, 0, 0, 15, 15},
		{256, 128, 1, 0, 16, 0},
		{511, 128, 1, 0, 31, 15},
	}

	for _, tt := range tests {
		cx, cz, wx, wz := slotToWorld(tt.slot, tt.y)
		if cx != tt.wantChunkX || cz != tt.wantChunkZ || wx != tt.wantWorldX || wz != tt.wantWorldZ {
			t.Errorf("slotToWorld(%d, %d): got chunk=(%d,%d) world=(%d,%d), want chunk=(%d,%d) world=(%d,%d)",
				tt.slot, tt.y, cx, cz, wx, wz,
				tt.wantChunkX, tt.wantChunkZ, tt.wantWorldX, tt.wantWorldZ)
		}
	}
}

func collectRows(ch <-chan Row) []Row {
	var rows []Row
	for r := range ch {
		rows = append(rows, r)
	}
	return rows
}
