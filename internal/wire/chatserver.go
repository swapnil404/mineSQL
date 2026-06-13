package wire

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"

	"github.com/swapnil404/minesql/internal/executor"
	"github.com/swapnil404/minesql/internal/parser"
)

type chatResponse struct {
	Columns   []string   `json:"columns"`
	Rows      [][]string `json:"rows"`
	RowCount  int        `json:"row_count"`
	Truncated bool       `json:"truncated"`
}

type ChatServer struct {
	addr string
	exec *executor.Executor
}

func NewChatServer(addr string, exec *executor.Executor) *ChatServer {
	return &ChatServer{addr: addr, exec: exec}
}

func (s *ChatServer) ListenAndServe(ctx context.Context) error {
	lc := net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", s.addr)
	if err != nil {
		return fmt.Errorf("chat listen: %w", err)
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
				if isClosed(err) {
					return nil
				}
				log.Printf("chat accept error: %v", err)
				continue
			}
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *ChatServer) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	query, err := reader.ReadString('\n')
	if err != nil {
		log.Printf("chat read query: %v", err)
		return
	}

	if len(query) > 10*1024*1024 {
		s.writeError(conn, "query too large")
		return
	}

	stmt, err := parser.Parse(query)
	if err != nil {
		s.writeError(conn, "syntax error: "+err.Error())
		return
	}

	result, err := s.exec.Execute(ctx, stmt, 1)
	if err != nil {
		s.writeError(conn, err.Error())
		return
	}

	resp := chatResponse{
		Columns:   result.Columns,
		Rows:      rowsToStrings(result.Rows),
		RowCount:  len(result.Rows),
		Truncated: false,
	}
	if resp.Columns == nil {
		resp.Columns = []string{}
	}
	if resp.Rows == nil {
		resp.Rows = [][]string{}
	}

	data, err := json.Marshal(resp)
	if err != nil {
		s.writeError(conn, "json marshal error: "+err.Error())
		return
	}

	s.writeSuccess(conn, data)
}

func rowsToStrings(rows [][]interface{}) [][]string {
	if rows == nil {
		return nil
	}
	out := make([][]string, len(rows))
	for i, row := range rows {
		out[i] = make([]string, len(row))
		for j, val := range row {
			out[i][j] = fmt.Sprintf("%v", val)
		}
	}
	return out
}

func (s *ChatServer) writeSuccess(conn net.Conn, data []byte) {
	conn.Write(data)
	conn.Write([]byte("\n"))
}

func (s *ChatServer) writeError(conn net.Conn, message string) {
	resp := map[string]string{"error": message}
	data, _ := json.Marshal(resp)
	conn.Write(data)
	conn.Write([]byte("\n"))
}

func isClosed(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, net.ErrClosed)
}
