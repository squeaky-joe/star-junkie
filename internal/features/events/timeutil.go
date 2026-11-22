package events

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var clockLayouts = []string{"15:04", "3:04pm", "3:04 pm", "3pm", "3 pm"}

// parseClock accepts "20:00", "8:30pm", "8pm".
func parseClock(s string) (h, m int, err error) {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, l := range clockLayouts {
		if t, e := time.Parse(l, s); e == nil {
			return t.Hour(), t.Minute(), nil
		}
	}
	return 0, 0, errors.New("time should look like 20:00 or 8pm")
}

// parseWhen builds a start time in loc from a "YYYY-MM-DD" date and a clock string.
// An empty date or clock falls back to base (used by /event edit).
func parseWhen(date, clock string, loc *time.Location, base time.Time) (time.Time, error) {
	y, mo, d := base.In(loc).Date()
	h, mi, _ := base.In(loc).Clock()
	if date = strings.TrimSpace(date); date != "" {
		t, err := time.Parse("2006-01-02", date)
		if err != nil {
			return time.Time{}, errors.New("date should look like 2026-12-05")
		}
		y, mo, d = t.Date()
	}
	if strings.TrimSpace(clock) != "" {
		var err error
		if h, mi, err = parseClock(clock); err != nil {
			return time.Time{}, err
		}
	}
	return time.Date(y, mo, d, h, mi, 0, 0, loc), nil
}

// timeTags renders Discord timestamp tags so everyone sees their own local time.
func timeTags(t time.Time) string {
	return fmt.Sprintf("<t:%d:F> (<t:%d:R>)", t.Unix(), t.Unix())
}

// reminderDue says what the scheduler should do for an event right now:
// "1h" (hour-before ping), "start" (starting ping), "skip" (missed by a lot, mark done silently), or "".
func reminderDue(start, now time.Time, sent1h, sentStart bool) string {
	if sentStart {
		return ""
	}
	if !now.Before(start) {
		if now.Sub(start) <= 15*time.Minute {
			return "start"
		}
		return "skip"
	}
	if !sent1h && start.Sub(now) <= time.Hour {
		return "1h"
	}
	return ""
}

// chunk splits ids into groups of at most n (Discord caps allowed-mention user lists at 100).
func chunk(ids []string, n int) [][]string {
	var out [][]string
	for len(ids) > n {
		out = append(out, ids[:n])
		ids = ids[n:]
	}
	if len(ids) > 0 {
		out = append(out, ids)
	}
	return out
}

// mentionList renders up to max user mentions, then "+N more". Empty list renders an em dash.
func mentionList(ids []string, max int) string {
	if len(ids) == 0 {
		return "—"
	}
	var b strings.Builder
	for n, id := range ids {
		if n == max {
			fmt.Fprintf(&b, " +%d more", len(ids)-max)
			break
		}
		if n > 0 {
			b.WriteByte(' ')
		}
		b.WriteString("<@" + id + ">")
	}
	return b.String()
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
