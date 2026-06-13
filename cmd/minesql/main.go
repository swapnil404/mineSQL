package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/swapnil404/minesql/internal/executor"
	"github.com/swapnil404/minesql/internal/hal"
	"github.com/swapnil404/minesql/internal/storage"
	"github.com/swapnil404/minesql/internal/wal"
	"github.com/swapnil404/minesql/internal/wire"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	minecraftAddr := os.Getenv("MINESQL_MINECRAFT_ADDR")
	if minecraftAddr == "" {
		minecraftAddr = "localhost:25576"
	}

	halClient, err := hal.NewClient(minecraftAddr)
	if err != nil {
		log.Fatalf("failed to connect to Minecraft plugin at %s: %v", minecraftAddr, err)
	}
	log.Printf("connected to Minecraft plugin at %s", minecraftAddr)

	walog := wal.NewWAL(halClient)
	if err := walog.Recover(ctx); err != nil {
		log.Fatalf("WAL recovery failed: %v", err)
	}
	log.Printf("WAL recovery complete (next LSN: %d)", walog.NextLSN())

	store := storage.NewStorage(halClient, walog)
	if err := store.LoadCatalog(ctx); err != nil {
		log.Fatalf("failed to load catalog: %v", err)
	}
	log.Printf("catalog loaded")

	exec := executor.NewExecutor(store)

	pgPort := os.Getenv("MINESQL_PORT")
	if pgPort == "" {
		pgPort = "5433"
	}

	chatPort := os.Getenv("MINESQL_CHAT_PORT")
	if chatPort == "" {
		chatPort = "5456"
	}

	pgSrv := wire.NewServer(":"+pgPort, exec)
	chatSrv := wire.NewChatServer(":"+chatPort, exec)

	errCh := make(chan error, 2)

	go func() {
		log.Printf("mineSQL wire server listening on :%s", pgPort)
		log.Printf("mineSQL ready. connect with: psql -h localhost -p 5432 -U minecraft -d minesql -w")
		errCh <- pgSrv.ListenAndServe(ctx)
	}()

	go func() {
		log.Printf("mineSQL chat server listening on :%s", chatPort)
		errCh <- chatSrv.ListenAndServe(ctx)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			log.Printf("server error: %v", err)
		}
		cancel()
	case <-ctx.Done():
	}

	log.Printf("shutting down...")

	done := make(chan struct{})
	go func() {
		<-errCh
		close(done)
	}()

	select {
	case <-done:
		log.Printf("all servers stopped")
	case <-time.After(30 * time.Second):
		log.Printf("shutdown timed out waiting for servers")
	}
}
