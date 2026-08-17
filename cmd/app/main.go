package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/betterlyrics/bete-node/pkg/cache"
	"github.com/betterlyrics/bete-node/pkg/config"
	"github.com/betterlyrics/bete-node/pkg/node"
	"github.com/betterlyrics/bete-node/pkg/origin"
)

func main() {
	envFlag := flag.String("env", "", "Path to custom .env file")
	modeFlag := flag.String("mode", "", "Force mode: origin | node | auto")
	portFlag := flag.String("port", "", "Override listening port")
	flag.Parse()

	cfg, err := config.LoadConfig(*envFlag)
	if err != nil {
		log.Fatalf("Failed to initialize configuration: %v", err)
	}

	if *modeFlag != "" {
		switch *modeFlag {
		case "origin":
			cfg.Mode = config.ModeOrigin
		case "node":
			cfg.Mode = config.ModeNode
		default:
			cfg.Mode = config.ModeAuto
		}
	}

	if *portFlag != "" {
		cfg.Port = *portFlag
	}

	fmt.Println("==================================================")
	fmt.Println("       🎵 BetterLyrics Multi-Node Accelerator      ")
	fmt.Println("==================================================")
	fmt.Printf(" [Platform]  : %s\n", cfg.Platform.String())
	fmt.Printf(" [Loaded Env]: %s\n", cfg.LoadedEnv)
	fmt.Printf(" [Mode]      : %s\n", cfg.Mode)
	fmt.Printf(" [Port]      : %s\n", cfg.Port)
	fmt.Printf(" [Upstream]  : %s\n", cfg.UpstreamURL)
	if cfg.Mode == config.ModeOrigin {
		fmt.Printf(" [Nodes]     : %d active\n", len(cfg.Nodes))
	}
	fmt.Println("==================================================")

	memCache := cache.NewMemoryCache(cfg.CacheMaxKeys, time.Duration(cfg.CacheTTLSec)*time.Second)

	var startServer func() error
	var stopServer func(ctx context.Context) error

	if cfg.Mode == config.ModeOrigin {
		originSrv := origin.NewServer(cfg, memCache)
		startServer = originSrv.Start
		stopServer = originSrv.Shutdown
	} else {
		nodeSrv := node.NewServer(cfg, memCache)
		startServer = nodeSrv.Start
		stopServer = nodeSrv.Shutdown
	}

	go func() {
		if err := startServer(); err != nil && err.Error() != "http: Server closed" {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Graceful shutdown handling
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down BetterLyrics server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := stopServer(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}
	log.Println("Server gracefully exited.")
}
