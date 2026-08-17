package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/betterlyrics/bete-node/internal/cache"
	"github.com/betterlyrics/bete-node/internal/config"
	"github.com/betterlyrics/bete-node/internal/node"
)

func main() {
	envFlag := flag.String("env", "", "Path to custom .env file")
	portFlag := flag.String("port", "", "Override listening port")
	flag.Parse()

	cfg, err := config.LoadConfig(*envFlag)
	if err != nil {
		log.Fatalf("Failed to initialize configuration: %v", err)
	}

	cfg.Mode = config.ModeNode
	if *portFlag != "" {
		cfg.Port = *portFlag
	}

	memCache := cache.NewMemoryCache(cfg.CacheMaxKeys, time.Duration(cfg.CacheTTLSec)*time.Second)
	srv := node.NewServer(cfg, memCache)

	go func() {
		if err := srv.Start(); err != nil && err.Error() != "http: Server closed" {
			log.Fatalf("Node Server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = srv.Shutdown(ctx)
	log.Println("Node Server stopped.")
}
