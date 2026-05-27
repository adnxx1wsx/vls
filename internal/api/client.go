package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"vless-audit/internal/model"
	"vless-audit/internal/store"

	"github.com/gin-gonic/gin"
)

// DeviceRegister handles POST /api/client/register — device registration.
func (h *Handler) DeviceRegister(c *gin.Context) {
	var d model.ClientDevice
	if err := c.ShouldBindJSON(&d); err != nil || d.DeviceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "device_id required"})
		return
	}
	d.CreatedAt = time.Now()
	d.LastSeen = time.Now()
	if err := h.Store.UpsertDevice(&d); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// Auto-create VLESS user if linked to a known email.
	if d.UserEmail != "" {
		u, _ := h.Store.GetUser(d.UserEmail)
		if u == nil {
			h.Store.CreateUser(&model.VLESSUser{
				Email:  d.UserEmail,
				UUID:   generateUUID(),
				Level:  0,
				Enable: true,
			})
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "device_id": d.DeviceID})
}

// ReportTelemetry handles POST /api/client/report — periodic telemetry.
func (h *Handler) ReportTelemetry(c *gin.Context) {
	var r model.ClientReport
	if err := c.ShouldBindJSON(&r); err != nil || r.DeviceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "device_id required"})
		return
	}
	r.ReportedAt = time.Now()
	if err := h.Store.InsertReport(&r); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// Update last seen.
	h.Store.TouchDevice(r.DeviceID)
	// Enrich IP geo for DNS queries.
	go enrichClientReport(h.Store, &r)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// ListDevices returns all registered client devices.
func (h *Handler) ListDevices(c *gin.Context) {
	devices, err := h.Store.ListDevices()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"devices": devices})
}

// DeviceReports returns telemetry reports for a device.
func (h *Handler) DeviceReports(c *gin.Context) {
	deviceID := c.Param("device_id")
	limit := 50
	if l := c.Query("limit"); l != "" {
		if v, err := parseInt(l); err == nil {
			limit = v
		}
	}
	reports, err := h.Store.DeviceReports(deviceID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"reports": reports})
}

func enrichClientReport(s *store.Store, r *model.ClientReport) {
	if r.DNSQueries == "" {
		return
	}
	queries := strings.Split(r.DNSQueries, ",")
	for _, q := range queries {
		parts := strings.SplitN(q, ":", 2)
		if len(parts) == 2 {
			// Store DNS query mapping for later use.
			_ = parts
		}
	}
}

func parseInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}
