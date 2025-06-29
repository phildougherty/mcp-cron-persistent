// SPDX-License-Identifier: AGPL-3.0-only
package scheduler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// DefaultHolidayProvider provides basic holiday checking
type DefaultHolidayProvider struct {
	cache map[string][]Holiday
	mu    sync.RWMutex
}

// NewDefaultHolidayProvider creates a new default holiday provider
func NewDefaultHolidayProvider() *DefaultHolidayProvider {
	return &DefaultHolidayProvider{
		cache: make(map[string][]Holiday),
	}
}

// IsHoliday checks if a date is a holiday
func (dhp *DefaultHolidayProvider) IsHoliday(date time.Time, timezone string) (bool, string, error) {
	// Simple implementation - can be enhanced with external APIs
	holidays := dhp.getDefaultHolidays(date.Year(), timezone)

	for _, holiday := range holidays {
		if holiday.Date.Year() == date.Year() &&
			holiday.Date.Month() == date.Month() &&
			holiday.Date.Day() == date.Day() {
			return true, holiday.Name, nil
		}
	}

	return false, "", nil
}

// GetHolidaysInRange returns holidays in a date range
func (dhp *DefaultHolidayProvider) GetHolidaysInRange(start, end time.Time, timezone string) ([]Holiday, error) {
	var holidays []Holiday

	for year := start.Year(); year <= end.Year(); year++ {
		yearHolidays := dhp.getDefaultHolidays(year, timezone)
		for _, holiday := range yearHolidays {
			if (holiday.Date.After(start) || holiday.Date.Equal(start)) &&
				(holiday.Date.Before(end) || holiday.Date.Equal(end)) {
				holidays = append(holidays, holiday)
			}
		}
	}

	return holidays, nil
}

// getDefaultHolidays returns default holidays for a year
func (dhp *DefaultHolidayProvider) getDefaultHolidays(year int, timezone string) []Holiday {
	// Check cache first
	cacheKey := fmt.Sprintf("%d_%s", year, timezone)
	dhp.mu.RLock()
	if holidays, exists := dhp.cache[cacheKey]; exists {
		dhp.mu.RUnlock()
		return holidays
	}
	dhp.mu.RUnlock()

	// This is a simplified implementation
	// In production, you'd want to use a proper holiday API or database

	loc, _ := time.LoadLocation(timezone)
	if loc == nil {
		loc = time.UTC
	}

	holidays := []Holiday{
		{
			Name:     "New Year's Day",
			Date:     time.Date(year, 1, 1, 0, 0, 0, 0, loc),
			Type:     "national",
			Timezone: timezone,
		},
		{
			Name:     "Christmas Day",
			Date:     time.Date(year, 12, 25, 0, 0, 0, 0, loc),
			Type:     "national",
			Timezone: timezone,
		},
		{
			Name:     "Independence Day",
			Date:     time.Date(year, 7, 4, 0, 0, 0, 0, loc),
			Type:     "national",
			Timezone: timezone,
		},
		{
			Name:     "Thanksgiving",
			Date:     calculateThanksgiving(year, loc),
			Type:     "national",
			Timezone: timezone,
		},
		// Add more holidays as needed
	}

	// Cache the result
	dhp.mu.Lock()
	dhp.cache[cacheKey] = holidays
	dhp.mu.Unlock()

	return holidays
}

// calculateThanksgiving calculates Thanksgiving (4th Thursday of November)
func calculateThanksgiving(year int, loc *time.Location) time.Time {
	nov1 := time.Date(year, 11, 1, 0, 0, 0, 0, loc)

	// Find the first Thursday of November
	daysUntilThursday := (4 - int(nov1.Weekday()) + 7) % 7
	firstThursday := nov1.AddDate(0, 0, daysUntilThursday)

	// Fourth Thursday is 21 days later
	return firstThursday.AddDate(0, 0, 21)
}

// APIHolidayProvider provides holiday checking via external API
type APIHolidayProvider struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	cache      map[string][]Holiday
	mu         sync.RWMutex
}

// NewAPIHolidayProvider creates a new API-based holiday provider
func NewAPIHolidayProvider(apiKey, baseURL string) *APIHolidayProvider {
	return &APIHolidayProvider{
		apiKey:     apiKey,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		cache:      make(map[string][]Holiday),
	}
}

// IsHoliday checks if a date is a holiday using external API
func (ahp *APIHolidayProvider) IsHoliday(date time.Time, timezone string) (bool, string, error) {
	holidays, err := ahp.getHolidaysForYear(date.Year(), timezone)
	if err != nil {
		return false, "", err
	}

	for _, holiday := range holidays {
		if holiday.Date.Year() == date.Year() &&
			holiday.Date.Month() == date.Month() &&
			holiday.Date.Day() == date.Day() {
			return true, holiday.Name, nil
		}
	}

	return false, "", nil
}

// GetHolidaysInRange returns holidays in a date range using external API
func (ahp *APIHolidayProvider) GetHolidaysInRange(start, end time.Time, timezone string) ([]Holiday, error) {
	var allHolidays []Holiday

	for year := start.Year(); year <= end.Year(); year++ {
		holidays, err := ahp.getHolidaysForYear(year, timezone)
		if err != nil {
			return nil, err
		}

		for _, holiday := range holidays {
			if (holiday.Date.After(start) || holiday.Date.Equal(start)) &&
				(holiday.Date.Before(end) || holiday.Date.Equal(end)) {
				allHolidays = append(allHolidays, holiday)
			}
		}
	}

	return allHolidays, nil
}

// getHolidaysForYear fetches holidays for a specific year
func (ahp *APIHolidayProvider) getHolidaysForYear(year int, timezone string) ([]Holiday, error) {
	cacheKey := fmt.Sprintf("%d_%s", year, timezone)

	// Check cache first
	ahp.mu.RLock()
	if holidays, exists := ahp.cache[cacheKey]; exists {
		ahp.mu.RUnlock()
		return holidays, nil
	}
	ahp.mu.RUnlock()

	// Make API request
	url := fmt.Sprintf("%s/holidays?year=%d&timezone=%s&key=%s", ahp.baseURL, year, timezone, ahp.apiKey)

	resp, err := ahp.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch holidays: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("holiday API returned status %d", resp.StatusCode)
	}

	var apiResponse struct {
		Holidays []struct {
			Name string `json:"name"`
			Date string `json:"date"`
			Type string `json:"type"`
		} `json:"holidays"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResponse); err != nil {
		return nil, fmt.Errorf("failed to parse holiday API response: %w", err)
	}

	// Convert API response to our format
	loc, _ := time.LoadLocation(timezone)
	if loc == nil {
		loc = time.UTC
	}

	var holidays []Holiday
	for _, h := range apiResponse.Holidays {
		date, err := time.ParseInLocation("2006-01-02", h.Date, loc)
		if err != nil {
			continue // Skip invalid dates
		}

		holidays = append(holidays, Holiday{
			Name:     h.Name,
			Date:     date,
			Type:     h.Type,
			Timezone: timezone,
		})
	}

	// Cache the result
	ahp.mu.Lock()
	ahp.cache[cacheKey] = holidays
	ahp.mu.Unlock()

	return holidays, nil
}
