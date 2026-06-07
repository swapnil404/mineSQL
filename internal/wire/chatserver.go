package wire

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"

	"github.com/swapnil404/minesql/internal/executor"
	"github.com/swapnil404/minesql/internal/parser"
)

type chatResponse struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
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

	var length uint32
	if err := binary.Read(conn, binary.BigEndian, &length); err != nil {
		log.Printf("chat read length: %v", err)
		return
	}

	if length > 10*1024*1024 {
		s.writeError(conn, "query too large")
		return
	}

	query := make([]byte, length)
	if _, err := io.ReadFull(conn, query); err != nil {
		log.Printf("chat read query: %v", err)
		return
	}

	stmt, err := parser.Parse(string(query))
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
		Columns: result.Columns,
		Rows:    rowsToStrings(result.Rows),
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
	conn.Write([]byte{0x00})
	binary.Write(conn, binary.BigEndian, uint32(len(data)))
	conn.Write(data)
}

func (s *ChatServer) writeError(conn net.Conn, message string) {
	msg := []byte(message)
	conn.Write([]byte{0xFF})
	binary.Write(conn, binary.BigEndian, uint32(len(msg)))
	conn.Write(msg)
}

func isClosed(err error) bool {
	if err == nil {
		return false
	}
	if opErr, ok := err.(*net.OpError); ok {
		return opErr.Err.Error() == "use of closed network connection"
	}
	return err.Error() == "use of closed network connection"
}
