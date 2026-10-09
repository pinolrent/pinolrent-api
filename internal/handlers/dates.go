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

// todayStr returns the current day as YYYY-MM-DD in the business time zone
// (API.Location, wired from BUSINESS_TIMEZONE). Taking it from the UTC clock
// would move the day boundary: in Managua (UTC-6) the UTC date rolls over at
// 18:00 local, and every booking for "today" made after that would be
// rejected as past. Callers compare it as a string against dates that
// parseDate already validated as canonical YYYY-MM-DD, which sorts
// chronologically. A nil Location means UTC, for tests that build the API
// without config.
func (a *API) todayStr() string {
	loc := a.Location
	if loc == nil {
		loc = time.UTC
	}
	return timeNow().In(loc).Format(dateLayout)
}

// timeNow is the wall clock, swapped by tests to pin the calendar day (the
// package's tests never run in parallel).
var timeNow = time.Now
