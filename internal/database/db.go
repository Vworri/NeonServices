package database

import (
	"database/sql"
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

func (db *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		email TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'user',
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
		city_name TEXT NOT NULL DEFAULT 'New York',
		latitude REAL NOT NULL DEFAULT 40.7128,
		longitude REAL NOT NULL DEFAULT -74.0060,
		timezone TEXT NOT NULL DEFAULT 'Local',
		calendar_url TEXT NOT NULL DEFAULT '',
		full_refresh_minutes INTEGER NOT NULL DEFAULT 30,
		partial_refresh_minutes INTEGER NOT NULL DEFAULT 1,
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
	_, err := db.conn.Exec(schema)
	return err
}

func (db *DB) CreateUser(u *User) error {
	query := `
	INSERT INTO users (username, email, password_hash, role, quota_bytes, storage_used_bytes, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`
	now := time.Now().UTC()
	u.CreatedAt = now
	u.UpdatedAt = now

	res, err := db.conn.Exec(query, u.Username, u.Email, u.PasswordHash, u.Role, u.QuotaBytes, u.StorageUsedBytes, now, now)
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

func (db *DB) GetUserByUsername(username string) (*User, error) {
	query := `
	SELECT id, username, email, password_hash, role, quota_bytes, storage_used_bytes, created_at, updated_at
	FROM users WHERE username = ?
	`
	var u User
	err := db.conn.QueryRow(query, username).Scan(
		&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role,
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
	SELECT id, username, email, password_hash, role, quota_bytes, storage_used_bytes, created_at, updated_at
	FROM users WHERE id = ?
	`
	var u User
	err := db.conn.QueryRow(query, id).Scan(
		&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role,
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
	SELECT id, username, email, role, quota_bytes, storage_used_bytes, created_at, updated_at
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
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.Role, &u.QuotaBytes, &u.StorageUsedBytes, &u.CreatedAt, &u.UpdatedAt); err != nil {
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
	query := `
	INSERT INTO display_configs (device_id, user_id, city_name, latitude, longitude, timezone, calendar_url, full_refresh_minutes, partial_refresh_minutes, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	ON CONFLICT(device_id) DO UPDATE SET
		user_id = excluded.user_id,
		city_name = excluded.city_name,
		latitude = excluded.latitude,
		longitude = excluded.longitude,
		timezone = excluded.timezone,
		calendar_url = excluded.calendar_url,
		full_refresh_minutes = excluded.full_refresh_minutes,
		partial_refresh_minutes = excluded.partial_refresh_minutes,
		updated_at = CURRENT_TIMESTAMP
	`
	_, err := db.conn.Exec(query,
		cfg.DeviceID, cfg.UserID, cfg.CityName, cfg.Latitude, cfg.Longitude,
		cfg.Timezone, cfg.CalendarURL, cfg.FullRefreshMinutes, cfg.PartialRefreshMinutes,
	)
	return err
}

func (db *DB) GetDisplayConfig(deviceID string) (*DisplayConfig, error) {
	query := `
	SELECT device_id, user_id, city_name, latitude, longitude, timezone, calendar_url, full_refresh_minutes, partial_refresh_minutes, created_at, updated_at
	FROM display_configs WHERE device_id = ?
	`
	var c DisplayConfig
	err := db.conn.QueryRow(query, deviceID).Scan(
		&c.DeviceID, &c.UserID, &c.CityName, &c.Latitude, &c.Longitude,
		&c.Timezone, &c.CalendarURL, &c.FullRefreshMinutes, &c.PartialRefreshMinutes,
		&c.CreatedAt, &c.UpdatedAt,
	)
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
	SELECT device_id, user_id, city_name, latitude, longitude, timezone, calendar_url, full_refresh_minutes, partial_refresh_minutes, created_at, updated_at
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
		if err := rows.Scan(
			&c.DeviceID, &c.UserID, &c.CityName, &c.Latitude, &c.Longitude,
			&c.Timezone, &c.CalendarURL, &c.FullRefreshMinutes, &c.PartialRefreshMinutes,
			&c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
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
