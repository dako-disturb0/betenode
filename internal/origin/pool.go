package origin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/betterlyrics/bete-node/internal/config"
	"github.com/betterlyrics/bete-node/internal/node"
	"github.com/betterlyrics/bete-node/internal/upstream"
)

// NodeHealth tracks the health and latency score of a node
type NodeHealth struct {
	Endpoint    config.NodeEndpoint     `json:"endpoint"`
	IsHealthy   bool                    `json:"is_healthy"`
	LatencyMs   int64                   `json:"latency_ms"`
	LastChecked time.Time               `json:"last_checked"`
	FailCount   int                     `json:"fail_count"`
	Info        *node.InterconnectStatus `json:"info,omitempty"`
}

// NodePool manages active node routing and health checks
type NodePool struct {
	mu           sync.RWMutex
	nodes        []*NodeHealth
	client       *upstream.Client
	rrIndex      uint64
	autoHealth   bool
	intervalSec  time.Duration
	stopChan     chan struct{}
}

// NewNodePool creates and initializes a node pool
func NewNodePool(endpoints []config.NodeEndpoint, autoHealth bool, intervalSec int) *NodePool {
	var list []*NodeHealth
	for _, ep := range endpoints {
		list = append(list, &NodeHealth{
			Endpoint:  ep,
			IsHealthy: true, // Optimistically healthy at start
			LatencyMs: 9999,
		})
	}

	if intervalSec <= 0 {
		intervalSec = 15
	}

	p := &NodePool{
		nodes:       list,
		client:      upstream.NewClient("", 5*time.Second),
		autoHealth:  autoHealth,
		intervalSec: time.Duration(intervalSec) * time.Second,
		stopChan:    make(chan struct{}),
	}

	// Trigger initial check
	p.CheckAll()

	if autoHealth {
		go p.startHealthLoop()
	}

	return p
}

func (p *NodePool) startHealthLoop() {
	ticker := time.NewTicker(p.intervalSec)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.CheckAll()
		case <-p.stopChan:
			return
		}
	}
}

// Close stops the health checking loop
func (p *NodePool) Close() {
	close(p.stopChan)
}

// CheckAll pings all configured nodes and evaluates health & latency
func (p *NodePool) CheckAll() {
	p.mu.RLock()
	nodesCopy := make([]*NodeHealth, len(p.nodes))
	copy(nodesCopy, p.nodes)
	p.mu.RUnlock()

	var wg sync.WaitGroup
	for _, n := range nodesCopy {
		wg.Add(1)
		go func(nh *NodeHealth) {
			defer wg.Done()
			p.checkSingleNode(nh)
		}(n)
	}
	wg.Wait()
}

func (p *NodePool) checkSingleNode(nh *NodeHealth) {
	url := fmt.Sprintf("%s%s", nh.Endpoint.BaseURL, nh.Endpoint.InterPath)
	start := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		p.markFailed(nh)
		return
	}

	resp, err := p.client.HTTPClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		p.markFailed(nh)
		if resp != nil {
			resp.Body.Close()
		}
		return
	}
	defer resp.Body.Close()

	latency := time.Since(start).Milliseconds()

	var status node.InterconnectStatus
	_ = json.NewDecoder(resp.Body).Decode(&status)

	p.mu.Lock()
	nh.IsHealthy = true
	nh.LatencyMs = latency
	nh.LastChecked = time.Now()
	nh.FailCount = 0
	nh.Info = &status
	p.mu.Unlock()
}

func (p *NodePool) markFailed(nh *NodeHealth) {
	p.mu.Lock()
	defer p.mu.Unlock()
	nh.FailCount++
	nh.LastChecked = time.Now()
	if nh.FailCount >= 2 {
		nh.IsHealthy = false
	}
}

// GetBestNode returns the fastest healthy node or uses round-robin
func (p *NodePool) GetBestNode() *NodeHealth {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var healthy []*NodeHealth
	for _, n := range p.nodes {
		if n.IsHealthy {
			healthy = append(healthy, n)
		}
	}

	if len(healthy) == 0 {
		return nil
	}

	// Find node with lowest latency
	best := healthy[0]
	for _, n := range healthy[1:] {
		if n.LatencyMs < best.LatencyMs {
			best = n
		}
	}
	return best
}

// GetNextNode returns nodes via round-robin
func (p *NodePool) GetNextNode() *NodeHealth {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var healthy []*NodeHealth
	for _, n := range p.nodes {
		if n.IsHealthy {
			healthy = append(healthy, n)
		}
	}

	if len(healthy) == 0 {
		return nil
	}

	idx := atomic.AddUint64(&p.rrIndex, 1) % uint64(len(healthy))
	return healthy[idx]
}

// GetAllStatus returns list of all node health statuses
func (p *NodePool) GetAllStatus() []NodeHealth {
	p.mu.RLock()
	defer p.mu.RUnlock()

	res := make([]NodeHealth, len(p.nodes))
	for i, n := range p.nodes {
		res[i] = *n
	}
	return res
}
