package display

import (
	"encoding/json"
	"fmt"
	"net/http"
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
