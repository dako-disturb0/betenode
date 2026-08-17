package config

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/betterlyrics/bete-node/internal/platform"
)

// Mode represents running mode of the binary
type Mode string

const (
	ModeAuto   Mode = "auto"
	ModeOrigin Mode = "origin"
	ModeNode   Mode = "node"
)

// NodeEndpoint defines an edge node configured in origin
type NodeEndpoint struct {
	ID        int    `json:"id"`
	RawURL    string `json:"raw_url"`
	BaseURL   string `json:"base_url"`
	InterPath string `json:"inter_path"`
}

// Config holds the full application settings
type Config struct {
	Mode         Mode           `json:"mode"`
	Role         string         `json:"role"`
	Port         string         `json:"port"`
	Host         string         `json:"host"`
	UpstreamURL  string         `json:"upstream_url"`
	OriginURL    string         `json:"origin_url"`
	NodeSecret   string         `json:"node_secret"`
	Nodes        []NodeEndpoint `json:"nodes"`
	CacheTTLSec  int            `json:"cache_ttl_sec"`
	CacheMaxKeys int            `json:"cache_max_keys"`
	AutoHealth   bool           `json:"auto_health"`
	HealthSec    int            `json:"health_sec"`
	LoadedEnv    string         `json:"loaded_env"`
	Platform     platform.Info  `json:"platform"`
}

var nodeRegex = regexp.MustCompile(`^NODE_?(\d+)$`)

// LoadConfig loads configurations with multi-path .env resolution and environment variable overrides
func LoadConfig(customEnvPath string) (*Config, error) {
	platInfo := platform.Detect()

	loadedEnvPath := discoverAndLoadEnv(customEnvPath)

	cfg := &Config{
		Platform:     platInfo,
		LoadedEnv:    loadedEnvPath,
		Mode:         ModeAuto,
		Role:         getEnvOrDefault("ROLE", "auto"),
		Host:         getEnvOrDefault("HOST", "0.0.0.0"),
		Port:         getEnvOrDefault("PORT", platInfo.DefaultPort),
		UpstreamURL:  getEnvOrDefault("UPSTREAM_URL", "https://lyrics.api.dacubeking.com"),
		OriginURL:    getEnvOrDefault("ORIGIN_URL", ""),
		NodeSecret:   getEnvOrDefault("NODE_SECRET", "betterlyrics-interconnect-key"),
		CacheTTLSec:  getEnvAsInt("CACHE_TTL_SECONDS", 86400*3), // 3 days default
		CacheMaxKeys: getEnvAsInt("CACHE_MAX_ITEMS", 50000),
		AutoHealth:   getEnvAsBool("AUTO_HEALTHCHECK", true),
		HealthSec:    getEnvAsInt("HEALTHCHECK_INTERVAL_SEC", 15),
	}

	// Pterodactyl override port if SERVER_PORT is present
	if srvPort := os.Getenv("SERVER_PORT"); srvPort != "" {
		cfg.Port = srvPort
	}

	// Determine Mode
	modeStr := strings.ToLower(strings.TrimSpace(getEnvOrDefault("MODE", cfg.Role)))
	switch modeStr {
	case "origin":
		cfg.Mode = ModeOrigin
	case "node":
		cfg.Mode = ModeNode
	default:
		cfg.Mode = ModeAuto
	}

	// Parse dynamic nodes (NODE1, NODE2, etc.)
	cfg.Nodes = extractNodeEndpoints()

	// If mode is AUTO, decide based on presence of nodes or explicit role
	if cfg.Mode == ModeAuto {
		if len(cfg.Nodes) > 0 || strings.EqualFold(cfg.Role, "origin") {
			cfg.Mode = ModeOrigin
		} else {
			cfg.Mode = ModeNode
		}
	}

	return cfg, nil
}

// discoverAndLoadEnv searches across priority paths for .env files
func discoverAndLoadEnv(customPath string) string {
	var searchPaths []string

	// 1. Explicit path from argument / env
	if customPath != "" {
		searchPaths = append(searchPaths, customPath)
	}
	if envFile := os.Getenv("ENV_FILE"); envFile != "" {
		searchPaths = append(searchPaths, envFile)
	}

	// 2. Near executable
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		searchPaths = append(searchPaths, filepath.Join(exeDir, ".env"))
		searchPaths = append(searchPaths, filepath.Join(exeDir, "bete-node", ".env"))
	}

	// 3. /home/container (Pterodactyl priority)
	searchPaths = append(searchPaths,
		"/home/container/bete-node/.env",
		"/home/container/.env",
	)

	// 4. Current working directory ($PWD)
	if cwd, err := os.Getwd(); err == nil {
		searchPaths = append(searchPaths,
			filepath.Join(cwd, "bete-node", ".env"),
			filepath.Join(cwd, ".env"),
		)
	}

	for _, p := range searchPaths {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			loadEnvFile(p)
			return p
		}
	}

	return "environment_variables"
}

// loadEnvFile reads a key=value .env file and sets non-existing os envs
func loadEnvFile(filePath string) {
	file, err := os.Open(filePath)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.TrimSpace(parts[1])
			// Strip quotes if present
			if (strings.HasPrefix(v, "\"") && strings.HasSuffix(v, "\"")) ||
				(strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'")) {
				v = v[1 : len(v)-1]
			}
			// Only set if not already set in OS environment
			if os.Getenv(k) == "" {
				_ = os.Setenv(k, v)
			}
		}
	}
}

type indexedNode struct {
	id  int
	url string
}

// extractNodeEndpoints searches for NODE1..NODEN and NODES environment variables
func extractNodeEndpoints() []NodeEndpoint {
	var collected []indexedNode

	for _, env := range os.Environ() {
		pair := strings.SplitN(env, "=", 2)
		if len(pair) != 2 {
			continue
		}
		k := strings.TrimSpace(pair[0])
		v := strings.TrimSpace(pair[1])
		if v == "" {
			continue
		}

		if match := nodeRegex.FindStringSubmatch(k); match != nil {
			id, err := strconv.Atoi(match[1])
			if err == nil {
				collected = append(collected, indexedNode{id: id, url: v})
			}
		}
	}

	// Check fallback NODES=node1,node2
	if commaNodes := os.Getenv("NODES"); commaNodes != "" {
		parts := strings.Split(commaNodes, ",")
		startID := 1000
		for _, raw := range parts {
			raw = strings.TrimSpace(raw)
			if raw != "" {
				collected = append(collected, indexedNode{id: startID, url: raw})
				startID++
			}
		}
	}

	// Sort nodes by numeric ID (NODE1, NODE2, ...)
	sort.Slice(collected, func(i, j int) bool {
		return collected[i].id < collected[j].id
	})

	var result []NodeEndpoint
	for _, item := range collected {
		normBase, normInter := normalizeNodeURL(item.url)
		result = append(result, NodeEndpoint{
			ID:        item.id,
			RawURL:    item.url,
			BaseURL:   normBase,
			InterPath: normInter,
		})
	}

	return result
}

// normalizeNodeURL ensures standard https/http protocol and interconnect endpoint
func normalizeNodeURL(raw string) (baseURL string, interPath string) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "https://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return strings.TrimRight(raw, "/"), "/interconnect"
	}

	base := fmt.Sprintf("%s://%s", u.Scheme, u.Host)
	path := strings.TrimRight(u.Path, "/")
	if path == "" {
		path = "/interconnect"
	}

	return base, path
}

func getEnvOrDefault(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return strings.TrimSpace(val)
	}
	return fallback
}

func getEnvAsInt(key string, fallback int) int {
	valStr := os.Getenv(key)
	if valStr == "" {
		return fallback
	}
	val, err := strconv.Atoi(strings.TrimSpace(valStr))
	if err != nil {
		return fallback
	}
	return val
}

func getEnvAsBool(key string, fallback bool) bool {
	valStr := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if valStr == "" {
		return fallback
	}
	return valStr == "true" || valStr == "1" || valStr == "yes" || valStr == "on"
}
