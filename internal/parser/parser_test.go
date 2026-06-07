package parser

import (
	"reflect"
	"testing"
)

func TestParseSelectStar(t *testing.T) {
	stmt, err := Parse("SELECT * FROM players")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stmt.Type != StmtSelect {
		t.Errorf("expected StmtSelect, got %v", stmt.Type)
	}
	if stmt.Select == nil {
		t.Fatal("expected non-nil Select")
	}
	if stmt.Select.Table != "players" {
		t.Errorf("expected table 'players', got %q", stmt.Select.Table)
	}
	if stmt.Select.Columns != nil {
		t.Errorf("expected nil columns for SELECT *, got %v", stmt.Select.Columns)
	}
	if len(stmt.Select.Where) != 0 {
		t.Errorf("expected empty where, got %v", stmt.Select.Where)
	}
}

func TestParseSelectWithWhere(t *testing.T) {
	stmt, err := Parse("SELECT name, kills FROM players WHERE kills > 10")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stmt.Type != StmtSelect {
		t.Errorf("expected StmtSelect, got %v", stmt.Type)
	}
	if stmt.Select == nil {
		t.Fatal("expected non-nil Select")
	}
	if stmt.Select.Table != "players" {
		t.Errorf("expected table 'players', got %q", stmt.Select.Table)
	}

	expectedCols := []string{"name", "kills"}
	if !reflect.DeepEqual(stmt.Select.Columns, expectedCols) {
		t.Errorf("expected columns %v, got %v", expectedCols, stmt.Select.Columns)
	}

	if len(stmt.Select.Where) != 1 {
		t.Fatalf("expected 1 where condition, got %v", stmt.Select.Where)
	}
	c := stmt.Select.Where[0]
	if c.Column != "kills" {
		t.Errorf("expected column 'kills', got %q", c.Column)
	}
	if c.Operator != ">" {
		t.Errorf("expected operator '>', got %q", c.Operator)
	}
	if c.Value != int64(10) {
		t.Errorf("expected value 10, got %v (%T)", c.Value, c.Value)
	}
}

func TestParseInsert(t *testing.T) {
	stmt, err := Parse("INSERT INTO players (name, kills) VALUES ('swapnil', 42)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stmt.Type != StmtInsert {
		t.Errorf("expected StmtInsert, got %v", stmt.Type)
	}
	if stmt.Insert == nil {
		t.Fatal("expected non-nil Insert")
	}
	if stmt.Insert.Table != "players" {
		t.Errorf("expected table 'players', got %q", stmt.Insert.Table)
	}

	expectedCols := []string{"name", "kills"}
	if !reflect.DeepEqual(stmt.Insert.Columns, expectedCols) {
		t.Errorf("expected columns %v, got %v", expectedCols, stmt.Insert.Columns)
	}

	if len(stmt.Insert.Values) != 2 {
		t.Fatalf("expected 2 values, got %v", stmt.Insert.Values)
	}
	if v, ok := stmt.Insert.Values[0].(string); !ok || v != "swapnil" {
		t.Errorf("expected value[0]='swapnil', got %v (%T)", stmt.Insert.Values[0], stmt.Insert.Values[0])
	}
	if v, ok := stmt.Insert.Values[1].(int64); !ok || v != 42 {
		t.Errorf("expected value[1]=42, got %v (%T)", stmt.Insert.Values[1], stmt.Insert.Values[1])
	}
}

func TestParseCreateTable(t *testing.T) {
	stmt, err := Parse("CREATE TABLE players (name TEXT, kills INT)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stmt.Type != StmtCreateTable {
		t.Errorf("expected StmtCreateTable, got %v", stmt.Type)
	}
	if stmt.CreateTable == nil {
		t.Fatal("expected non-nil CreateTable")
	}
	if stmt.CreateTable.Table != "players" {
		t.Errorf("expected table 'players', got %q", stmt.CreateTable.Table)
	}

	if len(stmt.CreateTable.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %v", stmt.CreateTable.Columns)
	}

	c0 := stmt.CreateTable.Columns[0]
	if c0.Name != "name" || c0.Type != "text" {
		t.Errorf("expected column name=TEXT, got name=%q type=%q", c0.Name, c0.Type)
	}

	c1 := stmt.CreateTable.Columns[1]
	if c1.Name != "kills" || c1.Type != "int4" {
		t.Errorf("expected column kills=INT, got name=%q type=%q", c1.Name, c1.Type)
	}
}

func TestParseDelete(t *testing.T) {
	stmt, err := Parse("DELETE FROM players WHERE name = 'swapnil'")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stmt.Type != StmtDelete {
		t.Errorf("expected StmtDelete, got %v", stmt.Type)
	}
	if stmt.Delete == nil {
		t.Fatal("expected non-nil Delete")
	}
	if stmt.Delete.Table != "players" {
		t.Errorf("expected table 'players', got %q", stmt.Delete.Table)
	}

	if len(stmt.Delete.Where) != 1 {
		t.Fatalf("expected 1 where condition, got %v", stmt.Delete.Where)
	}
	c := stmt.Delete.Where[0]
	if c.Column != "name" {
		t.Errorf("expected column 'name', got %q", c.Column)
	}
	if c.Operator != "=" {
		t.Errorf("expected operator '=', got %q", c.Operator)
	}
	if v, ok := c.Value.(string); !ok || v != "swapnil" {
		t.Errorf("expected value 'swapnil', got %v (%T)", c.Value, c.Value)
	}
}
