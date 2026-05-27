package api

import (
	"bufio"
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
)

// TailAccessLog returns the last N lines of the Xray access log.
func TailAccessLog(logPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		lines := 200
		if l := c.Query("lines"); l != "" {
			if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 2000 {
				lines = v
			}
		}
		if logPath == "" {
			c.JSON(http.StatusOK, gin.H{"lines": []string{}, "message": "access_log not configured"})
			return
		}

		f, err := os.Open(logPath)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"lines": []string{}, "message": "无法读取日志文件: " + err.Error()})
			return
		}
		defer f.Close()

		// Read all lines and return last N.
		var allLines []string
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 256*1024), 256*1024)
		for scanner.Scan() {
			allLines = append(allLines, scanner.Text())
		}
		if len(allLines) > lines {
			allLines = allLines[len(allLines)-lines:]
		}
		if allLines == nil {
			allLines = []string{}
		}
		c.JSON(http.StatusOK, gin.H{"lines": allLines, "file": logPath, "total": len(allLines)})
	}
}
