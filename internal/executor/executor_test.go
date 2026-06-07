package executor

import (
	"context"
	"testing"

	"github.com/swapnil404/minesql/internal/hal/mock"
	"github.com/swapnil404/minesql/internal/parser"
	"github.com/swapnil404/minesql/internal/storage"
)

func newTestExecutor(t *testing.T) *Executor {
	t.Helper()
	h := mock.NewStorage()
	s := storage.NewStorage(h)
	if err := s.LoadCatalog(context.Background()); err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	return NewExecutor(s)
}

func parseSQL(t *testing.T, sql string) *parser.Statement {
	t.Helper()
	stmt, err := parser.Parse(sql)
	if err != nil {
		t.Fatalf("Parse(%q): %v", sql, err)
	}
	return stmt
}

func TestFullFlow(t *testing.T) {
	ctx := context.Background()
	ex := newTestExecutor(t)

	// CREATE TABLE
	create := parseSQL(t, "CREATE TABLE players (name TEXT, kills INT)")
	result, err := ex.Execute(ctx, create, 1)
	if err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
	if result.Tag != "CREATE TABLE" {
		t.Errorf("expected 'CREATE TABLE', got %q", result.Tag)
	}

	// INSERT
	insert := parseSQL(t, "INSERT INTO players (name, kills) VALUES ('swapnil', 42)")
	result, err = ex.Execute(ctx, insert, 10)
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}
	if result.Tag != "INSERT 0 1" {
		t.Errorf("expected 'INSERT 0 1', got %q", result.Tag)
	}

	// INSERT another row
	insert2 := parseSQL(t, "INSERT INTO players (name, kills) VALUES ('alice', 100)")
	result, err = ex.Execute(ctx, insert2, 10)
	if err != nil {
		t.Fatalf("INSERT 2: %v", err)
	}

	// SELECT *
	sel := parseSQL(t, "SELECT * FROM players")
	result, err = ex.Execute(ctx, sel, 10)
	if err != nil {
		t.Fatalf("SELECT *: %v", err)
	}
	if result.Tag != "SELECT 2" {
		t.Errorf("expected 'SELECT 2', got %q", result.Tag)
	}
	if len(result.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %v", result.Columns)
	}
	if result.Columns[0] != "name" || result.Columns[1] != "kills" {
		t.Errorf("unexpected columns: %v", result.Columns)
	}
	if len(result.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(result.Rows))
	}
}

func TestSelectWithWhere(t *testing.T) {
	ctx := context.Background()
	ex := newTestExecutor(t)

	ex.Execute(ctx, parseSQL(t, "CREATE TABLE players (name TEXT, kills INT)"), 1)
	ex.Execute(ctx, parseSQL(t, "INSERT INTO players (name, kills) VALUES ('swapnil', 42)"), 5)
	ex.Execute(ctx, parseSQL(t, "INSERT INTO players (name, kills) VALUES ('alice', 100)"), 5)

	sel := parseSQL(t, "SELECT name, kills FROM players WHERE kills > 10")
	result, err := ex.Execute(ctx, sel, 10)
	if err != nil {
		t.Fatalf("SELECT WHERE: %v", err)
	}
	if result.Tag != "SELECT 2" {
		t.Errorf("expected 'SELECT 2', got %q", result.Tag)
	}
	if len(result.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(result.Rows))
	}

	// WHERE kills > 50 → only alice
	sel2 := parseSQL(t, "SELECT name, kills FROM players WHERE kills > 50")
	result, err = ex.Execute(ctx, sel2, 10)
	if err != nil {
		t.Fatalf("SELECT WHERE >50: %v", err)
	}
	if result.Tag != "SELECT 1" {
		t.Errorf("expected 'SELECT 1', got %q", result.Tag)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
}

func TestDelete(t *testing.T) {
	ctx := context.Background()
	ex := newTestExecutor(t)

	ex.Execute(ctx, parseSQL(t, "CREATE TABLE players (name TEXT, kills INT)"), 1)
	ex.Execute(ctx, parseSQL(t, "INSERT INTO players (name, kills) VALUES ('swapnil', 42)"), 5)
	ex.Execute(ctx, parseSQL(t, "INSERT INTO players (name, kills) VALUES ('alice', 100)"), 5)

	// DELETE alice
	del := parseSQL(t, "DELETE FROM players WHERE name = 'alice'")
	result, err := ex.Execute(ctx, del, 10)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	if result.Tag != "DELETE 1" {
		t.Errorf("expected 'DELETE 1', got %q", result.Tag)
	}

	// Only swapnil should remain visible at txid=15
	sel := parseSQL(t, "SELECT * FROM players")
	result, err = ex.Execute(ctx, sel, 15)
	if err != nil {
		t.Fatalf("SELECT after DELETE: %v", err)
	}
	if result.Tag != "SELECT 1" {
		t.Errorf("expected 'SELECT 1', got %q", result.Tag)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
}

func TestSelectSpecificColumns(t *testing.T) {
	ctx := context.Background()
	ex := newTestExecutor(t)

	ex.Execute(ctx, parseSQL(t, "CREATE TABLE players (name TEXT, kills INT)"), 1)
	ex.Execute(ctx, parseSQL(t, "INSERT INTO players (name, kills) VALUES ('swapnil', 42)"), 5)

	sel := parseSQL(t, "SELECT name FROM players")
	result, err := ex.Execute(ctx, sel, 10)
	if err != nil {
		t.Fatalf("SELECT name: %v", err)
	}
	if len(result.Columns) != 1 || result.Columns[0] != "name" {
		t.Errorf("expected [name], got %v", result.Columns)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
	if len(result.Rows[0]) != 1 {
		t.Fatalf("expected 1 value in row, got %d", len(result.Rows[0]))
	}
}

func TestWhereOperators(t *testing.T) {
	ctx := context.Background()
	ex := newTestExecutor(t)

	ex.Execute(ctx, parseSQL(t, "CREATE TABLE data (val INT)"), 1)
	ex.Execute(ctx, parseSQL(t, "INSERT INTO data (val) VALUES (10)"), 5)

	tests := []struct {
		cond     string
		txid     int64
		expected int
	}{
		{"val = 10", 10, 1},
		{"val != 10", 10, 0},
		{"val > 5", 10, 1},
		{"val < 20", 10, 1},
		{"val >= 10", 10, 1},
		{"val <= 9", 10, 0},
		{"val = 99", 10, 0},
	}

	for _, tt := range tests {
		sql := "SELECT val FROM data WHERE " + tt.cond
		result, err := ex.Execute(ctx, parseSQL(t, sql), tt.txid)
		if err != nil {
			t.Errorf("%s: %v", tt.cond, err)
			continue
		}
		if len(result.Rows) != tt.expected {
			t.Errorf("%s: expected %d rows, got %d", tt.cond, tt.expected, len(result.Rows))
		}
	}
}
