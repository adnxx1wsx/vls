package api

import (
	"fmt"
	"net/http"
	"time"

	"vless-audit/internal/store"

	"github.com/gin-gonic/gin"
)

// ExportConnectionsCSV exports connections as a CSV file.
func ExportConnectionsCSV(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := c.Query("user")
		limit := 10000
		offset := 0
		var start, end time.Time
		if s := c.Query("start"); s != "" {
			start, _ = time.Parse(time.RFC3339, s)
		}
		if e := c.Query("end"); e != "" {
			end, _ = time.Parse(time.RFC3339, e)
		}

		conns, _, err := s.ConnectionsPage(user, limit, offset, start, end)
		if err != nil {
			c.String(http.StatusInternalServerError, "error: %v", err)
			return
		}

		c.Header("Content-Type", "text/csv; charset=utf-8")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=vless-connections-%s.csv", time.Now().Format("20060102-150405")))
		// BOM for Excel compatibility.
		c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})
		c.Writer.WriteString("时间,用户,来源IP,来源国家,来源省市,来源城市,目标,目标域名,协议,上行,下行,耗时(ms),状态\n")
		for _, conn := range conns {
			line := fmt.Sprintf("%s,%s,%s,%s,%s,%s,%s,%s,%s,%d,%d,%d,%s\n",
				conn.Timestamp.Format("2006-01-02 15:04:05"),
				csvEscape(conn.UserEmail),
				csvEscape(conn.Source),
				csvEscape(conn.SourceCountry),
				csvEscape(conn.SourceRegion),
				csvEscape(conn.SourceCity),
				csvEscape(conn.Target),
				csvEscape(conn.TargetDomain),
				csvEscape(conn.Protocol),
				conn.UpBytes,
				conn.DownBytes,
				conn.Duration,
				csvEscape(conn.Status),
			)
			c.Writer.WriteString(line)
		}
	}
}

func csvEscape(s string) string {
	if s == "" {
		return ""
	}
	// Escape quotes and wrap in quotes if contains comma or quote.
	hasSpecial := false
	for _, ch := range s {
		if ch == ',' || ch == '"' || ch == '\n' {
			hasSpecial = true
			break
		}
	}
	if hasSpecial {
		escaped := ""
		for _, ch := range s {
			if ch == '"' {
				escaped += "\"\""
			} else {
				escaped += string(ch)
			}
		}
		return "\"" + escaped + "\""
	}
	return s
}
