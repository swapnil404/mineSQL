package storage

import (
	"math"
	"strings"
	"testing"
)

func TestEncodeInt32Roundtrip(t *testing.T) {
	tests := []int32{0, 1, -1, math.MaxInt32, math.MinInt32}
	for _, v := range tests {
		enc := EncodeInt32(v)
		dec, err := DecodeInt32(enc)
		if err != nil {
			t.Errorf("DecodeInt32(%q): %v", enc, err)
			continue
		}
		if dec != v {
			t.Errorf("EncodeInt32/DecodeInt32 roundtrip: got %d, want %d", dec, v)
		}
	}
}

func TestEncodeInt32Length(t *testing.T) {
	enc := EncodeInt32(42)
	if len(enc) != 12 {
		t.Errorf("EncodeInt32: expected 12-char hex string, got %q (len=%d)", enc, len(enc))
	}
}

func TestEncodeInt32ZeroPadding(t *testing.T) {
	enc := EncodeInt32(0)
	if enc != "000000000000" {
		t.Errorf("EncodeInt32(0): expected '000000000000', got %q", enc)
	}
	enc = EncodeInt32(1)
	if enc != "000000010000" {
		t.Errorf("EncodeInt32(1): expected '000000010000', got %q", enc)
	}
}

func TestEncodeInt32MinMax(t *testing.T) {
	enc := EncodeInt32(math.MaxInt32)
	dec, err := DecodeInt32(enc)
	if err != nil {
		t.Fatalf("DecodeInt32: %v", err)
	}
	if dec != math.MaxInt32 {
		t.Errorf("MaxInt32 roundtrip: got %d", dec)
	}

	enc = EncodeInt32(math.MinInt32)
	dec, err = DecodeInt32(enc)
	if err != nil {
		t.Fatalf("DecodeInt32: %v", err)
	}
	if dec != math.MinInt32 {
		t.Errorf("MinInt32 roundtrip: got %d", dec)
	}
}

func TestDecodeInt32InvalidHex(t *testing.T) {
	_, err := DecodeInt32("zzzzzzzzzzzz")
	if err == nil {
		t.Error("expected error for invalid hex")
	}
}

func TestDecodeInt32WrongLength(t *testing.T) {
	_, err := DecodeInt32("deadbeef")
	if err == nil {
		t.Error("expected error for wrong length")
	}
}

func TestEncodeInt64Roundtrip(t *testing.T) {
	tests := []int64{0, 1, -1, math.MaxInt64, math.MinInt64}
	for _, v := range tests {
		s1, s2 := EncodeInt64(v)
		if len(s1) != 12 || len(s2) != 12 {
			t.Errorf("EncodeInt64(%d): expected 12-char strings, got len=%d len=%d", v, len(s1), len(s2))
		}
		dec, err := DecodeInt64(s1, s2)
		if err != nil {
			t.Errorf("DecodeInt64(%q, %q): %v", s1, s2, err)
			continue
		}
		if dec != v {
			t.Errorf("EncodeInt64/DecodeInt64 roundtrip: got %d, want %d", dec, v)
		}
	}
}

func TestEncodeInt64Zero(t *testing.T) {
	s1, s2 := EncodeInt64(0)
	if s1 != "000000000000" || s2 != "000000000000" {
		t.Errorf("EncodeInt64(0): expected all zeros, got %q %q", s1, s2)
	}
}

func TestDecodeInt64InvalidHex(t *testing.T) {
	_, err := DecodeInt64("zzzzzzzzzzzz", "000000000000")
	if err == nil {
		t.Error("expected error for invalid hex in banner 1")
	}
	_, err = DecodeInt64("000000000000", "zzzzzzzzzzzz")
	if err == nil {
		t.Error("expected error for invalid hex in banner 2")
	}
}

func TestEncodeNull(t *testing.T) {
	s1, s2 := EncodeNull()
	if !IsNull(s1, s2) {
		t.Error("EncodeNull should satisfy IsNull")
	}
	if s1 != nullHex || s2 != nullHex {
		t.Errorf("EncodeNull: expected %q %q, got %q %q", nullHex, nullHex, s1, s2)
	}
}

func TestIsNullFalse(t *testing.T) {
	s1, s2 := EncodeInt64(0)
	if IsNull(s1, s2) {
		t.Error("IsNull should return false for zero int64")
	}
	s1, s2 = EncodeInt64(42)
	if IsNull(s1, s2) {
		t.Error("IsNull should return false for non-null int64")
	}
	if IsNull("000000000000", nullHex) {
		t.Error("IsNull should return false when only one banner matches null")
	}
}

func TestEncodeBoolRoundtrip(t *testing.T) {
	tests := []bool{true, false}
	for _, v := range tests {
		enc := EncodeBool(v)
		if len(enc) != 12 {
			t.Errorf("EncodeBool(%v): expected 12-char string, got %q (len=%d)", v, enc, len(enc))
		}
		dec, err := DecodeBool(enc)
		if err != nil {
			t.Errorf("DecodeBool(%q): %v", enc, err)
			continue
		}
		if dec != v {
			t.Errorf("EncodeBool/DecodeBool roundtrip: got %v, want %v", dec, v)
		}
	}
}

func TestEncodeBoolValues(t *testing.T) {
	if enc := EncodeBool(true); enc != "010000000000" {
		t.Errorf("EncodeBool(true): expected '010000000000', got %q", enc)
	}
	if enc := EncodeBool(false); enc != "000000000000" {
		t.Errorf("EncodeBool(false): expected '000000000000', got %q", enc)
	}
}

func TestDecodeBoolInvalidHex(t *testing.T) {
	_, err := DecodeBool("zzzzzzzzzzzz")
	if err == nil {
		t.Error("expected error for invalid hex")
	}
}

func TestEncodeTextEmpty(t *testing.T) {
	lines := EncodeText("")
	for i, line := range lines {
		if line != "" {
			t.Errorf("EncodeText(\"\"): line %d expected empty, got %q", i, line)
		}
	}
	dec := DecodeText(lines)
	if dec != "" {
		t.Errorf("DecodeText: expected empty, got %q", dec)
	}
}

func TestEncodeTextShort(t *testing.T) {
	s := "hello"
	lines := EncodeText(s)
	if lines[0] != "hello" {
		t.Errorf("EncodeText short: line 0 expected 'hello', got %q", lines[0])
	}
	for i := 1; i < signLines; i++ {
		if lines[i] != "" {
			t.Errorf("EncodeText short: line %d expected empty, got %q", i, lines[i])
		}
	}
	dec := DecodeText(lines)
	if dec != s {
		t.Errorf("DecodeText short: got %q, want %q", dec, s)
	}
}

func TestEncodeTextMultiLine(t *testing.T) {
	s := "abcdefghijklmnop" + "qrstuvwxyzABCDEF" + "GHIJKLMNOPQRSTUV" + "WXYZ0123456789.."
	if len(s) != 64 {
		t.Fatalf("test string length: expected 64, got %d", len(s))
	}
	lines := EncodeText(s)
	for i := 0; i < signLines; i++ {
		if len(lines[i]) != signLineLen {
			t.Errorf("line %d: expected %d chars, got %d", i, signLineLen, len(lines[i]))
		}
	}
	dec := DecodeText(lines)
	if dec != s {
		t.Errorf("DecodeText exactly 64: got %q, want %q", dec, s)
	}
}

func TestEncodeTextOver64(t *testing.T) {
	s := strings.Repeat("x", 100)
	lines := EncodeText(s)
	for i := 0; i < signLines-1; i++ {
		if len(lines[i]) != signLineLen {
			t.Errorf("line %d: expected %d chars, got %d", i, signLineLen, len(lines[i]))
		}
	}
	if !strings.HasSuffix(lines[3], "...") {
		t.Errorf("line 3 should end with '...': got %q", lines[3])
	}
	total := len(lines[0]) + len(lines[1]) + len(lines[2]) + len(lines[3])
	if total != 64 {
		t.Errorf("total chars: expected 64, got %d", total)
	}
}

func TestEncodeTextExactly64(t *testing.T) {
	s := strings.Repeat("a", 64)
	lines := EncodeText(s)
	if strings.Contains(lines[3], "...") {
		t.Error("exactly 64 chars should not have continuation marker")
	}
	dec := DecodeText(lines)
	if dec != s {
		t.Errorf("DecodeText exactly 64: got %q, want %q", dec, s)
	}
}

func TestEncodeTextExactly61(t *testing.T) {
	s := strings.Repeat("b", 61)
	lines := EncodeText(s)
	if strings.Contains(lines[3], "...") {
		t.Error("exactly 61 chars should not have continuation marker (<=64)")
	}
	dec := DecodeText(lines)
	if dec != s {
		t.Errorf("DecodeText exactly 61: got %q, want %q", dec, s)
	}
}

func TestEncodeTextEquals65(t *testing.T) {
	s := strings.Repeat("c", 65)
	lines := EncodeText(s)
	if !strings.HasSuffix(lines[3], "...") {
		t.Error("65 chars should have continuation marker")
	}
	dec := DecodeText(lines)
	// First 61 chars of content + "..." = 64 chars
	if len(dec) != 64 {
		t.Errorf("DecodeText over 64: expected 64 chars, got %d", len(dec))
	}
}

func TestNullText(t *testing.T) {
	lines := NullText()
	if !IsNullText(lines) {
		t.Error("NullText should satisfy IsNullText")
	}
	if lines[0] != nullTextMarker {
		t.Errorf("NullText line 0: expected %q, got %q", nullTextMarker, lines[0])
	}
	for i := 1; i < signLines; i++ {
		if lines[i] != "" {
			t.Errorf("NullText line %d: expected empty, got %q", i, lines[i])
		}
	}
}

func TestIsNullTextFalse(t *testing.T) {
	lines := EncodeText("hello")
	if IsNullText(lines) {
		t.Error("IsNullText should return false for non-null text")
	}
	lines = EncodeText("")
	if IsNullText(lines) {
		t.Error("IsNullText should return false for empty text")
	}
}
