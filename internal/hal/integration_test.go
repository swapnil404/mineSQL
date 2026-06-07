package hal

import (
	"context"
	"os"
	"testing"
)

func TestIntegrationReadBlock(t *testing.T) {
	addr := os.Getenv("MINESQL_MINECRAFT_ADDR")
	if addr == "" {
		t.Skip("MINESQL_MINECRAFT_ADDR not set")
	}

	client, err := NewClient(addr)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx := context.Background()
	data, err := client.ReadBlock(ctx, 0, 64, 0)
	if err != nil {
		t.Fatalf("ReadBlock: %v", err)
	}
	t.Logf("ReadBlock returned %d bytes", len(data))
}
