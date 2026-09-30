package display

import (
	"time"

	"github.com/neonphnx/NeonServices/internal/database"
)

type WeatherInfo struct {
	Temperature float64 `json:"temperature"`
	TempUnit    string  `json:"temp_unit"` // "°F" or "°C"
	Condition   string  `json:"condition"`
	WeatherCode int     `json:"weather_code"`
	TempMax     float64 `json:"temp_max"`
	TempMin     float64 `json:"temp_min"`
	Humidity    int     `json:"humidity"`
	WindSpeed   float64 `json:"wind_speed"`
}

type AirQualityInfo struct {
	AQI   int     `json:"aqi"`
	Level string  `json:"level"` // "Good", "Moderate", "Unhealthy", etc.
	PM25  float64 `json:"pm2_5"`
	PM10  float64 `json:"pm10"`
}

type EventInfo struct {
	Title     string    `json:"title"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Location  string    `json:"location,omitempty"`
	IsAllDay  bool      `json:"is_all_day"`
}

type DisplayData struct {
	DeviceID        string         `json:"device_id"`
	TimeStr         string         `json:"time_str"`
	DateStr         string         `json:"date_str"`
	DayOfWeek       string         `json:"day_of_week"`
	CityName        string         `json:"city_name"`
	Weather         WeatherInfo    `json:"weather"`
	AirQuality      AirQualityInfo `json:"air_quality"`
	Events          []EventInfo    `json:"events"`
	RefreshType     string         `json:"refresh_type"` // "partial" or "full"
	NextPollSeconds int            `json:"next_poll_seconds"`
	LastUpdated     time.Time      `json:"last_updated"`
}

type ConfigRequest struct {
	UserID                int64   `json:"user_id"`
	CityName              string  `json:"city_name"`
	Latitude              float64 `json:"latitude"`
	Longitude             float64 `json:"longitude"`
	Timezone              string  `json:"timezone"`
	CalendarURL           string  `json:"calendar_url"`
	FullRefreshMinutes    int     `json:"full_refresh_minutes"`
	PartialRefreshMinutes int     `json:"partial_refresh_minutes"`
	BLEMAC                string  `json:"ble_mac"`
	AutoPush              bool    `json:"auto_push"`
	PushIntervalSeconds   int     `json:"push_interval_seconds"`
}

func DefaultConfig(deviceID string, userID int64) *database.DisplayConfig {
	return &database.DisplayConfig{
		DeviceID:              deviceID,
		UserID:                userID,
		CityName:              "New York",
		Latitude:              40.7128,
		Longitude:             -74.0060,
		Timezone:              "Local",
		CalendarURL:           "",
		FullRefreshMinutes:    30,
		PartialRefreshMinutes: 1,
		BLEMAC:                "AC:27:6E:A6:AA:F5",
		AutoPush:              false,
		PushIntervalSeconds:   60,
	}
}
