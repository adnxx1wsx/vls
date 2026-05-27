package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"vless-audit/internal/model"
	"vless-audit/internal/store"
)

// StatsCollector periodically queries Xray's StatsService via the xray CLI.
type StatsCollector struct {
	store      *store.Store
	xrayBin    string // path to xray binary
	apiAddr    string
	interval   time.Duration
	stopCh     chan struct{}
	wg         sync.WaitGroup
	lastCounters map[string]int64
	mu         sync.Mutex
}

// NewStatsCollector creates a StatsCollector.
func NewStatsCollector(s *store.Store, xrayBin, apiAddr string, intervalSec int) *StatsCollector {
	if xrayBin == "" {
		xrayBin = "xray"
	}
	return &StatsCollector{
		store:        s,
		xrayBin:      xrayBin,
		apiAddr:      apiAddr,
		interval:     time.Duration(intervalSec) * time.Second,
		stopCh:       make(chan struct{}),
		lastCounters: make(map[string]int64),
	}
}

// Start begins polling Xray stats.
func (sc *StatsCollector) Start() error {
	sc.poll()

	sc.wg.Add(1)
	go func() {
		defer sc.wg.Done()
		ticker := time.NewTicker(sc.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sc.poll()
			case <-sc.stopCh:
				return
			}
		}
	}()

	log.Printf("[stats] polling via xray api statsquery --server=%s every %v", sc.apiAddr, sc.interval)
	return nil
}

// Stop signals the collector to stop.
func (sc *StatsCollector) Stop() {
	close(sc.stopCh)
	sc.wg.Wait()
}

// xrayStatsLine matches: "stat: <name: "..." value: 123>" (v26 text format)
var statsLineRe = regexp.MustCompile(`stat:\s*<name:\s*"([^"]+)"\s*value:\s*(\d+)>`)

// statEntry holds a single parsed stat name/value pair.
type statEntry struct {
	Name  string
	Value int64
}

// parseStatsOutput handles both JSON (v1.8.x) and text (v26) output formats.
func parseStatsOutput(output string) []statEntry {
	// Try JSON format first (v1.8.x: {"stat":[{"name":"...","value":"123"}]})
	output = strings.TrimSpace(output)
	if strings.HasPrefix(output, "{") {
		var resp struct {
			Stat []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"stat"`
		}
		if err := json.Unmarshal([]byte(output), &resp); err == nil {
			entries := make([]statEntry, 0, len(resp.Stat))
			for _, s := range resp.Stat {
				v, _ := strconv.ParseInt(s.Value, 10, 64)
				entries = append(entries, statEntry{Name: s.Name, Value: v})
			}
			return entries
		}
	}

	// Fall back to text format (v26: stat: <name: "..." value: 123>)
	matches := statsLineRe.FindAllStringSubmatch(output, -1)
	entries := make([]statEntry, 0, len(matches))
	for _, m := range matches {
		v, _ := strconv.ParseInt(m[2], 10, 64)
		entries = append(entries, statEntry{Name: m[1], Value: v})
	}
	return entries
}

// poll runs xray api statsquery and parses the output.
func (sc *StatsCollector) poll() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, sc.xrayBin, "api", "statsquery",
		"--server="+sc.apiAddr,
		"-timeout", "4",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		// Only log if it's not the first few failures (avoid spam).
		log.Printf("[stats] xray api error: %v (stderr: %s)", err, strings.TrimSpace(stderr.String()))
		return
	}

	output := stdout.String()
	stats := parseStatsOutput(output)
	if len(stats) == 0 {
		return
	}

	sc.mu.Lock()
	defer sc.mu.Unlock()

	now := time.Now()
	for _, s := range stats {
		name := s.Name
		value := s.Value

		// Only track user-level traffic counters (format: "user>>>email>>>traffic>>>direction")
		if !strings.HasPrefix(name, "user>>>") {
			continue
		}

		parts := strings.Split(name, ">>>")
		if len(parts) < 4 {
			continue
		}
		userEmail := parts[1]
		direction := parts[3]

		prevValue := sc.lastCounters[name]
		sc.lastCounters[name] = value

		delta := value - prevValue
		if delta < 0 {
			delta = value
		}
		if delta == 0 {
			continue
		}

		var upBytes, downBytes int64
		if direction == "uplink" {
			upBytes = delta
		} else if direction == "downlink" {
			downBytes = delta
		}

		snap := &model.TrafficSnapshot{
			UserEmail: userEmail,
			UpBytes:   upBytes,
			DownBytes: downBytes,
			CreatedAt: now,
		}
		if err := sc.store.InsertSnapshot(snap); err != nil {
			log.Printf("[stats] insert error: %v", err)
		}
	}
}
