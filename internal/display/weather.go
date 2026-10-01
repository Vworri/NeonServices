package display

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type WeatherClient struct {
	httpClient *http.Client
	cacheMu    sync.RWMutex
	cache      map[string]*weatherCacheItem
}

type weatherCacheItem struct {
	weather    WeatherInfo
	airQuality AirQualityInfo
	cachedAt   time.Time
}

func NewWeatherClient() *WeatherClient {
	return &WeatherClient{
		httpClient: &http.Client{Timeout: 8 * time.Second},
		cache:      make(map[string]*weatherCacheItem),
	}
}

func (c *WeatherClient) GetWeatherAndAQI(lat, lon float64) (WeatherInfo, AirQualityInfo, error) {
	cacheKey := fmt.Sprintf("%.2f,%.2f", lat, lon)

	c.cacheMu.RLock()
	if item, found := c.cache[cacheKey]; found && time.Since(item.cachedAt) < 15*time.Minute {
		c.cacheMu.RUnlock()
		return item.weather, item.airQuality, nil
	}
	c.cacheMu.RUnlock()

	// Fetch weather and AQI
	weather, err := c.fetchWeather(lat, lon)
	if err != nil {
		// Use cached even if expired as fallback
		c.cacheMu.RLock()
		if item, found := c.cache[cacheKey]; found {
			c.cacheMu.RUnlock()
			return item.weather, item.airQuality, nil
		}
		c.cacheMu.RUnlock()
		return defaultWeather(), defaultAirQuality(), err
	}

	aqi, _ := c.fetchAirQuality(lat, lon)

	c.cacheMu.Lock()
	c.cache[cacheKey] = &weatherCacheItem{
		weather:    weather,
		airQuality: aqi,
		cachedAt:   time.Now(),
	}
	c.cacheMu.Unlock()

	return weather, aqi, nil
}

type openMeteoWeatherResponse struct {
	Current struct {
		Temperature float64 `json:"temperature_2m"`
		Humidity    int     `json:"relative_humidity_2m"`
		WeatherCode int     `json:"weather_code"`
		WindSpeed   float64 `json:"wind_speed_10m"`
	} `json:"current"`
	Daily struct {
		TempMax []float64 `json:"temperature_2m_max"`
		TempMin []float64 `json:"temperature_2m_min"`
	} `json:"daily"`
}

func (c *WeatherClient) fetchWeather(lat, lon float64) (WeatherInfo, error) {
	url := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f&current=temperature_2m,relative_humidity_2m,weather_code,wind_speed_10m&daily=temperature_2m_max,temperature_2m_min&temperature_unit=fahrenheit&wind_speed_unit=mph&timezone=auto",
		lat, lon,
	)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return defaultWeather(), err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return defaultWeather(), fmt.Errorf("weather API returned status: %d", resp.StatusCode)
	}

	var data openMeteoWeatherResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return defaultWeather(), err
	}

	high := data.Current.Temperature
	low := data.Current.Temperature
	if len(data.Daily.TempMax) > 0 {
		high = data.Daily.TempMax[0]
	}
	if len(data.Daily.TempMin) > 0 {
		low = data.Daily.TempMin[0]
	}

	return WeatherInfo{
		Temperature: data.Current.Temperature,
		TempUnit:    "°F",
		Condition:   interpretWMOWeatherCode(data.Current.WeatherCode),
		WeatherCode: data.Current.WeatherCode,
		TempMax:     high,
		TempMin:     low,
		Humidity:    data.Current.Humidity,
		WindSpeed:   data.Current.WindSpeed,
	}, nil
}

type openMeteoAQIResponse struct {
	Current struct {
		USAQI float64 `json:"us_aqi"`
		PM25  float64 `json:"pm2_5"`
		PM10  float64 `json:"pm10"`
	} `json:"current"`
}

func (c *WeatherClient) fetchAirQuality(lat, lon float64) (AirQualityInfo, error) {
	url := fmt.Sprintf(
		"https://air-quality-api.open-meteo.com/v1/air-quality?latitude=%.4f&longitude=%.4f&current=us_aqi,pm2_5,pm10",
		lat, lon,
	)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return defaultAirQuality(), err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return defaultAirQuality(), fmt.Errorf("air quality API returned status: %d", resp.StatusCode)
	}

	var data openMeteoAQIResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return defaultAirQuality(), err
	}

	aqiInt := int(data.Current.USAQI)
	level := interpretAQILevel(aqiInt)

	return AirQualityInfo{
		AQI:   aqiInt,
		Level: level,
		PM25:  data.Current.PM25,
		PM10:  data.Current.PM10,
	}, nil
}

func interpretWMOWeatherCode(code int) string {
	switch code {
	case 0:
		return "Clear Sky"
	case 1:
		return "Mainly Clear"
	case 2:
		return "Partly Cloudy"
	case 3:
		return "Overcast"
	case 45, 48:
		return "Foggy"
	case 51, 53, 55:
		return "Drizzle"
	case 61, 63, 65:
		return "Rain"
	case 71, 73, 75:
		return "Snow"
	case 77:
		return "Snow Grains"
	case 80, 81, 82:
		return "Rain Showers"
	case 85, 86:
		return "Snow Showers"
	case 95, 96, 99:
		return "Thunderstorm"
	default:
		return "Fair"
	}
}

func interpretAQILevel(aqi int) string {
	switch {
	case aqi <= 50:
		return "Good"
	case aqi <= 100:
		return "Moderate"
	case aqi <= 150:
		return "Sensitive"
	case aqi <= 200:
		return "Unhealthy"
	case aqi <= 300:
		return "Very Unhealthy"
	default:
		return "Hazardous"
	}
}

func defaultWeather() WeatherInfo {
	return WeatherInfo{
		Temperature: 72.0,
		TempUnit:    "°F",
		Condition:   "Partly Cloudy",
		WeatherCode: 2,
		TempMax:     78.0,
		TempMin:     62.0,
		Humidity:    50,
		WindSpeed:   5.0,
	}
}

func defaultAirQuality() AirQualityInfo {
	return AirQualityInfo{
		AQI:   35,
		Level: "Good",
		PM25:  8.5,
		PM10:  12.0,
	}
}


// ZipLocation represents geocoded location data for a postal/zip code
type ZipLocation struct {
	ZipCode   string  `json:"zip_code"`
	CityName  string  `json:"city_name"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Timezone  string  `json:"timezone"`
}

// LookupZipCode resolves a US ZIP code to City, Latitude, Longitude, and Timezone
func LookupZipCode(zip string) (*ZipLocation, error) {
	zip = strings.TrimSpace(zip)
	if len(zip) > 5 {
		zip = zip[:5]
	}
	if len(zip) < 3 {
		return nil, fmt.Errorf("invalid zip code: %s", zip)
	}

	client := &http.Client{Timeout: 5 * time.Second}

	// 1. Try Zippopotam API
	urlZippo := fmt.Sprintf("https://api.zippopotam.us/us/%s", zip)
	if resp, err := client.Get(urlZippo); err == nil && resp.StatusCode == http.StatusOK {
		defer resp.Body.Close()
		var res struct {
			Places []struct {
				PlaceName string `json:"place name"`
				Longitude string `json:"longitude"`
				Latitude  string `json:"latitude"`
				StateAbbr string `json:"state abbreviation"`
			} `json:"places"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&res); err == nil && len(res.Places) > 0 {
			p := res.Places[0]
			var lat, lon float64
			fmt.Sscanf(p.Latitude, "%f", &lat)
			fmt.Sscanf(p.Longitude, "%f", &lon)
			tz := inferTimezone(p.StateAbbr, lon)
			return &ZipLocation{
				ZipCode:   zip,
				CityName:  fmt.Sprintf("%s, %s", p.PlaceName, p.StateAbbr),
				Latitude:  lat,
				Longitude: lon,
				Timezone:  tz,
			}, nil
		}
	}

	// 2. Try Open-Meteo Geocoding API
	urlMeteo := fmt.Sprintf("https://geocoding-api.open-meteo.com/v1/search?name=%s&count=1&language=en&format=json", zip)
	if resp, err := client.Get(urlMeteo); err == nil && resp.StatusCode == http.StatusOK {
		defer resp.Body.Close()
		var res struct {
			Results []struct {
				Name      string  `json:"name"`
				Latitude  float64 `json:"latitude"`
				Longitude float64 `json:"longitude"`
				Timezone  string  `json:"timezone"`
				Admin1    string  `json:"admin1"`
			} `json:"results"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&res); err == nil && len(res.Results) > 0 {
			r := res.Results[0]
			tz := r.Timezone
			if tz == "" {
				tz = inferTimezone(r.Admin1, r.Longitude)
			}
			return &ZipLocation{
				ZipCode:   zip,
				CityName:  fmt.Sprintf("%s, %s", r.Name, r.Admin1),
				Latitude:  r.Latitude,
				Longitude: r.Longitude,
				Timezone:  tz,
			}, nil
		}
	}

	// 3. Built-in Offline Fallback based on 3-digit Zip prefix
	return offlineZipFallback(zip), nil
}

func inferTimezone(state string, lon float64) string {
	state = strings.ToUpper(strings.TrimSpace(state))
	switch state {
	case "CT", "DE", "FL", "GA", "IN", "KY", "ME", "MD", "MA", "MI", "NH", "NJ", "NY", "NC", "OH", "PA", "RI", "SC", "VT", "VA", "WV", "DC":
		return "America/New_York"
	case "AL", "AR", "IL", "IA", "KS", "LA", "MN", "MS", "MO", "NE", "ND", "OK", "SD", "TN", "TX", "WI":
		return "America/Chicago"
	case "AZ":
		return "America/Phoenix"
	case "CO", "ID", "MT", "NM", "UT", "WY":
		return "America/Denver"
	case "CA", "NV", "OR", "WA":
		return "America/Los_Angeles"
	case "AK":
		return "America/Anchorage"
	case "HI":
		return "Pacific/Honolulu"
	default:
		if lon > -85.0 {
			return "America/New_York"
		} else if lon > -100.0 {
			return "America/Chicago"
		} else if lon > -115.0 {
			return "America/Denver"
		} else {
			return "America/Los_Angeles"
		}
	}
}

func offlineZipFallback(zip string) *ZipLocation {
	prefix := 100
	if len(zip) >= 3 {
		fmt.Sscanf(zip[:3], "%d", &prefix)
	}

	switch {
	case prefix <= 69: // New England
		return &ZipLocation{ZipCode: zip, CityName: "Boston, MA", Latitude: 42.3601, Longitude: -71.0589, Timezone: "America/New_York"}
	case prefix <= 99: // NJ / NY metro
		return &ZipLocation{ZipCode: zip, CityName: "Newark, NJ", Latitude: 40.7357, Longitude: -74.1724, Timezone: "America/New_York"}
	case prefix <= 149: // NY state
		return &ZipLocation{ZipCode: zip, CityName: "New York, NY", Latitude: 40.7128, Longitude: -74.0060, Timezone: "America/New_York"}
	case prefix <= 196: // Pennsylvania
		return &ZipLocation{ZipCode: zip, CityName: "Philadelphia, PA", Latitude: 39.9526, Longitude: -75.1652, Timezone: "America/New_York"}
	case prefix <= 246: // VA / WV / DC
		return &ZipLocation{ZipCode: zip, CityName: "Washington, DC", Latitude: 38.9072, Longitude: -77.0369, Timezone: "America/New_York"}
	case prefix <= 299: // NC / SC
		return &ZipLocation{ZipCode: zip, CityName: "Charlotte, NC", Latitude: 35.2271, Longitude: -80.8431, Timezone: "America/New_York"}
	case prefix <= 319: // GA
		return &ZipLocation{ZipCode: zip, CityName: "Atlanta, GA", Latitude: 33.7490, Longitude: -84.3880, Timezone: "America/New_York"}
	case prefix <= 349: // FL
		return &ZipLocation{ZipCode: zip, CityName: "Orlando, FL", Latitude: 28.5383, Longitude: -81.3792, Timezone: "America/New_York"}
	case prefix <= 399: // AL / TN / MS
		return &ZipLocation{ZipCode: zip, CityName: "Nashville, TN", Latitude: 36.1627, Longitude: -86.7816, Timezone: "America/Chicago"}
	case prefix <= 459: // OH / KY
		return &ZipLocation{ZipCode: zip, CityName: "Columbus, OH", Latitude: 39.9612, Longitude: -82.9988, Timezone: "America/New_York"}
	case prefix <= 499: // MI / IN
		return &ZipLocation{ZipCode: zip, CityName: "Detroit, MI", Latitude: 42.3314, Longitude: -83.0458, Timezone: "America/New_York"}
	case prefix <= 599: // WI / MN / IA
		return &ZipLocation{ZipCode: zip, CityName: "Minneapolis, MN", Latitude: 44.9778, Longitude: -93.2650, Timezone: "America/Chicago"}
	case prefix <= 699: // IL / MO / KS
		return &ZipLocation{ZipCode: zip, CityName: "Chicago, IL", Latitude: 41.8781, Longitude: -87.6298, Timezone: "America/Chicago"}
	case prefix <= 799: // TX / LA / OK
		return &ZipLocation{ZipCode: zip, CityName: "Dallas, TX", Latitude: 32.7767, Longitude: -96.7970, Timezone: "America/Chicago"}
	case prefix <= 849: // CO / UT / WY
		return &ZipLocation{ZipCode: zip, CityName: "Denver, CO", Latitude: 39.7392, Longitude: -104.9903, Timezone: "America/Denver"}
	case prefix <= 889: // AZ / NM / NV
		return &ZipLocation{ZipCode: zip, CityName: "Phoenix, AZ", Latitude: 33.4484, Longitude: -112.0740, Timezone: "America/Phoenix"}
	case prefix <= 966: // CA / OR / WA
		return &ZipLocation{ZipCode: zip, CityName: "Los Angeles, CA", Latitude: 34.0522, Longitude: -118.2437, Timezone: "America/Los_Angeles"}
	case prefix <= 969: // HI
		return &ZipLocation{ZipCode: zip, CityName: "Honolulu, HI", Latitude: 21.3069, Longitude: -157.8583, Timezone: "Pacific/Honolulu"}
	default: // AK
		return &ZipLocation{ZipCode: zip, CityName: "Anchorage, AK", Latitude: 61.2181, Longitude: -149.9003, Timezone: "America/Anchorage"}
	}
}
