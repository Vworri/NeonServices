package display

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/neonphnx/NeonServices/internal/database"
)

type deviceState struct {
	lastStateHash   string
	lastFullRefresh time.Time
}

type Service struct {
	db             *database.DB
	weatherClient  *WeatherClient
	calendarClient *CalendarClient
	renderer       *Renderer

	stateMu sync.Mutex
	states  map[string]*deviceState
}

func NewService(db *database.DB) *Service {
	return &Service{
		db:             db,
		weatherClient:  NewWeatherClient(),
		calendarClient: NewCalendarClient(),
		renderer:       NewRenderer(),
		states:         make(map[string]*deviceState),
	}
}

// GetData builds the current DisplayData and decides partial vs full refresh
func (s *Service) GetData(deviceID string) (*DisplayData, error) {
	cfg, err := s.db.GetDisplayConfig(deviceID)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		// Fallback to default config
		cfg = DefaultConfig(deviceID, 1)
	}

	// Resolve local time in configured timezone
	loc := time.Local
	if cfg.Timezone != "" && cfg.Timezone != "Local" {
		if l, err := time.LoadLocation(cfg.Timezone); err == nil {
			loc = l
		}
	}
	now := time.Now().In(loc)

	// Fetch weather & air quality
	weather, aqi, _ := s.weatherClient.GetWeatherAndAQI(cfg.Latitude, cfg.Longitude)

	// Fetch upcoming calendar events
	events := s.calendarClient.GetEvents(s.db, cfg.DeviceID, cfg.CalendarURL, 5)

	// Partial Refresh Logic
	// Compute hash of content (excluding time & nanosecond timestamps)
	var eventSignatures []string
	for _, e := range events {
		eventSignatures = append(eventSignatures, fmt.Sprintf("%s:%s:%s", e.Title, e.Location, e.StartTime.Format("2006-01-02 15:04")))
	}
	stateData := fmt.Sprintf("%v-%v-%v", weather, aqi, eventSignatures)
	hasher := sha256.New()
	hasher.Write([]byte(stateData))
	currentHash := hex.EncodeToString(hasher.Sum(nil))

	s.stateMu.Lock()
	state, found := s.states[deviceID]
	if !found {
		state = &deviceState{
			lastFullRefresh: time.Time{},
		}
		s.states[deviceID] = state
	}

	fullRefreshInterval := time.Duration(cfg.FullRefreshMinutes) * time.Minute
	if fullRefreshInterval <= 0 {
		fullRefreshInterval = 30 * time.Minute
	}

	// Determine refresh type
	refreshType := "partial"
	if state.lastFullRefresh.IsZero() || time.Since(state.lastFullRefresh) >= fullRefreshInterval || state.lastStateHash != currentHash {
		refreshType = "full"
		state.lastFullRefresh = now
		state.lastStateHash = currentHash
	}
	s.stateMu.Unlock()

	pollSeconds := cfg.PartialRefreshMinutes * 60
	if pollSeconds <= 0 {
		pollSeconds = 60
	}

	return &DisplayData{
		DeviceID:        deviceID,
		TimeStr:         now.Format("03:04 PM"),
		DateStr:         now.Format("January 02, 2006"),
		DayOfWeek:       strings.ToUpper(now.Format("Monday")),
		CityName:        cfg.CityName,
		Weather:         weather,
		AirQuality:      aqi,
		Events:          events,
		RefreshType:     refreshType,
		NextPollSeconds: pollSeconds,
		LastUpdated:     now,
	}, nil
}

// RegisterRoutes registers the reTerminal E1001 OpenDisplay endpoints
func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	// 1. Image Render Endpoints
	mux.HandleFunc("GET /api/v1/display/{device_id}/image.png", s.handleRenderPNG)
	mux.HandleFunc("GET /api/v1/display/{device_id}/image.bmp", s.handleRenderBMP)
	mux.HandleFunc("GET /api/v1/display/{device_id}/image.raw", s.handleRenderRaw)
	mux.HandleFunc("GET /api/v1/display/{device_id}/image", s.handleRenderPNG) // default to png
	mux.HandleFunc("GET /api/v1/display/render", s.handleRenderPNG)

	// Convenient Short URLs for Easy User Setup (minimal typing):
	mux.HandleFunc("GET /screen", func(w http.ResponseWriter, r *http.Request) {
		r.SetPathValue("device_id", "reterminal-01")
		s.handleRenderPNG(w, r)
	})
	mux.HandleFunc("GET /d/{device_id}", s.handleRenderPNG)
	mux.HandleFunc("GET /reterminal.sh", s.handleSetupScript)

	// 2. Data & Status Endpoint
	mux.HandleFunc("GET /api/v1/display/{device_id}/status", s.handleStatus)

	// 3. Configuration Management
	mux.HandleFunc("GET /api/v1/display/{device_id}/config", s.handleGetConfig)
	mux.HandleFunc("POST /api/v1/display/{device_id}/config", s.handleSaveConfig)

	// 4. Custom Calendar Events
	mux.HandleFunc("POST /api/v1/display/{device_id}/events", s.handleAddEvent)
}

func (s *Service) handleRenderPNG(w http.ResponseWriter, r *http.Request) {
	deviceID := r.PathValue("device_id")
	if deviceID == "" {
		deviceID = r.URL.Query().Get("device_id")
	}
	if deviceID == "" {
		deviceID = "reterminal-01"
	}
	data, err := s.GetData(deviceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	pngBytes, err := s.renderer.RenderPNG(data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// OpenDisplay headers for e-Paper partial vs full refresh
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Refresh-Type", data.RefreshType)
	w.Header().Set("X-Next-Poll-Seconds", fmt.Sprintf("%d", data.NextPollSeconds))
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pngBytes)
}

func (s *Service) handleRenderBMP(w http.ResponseWriter, r *http.Request) {
	deviceID := r.PathValue("device_id")
	data, err := s.GetData(deviceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	bmpBytes, err := s.renderer.RenderBMP(data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/bmp")
	w.Header().Set("X-Refresh-Type", data.RefreshType)
	w.Header().Set("X-Next-Poll-Seconds", fmt.Sprintf("%d", data.NextPollSeconds))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(bmpBytes)
}

func (s *Service) handleRenderRaw(w http.ResponseWriter, r *http.Request) {
	deviceID := r.PathValue("device_id")
	data, err := s.GetData(deviceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rawBytes := s.renderer.Render1BitRaw(data)

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Refresh-Type", data.RefreshType)
	w.Header().Set("X-Next-Poll-Seconds", fmt.Sprintf("%d", data.NextPollSeconds))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(rawBytes)
}

func (s *Service) handleStatus(w http.ResponseWriter, r *http.Request) {
	deviceID := r.PathValue("device_id")
	data, err := s.GetData(deviceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    data,
	})
}

func (s *Service) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	deviceID := r.PathValue("device_id")
	cfg, err := s.db.GetDisplayConfig(deviceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if cfg == nil {
		cfg = DefaultConfig(deviceID, 1)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    cfg,
	})
}

func (s *Service) handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	deviceID := r.PathValue("device_id")

	var req ConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if req.FullRefreshMinutes <= 0 {
		req.FullRefreshMinutes = 30
	}
	if req.PartialRefreshMinutes <= 0 {
		req.PartialRefreshMinutes = 1
	}

	cfg := &database.DisplayConfig{
		DeviceID:              deviceID,
		UserID:                req.UserID,
		CityName:              req.CityName,
		Latitude:              req.Latitude,
		Longitude:             req.Longitude,
		Timezone:              req.Timezone,
		CalendarURL:           req.CalendarURL,
		FullRefreshMinutes:    req.FullRefreshMinutes,
		PartialRefreshMinutes: req.PartialRefreshMinutes,
	}

	if err := s.db.SaveDisplayConfig(cfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    cfg,
	})
}

func (s *Service) handleAddEvent(w http.ResponseWriter, r *http.Request) {
	deviceID := r.PathValue("device_id")

	var event database.CalendarEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(w, "invalid event payload", http.StatusBadRequest)
		return
	}
	event.DeviceID = deviceID

	if err := s.db.AddCalendarEvent(&event); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    event,
	})
}


func (s *Service) handleSetupScript(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if host == "" {
		host = "192.168.3.54:8080"
	}
	script := fmt.Sprintf(`#!/usr/bin/env bash
set -e
echo "======================================================"
echo "  Seeed reTerminal E1001 OpenDisplay Auto-Installer   "
echo "======================================================"

TARGET_URL="http://%s/screen"
echo "[+] Configuring dashboard feed: "

# 1. Update OpenDisplay config if present
if [ -d /etc/opendisplay ]; then
    cat << EOF > /etc/opendisplay/config.json
{
  "image_url": "",
  "interval_seconds": 60,
  "rotation": 0
}
EOF
    systemctl restart opendisplay || true
    echo "[+] OpenDisplay service reloaded!"
fi

# 2. Setup periodic curl updater for Linux framebuffer or e-paper service
cat << 'EOF' > /usr/local/bin/update-e-paper.sh
#!/usr/bin/env bash
URL="http://%s/screen"
TMP_IMG="/tmp/epaper_next.png"

# Fetch image with partial refresh detection
HEADERS=$(curl -sSL -D - "$URL" -o "$TMP_IMG")
REFRESH_TYPE=$(echo "$HEADERS" | grep -i "^X-Refresh-Type:" | tr -d '
' | awk '{print $2}')

echo "[$(date)] Screen updated: $REFRESH_TYPE"
EOF
chmod +x /usr/local/bin/update-e-paper.sh

# Run once immediately
/usr/local/bin/update-e-paper.sh || true

echo ""
echo "🎉 reTerminal E1001 setup complete! Screen will refresh every minute."
echo "======================================================"
`, host, host)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(script))
}
