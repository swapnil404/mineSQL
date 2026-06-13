package wal

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/swapnil404/minesql/internal/hal"
	"github.com/swapnil404/minesql/internal/hal/mock"
)

func newTestWAL(t *testing.T) (*WAL, *mock.Storage) {
	t.Helper()
	h := mock.NewStorage()
	w := NewWAL(h)
	return w, h
}

func TestNextLSN(t *testing.T) {
	w, _ := newTestWAL(t)
	if w.NextLSN() != 1 {
		t.Errorf("expected initial NextLSN=1, got %d", w.NextLSN())
	}
}

func TestAppendWritesLectern(t *testing.T) {
	ctx := context.Background()
	w, h := newTestWAL(t)

	lsn, err := w.Append(ctx, 10, "INSERT", 1, 0, 64, 100, `{"name":"swapnil"}`)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if lsn != 1 {
		t.Errorf("expected LSN=1, got %d", lsn)
	}

	walZ := -int(lsn)
	data, err := h.ReadBlock(ctx, 0, walY, walZ)
	if err != nil {
		t.Fatalf("ReadBlock: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected lectern block at Z=-1")
	}

	book := string(data)
	if !strings.Contains(book, "LSN: 1") {
		t.Errorf("expected 'LSN: 1' in book, got %q", book)
	}
	if !strings.Contains(book, "TXID: 10") {
		t.Errorf("expected 'TXID: 10' in book, got %q", book)
	}
	if !strings.Contains(book, "STATUS: PENDING") {
		t.Errorf("expected 'STATUS: PENDING' in book, got %q", book)
	}
	if !strings.Contains(book, "OP: INSERT") {
		t.Errorf("expected 'OP: INSERT' in book, got %q", book)
	}
	if !strings.Contains(book, "TABLE: 1") {
		t.Errorf("expected 'TABLE: 1' in book, got %q", book)
	}
	if !strings.Contains(book, "X: 0") {
		t.Errorf("expected 'X: 0' in book, got %q", book)
	}
	if !strings.Contains(book, "Y: 64") {
		t.Errorf("expected 'Y: 64' in book, got %q", book)
	}
	if !strings.Contains(book, "Z: 100") {
		t.Errorf("expected 'Z: 100' in book, got %q", book)
	}
}

func TestAppendIncrementsLSN(t *testing.T) {
	ctx := context.Background()
	w, _ := newTestWAL(t)

	lsn1, err := w.Append(ctx, 1, "INSERT", 1, 0, 64, 0, "v1")
	if err != nil {
		t.Fatalf("Append 1: %v", err)
	}
	lsn2, err := w.Append(ctx, 1, "INSERT", 1, 0, 64, 1, "v2")
	if err != nil {
		t.Fatalf("Append 2: %v", err)
	}

	if lsn1 != 1 {
		t.Errorf("expected first LSN=1, got %d", lsn1)
	}
	if lsn2 != 2 {
		t.Errorf("expected second LSN=2, got %d", lsn2)
	}
	if w.NextLSN() != 3 {
		t.Errorf("expected NextLSN=3, got %d", w.NextLSN())
	}
}

func TestAppendDifferentOps(t *testing.T) {
	ctx := context.Background()
	w, h := newTestWAL(t)

	_, err := w.Append(ctx, 5, "INSERT", 1, 0, 64, 50, `{"k":"v"}`)
	if err != nil {
		t.Fatalf("Append INSERT: %v", err)
	}

	_, err = w.Append(ctx, 10, "UPDATE_XMAX", 1, 2, 64, 50, "10")
	if err != nil {
		t.Fatalf("Append UPDATE_XMAX: %v", err)
	}

	insData, _ := h.ReadBlock(ctx, 0, walY, -1)
	updData, _ := h.ReadBlock(ctx, 0, walY, -2)

	if !strings.Contains(string(insData), "OP: INSERT") {
		t.Error("INSERT entry missing OP: INSERT")
	}
	if !strings.Contains(string(updData), "OP: UPDATE_XMAX") {
		t.Error("UPDATE_XMAX entry missing OP: UPDATE_XMAX")
	}
}

func TestCommitUpdatesStatus(t *testing.T) {
	ctx := context.Background()
	w, h := newTestWAL(t)

	lsn, err := w.Append(ctx, 10, "INSERT", 1, 0, 64, 100, `{"name":"test"}`)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	if err := w.Commit(ctx, lsn); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	walZ := -int(lsn)
	data, err := h.ReadBlock(ctx, 0, walY, walZ)
	if err != nil {
		t.Fatalf("ReadBlock: %v", err)
	}

	book := string(data)
	if !strings.Contains(book, "STATUS: COMMITTED") {
		t.Errorf("expected 'STATUS: COMMITTED', got %q", book)
	}
	if strings.Contains(book, "STATUS: PENDING") {
		t.Error("entry should not contain PENDING after commit")
	}
}

func TestCommitNonexistentLSN(t *testing.T) {
	ctx := context.Background()
	w, _ := newTestWAL(t)

	err := w.Commit(ctx, 99)
	if err == nil {
		t.Fatal("expected error for nonexistent LSN")
	}
}

func TestCommitAlreadyCommitted(t *testing.T) {
	ctx := context.Background()
	w, _ := newTestWAL(t)

	lsn, err := w.Append(ctx, 10, "INSERT", 1, 0, 64, 100, "v")
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := w.Commit(ctx, lsn); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	err = w.Commit(ctx, lsn)
	if err == nil {
		t.Fatal("expected error for double commit")
	}
}

func TestRecoverNoEntries(t *testing.T) {
	ctx := context.Background()
	w, _ := newTestWAL(t)

	if err := w.Recover(ctx); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if w.NextLSN() != 1 {
		t.Errorf("expected NextLSN=1 after empty recover, got %d", w.NextLSN())
	}
}

func TestRecoverAllCommitted(t *testing.T) {
	ctx := context.Background()
	w, h := newTestWAL(t)

	// Simulate 3 committed entries
	for i := int64(1); i <= 3; i++ {
		lsn, err := w.Append(ctx, i*10, "INSERT", 1, 0, 64, int(i*100), `{"v":1}`)
		if err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
		if err := w.Commit(ctx, lsn); err != nil {
			t.Fatalf("Commit %d: %v", i, err)
		}
	}

	// Create a fresh WAL to simulate restart
	w2 := NewWAL(h)
	if err := w2.Recover(ctx); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if w2.NextLSN() != 4 {
		t.Errorf("expected NextLSN=4 after recover, got %d", w2.NextLSN())
	}
}

func TestRecoverPendingUpdateXmax(t *testing.T) {
	ctx := context.Background()
	w, h := newTestWAL(t)

	// Manually write data blocks for the row (xmin, xmax, one INT column)
	xmin0, xmin1 := testEncodeInt64(15)
	null0, null1 := testEncodeNull()

	h.WriteBlock(ctx, 0, 64, 100, hal.BlockTypeBanner, []byte(xmin0))
	h.WriteBlock(ctx, 1, 64, 100, hal.BlockTypeBanner, []byte(xmin1))
	h.WriteBlock(ctx, 2, 64, 100, hal.BlockTypeBanner, []byte(null0))
	h.WriteBlock(ctx, 3, 64, 100, hal.BlockTypeBanner, []byte(null1))

	// Append a PENDING UPDATE_XMAX entry but don't commit
	lsn, err := w.Append(ctx, 20, "UPDATE_XMAX", 2, 2, 64, 100, "20")
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Simulate restart — create fresh WAL and recover
	w2 := NewWAL(h)
	if err := w2.Recover(ctx); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if w2.NextLSN() != lsn+1 {
		t.Errorf("expected NextLSN=%d, got %d", lsn+1, w2.NextLSN())
	}

	// Verify the WAL entry is now COMMITTED
	walZ := -int(lsn)
	data, err := h.ReadBlock(ctx, 0, walY, walZ)
	if err != nil {
		t.Fatalf("ReadBlock: %v", err)
	}
	book := string(data)
	if !strings.Contains(book, "STATUS: COMMITTED") {
		t.Errorf("expected COMMITTED status after recover, got %q", book)
	}

	// Verify xmax banners were replayed
	bX0, _ := h.ReadBlock(ctx, 2, 64, 100)
	bX1, _ := h.ReadBlock(ctx, 3, 64, 100)
	xmax, err := decodeInt64(string(bX0), string(bX1))
	if err != nil {
		t.Fatalf("decode xmax: %v", err)
	}
	if xmax != 20 {
		t.Errorf("expected xmax=20 after replay, got %d", xmax)
	}
}

func TestRecoverPendingInsertTargetWritten(t *testing.T) {
	ctx := context.Background()
	w, h := newTestWAL(t)

	// Write the target block (xmin banner 0) to simulate INSERT data was written
	h.WriteBlock(ctx, 0, 64, 200, hal.BlockTypeBanner, []byte("some-banner-data"))

	// Append a PENDING INSERT entry but don't commit
	lsn, err := w.Append(ctx, 5, "INSERT", 1, 0, 64, 200, `{"name":"test"}`)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Simulate restart
	w2 := NewWAL(h)
	if err := w2.Recover(ctx); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if w2.NextLSN() != lsn+1 {
		t.Errorf("expected NextLSN=%d, got %d", lsn+1, w2.NextLSN())
	}

	// Verify the WAL entry is now COMMITTED
	walZ := -int(lsn)
	data, err := h.ReadBlock(ctx, 0, walY, walZ)
	if err != nil {
		t.Fatalf("ReadBlock: %v", err)
	}
	book := string(data)
	if !strings.Contains(book, "STATUS: COMMITTED") {
		t.Errorf("expected COMMITTED status after recover, got %q", book)
	}

	// Verify target block still exists
	target, _ := h.ReadBlock(ctx, 0, 64, 200)
	if len(target) == 0 {
		t.Error("target block should still exist after recover")
	}
}

func TestRecoverPendingInsertTargetEmpty(t *testing.T) {
	ctx := context.Background()
	w, h := newTestWAL(t)

	// Append a PENDING INSERT entry (no data blocks written — simulating crash before data write)
	lsn, err := w.Append(ctx, 5, "INSERT", 1, 0, 64, 300, `{"name":"lost"}`)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Simulate restart
	w2 := NewWAL(h)
	if err := w2.Recover(ctx); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if w2.NextLSN() != lsn+1 {
		t.Errorf("expected NextLSN=%d, got %d", lsn+1, w2.NextLSN())
	}

	// Verify the WAL entry is COMMITTED (we mark it committed even though data is lost)
	walZ := -int(lsn)
	data, err := h.ReadBlock(ctx, 0, walY, walZ)
	if err != nil {
		t.Fatalf("ReadBlock: %v", err)
	}
	book := string(data)
	if !strings.Contains(book, "STATUS: COMMITTED") {
		t.Errorf("expected COMMITTED status after recover, got %q", book)
	}
}

func TestRecoverStopsAtEmptyLectern(t *testing.T) {
	ctx := context.Background()
	w, h := newTestWAL(t)

	// Write 2 committed entries
	for i := int64(1); i <= 2; i++ {
		lsn, err := w.Append(ctx, i*10, "INSERT", 1, 0, 64, int(i*100), `{"v":1}`)
		if err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
		if err := w.Commit(ctx, lsn); err != nil {
			t.Fatalf("Commit %d: %v", i, err)
		}
	}

	// Simulate restart
	w2 := NewWAL(h)
	if err := w2.Recover(ctx); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if w2.NextLSN() != 3 {
		t.Errorf("expected NextLSN=3, got %d", w2.NextLSN())
	}

	// Verify Z=-3 is empty (no more entries beyond what we wrote)
	data, err := h.ReadBlock(ctx, 0, walY, -3)
	if err != nil {
		t.Fatalf("ReadBlock Z=-3: %v", err)
	}
	if len(data) != 0 {
		t.Error("expected empty block at Z=-3")
	}
}

func TestRecoverUpdateXmaxAlreadyMatches(t *testing.T) {
	ctx := context.Background()
	w, h := newTestWAL(t)

	// Write xmax banners that already match the expected txid
	expectedTxid := int64(25)
	s1, s2 := encodeInt64(expectedTxid)
	h.WriteBlock(ctx, 0, 64, 50, hal.BlockTypeBanner, []byte("xmin-data"))
	h.WriteBlock(ctx, 1, 64, 50, hal.BlockTypeBanner, []byte("xmin-data"))
	h.WriteBlock(ctx, 2, 64, 50, hal.BlockTypeBanner, []byte(s1))
	h.WriteBlock(ctx, 3, 64, 50, hal.BlockTypeBanner, []byte(s2))

	// Append PENDING UPDATE_XMAX with the same txid (xmax already matches)
	lsn, err := w.Append(ctx, expectedTxid, "UPDATE_XMAX", 2, 2, 64, 50, "25")
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Simulate restart
	w2 := NewWAL(h)
	if err := w2.Recover(ctx); err != nil {
		t.Fatalf("Recover: %v", err)
	}

	// Verify still COMMITTED
	walZ := -int(lsn)
	data, _ := h.ReadBlock(ctx, 0, walY, walZ)
	if !strings.Contains(string(data), "STATUS: COMMITTED") {
		t.Error("expected COMMITTED after recover")
	}
}

func TestRecoverMixedEntries(t *testing.T) {
	ctx := context.Background()
	w, h := newTestWAL(t)

	// Entry 1: committed INSERT
	lsn1, _ := w.Append(ctx, 10, "INSERT", 1, 0, 64, 100, `{"v":1}`)
	w.Commit(ctx, lsn1)

	// Entry 2: pending UPDATE_XMAX (will replay)
	xmin0, xmin1 := testEncodeInt64(15)
	null0, null1 := testEncodeNull()
	h.WriteBlock(ctx, 0, 64, 200, hal.BlockTypeBanner, []byte(xmin0))
	h.WriteBlock(ctx, 1, 64, 200, hal.BlockTypeBanner, []byte(xmin1))
	h.WriteBlock(ctx, 2, 64, 200, hal.BlockTypeBanner, []byte(null0))
	h.WriteBlock(ctx, 3, 64, 200, hal.BlockTypeBanner, []byte(null1))

	lsn2, _ := w.Append(ctx, 30, "UPDATE_XMAX", 3, 2, 64, 200, "30")
	// Don't commit entry 2

	// Entry 3: committed INSERT
	lsn3, _ := w.Append(ctx, 20, "INSERT", 2, 0, 64, 300, `{"v":2}`)
	w.Commit(ctx, lsn3)

	// Simulate restart
	w2 := NewWAL(h)
	if err := w2.Recover(ctx); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if w2.NextLSN() != 4 {
		t.Errorf("expected NextLSN=4, got %d", w2.NextLSN())
	}

	// Entry 2 should now be COMMITTED
	data2, _ := h.ReadBlock(ctx, 0, walY, -int(lsn2))
	if !strings.Contains(string(data2), "STATUS: COMMITTED") {
		t.Errorf("entry 2 should be COMMITTED after recover, got %q", string(data2))
	}

	// xmax banners for entry 2 should be replayed
	bX0, _ := h.ReadBlock(ctx, 2, 64, 200)
	bX1, _ := h.ReadBlock(ctx, 3, 64, 200)
	xmax, err := decodeInt64(string(bX0), string(bX1))
	if err != nil {
		t.Fatalf("decode xmax: %v", err)
	}
	if xmax != 30 {
		t.Errorf("expected xmax=30, got %d", xmax)
	}
}

func TestSerializeRoundtrip(t *testing.T) {
	entry := &Entry{
		LSN:      42,
		TXID:     99,
		Status:   "PENDING",
		Op:       "INSERT",
		TableID:  3,
		TargetX:  0,
		TargetY:  64,
		TargetZ:  500,
		NewValue: `{"name":"swapnil","score":100}`,
	}

	book := serializeEntry(entry.LSN, entry.TXID, entry.Status, entry.Op,
		entry.TableID, entry.TargetX, entry.TargetY, entry.TargetZ, entry.NewValue)

	decoded, err := deserializeEntry(book)
	if err != nil {
		t.Fatalf("deserializeEntry: %v", err)
	}

	if decoded.LSN != entry.LSN {
		t.Errorf("LSN: expected %d, got %d", entry.LSN, decoded.LSN)
	}
	if decoded.TXID != entry.TXID {
		t.Errorf("TXID: expected %d, got %d", entry.TXID, decoded.TXID)
	}
	if decoded.Status != entry.Status {
		t.Errorf("Status: expected %q, got %q", entry.Status, decoded.Status)
	}
	if decoded.Op != entry.Op {
		t.Errorf("Op: expected %q, got %q", entry.Op, decoded.Op)
	}
	if decoded.TableID != entry.TableID {
		t.Errorf("TableID: expected %d, got %d", entry.TableID, decoded.TableID)
	}
	if decoded.TargetX != entry.TargetX {
		t.Errorf("TargetX: expected %d, got %d", entry.TargetX, decoded.TargetX)
	}
	if decoded.TargetY != entry.TargetY {
		t.Errorf("TargetY: expected %d, got %d", entry.TargetY, decoded.TargetY)
	}
	if decoded.TargetZ != entry.TargetZ {
		t.Errorf("TargetZ: expected %d, got %d", entry.TargetZ, decoded.TargetZ)
	}
	if decoded.NewValue != entry.NewValue {
		t.Errorf("NewValue: expected %q, got %q", entry.NewValue, decoded.NewValue)
	}
}

func TestSerializeLongNewValue(t *testing.T) {
	// Create a NewValue longer than 200 chars
	longValue := strings.Repeat("x", 500)
	book := serializeEntry(1, 2, "PENDING", "INSERT", 1, 0, 64, 0, longValue)

	decoded, err := deserializeEntry(book)
	if err != nil {
		t.Fatalf("deserializeEntry: %v", err)
	}

	// NewValue in the entry should still be the full original value
	if decoded.NewValue != longValue {
		t.Errorf("expected full NewValue length %d, got %d", len(longValue), len(decoded.NewValue))
	}

	// But page 4 (the first summary page at index 3) should be <= 200 chars
	pages := strings.Split(book, pageSep)
	if len(pages[3]) > 200 {
		t.Errorf("page 4 summary should be <= 200 chars, got %d", len(pages[3]))
	}
}

func TestConcurrentAppend(t *testing.T) {
	ctx := context.Background()
	w, _ := newTestWAL(t)

	var wg sync.WaitGroup
	lsns := make([]int64, 100)
	errs := make([]error, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			values := map[string]interface{}{"v": idx}
			vJSON, _ := json.Marshal(values)
			lsn, err := w.Append(ctx, int64(idx), "INSERT", 1, 0, 64, int(100+idx), string(vJSON))
			lsns[idx] = lsn
			errs[idx] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("Append %d error: %v", i, err)
		}
	}

	// All LSNs should be unique
	seen := make(map[int64]bool)
	for _, lsn := range lsns {
		if seen[lsn] {
			t.Errorf("duplicate LSN: %d", lsn)
		}
		seen[lsn] = true
	}

	if w.NextLSN() != 101 {
		t.Errorf("expected NextLSN=101, got %d", w.NextLSN())
	}
}

func TestWALImplementsExpectedInterface(t *testing.T) {
	// Ensure NewWAL accepts hal.Storage
	h := mock.NewStorage()
	_ = NewWAL(h)
}

func testEncodeInt64(v int64) (string, string) {
	return encodeInt64(v)
}

func testEncodeNull() (string, string) {
	return "aaaaaaaaaaaa", "aaaaaaaaaaaa"
}
