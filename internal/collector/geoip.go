package collector

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// geoIPResult holds the geolocation response from ip-api.com.
type geoIPResult struct {
	Country   string `json:"country"`
	City      string `json:"city"`
	Region    string `json:"regionName"`
	ISP       string `json:"isp"`
	Query     string `json:"query"`
	Status    string `json:"status"`
	Message   string `json:"message"`
}

// geoIPCache caches geolocation results to avoid repeated API calls.
type geoIPCache struct {
	mu    sync.RWMutex
	cache map[string]geoIPResult
}

var geoCache = &geoIPCache{cache: make(map[string]geoIPResult)}

// LookupGeoIP queries ip-api.com for geolocation data.
// Returns country, region (province), and city strings.
func LookupGeoIP(ipPort string) (country, region, city string) {
	ip, _, err := net.SplitHostPort(ipPort)
	if err != nil {
		ip = ipPort
	}

	// Skip local addresses.
	if isPrivateIP(ip) {
		return "本地", "", ""
	}

	// Check for CDN IPs.
	if isCDNIP(ip) {
		return "CDN", "CDN节点", "真实IP已隐藏"
	}

	// Check cache.
	geoCache.mu.RLock()
	if r, ok := geoCache.cache[ip]; ok {
		geoCache.mu.RUnlock()
		return r.Country, r.Region, r.City
	}
	geoCache.mu.RUnlock()

	// Query ip-api.com (free tier: 45 req/min).
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://ip-api.com/json/%s?fields=country,regionName,city,status,message", ip))
	if err != nil {
		return "", "", ""
	}
	defer resp.Body.Close()

	var result geoIPResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", ""
	}

	if result.Status != "success" {
		return "", "", ""
	}

	// Cache result.
	geoCache.mu.Lock()
	geoCache.cache[ip] = result
	geoCache.mu.Unlock()

	return result.Country, result.Region, result.City
}

// ReverseDNS does a PTR lookup on an IP to find its hostname.
func ReverseDNS(ipPort string) string {
	ip, _, err := net.SplitHostPort(ipPort)
	if err != nil {
		ip = ipPort
	}

	// Skip private IPs.
	if isPrivateIP(ip) {
		return ""
	}

	names, err := net.LookupAddr(ip)
	if err != nil || len(names) == 0 {
		return ""
	}

	// Clean up: remove trailing dot and return the shortest name.
	best := strings.TrimSuffix(names[0], ".")
	for _, n := range names[1:] {
		n = strings.TrimSuffix(n, ".")
		if len(n) < len(best) {
			best = n
		}
	}
	return best
}

// isPrivateIP checks if an IP is in a private/loopback range.
func isPrivateIP(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return true
	}
	if parsed.IsLoopback() || parsed.IsPrivate() || parsed.IsLinkLocalUnicast() {
		return true
	}
	return false
}

// isCDNIP checks if an IP likely belongs to a CDN provider.
// Uses reverse DNS keywords to detect CDN nodes.
func isCDNIP(ip string) bool {
	names, err := net.LookupAddr(ip)
	if err != nil {
		return false
	}
	cdnPatterns := []string{
		"cloudfront", "fastly", "akamai", "cloudflare",
		"cdn77", "bunnycdn", "stackpath", "keycdn",
		"azureedge", "azurefd", "edgecast", "highwinds",
		"kxcdn", "section.io", "belugacdn", "cdn",
	}
	for _, name := range names {
		lower := strings.ToLower(name)
		for _, p := range cdnPatterns {
			if strings.Contains(lower, p) {
				return true
			}
		}
	}
	return false
}


