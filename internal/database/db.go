package database

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrUserNotFound      = errors.New("user not found")
	ErrUserAlreadyExists = errors.New("username or email already taken")
)

type DB struct {
	conn *sql.DB
}

// Open initializes and migrates the SQLite database
func Open(dbPath string) (*DB, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create db directory %q: %w", dir, err)
	}

	conn, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return db, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func GenerateAPIKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "neon_" + hex.EncodeToString(b)
}

func (db *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		email TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'user',
		api_key TEXT UNIQUE,
		quota_bytes INTEGER NOT NULL DEFAULT 53687091200,
		storage_used_bytes INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);
	CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);

	CREATE TABLE IF NOT EXISTS audit_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER,
		username TEXT,
		action TEXT NOT NULL,
		ip_address TEXT,
		details TEXT,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS display_configs (
		device_id TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		zip_code TEXT NOT NULL DEFAULT '',
		city_name TEXT NOT NULL DEFAULT 'New York',
		latitude REAL NOT NULL DEFAULT 40.7128,
		longitude REAL NOT NULL DEFAULT -74.0060,
		timezone TEXT NOT NULL DEFAULT 'Local',
		calendar_url TEXT NOT NULL DEFAULT '',
		full_refresh_minutes INTEGER NOT NULL DEFAULT 30,
		partial_refresh_minutes INTEGER NOT NULL DEFAULT 1,
		ble_mac TEXT NOT NULL DEFAULT '',
		auto_push INTEGER NOT NULL DEFAULT 0,
		push_interval_seconds INTEGER NOT NULL DEFAULT 60,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS calendar_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		device_id TEXT NOT NULL,
		title TEXT NOT NULL,
		start_time DATETIME NOT NULL,
		end_time DATETIME NOT NULL,
		location TEXT,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	`
	if _, err := db.conn.Exec(schema); err != nil {
		return err
	}

	// Safe alter table for existing databases created before api_key column
	_, _ = db.conn.Exec("ALTER TABLE users ADD COLUMN api_key TEXT;")
	_, _ = db.conn.Exec("ALTER TABLE display_configs ADD COLUMN zip_code TEXT NOT NULL DEFAULT '';")
	_, _ = db.conn.Exec("ALTER TABLE display_configs ADD COLUMN ble_mac TEXT NOT NULL DEFAULT '';")
	_, _ = db.conn.Exec("ALTER TABLE display_configs ADD COLUMN auto_push INTEGER NOT NULL DEFAULT 0;")
	_, _ = db.conn.Exec("ALTER TABLE display_configs ADD COLUMN push_interval_seconds INTEGER NOT NULL DEFAULT 60;")
	_, _ = db.conn.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_users_api_key ON users(api_key);")

	// Backfill any users without an API key
	rows, err := db.conn.Query("SELECT id FROM users WHERE api_key IS NULL OR api_key = ''")
	if err == nil {
		var ids []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
		for _, id := range ids {
			_, _ = db.conn.Exec("UPDATE users SET api_key = ? WHERE id = ?", GenerateAPIKey(), id)
		}
	}

	return nil
}

func (db *DB) CreateUser(u *User) error {
	query := `
	INSERT INTO users (username, email, password_hash, role, api_key, quota_bytes, storage_used_bytes, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	now := time.Now().UTC()
	u.CreatedAt = now
	u.UpdatedAt = now
	if u.APIKey == "" {
		u.APIKey = GenerateAPIKey()
	}

	res, err := db.conn.Exec(query, u.Username, u.Email, u.PasswordHash, u.Role, u.APIKey, u.QuotaBytes, u.StorageUsedBytes, now, now)
	if err != nil {
		return ErrUserAlreadyExists
	}

	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	u.ID = id
	return nil
}

func (db *DB) UpdateUser(u *User) error {
	var query string
	var args []interface{}

	if u.PasswordHash != "" {
		if u.APIKey != "" {
			query = `
			UPDATE users
			SET username = ?, email = ?, password_hash = ?, role = ?, quota_bytes = ?, api_key = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?
			`
			args = []interface{}{u.Username, u.Email, u.PasswordHash, u.Role, u.QuotaBytes, u.APIKey, u.ID}
		} else {
			query = `
			UPDATE users
			SET username = ?, email = ?, password_hash = ?, role = ?, quota_bytes = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?
			`
			args = []interface{}{u.Username, u.Email, u.PasswordHash, u.Role, u.QuotaBytes, u.ID}
		}
	} else {
		if u.APIKey != "" {
			query = `
			UPDATE users
			SET username = ?, email = ?, role = ?, quota_bytes = ?, api_key = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?
			`
			args = []interface{}{u.Username, u.Email, u.Role, u.QuotaBytes, u.APIKey, u.ID}
		} else {
			query = `
			UPDATE users
			SET username = ?, email = ?, role = ?, quota_bytes = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?
			`
			args = []interface{}{u.Username, u.Email, u.Role, u.QuotaBytes, u.ID}
		}
	}

	_, err := db.conn.Exec(query, args...)
	return err
}

func (db *DB) DeleteUser(userID int64) error {
	// Delete user's devices
	_, _ = db.conn.Exec("DELETE FROM display_configs WHERE user_id = ?", userID)
	// Delete user record
	_, err := db.conn.Exec("DELETE FROM users WHERE id = ?", userID)
	return err
}

func (db *DB) RegenerateAPIKey(userID int64) (string, error) {
	newKey := GenerateAPIKey()
	query := "UPDATE users SET api_key = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?"
	_, err := db.conn.Exec(query, newKey, userID)
	if err != nil {
		return "", err
	}
	return newKey, nil
}

func (db *DB) GetUserByAPIKey(apiKey string) (*User, error) {
	if apiKey == "" {
		return nil, ErrUserNotFound
	}
	query := `
	SELECT id, username, email, password_hash, role, coalesce(api_key, ''), quota_bytes, storage_used_bytes, created_at, updated_at
	FROM users WHERE api_key = ?
	`
	var u User
	err := db.conn.QueryRow(query, apiKey).Scan(
		&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.APIKey,
		&u.QuotaBytes, &u.StorageUsedBytes, &u.CreatedAt, &u.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (db *DB) GetUserByUsername(username string) (*User, error) {
	query := `
	SELECT id, username, email, password_hash, role, coalesce(api_key, ''), quota_bytes, storage_used_bytes, created_at, updated_at
	FROM users WHERE username = ?
	`
	var u User
	err := db.conn.QueryRow(query, username).Scan(
		&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.APIKey,
		&u.QuotaBytes, &u.StorageUsedBytes, &u.CreatedAt, &u.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (db *DB) GetUserByID(id int64) (*User, error) {
	query := `
	SELECT id, username, email, password_hash, role, coalesce(api_key, ''), quota_bytes, storage_used_bytes, created_at, updated_at
	FROM users WHERE id = ?
	`
	var u User
	err := db.conn.QueryRow(query, id).Scan(
		&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.APIKey,
		&u.QuotaBytes, &u.StorageUsedBytes, &u.CreatedAt, &u.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (db *DB) ListUsers() ([]User, error) {
	query := `
	SELECT id, username, email, role, coalesce(api_key, ''), quota_bytes, storage_used_bytes, created_at, updated_at
	FROM users ORDER BY id ASC
	`
	rows, err := db.conn.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.Role, &u.APIKey, &u.QuotaBytes, &u.StorageUsedBytes, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (db *DB) CountUsers() (int, error) {
	var count int
	err := db.conn.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	return count, err
}

func (db *DB) UpdateStorageUsed(userID int64, bytesDelta int64) error {
	query := `
	UPDATE users
	SET storage_used_bytes = MAX(0, storage_used_bytes + ?), updated_at = CURRENT_TIMESTAMP
	WHERE id = ?
	`
	_, err := db.conn.Exec(query, bytesDelta, userID)
	return err
}

func (db *DB) SetStorageUsedExact(userID int64, totalBytes int64) error {
	query := `
	UPDATE users
	SET storage_used_bytes = ?, updated_at = CURRENT_TIMESTAMP
	WHERE id = ?
	`
	_, err := db.conn.Exec(query, totalBytes, userID)
	return err
}

func (db *DB) LogAudit(log *AuditLog) error {
	query := `
	INSERT INTO audit_logs (user_id, username, action, ip_address, details, created_at)
	VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`
	_, err := db.conn.Exec(query, log.UserID, log.Username, log.Action, log.IPAddress, log.Details)
	return err
}

// Display and Calendar database methods

func (db *DB) SaveDisplayConfig(cfg *DisplayConfig) error {
	autoPushInt := 0
	if cfg.AutoPush {
		autoPushInt = 1
	}
	if cfg.PushIntervalSeconds <= 0 {
		cfg.PushIntervalSeconds = 60
	}
	query := `
	INSERT INTO display_configs (device_id, user_id, zip_code, city_name, latitude, longitude, timezone, calendar_url, full_refresh_minutes, partial_refresh_minutes, ble_mac, auto_push, push_interval_seconds, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	ON CONFLICT(device_id) DO UPDATE SET
		user_id = excluded.user_id,
		zip_code = excluded.zip_code,
		city_name = excluded.city_name,
		latitude = excluded.latitude,
		longitude = excluded.longitude,
		timezone = excluded.timezone,
		calendar_url = excluded.calendar_url,
		full_refresh_minutes = excluded.full_refresh_minutes,
		partial_refresh_minutes = excluded.partial_refresh_minutes,
		ble_mac = excluded.ble_mac,
		auto_push = excluded.auto_push,
		push_interval_seconds = excluded.push_interval_seconds,
		updated_at = CURRENT_TIMESTAMP
	`
	_, err := db.conn.Exec(query,
		cfg.DeviceID, cfg.UserID, cfg.ZipCode, cfg.CityName, cfg.Latitude, cfg.Longitude,
		cfg.Timezone, cfg.CalendarURL, cfg.FullRefreshMinutes, cfg.PartialRefreshMinutes,
		cfg.BLEMAC, autoPushInt, cfg.PushIntervalSeconds,
	)
	return err
}

func (db *DB) DeleteDisplayConfig(deviceID string) error {
	_, err := db.conn.Exec("DELETE FROM display_configs WHERE device_id = ?", deviceID)
	return err
}

func (db *DB) GetDisplayConfig(deviceID string) (*DisplayConfig, error) {
	query := `
	SELECT device_id, user_id, zip_code, city_name, latitude, longitude, timezone, calendar_url, full_refresh_minutes, partial_refresh_minutes, ble_mac, auto_push, push_interval_seconds, created_at, updated_at
	FROM display_configs WHERE device_id = ?
	`
	var c DisplayConfig
	var autoPushInt int
	err := db.conn.QueryRow(query, deviceID).Scan(
		&c.DeviceID, &c.UserID, &c.ZipCode, &c.CityName, &c.Latitude, &c.Longitude,
		&c.Timezone, &c.CalendarURL, &c.FullRefreshMinutes, &c.PartialRefreshMinutes,
		&c.BLEMAC, &autoPushInt, &c.PushIntervalSeconds,
		&c.CreatedAt, &c.UpdatedAt,
	)
	c.AutoPush = (autoPushInt == 1)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil // not found
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (db *DB) ListDisplayConfigs(userID int64) ([]DisplayConfig, error) {
	query := `
	SELECT device_id, user_id, zip_code, city_name, latitude, longitude, timezone, calendar_url, full_refresh_minutes, partial_refresh_minutes, ble_mac, auto_push, push_interval_seconds, created_at, updated_at
	FROM display_configs WHERE user_id = ? ORDER BY device_id ASC
	`
	rows, err := db.conn.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []DisplayConfig
	for rows.Next() {
		var c DisplayConfig
		var autoPushInt int
		if err := rows.Scan(
			&c.DeviceID, &c.UserID, &c.ZipCode, &c.CityName, &c.Latitude, &c.Longitude,
			&c.Timezone, &c.CalendarURL, &c.FullRefreshMinutes, &c.PartialRefreshMinutes,
			&c.BLEMAC, &autoPushInt, &c.PushIntervalSeconds,
			&c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		c.AutoPush = (autoPushInt == 1)
		list = append(list, c)
	}
	return list, rows.Err()
}

func (db *DB) GetAutoPushConfigs() ([]DisplayConfig, error) {
	query := `
	SELECT device_id, user_id, zip_code, city_name, latitude, longitude, timezone, calendar_url, full_refresh_minutes, partial_refresh_minutes, ble_mac, auto_push, push_interval_seconds, created_at, updated_at
	FROM display_configs WHERE auto_push = 1 AND ble_mac != ''
	`
	rows, err := db.conn.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []DisplayConfig
	for rows.Next() {
		var c DisplayConfig
		var autoPushInt int
		if err := rows.Scan(
			&c.DeviceID, &c.UserID, &c.ZipCode, &c.CityName, &c.Latitude, &c.Longitude,
			&c.Timezone, &c.CalendarURL, &c.FullRefreshMinutes, &c.PartialRefreshMinutes,
			&c.BLEMAC, &autoPushInt, &c.PushIntervalSeconds,
			&c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		c.AutoPush = true
		list = append(list, c)
	}
	return list, rows.Err()
}

func (db *DB) AddCalendarEvent(event *CalendarEvent) error {
	query := `
	INSERT INTO calendar_events (device_id, title, start_time, end_time, location, created_at)
	VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`
	res, err := db.conn.Exec(query, event.DeviceID, event.Title, event.StartTime, event.EndTime, event.Location)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err == nil {
		event.ID = id
	}
	return nil
}

func (db *DB) GetUpcomingCalendarEvents(deviceID string, limit int) ([]CalendarEvent, error) {
	if limit <= 0 {
		limit = 5
	}
	query := `
	SELECT id, device_id, title, start_time, end_time, location
	FROM calendar_events
	WHERE device_id = ? AND end_time >= datetime('now', '-1 hour')
	ORDER BY start_time ASC
	LIMIT ?
	`
	rows, err := db.conn.Query(query, deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []CalendarEvent
	for rows.Next() {
		var e CalendarEvent
		if err := rows.Scan(&e.ID, &e.DeviceID, &e.Title, &e.StartTime, &e.EndTime, &e.Location); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
