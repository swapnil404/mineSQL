package wire

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"

	"github.com/jackc/pgproto3/v2"
)

type Server struct {
	addr string
}

func NewServer(addr string) *Server {
	return &Server{addr: addr}
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
			if m.String == "SELECT 1" {
				s.sendSelectOne(backend)
			} else {
				s.sendNotImplemented(backend)
			}
			if err := backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'}); err != nil {
				log.Printf("send ReadyForQuery: %v", err)
				return
			}
		case *pgproto3.Terminate:
			return
		default:
			s.sendNotImplemented(backend)
			if err := backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'}); err != nil {
				log.Printf("send ReadyForQuery: %v", err)
				return
			}
		}
	}
}

func (s *Server) startupHandshake(conn net.Conn, backend *pgproto3.Backend) bool {
	for {
		msg, err := backend.ReceiveStartupMessage()
		if err != nil {
			log.Printf("startup error: %v", err)
			return false
		}

		switch msg.(type) {
		case *pgproto3.SSLRequest, *pgproto3.GSSEncRequest:
			conn.Write([]byte("N"))
			continue
		case *pgproto3.CancelRequest:
			return false
		case *pgproto3.StartupMessage:
			break
		}

		break
	}

	backend.Send(&pgproto3.AuthenticationOk{})
	backend.Send(&pgproto3.ParameterStatus{Name: "server_version", Value: "15.0"})
	backend.Send(&pgproto3.ParameterStatus{Name: "client_encoding", Value: "UTF8"})
	backend.Send(&pgproto3.ParameterStatus{Name: "standard_conforming_strings", Value: "on"})
	backend.Send(&pgproto3.BackendKeyData{ProcessID: 1, SecretKey: 0})
	backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})

	return true
}

func (s *Server) sendSelectOne(backend *pgproto3.Backend) {
	backend.Send(&pgproto3.RowDescription{
		Fields: []pgproto3.FieldDescription{
			{
				Name:                 []byte("?column?"),
				TableOID:             0,
				TableAttributeNumber: 0,
				DataTypeOID:          23,
				DataTypeSize:         4,
				TypeModifier:         -1,
				Format:               0,
			},
		},
	})
	backend.Send(&pgproto3.DataRow{
		Values: [][]byte{[]byte("1")},
	})
	backend.Send(&pgproto3.CommandComplete{
		CommandTag: []byte("SELECT 1"),
	})
}

func (s *Server) sendNotImplemented(backend *pgproto3.Backend) {
	backend.Send(&pgproto3.ErrorResponse{
		Severity: "ERROR",
		Code:     "0A000",
		Message:  "not implemented",
	})
}
