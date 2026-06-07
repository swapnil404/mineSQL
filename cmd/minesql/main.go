package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/swapnil404/minesql/internal/executor"
	"github.com/swapnil404/minesql/internal/hal"
	"github.com/swapnil404/minesql/internal/storage"
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

	store := storage.NewStorage(halClient)
	if err := store.LoadCatalog(ctx); err != nil {
		log.Fatalf("failed to load catalog: %v", err)
	}
	log.Printf("catalog loaded")

	exec := executor.NewExecutor(store)

	port := os.Getenv("MINESQL_PORT")
	if port == "" {
		port = "5433"
	}
	addr := ":" + port

	srv := wire.NewServer(addr, exec)
	log.Printf("mineSQL wire server listening on %s", addr)
	if err := srv.ListenAndServe(ctx); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
