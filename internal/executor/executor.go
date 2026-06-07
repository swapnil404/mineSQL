package executor

import (
	"context"
	"fmt"
	"strconv"

	"github.com/swapnil404/minesql/internal/hal"
	"github.com/swapnil404/minesql/internal/parser"
	"github.com/swapnil404/minesql/internal/storage"
)

type Result struct {
	Columns     []string
	ColumnTypes []string
	Rows        [][]interface{}
	Tag         string
}

type Executor struct {
	store *storage.Storage
}

func NewExecutor(s *storage.Storage) *Executor {
	return &Executor{store: s}
}

func (e *Executor) Execute(ctx context.Context, stmt *parser.Statement, txid int64) (*Result, error) {
	switch stmt.Type {
	case parser.StmtCreateTable:
		return e.executeCreateTable(ctx, stmt)
	case parser.StmtInsert:
		return e.executeInsert(ctx, stmt, txid)
	case parser.StmtSelect:
		return e.executeSelect(ctx, stmt, txid)
	case parser.StmtDelete:
		return e.executeDelete(ctx, stmt, txid)
	default:
		return nil, fmt.Errorf("executor: unsupported statement type")
	}
}

func (e *Executor) executeCreateTable(ctx context.Context, stmt *parser.Statement) (*Result, error) {
	ct := stmt.CreateTable
	if ct == nil {
		return nil, fmt.Errorf("executor: nil CreateTable")
	}

	cols := make([]storage.ColumnDef, len(ct.Columns))
	for i, c := range ct.Columns {
		cols[i] = storage.ColumnDef{
			Name:    c.Name,
			Ordinal: i,
			Type:    c.Type,
		}
	}

	if err := e.store.CreateTable(ctx, ct.Table, cols); err != nil {
		return nil, fmt.Errorf("executor: %w", err)
	}

	return &Result{Tag: "CREATE TABLE"}, nil
}

func (e *Executor) executeInsert(ctx context.Context, stmt *parser.Statement, txid int64) (*Result, error) {
	ins := stmt.Insert
	if ins == nil {
		return nil, fmt.Errorf("executor: nil Insert")
	}

	meta, err := e.store.GetTable(ctx, ins.Table)
	if err != nil {
		return nil, fmt.Errorf("executor: %w", err)
	}

	values := make(map[string]interface{})
	if len(ins.Columns) == 0 {
		for i, col := range meta.Columns {
			if i < len(ins.Values) {
				values[col.Name] = ins.Values[i]
			}
		}
	} else {
		for i, colName := range ins.Columns {
			if i < len(ins.Values) {
				values[colName] = ins.Values[i]
			}
		}
	}

	if _, err := e.store.InsertRow(ctx, meta, values, txid); err != nil {
		return nil, fmt.Errorf("executor: %w", err)
	}

	return &Result{Tag: "INSERT 0 1"}, nil
}

func (e *Executor) executeSelect(ctx context.Context, stmt *parser.Statement, txid int64) (*Result, error) {
	sel := stmt.Select
	if sel == nil {
		return nil, fmt.Errorf("executor: nil Select")
	}

	meta, err := e.store.GetTable(ctx, sel.Table)
	if err != nil {
		return nil, fmt.Errorf("executor: %w", err)
	}

	colMap := buildColumnMap(meta.Columns)
	outCols := resolveOutputColumns(sel.Columns, meta.Columns)

	ch, err := e.store.SeqScan(ctx, meta, txid)
	if err != nil {
		return nil, fmt.Errorf("executor: %w", err)
	}

	var rows [][]interface{}
	for result := range ch {
		if result.Err != nil {
			return nil, fmt.Errorf("executor: %w", result.Err)
		}
		row := result.Row
		if !evaluateWhere(row, sel.Where, colMap) {
			continue
		}

		rowData := make([]interface{}, len(outCols))
		for i, col := range outCols {
			key := fmt.Sprintf("c%d", col.Ordinal)
			rowData[i] = row[key]
		}
		rows = append(rows, rowData)
	}

	return &Result{
		Columns:     columnNames(outCols),
		ColumnTypes: columnTypes(outCols),
		Rows:        rows,
		Tag:         fmt.Sprintf("SELECT %d", len(rows)),
	}, nil
}

func (e *Executor) executeDelete(ctx context.Context, stmt *parser.Statement, txid int64) (*Result, error) {
	del := stmt.Delete
	if del == nil {
		return nil, fmt.Errorf("executor: nil Delete")
	}

	meta, err := e.store.GetTable(ctx, del.Table)
	if err != nil {
		return nil, fmt.Errorf("executor: %w", err)
	}

	colMap := buildColumnMap(meta.Columns)

	ch, err := e.store.SeqScan(ctx, meta, txid)
	if err != nil {
		return nil, fmt.Errorf("executor: %w", err)
	}

	count := 0
	for result := range ch {
		if result.Err != nil {
			return nil, fmt.Errorf("executor: %w", result.Err)
		}
		row := result.Row
		if !evaluateWhere(row, del.Where, colMap) {
			continue
		}

		pos, err := rowBlockPos(row)
		if err != nil {
			continue
		}

		if err := e.store.MarkDeleted(ctx, pos, txid); err != nil {
			return nil, fmt.Errorf("executor: %w", err)
		}
		count++
	}

	return &Result{Tag: fmt.Sprintf("DELETE %d", count)}, nil
}

func buildColumnMap(cols []storage.ColumnDef) map[string]int {
	m := make(map[string]int)
	for _, c := range cols {
		m[c.Name] = c.Ordinal
	}
	return m
}

func resolveOutputColumns(requested []string, defs []storage.ColumnDef) []storage.ColumnDef {
	if requested == nil {
		return defs
	}

	var out []storage.ColumnDef
	for _, name := range requested {
		for _, c := range defs {
			if c.Name == name {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

func columnTypes(cols []storage.ColumnDef) []string {
	types := make([]string, len(cols))
	for i, c := range cols {
		types[i] = c.Type
	}
	return types
}

func columnNames(cols []storage.ColumnDef) []string {
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
	}
	return names
}

func evaluateWhere(row storage.Row, conditions []parser.Condition, colMap map[string]int) bool {
	if len(conditions) == 0 {
		return true
	}

	for _, cond := range conditions {
		ordinal, ok := colMap[cond.Column]
		if !ok {
			return false
		}

		key := fmt.Sprintf("c%d", ordinal)
		rowVal := row[key]

		if !compareValue(rowVal, cond.Value, cond.Operator) {
			return false
		}
	}

	return true
}

func compareValue(rowVal, condVal interface{}, op string) bool {
	if rowVal == nil && condVal == nil {
		return op == "="
	}
	if rowVal == nil || condVal == nil {
		return op == "!="
	}

	rn, rOk := toFloat(rowVal)
	cn, cOk := toFloat(condVal)
	if rOk && cOk {
		return compareNum(rn, cn, op)
	}

	rs, rOk := rowVal.(string)
	cs, cOk := condVal.(string)
	if rOk && cOk {
		return compareStr(rs, cs, op)
	}

	rs2, rOk2 := rowVal.(string)
	cs2 := fmt.Sprintf("%v", condVal)
	if rOk2 {
		return compareStr(rs2, cs2, op)
	}

	return false
}

func toFloat(v interface{}) (float64, bool) {
	switch v := v.(type) {
	case float64:
		return v, true
	case int64:
		return float64(v), true
	case int:
		return float64(v), true
	case string:
		f, err := strconv.ParseFloat(v, 64)
		return f, err == nil
	}
	return 0, false
}

func compareNum(a, b float64, op string) bool {
	switch op {
	case "=":
		return a == b
	case ">":
		return a > b
	case "<":
		return a < b
	case ">=":
		return a >= b
	case "<=":
		return a <= b
	case "!=":
		return a != b
	}
	return false
}

func compareStr(a, b string, op string) bool {
	switch op {
	case "=":
		return a == b
	case ">":
		return a > b
	case "<":
		return a < b
	case ">=":
		return a >= b
	case "<=":
		return a <= b
	case "!=":
		return a != b
	}
	return false
}

func rowBlockPos(row storage.Row) (hal.BlockPos, error) {
	var pos hal.BlockPos

	x, ok := row["_x"]
	if !ok {
		return pos, fmt.Errorf("executor: row missing _x")
	}
	pos.X = toInt(x)

	y, ok := row["_y"]
	if !ok {
		return pos, fmt.Errorf("executor: row missing _y")
	}
	pos.Y = toInt(y)

	z, ok := row["_z"]
	if !ok {
		return pos, fmt.Errorf("executor: row missing _z")
	}
	pos.Z = toInt(z)

	return pos, nil
}

func toInt(v interface{}) int {
	switch v := v.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return 0
}
