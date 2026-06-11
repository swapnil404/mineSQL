package storage

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
)

const (
	bytesPerBanner = 6
	signLineLen    = 16
	signLines      = 4
	signMaxLen     = signLineLen * signLines // 64
	nullHex        = "ffffffffffff"
	nullTextMarker = "\x00NULL\x00"
)

// EncodeInt32 encodes an int32 into 1 banner (6 bytes, 4 used, 2 zero-padded).
// Returns a 12-char hex string.
func EncodeInt32(v int32) string {
	b := make([]byte, bytesPerBanner)
	binary.BigEndian.PutUint32(b[0:4], uint32(v))
	return hex.EncodeToString(b)
}

// DecodeInt32 decodes an int32 from a 12-char hex banner string.
func DecodeInt32(s string) (int32, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return 0, fmt.Errorf("codec: invalid hex: %w", err)
	}
	if len(b) != bytesPerBanner {
		return 0, fmt.Errorf("codec: expected %d bytes, got %d", bytesPerBanner, len(b))
	}
	return int32(binary.BigEndian.Uint32(b[0:4])), nil
}

// EncodeInt64 encodes an int64 into 2 banners (12 bytes total).
// Returns two 12-char hex strings.
func EncodeInt64(v int64) (string, string) {
	b := make([]byte, bytesPerBanner*2)
	binary.BigEndian.PutUint64(b[0:8], uint64(v))
	return hex.EncodeToString(b[0:6]), hex.EncodeToString(b[6:12])
}

// DecodeInt64 decodes an int64 from two 12-char hex banner strings.
func DecodeInt64(s1, s2 string) (int64, error) {
	b1, err := hex.DecodeString(s1)
	if err != nil {
		return 0, fmt.Errorf("codec: invalid hex (banner 1): %w", err)
	}
	b2, err := hex.DecodeString(s2)
	if err != nil {
		return 0, fmt.Errorf("codec: invalid hex (banner 2): %w", err)
	}
	if len(b1) != bytesPerBanner || len(b2) != bytesPerBanner {
		return 0, fmt.Errorf("codec: expected %d bytes per banner", bytesPerBanner)
	}
	b := make([]byte, 8)
	copy(b[0:6], b1)
	copy(b[6:8], b2[0:2])
	return int64(binary.BigEndian.Uint64(b)), nil
}

// EncodeNull returns the null sentinel for int64.
// 0xFFFFFFFFFFFFFFFF encoded as two banners.
func EncodeNull() (string, string) {
	return nullHex, nullHex
}

// IsNull returns true if the two banner strings represent a null int64.
func IsNull(s1, s2 string) bool {
	return s1 == nullHex && s2 == nullHex
}

// EncodeBool encodes a bool into 1 banner (1 byte used, 5 zero-padded).
func EncodeBool(v bool) string {
	b := make([]byte, bytesPerBanner)
	if v {
		b[0] = 1
	}
	return hex.EncodeToString(b)
}

// DecodeBool decodes a bool from a 12-char hex banner string.
func DecodeBool(s string) (bool, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return false, fmt.Errorf("codec: invalid hex: %w", err)
	}
	if len(b) != bytesPerBanner {
		return false, fmt.Errorf("codec: expected %d bytes, got %d", bytesPerBanner, len(b))
	}
	return b[0] != 0, nil
}

// EncodeText splits a UTF-8 string into sign lines: 4 lines of 16 chars each.
// If text is longer than 64 chars, truncates with "..." continuation marker.
func EncodeText(s string) [4]string {
	var lines [4]string
	b := []byte(s)

	if len(b) <= signMaxLen {
		pos := 0
		for i := 0; i < signLines; i++ {
			n := signLineLen
			if pos+n > len(b) {
				n = len(b) - pos
			}
			if n > 0 {
				lines[i] = string(b[pos : pos+n])
			}
			pos += n
		}
		return lines
	}

	maxContent := signMaxLen - 3 // 61 bytes for content, 3 for "..."
	content := b[:maxContent]

	for i := 0; i < signLines-1; i++ {
		start := i * signLineLen
		end := start + signLineLen
		if start < len(content) {
			if end > len(content) {
				end = len(content)
			}
			lines[i] = string(content[start:end])
		}
	}

	start := (signLines - 1) * signLineLen
	if start < len(content) {
		lines[3] = string(content[start:]) + "..."
	} else {
		lines[3] = "..."
	}

	return lines
}

// DecodeText reconstructs a UTF-8 string from 4 sign lines.
func DecodeText(lines [4]string) string {
	return lines[0] + lines[1] + lines[2] + lines[3]
}

// NullText returns the null sentinel for TEXT.
func NullText() [4]string {
	return [4]string{nullTextMarker, "", "", ""}
}

// IsNullText returns true if the lines represent a null TEXT value.
func IsNullText(lines [4]string) bool {
	return lines[0] == nullTextMarker
}
