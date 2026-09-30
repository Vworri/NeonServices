package display

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/neonphnx/NeonServices/internal/database"
)

func TestDisplayRendererAndFormats(t *testing.T) {
	renderer := NewRenderer()

	data := &DisplayData{
		DeviceID:  "reterminal-test",
		TimeStr:   "10:45 AM",
		DateStr:   "September 30, 2026",
		DayOfWeek: "WEDNESDAY",
		CityName:  "New York",
		Weather: WeatherInfo{
			Temperature: 72.5,
			TempUnit:    "°F",
			Condition:   "Partly Cloudy",
			WeatherCode: 2,
			TempMax:     78.0,
			TempMin:     62.0,
			Humidity:    55,
			WindSpeed:   6.5,
		},
		AirQuality: AirQualityInfo{
			AQI:   32,
			Level: "Good",
			PM25:  7.8,
			PM10:  11.2,
		},
		Events: []EventInfo{
			{
				Title:     "Team Architecture Review",
				StartTime: time.Now(),
				EndTime:   time.Now().Add(1 * time.Hour),
				Location:  "Zoom Room 1",
			},
			{
				Title:     "Deploy reTerminal Display Service",
				StartTime: time.Now().Add(2 * time.Hour),
				EndTime:   time.Now().Add(3 * time.Hour),
				Location:  "Lab Machine",
			},
		},
		RefreshType:     "partial",
		NextPollSeconds: 60,
		LastUpdated:     time.Now(),
	}

	// 1. Test Canvas
	canvas := renderer.RenderCanvas(data)
	if canvas.Bounds().Dx() != DisplayWidth || canvas.Bounds().Dy() != DisplayHeight {
		t.Fatalf("Canvas dimensions mismatch: got %dx%d, expected %dx%d",
			canvas.Bounds().Dx(), canvas.Bounds().Dy(), DisplayWidth, DisplayHeight)
	}

	// 2. Test PNG encoding
	pngBytes, err := renderer.RenderPNG(data)
	if err != nil {
		t.Fatalf("RenderPNG failed: %v", err)
	}
	if len(pngBytes) == 0 {
		t.Fatalf("PNG bytes empty")
	}
	decoded, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("Decoded PNG failed: %v", err)
	}
	if decoded.Bounds().Dx() != 800 || decoded.Bounds().Dy() != 480 {
		t.Errorf("Decoded PNG dimensions mismatch: %v", decoded.Bounds())
	}

	// 3. Test BMP encoding
	bmpBytes, err := renderer.RenderBMP(data)
	if err != nil {
		t.Fatalf("RenderBMP failed: %v", err)
	}
	if len(bmpBytes) < 54 {
		t.Fatalf("BMP too small")
	}
	// Check BMP magic header
	if bmpBytes[0] != 'B' || bmpBytes[1] != 'M' {
		t.Errorf("Invalid BMP header")
	}

	// 4. Test 1-Bit Raw Framebuffer (800x480 / 8 = 48,000 bytes)
	rawBytes := renderer.Render1BitRaw(data)
	expectedLen := (800 * 480) / 8
	if len(rawBytes) != expectedLen {
		t.Fatalf("Raw 1-bit buffer length mismatch: got %d, expected %d", len(rawBytes), expectedLen)
	}

	_ = image.Point{}
}

func TestPartialRefreshLogic(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "neon_display_test_*")
	if err != nil {
		t.Fatalf("TempDir failed: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("Database Open failed: %v", err)
	}
	defer db.Close()

	service := NewService(db)

	cfg := &database.DisplayConfig{
		DeviceID:              "e1001-office",
		UserID:                1,
		CityName:              "Austin",
		Latitude:              30.2672,
		Longitude:             -97.7431,
		Timezone:              "America/Chicago",
		FullRefreshMinutes:    30,
		PartialRefreshMinutes: 1,
	}
	if err := db.SaveDisplayConfig(cfg); err != nil {
		t.Fatalf("SaveDisplayConfig failed: %v", err)
	}

	// First query should be a "full" refresh
	data1, err := service.GetData("e1001-office")
	if err != nil {
		t.Fatalf("GetData failed: %v", err)
	}
	if data1.RefreshType != "full" {
		t.Errorf("First refresh should be 'full', got '%s'", data1.RefreshType)
	}

	// Immediate second query (same content, within 30 min window) should be "partial" refresh
	data2, err := service.GetData("e1001-office")
	if err != nil {
		t.Fatalf("GetData 2 failed: %v", err)
	}
	if data2.RefreshType != "partial" {
		t.Errorf("Subsequent refresh should be 'partial', got '%s'", data2.RefreshType)
	}
}
