package handlers

import (
	"errors"
	"time"
)

const dateLayout = "2006-01-02"

// maxRentalDays caps how long a single reservation may span.
const maxRentalDays = 30

func parseDate(s string) (time.Time, error) {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return time.Time{}, err
	}
	if t.Format(dateLayout) != s {
		return time.Time{}, errors.New("invalid date")
	}
	return t, nil
}

func todayStart() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}
