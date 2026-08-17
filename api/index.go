package handler

import (
	"net/http"
	"sync"
	"time"

	"github.com/betterlyrics/bete-node/pkg/cache"
	"github.com/betterlyrics/bete-node/pkg/config"
	"github.com/betterlyrics/bete-node/pkg/node"
	"github.com/betterlyrics/bete-node/pkg/origin"
)

var (
	once        sync.Once
	httpHandler http.Handler
)

func initServerless() {
	cfg, err := config.LoadConfig("")
	if err != nil {
		panic(err)
	}

	memCache := cache.NewMemoryCache(cfg.CacheMaxKeys, time.Duration(cfg.CacheTTLSec)*time.Second)

	if cfg.Mode == config.ModeOrigin {
		srv := origin.NewServer(cfg, memCache)
		httpHandler = srv
	} else {
		srv := node.NewServer(cfg, memCache)
		httpHandler = srv
	}
}

// Handler is the entrypoint for Vercel / Netlify serverless functions
func Handler(w http.ResponseWriter, r *http.Request) {
	once.Do(initServerless)
	httpHandler.ServeHTTP(w, r)
}
