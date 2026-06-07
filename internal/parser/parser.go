package parser

import (
	"fmt"

	pg_query "github.com/pganalyze/pg_query_go/v5"
)

type StatementType int

const (
	StmtSelect StatementType = iota
	StmtInsert
	StmtCreateTable
	StmtDelete
)

type Statement struct {
	Type        StatementType
	Select      *SelectStmt
	Insert      *InsertStmt
	CreateTable *CreateTableStmt
	Delete      *DeleteStmt
}

type SelectStmt struct {
	Table   string
	Columns []string
	Where   []Condition
}

type InsertStmt struct {
	Table   string
	Columns []string
	Values  []interface{}
}

type CreateTableStmt struct {
	Table   string
	Columns []ColumnDef
}

type ColumnDef struct {
	Name string
	Type string
}

type DeleteStmt struct {
	Table string
	Where []Condition
}

type Condition struct {
	Column   string
	Operator string
	Value    interface{}
}

func Parse(sql string) (*Statement, error) {
	tree, err := pg_query.Parse(sql)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	if len(tree.Stmts) == 0 {
		return nil, fmt.Errorf("parse: no statements found")
	}

	raw := tree.Stmts[0]
	node := raw.GetStmt()

	switch {
	case node.GetSelectStmt() != nil:
		return parseSelect(node.GetSelectStmt())
	case node.GetInsertStmt() != nil:
		return parseInsert(node.GetInsertStmt())
	case node.GetCreateStmt() != nil:
		return parseCreateTable(node.GetCreateStmt())
	case node.GetDeleteStmt() != nil:
		return parseDelete(node.GetDeleteStmt())
	default:
		return nil, fmt.Errorf("parse: unsupported statement type")
	}
}

func parseSelect(stmt *pg_query.SelectStmt) (*Statement, error) {
	s := &Statement{Type: StmtSelect, Select: &SelectStmt{}}

	if len(stmt.FromClause) > 0 {
		rv := stmt.FromClause[0].GetRangeVar()
		if rv != nil {
			s.Select.Table = rv.Relname
		}
	}

	s.Select.Columns = parseTargetList(stmt.TargetList)
	s.Select.Where = parseWhereClause(stmt.WhereClause)

	return s, nil
}

func parseInsert(stmt *pg_query.InsertStmt) (*Statement, error) {
	s := &Statement{Type: StmtInsert, Insert: &InsertStmt{}}

	if stmt.Relation != nil {
		s.Insert.Table = stmt.Relation.Relname
	}

	for _, col := range stmt.Cols {
		rt := col.GetResTarget()
		if rt != nil {
			s.Insert.Columns = append(s.Insert.Columns, rt.Name)
		}
	}

	sel := stmt.SelectStmt.GetSelectStmt()
	if sel != nil && len(sel.ValuesLists) > 0 {
		list := sel.ValuesLists[0].GetList()
		if list != nil {
			for _, item := range list.Items {
				s.Insert.Values = append(s.Insert.Values, extractConstValue(item))
			}
		}
	}

	return s, nil
}

func parseCreateTable(stmt *pg_query.CreateStmt) (*Statement, error) {
	s := &Statement{Type: StmtCreateTable, CreateTable: &CreateTableStmt{}}

	if stmt.Relation != nil {
		s.CreateTable.Table = stmt.Relation.Relname
	}

	for _, elt := range stmt.TableElts {
		cd := elt.GetColumnDef()
		if cd == nil {
			continue
		}

		colDef := ColumnDef{Name: cd.Colname}

		if cd.TypeName != nil && len(cd.TypeName.Names) > 0 {
			colDef.Type = extractTypeName(cd.TypeName.Names)
		}

		s.CreateTable.Columns = append(s.CreateTable.Columns, colDef)
	}

	return s, nil
}

func parseDelete(stmt *pg_query.DeleteStmt) (*Statement, error) {
	s := &Statement{Type: StmtDelete, Delete: &DeleteStmt{}}

	if stmt.Relation != nil {
		s.Delete.Table = stmt.Relation.Relname
	}

	s.Delete.Where = parseWhereClause(stmt.WhereClause)

	return s, nil
}

func parseTargetList(targets []*pg_query.Node) []string {
	var cols []string

	for _, t := range targets {
		rt := t.GetResTarget()
		if rt == nil {
			continue
		}

		if rt.Val != nil && rt.Val.GetAStar() != nil {
			return nil
		}

		name := rt.Name
		if name == "" {
			cr := rt.Val.GetColumnRef()
			if cr != nil {
				name = extractStringList(cr.Fields)
			}
		}
		if name != "" {
			cols = append(cols, name)
		}
	}

	return cols
}

func parseWhereClause(where *pg_query.Node) []Condition {
	if where == nil {
		return nil
	}

	var conds []Condition

	be := where.GetBoolExpr()
	if be != nil && be.Boolop == pg_query.BoolExprType_AND_EXPR {
		for _, arg := range be.Args {
			c := parseCondition(arg)
			if c != nil {
				conds = append(conds, *c)
			}
		}
		return conds
	}

	c := parseCondition(where)
	if c != nil {
		conds = append(conds, *c)
	}

	return conds
}

func parseCondition(node *pg_query.Node) *Condition {
	ae := node.GetAExpr()
	if ae == nil {
		return nil
	}

	if ae.Kind != pg_query.A_Expr_Kind_AEXPR_OP {
		return nil
	}

	c := &Condition{}

	c.Operator = extractStringList(ae.Name)

	if ae.Lexpr != nil {
		cr := ae.Lexpr.GetColumnRef()
		if cr != nil {
			c.Column = extractStringList(cr.Fields)
		}
	}

	if ae.Rexpr != nil {
		c.Value = extractConstValue(ae.Rexpr)
	}

	return c
}

func extractConstValue(node *pg_query.Node) interface{} {
	c := node.GetAConst()
	if c == nil {
		return nil
	}

	if c.Isnull {
		return nil
	}

	if ival := c.GetIval(); ival != nil {
		return int64(ival.Ival)
	}
	if sval := c.GetSval(); sval != nil {
		return sval.Sval
	}
	if fval := c.GetFval(); fval != nil {
		return fval.Fval
	}
	if bval := c.GetBoolval(); bval != nil {
		return bval.Boolval
	}

	return nil
}

func extractTypeName(nodes []*pg_query.Node) string {
	if len(nodes) == 0 {
		return ""
	}
	last := nodes[len(nodes)-1]
	s := last.GetString_()
	if s != nil {
		return s.Sval
	}
	return ""
}

func extractStringList(nodes []*pg_query.Node) string {
	var result string
	for i, n := range nodes {
		s := n.GetString_()
		if s != nil {
			if i > 0 {
				result += "."
			}
			result += s.Sval
		}
	}
	return result
}
