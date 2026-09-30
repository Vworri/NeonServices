package display

import (
	"bufio"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/neonphnx/NeonServices/internal/database"
)

type CalendarClient struct {
	httpClient *http.Client
}

func NewCalendarClient() *CalendarClient {
	return &CalendarClient{
		httpClient: &http.Client{Timeout: 8 * time.Second},
	}
}

func (c *CalendarClient) GetEvents(db *database.DB, deviceID string, calendarURL string, limit int) []EventInfo {
	if limit <= 0 {
		limit = 5
	}

	var allEvents []EventInfo

	// 1. Fetch DB events
	if db != nil {
		dbEvents, err := db.GetUpcomingCalendarEvents(deviceID, limit)
		if err == nil {
			for _, e := range dbEvents {
				allEvents = append(allEvents, EventInfo{
					Title:     e.Title,
					StartTime: e.StartTime,
					EndTime:   e.EndTime,
					Location:  e.Location,
				})
			}
		}
	}

	// 2. Fetch iCal URL if provided
	if calendarURL != "" {
		icsEvents, err := c.fetchICS(calendarURL)
		if err == nil {
			allEvents = append(allEvents, icsEvents...)
		}
	}

	// Filter out events that ended before now
	now := time.Now()
	var upcoming []EventInfo
	for _, e := range allEvents {
		if e.EndTime.After(now) {
			upcoming = append(upcoming, e)
		}
	}

	// Sort chronologically
	sort.Slice(upcoming, func(i, j int) bool {
		return upcoming[i].StartTime.Before(upcoming[j].StartTime)
	})

	if len(upcoming) > limit {
		upcoming = upcoming[:limit]
	}

	// If no events found, provide default placeholder
	if len(upcoming) == 0 {
		upcoming = []EventInfo{
			{
				Title:     "No upcoming events",
				StartTime: now,
				EndTime:   now.Add(1 * time.Hour),
				Location:  "Calendar synced",
			},
		}
	}

	return upcoming
}

// fetchICS downloads and parses basic iCal VEVENT entries
func (c *CalendarClient) fetchICS(url string) ([]EventInfo, error) {
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return parseICS(resp.Body)
}

func parseICS(r io.Reader) ([]EventInfo, error) {
	scanner := bufio.NewScanner(r)
	var events []EventInfo
	var inEvent bool
	var summary, location string
	var startTime, endTime time.Time

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "BEGIN:VEVENT" {
			inEvent = true
			summary = ""
			location = ""
			startTime = time.Time{}
			endTime = time.Time{}
			continue
		}
		if line == "END:VEVENT" {
			if inEvent && summary != "" {
				if endTime.IsZero() {
					endTime = startTime.Add(1 * time.Hour)
				}
				events = append(events, EventInfo{
					Title:     summary,
					StartTime: startTime,
					EndTime:   endTime,
					Location:  location,
				})
			}
			inEvent = false
			continue
		}

		if !inEvent {
			continue
		}

		if strings.HasPrefix(line, "SUMMARY:") {
			summary = strings.TrimPrefix(line, "SUMMARY:")
		} else if strings.HasPrefix(line, "LOCATION:") {
			location = strings.TrimPrefix(line, "LOCATION:")
		} else if strings.HasPrefix(line, "DTSTART") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				startTime = parseICalTime(parts[1])
			}
		} else if strings.HasPrefix(line, "DTEND") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				endTime = parseICalTime(parts[1])
			}
		}
	}

	return events, scanner.Err()
}

func parseICalTime(s string) time.Time {
	// Standard iCal formats: 20260930T150000Z or 20260930T150000 or 20260930
	formats := []string{
		"20060102T150405Z",
		"20060102T150405",
		"20060102",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Now()
}
