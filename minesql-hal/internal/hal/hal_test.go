package hal

import (
	"encoding/binary"
	"io"
	"net"
	"os"
	"testing"
)

const (
	opWrite     = 0x01
	opRead      = 0x02
	opBatchRead = 0x03
	opAck       = 0x10
	opData      = 0x11
	opBatchData = 0x12

	blockTypeBarrel    = 0x00
	blockTypeBanner    = 0x01
	blockTypeWallSign  = 0x02
	blockTypeLectern   = 0x03
)

func addr() string {
	if a := os.Getenv("MINESQL_MINECRAFT_ADDR"); a != "" {
		return a
	}
	return "localhost:25576"
}

func TestRoundTrip(t *testing.T) {
	if os.Getenv("MINESQL_MINECRAFT_ADDR") == "" {
		t.Skip("MINESQL_MINECRAFT_ADDR not set, skipping integration test")
	}

	conn, err := net.Dial("tcp", addr())
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	want := `{"xmin":1,"xmax":null,"c0":"swapnil"}`

	sendWrite(t, conn, 10, 64, 10, blockTypeBarrel, want)
	readAck(t, conn)

	sendRead(t, conn, 10, 64, 10)
	got := readData(t, conn)

	if got != want {
		t.Errorf("data mismatch\n  want: %s\n  got:  %s", want, got)
	}
}

func sendWrite(t *testing.T, conn net.Conn, x, y, z int32, blockType byte, data string) {
	t.Helper()
	dataLen := uint32(len(data))

	packetLen := uint32(1 + 4 + 4 + 4 + 1 + 4 + dataLen)

	buf := make([]byte, 0, 4+packetLen)
	buf = binary.BigEndian.AppendUint32(buf, packetLen)
	buf = append(buf, opWrite)
	buf = binary.BigEndian.AppendUint32(buf, uint32(x))
	buf = binary.BigEndian.AppendUint32(buf, uint32(y))
	buf = binary.BigEndian.AppendUint32(buf, uint32(z))
	buf = append(buf, blockType)
	buf = binary.BigEndian.AppendUint32(buf, dataLen)
	buf = append(buf, []byte(data)...)

	if _, err := conn.Write(buf); err != nil {
		t.Fatalf("write failed: %v", err)
	}
}

func readAck(t *testing.T, conn net.Conn) {
	t.Helper()
	packetLen := readUint32(t, conn)
	if packetLen != 1 {
		t.Fatalf("expected ack packet length 1, got %d", packetLen)
	}
	opcode := readByte(t, conn)
	if opcode != opAck {
		t.Fatalf("expected opcode 0x10, got 0x%02X", opcode)
	}
}

func sendRead(t *testing.T, conn net.Conn, x, y, z int32) {
	t.Helper()
	packetLen := uint32(1 + 4 + 4 + 4)

	buf := make([]byte, 0, 4+packetLen)
	buf = binary.BigEndian.AppendUint32(buf, packetLen)
	buf = append(buf, opRead)
	buf = binary.BigEndian.AppendUint32(buf, uint32(x))
	buf = binary.BigEndian.AppendUint32(buf, uint32(y))
	buf = binary.BigEndian.AppendUint32(buf, uint32(z))

	if _, err := conn.Write(buf); err != nil {
		t.Fatalf("write failed: %v", err)
	}
}

func readData(t *testing.T, conn net.Conn) string {
	t.Helper()
	_ = readUint32(t, conn) // packet length

	opcode := readByte(t, conn)
	if opcode != opData {
		t.Fatalf("expected opcode 0x11, got 0x%02X", opcode)
	}

	dataLen := readUint32(t, conn)
	if dataLen == 0 {
		return ""
	}

	buf := make([]byte, dataLen)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("failed to read data: %v", err)
	}
	return string(buf)
}

func TestBatchRead(t *testing.T) {
	if os.Getenv("MINESQL_MINECRAFT_ADDR") == "" {
		t.Skip("MINESQL_MINECRAFT_ADDR not set, skipping integration test")
	}

	conn, err := net.Dial("tcp", addr())
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	row0 := `{"a":1,"b":"hello"}`
	row1 := `{"c":2,"d":"world"}`

	sendWrite(t, conn, 20, 64, 20, blockTypeBarrel, row0)
	readAck(t, conn)
	sendWrite(t, conn, 20, 64, 21, blockTypeBarrel, row1)
	readAck(t, conn)

	sendBatchRead(t, conn, [][3]int32{
		{20, 64, 20},
		{20, 64, 21},
		{20, 64, 22},
	})
	results := readBatchData(t, conn, 3)

	if results[0] != row0 {
		t.Errorf("entry 0 mismatch\n  want: %s\n  got:  %s", row0, results[0])
	}
	if results[1] != row1 {
		t.Errorf("entry 1 mismatch\n  want: %s\n  got:  %s", row1, results[1])
	}
	if results[2] != "" {
		t.Errorf("entry 2 expected empty, got: %s", results[2])
	}
}

func sendBatchRead(t *testing.T, conn net.Conn, positions [][3]int32) {
	t.Helper()
	count := uint32(len(positions))
	packetLen := uint32(1 + 4 + count*4*3)

	buf := make([]byte, 0, 4+packetLen)
	buf = binary.BigEndian.AppendUint32(buf, packetLen)
	buf = append(buf, opBatchRead)
	buf = binary.BigEndian.AppendUint32(buf, count)

	for _, p := range positions {
		buf = binary.BigEndian.AppendUint32(buf, uint32(p[0]))
		buf = binary.BigEndian.AppendUint32(buf, uint32(p[1]))
		buf = binary.BigEndian.AppendUint32(buf, uint32(p[2]))
	}

	if _, err := conn.Write(buf); err != nil {
		t.Fatalf("write failed: %v", err)
	}
}

func readBatchData(t *testing.T, conn net.Conn, expectedCount int) []string {
	t.Helper()
	_ = readUint32(t, conn) // packet length

	opcode := readByte(t, conn)
	if opcode != opBatchData {
		t.Fatalf("expected opcode 0x12, got 0x%02X", opcode)
	}

	count := readUint32(t, conn)
	if int(count) != expectedCount {
		t.Fatalf("expected count %d, got %d", expectedCount, count)
	}

	results := make([]string, count)
	for i := uint32(0); i < count; i++ {
		dataLen := readUint32(t, conn)
		if dataLen == 0 {
			results[i] = ""
			continue
		}
		buf := make([]byte, dataLen)
		if _, err := io.ReadFull(conn, buf); err != nil {
			t.Fatalf("failed to read batch entry %d: %v", i, err)
		}
		results[i] = string(buf)
	}
	return results
}

func readUint32(t *testing.T, r io.Reader) uint32 {
	t.Helper()
	var buf [4]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		t.Fatalf("failed to read uint32: %v", err)
	}
	return binary.BigEndian.Uint32(buf[:])
}

func readByte(t *testing.T, r io.Reader) byte {
	t.Helper()
	var buf [1]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		t.Fatalf("failed to read byte: %v", err)
	}
	return buf[0]
}
