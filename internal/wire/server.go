package wire

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"

	"github.com/jackc/pgproto3/v2"

	"github.com/swapnil404/minesql/internal/executor"
	"github.com/swapnil404/minesql/internal/parser"
)

type Server struct {
	addr string
	exec *executor.Executor
}

func NewServer(addr string, exec *executor.Executor) *Server {
	return &Server{addr: addr, exec: exec}
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	lc := net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer ln.Close()

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				if errors.Is(err, net.ErrClosed) {
					return nil
				}
				log.Printf("accept error: %v", err)
				continue
			}
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	backend := pgproto3.NewBackend(pgproto3.NewChunkReader(conn), conn)

	if !s.startupHandshake(conn, backend) {
		return
	}

	for {
		msg, err := backend.Receive()
		if err != nil {
			log.Printf("receive: %v", err)
			return
		}

		switch m := msg.(type) {
		case *pgproto3.Query:
			s.handleQuery(ctx, backend, m.String)
		case *pgproto3.Terminate:
			return
		case *pgproto3.PasswordMessage:
			continue
		default:
			s.sendError(backend, "0A000", "not implemented")
			backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
		}
	}
}

func (s *Server) handleQuery(ctx context.Context, backend *pgproto3.Backend, sql string) {
	stmt, err := parser.Parse(sql)
	if err != nil {
		s.sendError(backend, "42601", "syntax error: "+err.Error())
		backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
		return
	}

	result, err := s.exec.Execute(ctx, stmt, 1)
	if err != nil {
		s.sendError(backend, "0A000", err.Error())
		backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
		return
	}

	switch stmt.Type {
	case parser.StmtSelect:
		s.sendSelectResult(backend, result)
	case parser.StmtInsert, parser.StmtCreateTable, parser.StmtDelete:
		backend.Send(&pgproto3.CommandComplete{
			CommandTag: []byte(result.Tag),
		})
	}

	backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
}

func (s *Server) sendSelectResult(backend *pgproto3.Backend, result *executor.Result) {
	fields := make([]pgproto3.FieldDescription, len(result.Columns))
	for i, name := range result.Columns {
		oid, size := typeToOID(result.ColumnTypes[i])
		fields[i] = pgproto3.FieldDescription{
			Name:                 []byte(name),
			TableOID:             0,
			TableAttributeNumber: 0,
			DataTypeOID:          oid,
			DataTypeSize:         size,
			TypeModifier:         -1,
			Format:               0,
		}
	}
	backend.Send(&pgproto3.RowDescription{Fields: fields})

	for _, row := range result.Rows {
		values := make([][]byte, len(row))
		for i, val := range row {
			values[i] = encodeValue(val)
		}
		backend.Send(&pgproto3.DataRow{Values: values})
	}

	backend.Send(&pgproto3.CommandComplete{
		CommandTag: []byte(result.Tag),
	})
}

func (s *Server) startupHandshake(conn net.Conn, backend *pgproto3.Backend) bool {
handshake:
	for {
		msg, err := backend.ReceiveStartupMessage()
		if err != nil {
			log.Printf("startup error: %v", err)
			return false
		}

		switch msg.(type) {
		case *pgproto3.SSLRequest, *pgproto3.GSSEncRequest:
			if _, err := conn.Write([]byte("N")); err != nil {
				log.Printf("ssl reject write: %v", err)
				return false
			}
			continue
		case *pgproto3.CancelRequest:
			return false
		case *pgproto3.StartupMessage:
			break handshake
		}
	}

	backend.Send(&pgproto3.AuthenticationOk{})
	backend.SetAuthType(0)
	backend.Send(&pgproto3.ParameterStatus{Name: "server_version", Value: "15.0"})
	backend.Send(&pgproto3.ParameterStatus{Name: "client_encoding", Value: "UTF8"})
	backend.Send(&pgproto3.ParameterStatus{Name: "standard_conforming_strings", Value: "on"})
	backend.Send(&pgproto3.BackendKeyData{ProcessID: 1, SecretKey: 0})
	backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})

	return true
}

func (s *Server) sendError(backend *pgproto3.Backend, code, message string) {
	backend.Send(&pgproto3.ErrorResponse{
		Severity: "ERROR",
		Code:     code,
		Message:  message,
	})
}

func typeToOID(typeName string) (uint32, int16) {
	switch typeName {
	case "int4", "INT", "int", "integer":
		return 23, 4
	case "int8", "BIGINT", "bigint":
		return 20, 8
	case "int2", "SMALLINT", "smallint":
		return 21, 2
	case "float4", "real":
		return 700, 4
	case "float8", "FLOAT", "float", "double precision":
		return 701, 8
	case "bool", "BOOLEAN", "boolean":
		return 16, 1
	default:
		return 25, -1
	}
}

func encodeValue(val interface{}) []byte {
	if val == nil {
		return nil
	}
	switch v := val.(type) {
	case []byte:
		return v
	case string:
		return []byte(v)
	default:
		return []byte(fmt.Sprintf("%v", v))
	}
}
