package store

import (
	"time"

	"vless-audit/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// Store wraps the SQLite database.
type Store struct {
	DB *gorm.DB
}

// New opens (or creates) the SQLite database and runs migrations.
func New(dbPath string) (*Store, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}

	// WAL mode for better concurrent reads.
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA busy_timeout=5000")

	if err := db.AutoMigrate(
		&model.TrafficSnapshot{},
		&model.Connection{},
		&model.HourlyAggregate{},
		&model.VLESSUser{},
		&model.ClientDevice{},
		&model.ClientReport{},
	); err != nil {
		return nil, err
	}
	// Create archive tables if not exist (mirror structure).
	db.Exec(`CREATE TABLE IF NOT EXISTS connections_archive AS SELECT * FROM connections WHERE 1=0`)
	db.Exec(`CREATE TABLE IF NOT EXISTS traffic_snapshots_archive AS SELECT * FROM traffic_snapshots WHERE 1=0`)
	db.Exec(`CREATE TABLE IF NOT EXISTS hourly_aggregates_archive AS SELECT * FROM hourly_aggregates WHERE 1=0`)
	// Add indexes to archive tables.
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_ca_ts ON connections_archive(timestamp)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_ca_user ON connections_archive(user_email)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_tsa_ts ON traffic_snapshots_archive(created_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_ha_hour ON hourly_aggregates_archive(hour)`)
	if err != nil {
		return nil, err
	}

	return &Store{DB: db}, nil
}

// InsertSnapshot writes a traffic snapshot.
func (s *Store) InsertSnapshot(snap *model.TrafficSnapshot) error {
	return s.DB.Create(snap).Error
}

// InsertConnection writes a connection record.
func (s *Store) InsertConnection(conn *model.Connection) error {
	return s.DB.Create(conn).Error
}

// UpdateConnectionEnrichment updates geoip and rdns fields after async lookup.
func (s *Store) UpdateConnectionEnrichment(conn *model.Connection) {
	s.DB.Model(conn).Where("id = ?", conn.ID).Updates(map[string]interface{}{
		"source_country": conn.SourceCountry,
		"source_region":  conn.SourceRegion,
		"source_city":    conn.SourceCity,
		"target_domain":  conn.TargetDomain,
	})
}

// UpsertHourly upserts an hourly aggregate.
func (s *Store) UpsertHourly(hour time.Time, userEmail string, up, down, conns int64) error {
	hour = hour.Truncate(time.Hour)
	agg := model.HourlyAggregate{
		Hour:      hour,
		UserEmail: userEmail,
		UpBytes:   up,
		DownBytes: down,
		ConnCount: conns,
	}
	return s.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "hour"}, {Name: "user_email"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"up_bytes":   gorm.Expr("up_bytes + ?", up),
			"down_bytes": gorm.Expr("down_bytes + ?", down),
			"conn_count": gorm.Expr("conn_count + ?", conns),
		}),
	}).Create(&agg).Error
}

// RecentSnapshots returns traffic snapshots for the last N minutes.
func (s *Store) RecentSnapshots(userEmail string, minutes int) ([]model.TrafficSnapshot, error) {
	var snaps []model.TrafficSnapshot
	q := s.DB.Where("created_at >= ?", time.Now().Add(-time.Duration(minutes)*time.Minute))
	if userEmail != "" {
		q = q.Where("user_email = ?", userEmail)
	}
	err := q.Order("created_at ASC").Find(&snaps).Error
	return snaps, err
}

// ConnectionsPage returns a paginated list of connections.
func (s *Store) ConnectionsPage(userEmail string, limit, offset int, start, end time.Time) ([]model.Connection, int64, error) {
	var conns []model.Connection
	var total int64

	q := s.DB.Model(&model.Connection{})
	if userEmail != "" {
		q = q.Where("user_email = ?", userEmail)
	}
	if !start.IsZero() {
		q = q.Where("timestamp >= ?", start)
	}
	if !end.IsZero() {
		q = q.Where("timestamp <= ?", end)
	}

	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := q.Order("timestamp DESC").Limit(limit).Offset(offset).Find(&conns).Error
	return conns, total, err
}

// TrafficSummary returns total up/down for a period.
func (s *Store) TrafficSummary(period time.Duration) (up, down int64, err error) {
	since := time.Now().Add(-period)
	err = s.DB.Model(&model.TrafficSnapshot{}).
		Select("COALESCE(SUM(up_bytes),0), COALESCE(SUM(down_bytes),0)").
		Where("created_at >= ?", since).
		Row().Scan(&up, &down)
	return
}

// TopUsers returns top N users by traffic in a period.
func (s *Store) TopUsers(period time.Duration, limit int) ([]struct {
	UserEmail string
	UpBytes   int64
	DownBytes int64
}, error) {
	since := time.Now().Add(-period)
	var result []struct {
		UserEmail string
		UpBytes   int64
		DownBytes int64
	}
	err := s.DB.Model(&model.TrafficSnapshot{}).
		Select("user_email, SUM(up_bytes) as up_bytes, SUM(down_bytes) as down_bytes").
		Where("created_at >= ?", since).
		Group("user_email").
		Order("up_bytes + down_bytes DESC").
		Limit(limit).
		Find(&result).Error
	return result, err
}

// DistinctUsers returns the list of user emails seen in connections.
func (s *Store) DistinctUsers() ([]string, error) {
	var users []string
	err := s.DB.Model(&model.Connection{}).
		Distinct("user_email").
		Where("user_email != ''").
		Pluck("user_email", &users).Error
	return users, err
}

// TopTargets returns top N target domains/IPs by connection count.
func (s *Store) TopTargets(period time.Duration, limit int) ([]struct {
	Target       string
	TargetDomain string
	Count        int64
}, error) {
	since := time.Now().Add(-period)
	var rows []struct {
		Target       string
		TargetDomain string
		Count        int64
	}
	err := s.DB.Model(&model.Connection{}).
		Select("target, target_domain, COUNT(*) as count").
		Where("timestamp >= ?", since).
		Group("target").
		Order("count DESC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

// UserTimeline returns connection history for a specific user.
func (s *Store) UserTimeline(userEmail string, limit, offset int, start, end time.Time) ([]model.Connection, int64, error) {
	return s.ConnectionsPage(userEmail, limit, offset, start, end)
}

// UserTraffic returns traffic curve for a specific user.
func (s *Store) UserTraffic(userEmail string, minutes int) ([]model.TrafficSnapshot, error) {
	return s.RecentSnapshots(userEmail, minutes)
}

// ActiveUsers returns users with connections in the last N minutes.
func (s *Store) ActiveUsers(minutes int) ([]string, error) {
	var users []string
	since := time.Now().Add(-time.Duration(minutes) * time.Minute)
	err := s.DB.Model(&model.Connection{}).
		Distinct("user_email").
		Where("timestamp >= ? AND user_email != ''", since).
		Pluck("user_email", &users).Error
	return users, err
}

// HourlyHeatmap returns connection counts per hour for a period.
func (s *Store) HourlyHeatmap(period time.Duration) (map[string]int64, error) {
	since := time.Now().Add(-period)
	rows, err := s.DB.Model(&model.Connection{}).
		Select("strftime('%Y-%m-%d %H', timestamp) as hour, COUNT(*) as count").
		Where("timestamp >= ?", since).
		Group("hour").
		Order("hour ASC").
		Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]int64)
	for rows.Next() {
		var hour string
		var count int64
		if err := rows.Scan(&hour, &count); err != nil {
			continue
		}
		result[hour] = count
	}
	return result, nil
}

// ── Client Device Store ──

func (s *Store) UpsertDevice(d *model.ClientDevice) error {
	return s.DB.Where("device_id = ?", d.DeviceID).Assign(d).FirstOrCreate(d).Error
}

func (s *Store) TouchDevice(deviceID string) {
	s.DB.Model(&model.ClientDevice{}).Where("device_id = ?", deviceID).Update("last_seen", time.Now())
}

func (s *Store) InsertReport(r *model.ClientReport) error {
	return s.DB.Create(r).Error
}

func (s *Store) ListDevices() ([]model.ClientDevice, error) {
	var devices []model.ClientDevice
	err := s.DB.Order("last_seen DESC").Find(&devices).Error
	return devices, err
}

func (s *Store) DeviceReports(deviceID string, limit int) ([]model.ClientReport, error) {
	var reports []model.ClientReport
	err := s.DB.Where("device_id = ?", deviceID).Order("reported_at DESC").Limit(limit).Find(&reports).Error
	return reports, err
}

// PurgeOld archives data older than retention days to *_archive tables.
func (s *Store) PurgeOld(retentionDays int) error {
	if retentionDays <= 0 {
		return nil // 0 = keep forever
	}
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)

	// Move old connections to archive.
	s.DB.Exec("INSERT OR IGNORE INTO connections_archive SELECT * FROM connections WHERE timestamp < ?", cutoff)
	s.DB.Where("timestamp < ?", cutoff).Delete(&model.Connection{})

	// Move old snapshots to archive.
	s.DB.Exec("INSERT OR IGNORE INTO traffic_snapshots_archive SELECT * FROM traffic_snapshots WHERE created_at < ?", cutoff)
	s.DB.Where("created_at < ?", cutoff).Delete(&model.TrafficSnapshot{})

	// Move old aggregates to archive.
	s.DB.Exec("INSERT OR IGNORE INTO hourly_aggregates_archive SELECT * FROM hourly_aggregates WHERE hour < ?", cutoff)
	s.DB.Where("hour < ?", cutoff).Delete(&model.HourlyAggregate{})
	return nil
}

// StorageStats returns DB size and record counts.
func (s *Store) StorageStats() (map[string]interface{}, error) {
	var connCount, snapCount, archivedCount int64
	s.DB.Model(&model.Connection{}).Count(&connCount)
	s.DB.Model(&model.TrafficSnapshot{}).Count(&snapCount)
	s.DB.Raw("SELECT COUNT(*) FROM connections_archive").Scan(&archivedCount)

	var dbSize int64
	s.DB.Raw("SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size()").Scan(&dbSize)

	return map[string]interface{}{
		"connections":  connCount,
		"snapshots":    snapCount,
		"archived":     archivedCount,
		"db_size":      dbSize,
	}, nil
}

// ── VLESS User Management ──

func (s *Store) CreateUser(user *model.VLESSUser) error {
	return s.DB.Create(user).Error
}

func (s *Store) DeleteUser(email string) error {
	return s.DB.Where("email = ?", email).Delete(&model.VLESSUser{}).Error
}

func (s *Store) ListUsers() ([]model.VLESSUser, error) {
	var users []model.VLESSUser
	err := s.DB.Order("created_at ASC").Find(&users).Error
	return users, err
}

func (s *Store) GetUser(email string) (*model.VLESSUser, error) {
	var u model.VLESSUser
	err := s.DB.Where("email = ?", email).First(&u).Error
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// UserTrafficSummary returns total up/down for a specific user.
func (s *Store) UserTrafficSummary(email string, period time.Duration) (up, down int64, err error) {
	since := time.Now().Add(-period)
	err = s.DB.Model(&model.TrafficSnapshot{}).
		Select("COALESCE(SUM(up_bytes),0), COALESCE(SUM(down_bytes),0)").
		Where("user_email = ? AND created_at >= ?", email, since).
		Row().Scan(&up, &down)
	return
}

// Close closes the database connection.
func (s *Store) Close() error {
	sqlDB, err := s.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
