package bundle

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseSince accepts either an absolute date (YYYY-MM-DD, interpreted as UTC
// midnight) or a relative duration ending in d/h/m (e.g. 7d, 48h, 90m) measured
// back from now. It returns the cutoff instant; sessions updated at or after it
// pass the filter. It is shared by the CLI (`export --since`) and the desktop UI.
func ParseSince(s string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	if d, err := parseDayDuration(s); err == nil {
		return time.Now().Add(-d), nil
	}
	return time.Time{}, fmt.Errorf("invalid --since %q: use a date (YYYY-MM-DD) or a duration like 7d, 48h, 90m", s)
}

// parseDayDuration extends time.ParseDuration with a "d" (days) unit, which the
// standard library does not support.
func parseDayDuration(s string) (time.Duration, error) {
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("invalid day duration %q", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid duration %q", s)
	}
	return d, nil
}
