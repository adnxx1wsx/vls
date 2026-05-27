package api

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"vless-audit/internal/store"
)

// SyncXrayUsers reads all managed users from DB, writes them into Xray config,
// and triggers a config reload on the running Xray instance.
func SyncXrayUsers(s *store.Store, configPath, xrayBin string) error {
	if configPath == "" {
		return nil
	}
	users, err := s.ListUsers()
	if err != nil {
		return err
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("读取 Xray 配置失败: %w", err)
	}
	var xrayCfg map[string]interface{}
	if err := json.Unmarshal(data, &xrayCfg); err != nil {
		return fmt.Errorf("解析 Xray 配置失败: %w", err)
	}

	clients := make([]map[string]interface{}, 0, len(users))
	for _, u := range users {
		if !u.Enable {
			continue
		}
		clients = append(clients, map[string]interface{}{
			"id":    u.UUID,
			"email": u.Email,
			"level": u.Level,
		})
	}

	inbounds, _ := xrayCfg["inbounds"].([]interface{})
	for _, ib := range inbounds {
		ibMap, _ := ib.(map[string]interface{})
		if ibMap != nil && ibMap["tag"] == "vless-in" {
			settings, _ := ibMap["settings"].(map[string]interface{})
			if settings == nil {
				settings = make(map[string]interface{})
				ibMap["settings"] = settings
			}
			settings["clients"] = clients
			break
		}
	}

	out, _ := json.MarshalIndent(xrayCfg, "", "  ")
	if err := os.WriteFile(configPath, out, 0o644); err != nil {
		return err
	}

	// Reload Xray by restarting the logger (triggers config reload).
	if xrayBin != "" {
		exec.Command(xrayBin, "api", "restartlogger", "--server=127.0.0.1:10086").Run()
	}
	return nil
}

func generateUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
