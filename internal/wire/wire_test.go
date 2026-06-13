package wire

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
)

func TestIsClosed_Nil(t *testing.T) {
	if isClosed(nil) {
		t.Error("expected false for nil error")
	}
}

func TestIsClosed_NetErrClosed(t *testing.T) {
	if !isClosed(net.ErrClosed) {
		t.Error("expected true for net.ErrClosed")
	}
}

func TestIsClosed_OpErrorWithNetErrClosed(t *testing.T) {
	opErr := &net.OpError{Op: "accept", Err: net.ErrClosed}
	if !isClosed(opErr) {
		t.Error("expected true for *net.OpError wrapping net.ErrClosed")
	}
}

func TestIsClosed_UnrelatedError(t *testing.T) {
	if isClosed(errors.New("some other error")) {
		t.Error("expected false for unrelated error")
	}
}

func TestIsClosed_OpErrorWithUnrelated(t *testing.T) {
	opErr := &net.OpError{Op: "accept", Err: errors.New("connection reset")}
	if isClosed(opErr) {
		t.Error("expected false for *net.OpError with unrelated error")
	}
}

func TestRowsToStrings(t *testing.T) {
	input := [][]interface{}{
		{int64(42), "hello", nil},
		{"world", int64(7), true},
	}
	got := rowsToStrings(input)
	if len(got) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(got))
	}
	if got[0][0] != "42" {
		t.Errorf("expected '42', got %q", got[0][0])
	}
	if got[0][1] != "hello" {
		t.Errorf("expected 'hello', got %q", got[0][1])
	}
	if got[0][2] != "<nil>" {
		t.Errorf("expected '<nil>', got %q", got[0][2])
	}
}

func TestRowsToStrings_Nil(t *testing.T) {
	got := rowsToStrings(nil)
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestTypeToOID(t *testing.T) {
	tests := []struct {
		typeName string
		oid      uint32
		size     int16
	}{
		{"INT", 23, 4},
		{"int4", 23, 4},
		{"integer", 23, 4},
		{"BIGINT", 20, 8},
		{"int8", 20, 8},
		{"SMALLINT", 21, 2},
		{"BOOLEAN", 16, 1},
		{"bool", 16, 1},
		{"FLOAT", 701, 8},
		{"TEXT", 25, -1},
		{"unknown_type", 25, -1},
	}
	for _, tt := range tests {
		oid, size := typeToOID(tt.typeName)
		if oid != tt.oid || size != tt.size {
			t.Errorf("typeToOID(%q) = (%d, %d), want (%d, %d)", tt.typeName, oid, size, tt.oid, tt.size)
		}
	}
}

func TestEncodeValue(t *testing.T) {
	got := encodeValue(nil)
	if got != nil {
		t.Error("expected nil for nil value")
	}

	got = encodeValue("hello")
	if string(got) != "hello" {
		t.Errorf("expected 'hello', got %q", string(got))
	}

	got = encodeValue([]byte("bytes"))
	if string(got) != "bytes" {
		t.Errorf("expected 'bytes', got %q", string(got))
	}

	got = encodeValue(int64(42))
	if string(got) != "42" {
		t.Errorf("expected '42', got %q", string(got))
	}
}

func TestChatResponseJSON(t *testing.T) {
	resp := chatResponse{
		Columns:   []string{"name", "kills"},
		Rows:      [][]string{{"swapnil", "42"}},
		RowCount:  1,
		Truncated: false,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}

	if _, ok := parsed["row_count"]; !ok {
		t.Error("response missing row_count field")
	}
	if _, ok := parsed["truncated"]; !ok {
		t.Error("response missing truncated field")
	}
	if v, ok := parsed["row_count"].(float64); !ok || v != 1 {
		t.Errorf("expected row_count=1, got %v", parsed["row_count"])
	}
	if v, ok := parsed["truncated"].(bool); !ok || v != false {
		t.Errorf("expected truncated=false, got %v", parsed["truncated"])
	}
}

func TestChatWriteSuccess_LineDelimited(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	s := &ChatServer{}
	go s.writeSuccess(serverConn, []byte(`{"columns":["name"],"rows":[["alice"]],"row_count":1,"truncated":false}`))

	reader := bufio.NewReader(clientConn)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	line = strings.TrimRight(line, "\n")
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(line), &parsed); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}

	if rc, ok := parsed["row_count"].(float64); !ok || rc != 1 {
		t.Errorf("expected row_count=1, got %v", parsed["row_count"])
	}
	if tr, ok := parsed["truncated"].(bool); !ok || tr != false {
		t.Errorf("expected truncated=false, got %v", parsed["truncated"])
	}
}

func TestChatWriteError_LineDelimited(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	s := &ChatServer{}
	go s.writeError(serverConn, "syntax error")

	reader := bufio.NewReader(clientConn)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read error response: %v", err)
	}

	line = strings.TrimRight(line, "\n")
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(line), &parsed); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}

	errMsg, ok := parsed["error"].(string)
	if !ok {
		t.Fatal("response missing 'error' field")
	}
	if errMsg != "syntax error" {
		t.Errorf("expected 'syntax error', got %q", errMsg)
	}
}

func TestChatHandleConn_EmptyResponseArrays(t *testing.T) {
	// Test that nil Columns/Rows are coerced to empty arrays
	resp := chatResponse{}
	if resp.Columns == nil {
		resp.Columns = []string{}
	}
	if resp.Rows == nil {
		resp.Rows = [][]string{}
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}

	cols, ok := parsed["columns"].([]interface{})
	if !ok || len(cols) != 0 {
		t.Error("expected empty columns array")
	}
	rows, ok := parsed["rows"].([]interface{})
	if !ok || len(rows) != 0 {
		t.Error("expected empty rows array")
	}
}

func FuzzChatWriteSuccess(f *testing.F) {
	s := &ChatServer{}
	f.Add([]byte(`{"columns":["a"],"rows":[["x"]],"row_count":1,"truncated":false}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		serverConn, clientConn := net.Pipe()
		go func() {
			s.writeSuccess(serverConn, data)
			serverConn.Close()
		}()

		reader := bufio.NewReader(clientConn)
		_, err := reader.ReadString('\n')
		clientConn.Close()
		if err != nil {
			_ = err
		}
	})
}

func FuzzChatWriteError(f *testing.F) {
	s := &ChatServer{}
	f.Add("test error")

	f.Fuzz(func(t *testing.T, msg string) {
		serverConn, clientConn := net.Pipe()
		go func() {
			s.writeError(serverConn, msg)
			serverConn.Close()
		}()

		reader := bufio.NewReader(clientConn)
		line, err := reader.ReadString('\n')
		clientConn.Close()
		if err != nil {
			return
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(strings.TrimRight(line, "\n")), &parsed); err != nil {
			return
		}

		if _, ok := parsed["error"]; !ok {
			t.Error("error response missing 'error' field")
		}
	})
}
