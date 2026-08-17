package platform

import (
	"os"
	"strings"
)

// Platform represents the hosting environment
type Platform string

const (
	PlatformPterodactyl Platform = "pterodactyl"
	PlatformKoyeb       Platform = "koyeb"
	PlatformVercel      Platform = "vercel"
	PlatformNetlify     Platform = "netlify"
	PlatformDocker      Platform = "docker"
	PlatformVPS         Platform = "vps"
	PlatformUnknown     Platform = "unknown"
)

// Info holds platform metadata
type Info struct {
	Platform    Platform
	DefaultPort string
	WorkDir     string
	IsContainer bool
}

// Detect inspects environment variables and filesystem to detect the runtime platform
func Detect() Info {
	info := Info{
		Platform:    PlatformUnknown,
		DefaultPort: "8080",
		WorkDir:     ".",
		IsContainer: false,
	}

	// 1. Check Pterodactyl environment
	// Pterodactyl sets P_SERVER_LOCATION, SERVER_PORT, or standard /home/container path
	if os.Getenv("P_SERVER_LOCATION") != "" || os.Getenv("SERVER_PORT") != "" || exists("/home/container") {
		info.Platform = PlatformPterodactyl
		info.IsContainer = true
		info.WorkDir = "/home/container"
		if port := os.Getenv("SERVER_PORT"); port != "" {
			info.DefaultPort = port
		}
		return info
	}

	// 2. Check Koyeb
	if os.Getenv("KOYEB_APP_NAME") != "" || os.Getenv("KOYEB_SERVICE_NAME") != "" {
		info.Platform = PlatformKoyeb
		info.IsContainer = true
		if port := os.Getenv("PORT"); port != "" {
			info.DefaultPort = port
		}
		return info
	}

	// 3. Check Vercel
	if os.Getenv("VERCEL") == "1" || os.Getenv("VERCEL_ENV") != "" {
		info.Platform = PlatformVercel
		info.IsContainer = true
		return info
	}

	// 4. Check Netlify
	if os.Getenv("NETLIFY") == "true" || os.Getenv("NETLIFY_IMAGES_CDN_DOMAIN") != "" {
		info.Platform = PlatformNetlify
		info.IsContainer = true
		return info
	}

	// 5. Check Docker container
	if exists("/.dockerenv") || os.Getenv("DOCKER_CONTAINER") != "" {
		info.Platform = PlatformDocker
		info.IsContainer = true
		if port := os.Getenv("PORT"); port != "" {
			info.DefaultPort = port
		}
		return info
	}

	// 6. Default to VPS / Standard Linux
	if port := os.Getenv("PORT"); port != "" {
		info.DefaultPort = port
	}
	info.Platform = PlatformVPS
	return info
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// String returns a pretty string of the detected platform
func (i Info) String() string {
	return strings.ToUpper(string(i.Platform))
}
