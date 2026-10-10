package uptime

import (
	"testing"
	"time"
)

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestBarsFillsGapsAndOrdersOldestFirst(t *testing.T) {
	today := time.Date(2026, 3, 10, 15, 4, 5, 0, time.UTC)
	bars := Bars([]DayRollup{
		{Day: day("2026-03-10"), Total: 100, Up: 99, Degraded: 3, Incidents: 1},
		{Day: day("2026-03-08"), Total: 10, Up: 10},
		{Day: day("2025-12-01"), Total: 10, Up: 0}, // outside the window
	}, today, 5)

	if len(bars) != 5 {
		t.Fatalf("len = %d, want 5", len(bars))
	}
	wantDays := []string{"2026-03-06", "2026-03-07", "2026-03-08", "2026-03-09", "2026-03-10"}
	for i, b := range bars {
		if b.Day != wantDays[i] {
			t.Fatalf("bar %d day = %s, want %s", i, b.Day, wantDays[i])
		}
	}
	if bars[0].Uptime != nil || bars[3].Uptime != nil {
		t.Fatal("days without data must have nil uptime")
	}
	if got := *bars[2].Uptime; got != 100 {
		t.Fatalf("2026-03-08 uptime = %v, want 100", got)
	}
	last := bars[4]
	if *last.Uptime != 99 || last.Degraded != 3 || last.Incidents != 1 || last.Total != 100 {
		t.Fatalf("unexpected last bar %+v", last)
	}
}

func TestBarsNinetyDaysAcrossMonthBoundary(t *testing.T) {
	today := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	bars := Bars(nil, today, 90)
	if len(bars) != 90 {
		t.Fatalf("len = %d", len(bars))
	}
	if bars[89].Day != "2026-03-01" || bars[0].Day != "2025-12-02" {
		t.Fatalf("window = %s..%s", bars[0].Day, bars[89].Day)
	}
}

func TestBarsUsesUTCDay(t *testing.T) {
	// 23:30 in UTC-5 is already the next day in UTC.
	loc := time.FixedZone("EST", -5*3600)
	today := time.Date(2026, 3, 9, 23, 30, 0, 0, loc)
	bars := Bars(nil, today, 1)
	if bars[0].Day != "2026-03-10" {
		t.Fatalf("day = %s, want 2026-03-10", bars[0].Day)
	}
}

func TestBarsNonPositiveN(t *testing.T) {
	if got := Bars(nil, time.Now(), 0); len(got) != 0 {
		t.Fatalf("want empty, got %d", len(got))
	}
}

func TestOverallAndPercent(t *testing.T) {
	if Overall(nil) != nil {
		t.Fatal("no data must be nil")
	}
	got := Overall([]DayRollup{{Total: 3, Up: 2}, {Total: 1, Up: 1}})
	if got == nil || *got != 75 {
		t.Fatalf("overall = %v, want 75", got)
	}
	if p := Percent(2, 3); *p != 66.667 {
		t.Fatalf("Percent(2,3) = %v, want 66.667", *p)
	}
	if p := Percent(5, 3); *p != 100 {
		t.Fatalf("Percent clamps, got %v", *p)
	}
}
