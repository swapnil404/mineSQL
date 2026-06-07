package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/swapnil404/minesql/internal/wire"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	srv := wire.NewServer(":5432")
	log.Printf("mineSQL wire server listening on :5432")
	if err := srv.ListenAndServe(ctx); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
