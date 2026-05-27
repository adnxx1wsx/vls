package model

import "time"

// TrafficSnapshot records a point-in-time traffic counter from Xray stats.
type TrafficSnapshot struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	UserEmail string    `gorm:"index;size:128" json:"user_email"`
	Inbound   string    `gorm:"index;size:64" json:"inbound"`
	UpBytes   int64     `json:"up_bytes"`
	DownBytes int64     `json:"down_bytes"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}

// Connection is a parsed access-log entry for a proxied connection.
type Connection struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	Timestamp     time.Time `gorm:"index" json:"timestamp"`
	UserEmail     string    `gorm:"index;size:128" json:"user_email"`
	Inbound       string    `gorm:"size:64" json:"inbound"`
	Protocol      string    `gorm:"size:16" json:"protocol"`
	Source        string    `gorm:"size:64" json:"source"`
	SourceCountry string    `gorm:"size:64" json:"source_country"`
	SourceRegion  string    `gorm:"size:64" json:"source_region"`
	SourceCity    string    `gorm:"size:64" json:"source_city"`
	Target        string    `gorm:"size:256" json:"target"`
	TargetDomain  string    `gorm:"size:256" json:"target_domain"`
	UpBytes       int64     `json:"up_bytes"`
	DownBytes     int64     `json:"down_bytes"`
	Duration      int64     `json:"duration_ms"`
	Status        string    `gorm:"size:32" json:"status"`
	RawLog        string    `gorm:"type:text" json:"raw_log"`
	CreatedAt     time.Time `json:"-"`
}

// VLESSUser represents a managed VLESS user account.
type VLESSUser struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Email       string    `gorm:"uniqueIndex;size:128" json:"email"`
	DisplayName string    `gorm:"size:64" json:"display_name"`
	UUID        string    `gorm:"uniqueIndex;size:64" json:"uuid"`
	Level       int       `json:"level"`
	Enable      bool      `gorm:"default:true" json:"enable"`
	CreatedAt   time.Time `json:"created_at"`
}

// ClientDevice represents a registered client app instance.
type ClientDevice struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	DeviceID    string    `gorm:"uniqueIndex;size:64" json:"device_id"`
	UserEmail   string    `gorm:"index;size:128" json:"user_email"`
	Brand       string    `gorm:"size:64" json:"brand"`
	Model       string    `gorm:"size:64" json:"model"`
	OSVersion   string    `gorm:"size:32" json:"os_version"`
	PhoneNumber string    `gorm:"size:32" json:"phone_number"`
	IMEI        string    `gorm:"size:32" json:"imei"`
	IMSI        string    `gorm:"size:32" json:"imsi"`
	MacAddr     string    `gorm:"size:32" json:"mac_addr"`
	ScreenSize  string    `gorm:"size:32" json:"screen_size"`
	Carrier     string    `gorm:"size:64" json:"carrier"`
	NetworkType string    `gorm:"size:16" json:"network_type"`
	WifiSSID    string    `gorm:"size:64" json:"wifi_ssid"`
	InstalledApps string  `gorm:"type:text" json:"installed_apps"`
	CreatedAt   time.Time `json:"created_at"`
	LastSeen    time.Time `json:"last_seen"`
}

// ClientReport stores periodic telemetry reports from client apps.
type ClientReport struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	DeviceID  string    `gorm:"index;size:64" json:"device_id"`
	UserEmail string    `gorm:"index;size:128" json:"user_email"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	Battery   int       `json:"battery"`
	IsCharging bool     `json:"is_charging"`
	ScreenOn  bool      `json:"screen_on"`
	AppTraffic string   `gorm:"type:text" json:"app_traffic"`
	DNSQueries string   `gorm:"type:text" json:"dns_queries"`
	Latency    int      `json:"latency_ms"`
	ReportedAt time.Time `gorm:"index" json:"reported_at"`
}

// HourlyAggregate stores traffic aggregated per user per hour.
type HourlyAggregate struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Hour      time.Time `gorm:"uniqueIndex:idx_hour_user;index" json:"hour"`
	UserEmail string    `gorm:"uniqueIndex:idx_hour_user;size:128" json:"user_email"`
	UpBytes   int64     `json:"up_bytes"`
	DownBytes int64     `json:"down_bytes"`
	ConnCount int64     `json:"conn_count"`
}
