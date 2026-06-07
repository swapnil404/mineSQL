package hal

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const (
	opWrite       byte = 0x01
	opRead        byte = 0x02
	opBatchRead   byte = 0x03
	opForceLoad   byte = 0x04
	opIsChunkLoad byte = 0x05
	opBatchWrite  byte = 0x06
	opAck         byte = 0x10
	opData        byte = 0x11
	opBatchData   byte = 0x12
	opError       byte = 0xFF
)

const (
	initialBackoff = 100 * time.Millisecond
	maxBackoff     = 30 * time.Second
	dialTimeout    = 5 * time.Second
)

type BlockPos struct {
	X, Y, Z int
}

type BlockWrite struct {
	Pos  BlockPos
	Data []byte
}

type Storage interface {
	ReadBlock(ctx context.Context, x, y, z int) ([]byte, error)
	WriteBlock(ctx context.Context, x, y, z int, data []byte) error
	BatchRead(ctx context.Context, positions []BlockPos) ([][]byte, error)
	BatchWrite(ctx context.Context, writes []BlockWrite) error
	ForceLoadChunk(ctx context.Context, chunkX, chunkZ int) error
	IsChunkLoaded(ctx context.Context, chunkX, chunkZ int) (bool, error)
}

type Client struct {
	addr string
	mu   sync.Mutex
	conn net.Conn
}

func NewClient(addr string) (*Client, error) {
	c := &Client{addr: addr}
	conn, err := net.DialTimeout("tcp", addr, dialTimeout)
	if err != nil {
		return nil, fmt.Errorf("hal connect: %w", err)
	}
	c.conn = conn
	return c, nil
}

func (c *Client) ReadBlock(ctx context.Context, x, y, z int) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.ensureConn(ctx); err != nil {
		return nil, err
	}

	payload := encodeInt32s(int32(x), int32(y), int32(z))
	if err := c.send(ctx, opRead, payload); err != nil {
		return nil, err
	}

	op, data, err := c.recv(ctx)
	if err != nil {
		return nil, err
	}

	switch op {
	case opData:
		return parseDataResponse(data)
	case opError:
		return nil, fmt.Errorf("plugin error: %s", parseErrorMessage(data))
	default:
		return nil, fmt.Errorf("hal: unexpected opcode 0x%02x for READ", op)
	}
}

func (c *Client) WriteBlock(ctx context.Context, x, y, z int, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.ensureConn(ctx); err != nil {
		return err
	}

	payload := make([]byte, 12+4+len(data))
	putInt32(payload[0:4], int32(x))
	putInt32(payload[4:8], int32(y))
	putInt32(payload[8:12], int32(z))
	binary.BigEndian.PutUint32(payload[12:16], uint32(len(data)))
	copy(payload[16:], data)

	if err := c.send(ctx, opWrite, payload); err != nil {
		return err
	}

	op, data, err := c.recv(ctx)
	if err != nil {
		return err
	}

	switch op {
	case opAck:
		return nil
	case opError:
		return fmt.Errorf("plugin error: %s", parseErrorMessage(data))
	default:
		return fmt.Errorf("hal: unexpected opcode 0x%02x for WRITE", op)
	}
}

func (c *Client) BatchRead(ctx context.Context, positions []BlockPos) ([][]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.ensureConn(ctx); err != nil {
		return nil, err
	}

	if len(positions) == 0 {
		return nil, nil
	}

	payload := make([]byte, 4+len(positions)*12)
	binary.BigEndian.PutUint32(payload[0:4], uint32(len(positions)))
	off := 4
	for _, p := range positions {
		putInt32(payload[off:off+4], int32(p.X))
		putInt32(payload[off+4:off+8], int32(p.Y))
		putInt32(payload[off+8:off+12], int32(p.Z))
		off += 12
	}

	if err := c.send(ctx, opBatchRead, payload); err != nil {
		return nil, err
	}

	op, data, err := c.recv(ctx)
	if err != nil {
		return nil, err
	}

	switch op {
	case opBatchData:
		return parseBatchDataResponse(data)
	case opError:
		return nil, fmt.Errorf("plugin error: %s", parseErrorMessage(data))
	default:
		return nil, fmt.Errorf("hal: unexpected opcode 0x%02x for BATCH_READ", op)
	}
}

func (c *Client) IsChunkLoaded(ctx context.Context, chunkX, chunkZ int) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.ensureConn(ctx); err != nil {
		return false, err
	}

	payload := encodeInt32s(int32(chunkX), int32(chunkZ))
	if err := c.send(ctx, opIsChunkLoad, payload); err != nil {
		return false, err
	}

	op, data, err := c.recv(ctx)
	if err != nil {
		return false, err
	}

	switch op {
	case opData:
		if len(data) < 1 {
			return false, fmt.Errorf("hal: short IS_CHUNK_LOADED response")
		}
		return data[0] == 0x01, nil
	case opError:
		return false, fmt.Errorf("plugin error: %s", parseErrorMessage(data))
	default:
		return false, fmt.Errorf("hal: unexpected opcode 0x%02x for IS_CHUNK_LOADED", op)
	}
}

func (c *Client) BatchWrite(ctx context.Context, writes []BlockWrite) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.ensureConn(ctx); err != nil {
		return err
	}

	if len(writes) == 0 {
		return nil
	}

	totalPayload := 4
	for _, w := range writes {
		totalPayload += 12 + 4 + len(w.Data)
	}
	payload := make([]byte, totalPayload)
	binary.BigEndian.PutUint32(payload[0:4], uint32(len(writes)))
	off := 4
	for _, w := range writes {
		putInt32(payload[off:off+4], int32(w.Pos.X))
		putInt32(payload[off+4:off+8], int32(w.Pos.Y))
		putInt32(payload[off+8:off+12], int32(w.Pos.Z))
		off += 12
		binary.BigEndian.PutUint32(payload[off:off+4], uint32(len(w.Data)))
		off += 4
		copy(payload[off:], w.Data)
		off += len(w.Data)
	}

	if err := c.send(ctx, opBatchWrite, payload); err != nil {
		return err
	}

	op, data, err := c.recv(ctx)
	if err != nil {
		return err
	}

	switch op {
	case opAck:
		return nil
	case opError:
		return fmt.Errorf("plugin error: %s", parseErrorMessage(data))
	default:
		return fmt.Errorf("hal: unexpected opcode 0x%02x for BATCH_WRITE", op)
	}
}

func (c *Client) ForceLoadChunk(ctx context.Context, chunkX, chunkZ int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.ensureConn(ctx); err != nil {
		return err
	}

	payload := encodeInt32s(int32(chunkX), int32(chunkZ))
	if err := c.send(ctx, opForceLoad, payload); err != nil {
		return err
	}

	op, data, err := c.recv(ctx)
	if err != nil {
		return err
	}

	switch op {
	case opAck:
		return nil
	case opError:
		return fmt.Errorf("plugin error: %s", parseErrorMessage(data))
	default:
		return fmt.Errorf("hal: unexpected opcode 0x%02x for FORCE_LOAD", op)
	}
}

func (c *Client) ensureConn(ctx context.Context) error {
	if c.conn != nil {
		return nil
	}
	return c.connect(ctx)
}

func (c *Client) connect(ctx context.Context) error {
	backoff := initialBackoff
	for {
		d := net.Dialer{Timeout: dialTimeout}
		conn, err := d.DialContext(ctx, "tcp", c.addr)
		if err == nil {
			c.conn = conn
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

func (c *Client) disconnect() {
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

func (c *Client) send(ctx context.Context, opcode byte, payload []byte) error {
	if err := c.setDeadline(ctx); err != nil {
		return err
	}

	length := uint32(1 + len(payload))
	buf := make([]byte, 4+length)
	binary.BigEndian.PutUint32(buf[0:4], length)
	buf[4] = opcode
	copy(buf[5:], payload)

	_, err := c.conn.Write(buf)
	if err != nil {
		c.disconnect()
		return fmt.Errorf("hal send: %w", err)
	}
	return nil
}

func (c *Client) recv(ctx context.Context) (byte, []byte, error) {
	if err := c.setDeadline(ctx); err != nil {
		return 0, nil, err
	}

	var lenBuf [4]byte
	if _, err := io.ReadFull(c.conn, lenBuf[:]); err != nil {
		c.disconnect()
		return 0, nil, fmt.Errorf("hal recv header: %w", err)
	}
	length := binary.BigEndian.Uint32(lenBuf[:])

	if length == 0 {
		return 0, nil, nil
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(c.conn, data); err != nil {
		c.disconnect()
		return 0, nil, fmt.Errorf("hal recv body: %w", err)
	}

	return data[0], data[1:], nil
}

func (c *Client) setDeadline(ctx context.Context) error {
	if c.conn == nil {
		return fmt.Errorf("hal: not connected")
	}

	if deadline, ok := ctx.Deadline(); ok {
		return c.conn.SetDeadline(deadline)
	}
	return c.conn.SetDeadline(time.Time{})
}

func encodeInt32s(vals ...int32) []byte {
	buf := make([]byte, len(vals)*4)
	for i, v := range vals {
		putInt32(buf[i*4:i*4+4], v)
	}
	return buf
}

func putInt32(buf []byte, v int32) {
	binary.BigEndian.PutUint32(buf, uint32(v))
}

func parseDataResponse(payload []byte) ([]byte, error) {
	if len(payload) < 4 {
		return nil, fmt.Errorf("hal: short DATA response")
	}
	dataLen := binary.BigEndian.Uint32(payload[0:4])
	if dataLen == 0 {
		return nil, nil
	}
	if uint32(len(payload)) < 4+dataLen {
		return nil, fmt.Errorf("hal: truncated DATA response")
	}
	return payload[4 : 4+dataLen], nil
}

func parseBatchDataResponse(payload []byte) ([][]byte, error) {
	if len(payload) < 4 {
		return nil, fmt.Errorf("hal: short BATCH_DATA response")
	}
	count := binary.BigEndian.Uint32(payload[0:4])
	results := make([][]byte, count)
	offset := 4
	for i := uint32(0); i < count; i++ {
		if offset+4 > len(payload) {
			return nil, fmt.Errorf("hal: truncated BATCH_DATA at entry %d", i)
		}
		dataLen := binary.BigEndian.Uint32(payload[offset : offset+4])
		offset += 4
		if dataLen > 0 {
			if offset+int(dataLen) > len(payload) {
				return nil, fmt.Errorf("hal: truncated BATCH_DATA data at entry %d", i)
			}
			results[i] = make([]byte, dataLen)
			copy(results[i], payload[offset:offset+int(dataLen)])
			offset += int(dataLen)
		}
	}
	return results, nil
}

func parseErrorMessage(payload []byte) string {
	if len(payload) < 4 {
		return "unknown error"
	}
	msgLen := binary.BigEndian.Uint32(payload[0:4])
	if msgLen > 0 && 4+int(msgLen) <= len(payload) {
		return string(payload[4 : 4+msgLen])
	}
	return "unknown error"
}
