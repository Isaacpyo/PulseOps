// Package uptime turns per-day probe rollups into the fixed-width uptime
// series shown on the public status page.
package uptime

import (
	"math"
	"time"
)

// DayRollup is one check's aggregate for a single UTC day.
type DayRollup struct {
	Day       time.Time
	Total     int
	Up        int
	Degraded  int
	Incidents int
}

// Bar is one slot of the uptime strip. Uptime is nil when there was no data.
type Bar struct {
	Day       string   `json:"day"`
	Uptime    *float64 `json:"uptime"`
	Total     int      `json:"total"`
	Degraded  int      `json:"degraded"`
	Incidents int      `json:"incidents"`
}

const dayLayout = "2006-01-02"

// Bars returns exactly n bars, oldest first, ending on today's UTC date.
// Days with no probes are returned with a nil Uptime so the UI can render
// "no data" rather than a misleading 100%.
func Bars(rollups []DayRollup, today time.Time, n int) []Bar {
	if n <= 0 {
		return []Bar{}
	}
	byDay := make(map[string]DayRollup, len(rollups))
	for _, r := range rollups {
		byDay[r.Day.UTC().Format(dayLayout)] = r
	}
	end := truncateDay(today)
	bars := make([]Bar, n)
	for i := 0; i < n; i++ {
		day := end.AddDate(0, 0, i-(n-1))
		key := day.Format(dayLayout)
		bar := Bar{Day: key}
		if r, ok := byDay[key]; ok {
			bar.Total, bar.Degraded, bar.Incidents = r.Total, r.Degraded, r.Incidents
			bar.Uptime = Percent(r.Up, r.Total)
		}
		bars[i] = bar
	}
	return bars
}

// Overall is the probe-weighted uptime across all given rollups.
func Overall(rollups []DayRollup) *float64 {
	var up, total int
	for _, r := range rollups {
		up += r.Up
		total += r.Total
	}
	return Percent(up, total)
}

// Percent returns up/total as a percentage rounded to 3 decimals, or nil
// when total is zero.
func Percent(up, total int) *float64 {
	if total <= 0 {
		return nil
	}
	if up > total {
		up = total
	}
	p := math.Round(float64(up)/float64(total)*100*1000) / 1000
	return &p
}

func truncateDay(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
