package collector

import (
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"vless-audit/internal/model"
	"vless-audit/internal/store"

	"github.com/nxadm/tail"
)

// Xray plain-text access log format:
// YYYY/MM/DD HH:MM:SS.MS from SOURCE_IP:PORT accepted PROTO:TARGET_IP:PORT [INBOUND >> OUTBOUND] email: USER
// Example:
// 2026/05/26 11:42:20.077009 from 127.0.0.1:51146 accepted tcp:54.87.65.228:80 [vless-in >> direct] email: test-user@example.com
var accessLogRe = regexp.MustCompile(
	`^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}(?:\.\d+)?) ` + // timestamp
		`(?:from )?((?:[\d.]+|\[[0-9a-fA-F:]+\]):\d+) ` + // source ip:port (IPv4 or [IPv6]:port)
		`accepted (\w+):([^ ]+) ` + // protocol:target
		`\[(\S+) >> (\S+)\] ` + // [inbound >> outbound]
		`email: (.+)$`, // email
)

// LogCollector tails the Xray access log and writes parsed entries to the store.
type LogCollector struct {
	store    *store.Store
	path     string
	stopCh   chan struct{}
	wg       sync.WaitGroup
	onConnCh chan *model.Connection // broadcast channel for SSE
}

// NewLogCollector creates a LogCollector.
func NewLogCollector(s *store.Store, logPath string) *LogCollector {
	return &LogCollector{
		store:    s,
		path:     logPath,
		stopCh:   make(chan struct{}),
		onConnCh: make(chan *model.Connection, 256),
	}
}

// ConnChannel returns a read-only channel of new connections (for SSE).
func (lc *LogCollector) ConnChannel() <-chan *model.Connection {
	return lc.onConnCh
}

// Start begins tailing the access log.
func (lc *LogCollector) Start() error {
	t, err := tail.TailFile(lc.path, tail.Config{
		Follow:    true,
		ReOpen:    true,
		MustExist: false,
		Poll:      true, // polling works reliably across platforms
		Location:  &tail.SeekInfo{Offset: 0, Whence: 2}, // start at end
	})
	if err != nil {
		return err
	}

	lc.wg.Add(1)
	go func() {
		defer lc.wg.Done()
		defer t.Cleanup()

		for {
			select {
			case line, ok := <-t.Lines:
				if !ok {
					return
				}
				if line.Err != nil {
					log.Printf("[logs] tail error: %v", line.Err)
					continue
				}
				conn := lc.parseLine(line.Text)
				if conn != nil {
					if err := lc.store.InsertConnection(conn); err != nil {
						log.Printf("[logs] insert error: %v", err)
						continue
					}
					// Aggregate hourly.
					if err := lc.store.UpsertHourly(conn.Timestamp, conn.UserEmail, conn.UpBytes, conn.DownBytes, 1); err != nil {
						log.Printf("[logs] aggregate error: %v", err)
					}
					// Enrich with GeoIP and reverse DNS (async).
					go lc.enrichConnection(conn)
					// Broadcast to SSE listeners (non-blocking).
					select {
					case lc.onConnCh <- conn:
					default:
					}
				}
			case <-lc.stopCh:
				return
			}
		}
	}()

	log.Printf("[logs] tailing %s", lc.path)
	return nil
}

// Stop signals the collector to stop.
func (lc *LogCollector) Stop() {
	close(lc.stopCh)
	lc.wg.Wait()
}

// parseLine parses a plain-text Xray access log line into a Connection model.
// Returns nil for non-matching or unparseable lines.
func (lc *LogCollector) parseLine(line string) *model.Connection {
	matches := accessLogRe.FindStringSubmatch(line)
	if matches == nil {
		return nil
	}

	// matches[1] = timestamp, [2] = source, [3] = protocol, [4] = target
	// matches[5] = inbound, [6] = outbound, [7] = email

	// Try with milliseconds (v26 format), then without (v1.8 format).
	ts, err := time.Parse("2006/01/02 15:04:05.000000", matches[1])
	if err != nil {
		ts, err = time.Parse("2006/01/02 15:04:05", matches[1])
		if err != nil {
			ts = time.Now()
		}
	}

	return &model.Connection{
		Timestamp: ts,
		UserEmail: strings.TrimSpace(matches[7]),
		Inbound:   strings.TrimSpace(matches[5]),
		Protocol:  strings.TrimSpace(matches[3]),
		Source:    cleanSource(strings.TrimSpace(matches[2])),
		Target:    strings.TrimSpace(matches[4]),
		UpBytes:   0, // plain-text log doesn't include byte counts
		DownBytes: 0,
		Duration:  0,
		Status:    "accepted",
		RawLog:    line,
	}
}

// cleanSource strips IPv6 brackets from source address.
func cleanSource(s string) string {
	return strings.NewReplacer("[", "", "]", "").Replace(s)
}

// enrichConnection performs async GeoIP lookup and reverse DNS for a connection.
func (lc *LogCollector) enrichConnection(conn *model.Connection) {
	// GeoIP for source IP.
	country, region, city := LookupGeoIP(conn.Source)
	if country != "" {
		conn.SourceCountry = country
		conn.SourceRegion = region
		conn.SourceCity = city
	}

	// Reverse DNS for target.
	domain := ReverseDNS(conn.Target)
	if domain != "" {
		conn.TargetDomain = domain
	}

	// Update the database record with enriched fields.
	if country != "" || domain != "" {
		lc.store.UpdateConnectionEnrichment(conn)
	}
}
