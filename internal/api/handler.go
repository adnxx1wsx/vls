package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"vless-audit/internal/model"
	"vless-audit/internal/store"

	"github.com/gin-gonic/gin"
)

// Handler holds dependencies for HTTP handlers.
type Handler struct {
	Store          *store.Store
	ConnChan       <-chan *model.Connection
	SSEBroker      *SSEBroker
	XrayConfigPath  string // path to Xray config.json for user sync
	XrayBinPath     string // path to Xray binary for restart
	RegisterSecret  string // passphrase for self-service registration
}

// === Traffic Stats ===

// RealtimeStats returns traffic trend data for the last N minutes.
func (h *Handler) RealtimeStats(c *gin.Context) {
	user := c.Query("user")
	minutes := 60
	if m := c.Query("minutes"); m != "" {
		if v, err := strconv.Atoi(m); err == nil && v > 0 && v <= 1440 {
			minutes = v
		}
	}

	snaps, err := h.Store.RecentSnapshots(user, minutes)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Aggregate by time buckets (1 minute).
	type bucket struct {
		Time string `json:"time"`
		Up   int64  `json:"up"`
		Down int64  `json:"down"`
	}

	buckets := make(map[string]*bucket)
	for _, s := range snaps {
		key := s.CreatedAt.Truncate(time.Minute).Format(time.RFC3339)
		b, ok := buckets[key]
		if !ok {
			b = &bucket{Time: key}
			buckets[key] = b
		}
		b.Up += s.UpBytes
		b.Down += s.DownBytes
	}

	// Convert to sorted slice.
	result := make([]bucket, 0, len(buckets))
	for _, b := range buckets {
		result = append(result, *b)
	}
	// Sort by time (simple insertion since typically small).
	for i := 1; i < len(result); i++ {
		for j := i; j > 0 && result[j].Time < result[j-1].Time; j-- {
			result[j], result[j-1] = result[j-1], result[j]
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"buckets": result,
		"minutes": minutes,
	})
}

// TrafficSummary returns total up/down for a period.
func (h *Handler) TrafficSummary(c *gin.Context) {
	periodStr := c.DefaultQuery("period", "24h")
	dur, err := time.ParseDuration(periodStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid period: use 24h, 7d, 30d etc"})
		return
	}

	up, down, err := h.Store.TrafficSummary(dur)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"period":    periodStr,
		"up_bytes":  up,
		"down_bytes": down,
		"total_bytes": up + down,
	})
}

// TopUsers returns top N users by traffic.
func (h *Handler) TopUsers(c *gin.Context) {
	periodStr := c.DefaultQuery("period", "24h")
	dur, err := time.ParseDuration(periodStr)
	if err != nil {
		dur = 24 * time.Hour
	}

	limit := 20
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}

	users, err := h.Store.TopUsers(dur, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"period": periodStr,
		"users":  users,
	})
}

// === Targets Top ===

// TopTargets returns top N destination domains/IPs by connection count.
func (h *Handler) TopTargets(c *gin.Context) {
	periodStr := c.DefaultQuery("period", "24h")
	dur, err := time.ParseDuration(periodStr)
	if err != nil {
		dur = 24 * time.Hour
	}
	limit := 20
	if l := c.Query("limit"); l != "" {
		if v, e := strconv.Atoi(l); e == nil && v > 0 && v <= 100 {
			limit = v
		}
	}

	targets, err := h.Store.TopTargets(dur, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"period":  periodStr,
		"targets": targets,
	})
}

// === User Detail ===

// UserTimeline returns paginated connection history for one user.
func (h *Handler) UserTimeline(c *gin.Context) {
	email := c.Param("email")
	limit := 50
	offset := 0
	if l := c.Query("limit"); l != "" {
		if v, _ := strconv.Atoi(l); v > 0 && v <= 500 {
			limit = v
		}
	}
	if o := c.Query("offset"); o != "" {
		if v, _ := strconv.Atoi(o); v >= 0 {
			offset = v
		}
	}
	var start, end time.Time
	if s := c.Query("start"); s != "" {
		start, _ = time.Parse(time.RFC3339, s)
	}
	if e := c.Query("end"); e != "" {
		end, _ = time.Parse(time.RFC3339, e)
	}

	conns, total, err := h.Store.UserTimeline(email, limit, offset, start, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"connections": conns,
		"total":       total,
		"limit":       limit,
		"offset":      offset,
	})
}

// UserTraffic returns traffic curve for a specific user.
func (h *Handler) UserTraffic(c *gin.Context) {
	email := c.Param("email")
	minutes := 60
	if m := c.Query("minutes"); m != "" {
		if v, _ := strconv.Atoi(m); v > 0 && v <= 1440 {
			minutes = v
		}
	}
	snaps, err := h.Store.UserTraffic(email, minutes)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	type bucket struct {
		Time string `json:"time"`
		Up   int64  `json:"up"`
		Down int64  `json:"down"`
	}
	buckets := make(map[string]*bucket)
	for _, s := range snaps {
		key := s.CreatedAt.Truncate(time.Minute).Format(time.RFC3339)
		b, ok := buckets[key]
		if !ok {
			b = &bucket{Time: key}
			buckets[key] = b
		}
		b.Up += s.UpBytes
		b.Down += s.DownBytes
	}
	result := make([]bucket, 0, len(buckets))
	for _, b := range buckets {
		result = append(result, *b)
	}
	for i := 1; i < len(result); i++ {
		for j := i; j > 0 && result[j].Time < result[j-1].Time; j-- {
			result[j], result[j-1] = result[j-1], result[j]
		}
	}
	c.JSON(http.StatusOK, gin.H{"buckets": result, "user": email, "minutes": minutes})
}

// === Online Users ===

// OnlineUsers returns users active in the last N minutes.
func (h *Handler) OnlineUsers(c *gin.Context) {
	minutes := 5
	if m := c.Query("minutes"); m != "" {
		if v, _ := strconv.Atoi(m); v > 0 && v <= 60 {
			minutes = v
		}
	}
	users, err := h.Store.ActiveUsers(minutes)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if users == nil {
		users = []string{}
	}
	c.JSON(http.StatusOK, gin.H{"online": users, "minutes": minutes})
}

// HourlyHeatmap returns hourly connection counts.
func (h *Handler) HourlyHeatmap(c *gin.Context) {
	periodStr := c.DefaultQuery("period", "24h")
	dur, err := time.ParseDuration(periodStr)
	if err != nil {
		dur = 24 * time.Hour
	}
	heatmap, err := h.Store.HourlyHeatmap(dur)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"period": periodStr, "heatmap": heatmap})
}

// === Connections ===

// Connections returns a paginated list of connection records.
func (h *Handler) Connections(c *gin.Context) {
	user := c.Query("user")
	limit := 50
	offset := 0
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 500 {
			limit = v
		}
	}
	if o := c.Query("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}

	var start, end time.Time
	if s := c.Query("start"); s != "" {
		start, _ = time.Parse(time.RFC3339, s)
	}
	if e := c.Query("end"); e != "" {
		end, _ = time.Parse(time.RFC3339, e)
	}

	conns, total, err := h.Store.ConnectionsPage(user, limit, offset, start, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"connections": conns,
		"total":       total,
		"limit":       limit,
		"offset":      offset,
	})
}

// === Storage ===

// StorageStats returns DB statistics.
func (h *Handler) StorageStats(c *gin.Context) {
	stats, err := h.Store.StorageStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

// === Users ===

// === Admin: User Management ===

// AdminListUsers returns all managed VLESS users with traffic stats.
func (h *Handler) AdminListUsers(c *gin.Context) {
	users, err := h.Store.ListUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	type userInfo struct {
		Email       string `json:"email"`
		DisplayName string `json:"display_name"`
		UUID        string `json:"uuid"`
		Level       int    `json:"level"`
		Enable      bool   `json:"enable"`
		UpBytes     int64  `json:"up_bytes"`
		DownBytes   int64  `json:"down_bytes"`
		CreatedAt   string `json:"created_at"`
	}
	result := make([]userInfo, 0, len(users))
	for _, u := range users {
		up, down, _ := h.Store.UserTrafficSummary(u.Email, 30*24*time.Hour)
		result = append(result, userInfo{
			Email:       u.Email,
			DisplayName: u.DisplayName,
			UUID:        u.UUID,
			Level:       u.Level,
			Enable:    u.Enable,
			UpBytes:   up,
			DownBytes: down,
			CreatedAt: u.CreatedAt.Format(time.RFC3339),
		})
	}
	c.JSON(http.StatusOK, gin.H{"users": result})
}

// AdminCreateUser creates a new VLESS user and syncs to Xray config.
func (h *Handler) AdminCreateUser(c *gin.Context) {
	var req struct {
		Email string `json:"email"`
		UUID  string `json:"uuid"`
		Level int    `json:"level"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email is required"})
		return
	}
	if req.UUID == "" {
		req.UUID = generateUUID()
	}
	if req.Level < 0 {
		req.Level = 0
	}

	user := &model.VLESSUser{
		Email:  req.Email,
		UUID:   req.UUID,
		Level:  req.Level,
		Enable: true,
	}
	if err := h.Store.CreateUser(user); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "用户已存在: " + err.Error()})
		return
	}

	// Sync to Xray config.
	if err := SyncXrayUsers(h.Store, h.XrayConfigPath, h.XrayBinPath); err != nil {
		c.JSON(http.StatusOK, gin.H{"user": user, "warning": "用户已创建但Xray同步失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"user": user})
}

// AdminDeleteUser deletes a VLESS user and syncs to Xray config.
func (h *Handler) AdminDeleteUser(c *gin.Context) {
	email := c.Param("email")
	if email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email required"})
		return
	}
	if err := h.Store.DeleteUser(email); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := SyncXrayUsers(h.Store, h.XrayConfigPath, h.XrayBinPath); err != nil {
		c.JSON(http.StatusOK, gin.H{"warning": "用户已删除但Xray同步失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已删除"})
}

// UsersList returns all known user emails.
func (h *Handler) UsersList(c *gin.Context) {
	users, err := h.Store.DistinctUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if users == nil {
		users = []string{}
	}
	c.JSON(http.StatusOK, gin.H{"users": users})
}

// === SSE ===

// EventsStream is the SSE endpoint for real-time updates.
func (h *Handler) EventsStream(c *gin.Context) {
	h.SSEBroker.ServeHTTP(c.Writer, c.Request)
}

// ServeStatic serves embedded static files.
func ServeStatic(staticFS http.FileSystem) gin.HandlerFunc {
	fileServer := http.FileServer(staticFS)
	return func(c *gin.Context) {
		// Try the requested path first.
		fileServer.ServeHTTP(c.Writer, c.Request)
	}
}

// SSEBroker manages Server-Sent Events clients.
type SSEBroker struct {
	clients    map[chan string]struct{}
	register   chan chan string
	unregister chan chan string
	broadcast  chan string
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewSSEBroker creates a new SSE broker.
func NewSSEBroker() *SSEBroker {
	ctx, cancel := context.WithCancel(context.Background())
	b := &SSEBroker{
		clients:    make(map[chan string]struct{}),
		register:   make(chan chan string),
		unregister: make(chan chan string),
		broadcast:  make(chan string, 256),
		ctx:        ctx,
		cancel:     cancel,
	}
	go b.run()
	return b
}

func (b *SSEBroker) run() {
	for {
		select {
		case ch := <-b.register:
			b.clients[ch] = struct{}{}
		case ch := <-b.unregister:
			delete(b.clients, ch)
			close(ch)
		case msg := <-b.broadcast:
			for ch := range b.clients {
				select {
				case ch <- msg:
				default:
					// Slow client, drop.
				}
			}
		case <-b.ctx.Done():
			return
		}
	}
}

// Broadcast sends a message to all connected SSE clients.
func (b *SSEBroker) Broadcast(msg string) {
	select {
	case b.broadcast <- msg:
	default:
	}
}

// ServeHTTP handles an SSE connection.
func (b *SSEBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := make(chan string, 64)
	b.register <- ch

	defer func() {
		b.unregister <- ch
	}()

	ctx := r.Context()
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-ctx.Done():
			return
		}
	}
}

// BridgeConnToSSE bridges the connection channel to the SSE broker.
func BridgeConnToSSE(connChan <-chan *model.Connection, broker *SSEBroker) {
	go func() {
		for conn := range connChan {
			data, err := json.Marshal(gin.H{
				"type":           "connection",
				"timestamp":      conn.Timestamp,
				"user_email":     conn.UserEmail,
				"inbound":        conn.Inbound,
				"protocol":       conn.Protocol,
				"source":         conn.Source,
				"source_country": conn.SourceCountry,
				"source_region":  conn.SourceRegion,
				"source_city":    conn.SourceCity,
				"target":         conn.Target,
				"target_domain":  conn.TargetDomain,
				"up_bytes":       conn.UpBytes,
				"down_bytes":     conn.DownBytes,
				"duration_ms":    conn.Duration,
				"status":         conn.Status,
			})
			if err != nil {
				continue
			}
			broker.Broadcast(string(data))
		}
	}()
}

// StartPeriodicSSE pushes aggregated stats every few seconds.
func StartPeriodicSSE(s *store.Store, broker *SSEBroker, intervalSec int) {
	go func() {
		ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			up, down, err := s.TrafficSummary(5 * time.Minute)
			if err != nil {
				log.Printf("[sse] summary error: %v", err)
				continue
			}
			data, _ := json.Marshal(gin.H{
				"type":         "traffic_summary",
				"period":       "5m",
				"up_bytes":     up,
				"down_bytes":   down,
				"total_bytes":  up + down,
			})
			broker.Broadcast(string(data))
		}
	}()
}
