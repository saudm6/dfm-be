// Package calendar tells whether DFM is trading at a given instant: its
// sessions, its trading week, and the days it is closed. All wall-clock
// times are in Asia/Dubai (domain.DubaiTZ).
package calendar

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/saudm6/dfm-be/internal/domain"
)

// Session is the phase of the DFM trading day.
type Session string

// Sessions of a DFM trading day, in order. ClosingAuction covers DFM's
// pre-closing session and the trade-at-last session that follows it.
const (
	PreOpen        Session = "pre-open"
	Continuous     Session = "continuous"
	ClosingAuction Session = "closing-auction"
	Closed         Session = "closed"
)

// TimeOfDay is a wall-clock time in Dubai, as minutes after midnight.
type TimeOfDay int

// ParseTimeOfDay parses a 24-hour "15:04" time such as "09:30".
func ParseTimeOfDay(s string) (TimeOfDay, error) {
	t, err := time.Parse("15:04", s)
	if err != nil || len(s) != len("15:04") {
		return 0, fmt.Errorf("calendar: time of day %q is not HH:MM", s)
	}
	return TimeOfDay(t.Hour()*60 + t.Minute()), nil
}

// String returns tod as "15:04".
func (tod TimeOfDay) String() string { return fmt.Sprintf("%02d:%02d", tod/60, tod%60) }

// UnmarshalText sets tod to ParseTimeOfDay(text).
func (tod *TimeOfDay) UnmarshalText(text []byte) error {
	v, err := ParseTimeOfDay(string(text))
	if err != nil {
		return err
	}
	*tod = v
	return nil
}

func (tod TimeOfDay) on(day time.Time) time.Time {
	return day.Add(time.Duration(tod) * time.Minute)
}

// Weekday is a day of the week that decodes from its English name.
type Weekday time.Weekday

// UnmarshalText sets d from a case-insensitive English day name, full or
// three-letter, such as "Mon" or "Monday".
func (d *Weekday) UnmarshalText(text []byte) error {
	s := strings.ToLower(strings.TrimSpace(string(text)))
	for wd := time.Sunday; wd <= time.Saturday; wd++ {
		name := strings.ToLower(wd.String())
		if s == name || s == name[:3] {
			*d = Weekday(wd)
			return nil
		}
	}
	return fmt.Errorf("calendar: %q is not a day of the week", text)
}

// Hours are the session start times and trading week of DFM.
// Sessions run PreOpen to Open, Open to ClosingAuction, and ClosingAuction
// to Close.
type Hours struct {
	PreOpen        TimeOfDay
	Open           TimeOfDay
	ClosingAuction TimeOfDay
	Close          TimeOfDay
	Week           []time.Weekday
}

// Validate reports whether the sessions follow each other within one day
// and the week has at least one trading day.
func (h Hours) Validate() error {
	if h.PreOpen < 0 || h.PreOpen >= h.Open || h.Open >= h.ClosingAuction ||
		h.ClosingAuction >= h.Close || h.Close > 24*60 {
		return fmt.Errorf("calendar: sessions %s, %s, %s, %s are not in order within one day",
			h.PreOpen, h.Open, h.ClosingAuction, h.Close)
	}
	if len(h.Week) == 0 {
		return errors.New("calendar: the trading week has no days")
	}
	for _, wd := range h.Week {
		if wd < time.Sunday || wd > time.Saturday {
			return fmt.Errorf("calendar: %d is not a day of the week", wd)
		}
	}
	return nil
}

type date struct {
	year  int
	month time.Month
	day   int
}

func dateOf(t time.Time) date {
	y, m, d := t.In(domain.DubaiTZ).Date()
	return date{y, m, d}
}

func (d date) start() time.Time { return domain.TradingDayStart(d.year, d.month, d.day) }

// Calendar answers whether DFM trades at an instant. It is immutable and
// safe for concurrent use.
type Calendar struct {
	hours  Hours
	week   [7]bool
	closed map[date]bool
}

// New returns the calendar with hours on which DFM does not trade on any
// day in closed. Each closed day is the Dubai date of the instant, as
// domain.TradingDayStart returns it.
func New(hours Hours, closed []time.Time) (*Calendar, error) {
	if err := hours.Validate(); err != nil {
		return nil, err
	}
	c := &Calendar{hours: hours, closed: make(map[date]bool, len(closed))}
	for _, wd := range hours.Week {
		c.week[wd] = true
	}
	for _, t := range closed {
		c.closed[dateOf(t)] = true
	}
	return c, nil
}

// IsTradingDay reports whether DFM trades on the Dubai date of day: a day
// of the trading week that is not a holiday or an unscheduled closure.
func (c *Calendar) IsTradingDay(day time.Time) bool { return c.isTradingDate(dateOf(day)) }

func (c *Calendar) isTradingDate(d date) bool {
	return c.week[d.start().Weekday()] && !c.closed[d]
}

// Session returns the session DFM is in at t.
func (c *Calendar) Session(t time.Time) Session {
	d := dateOf(t)
	if !c.isTradingDate(d) {
		return Closed
	}
	day := d.start()
	switch {
	case t.Before(c.hours.PreOpen.on(day)):
		return Closed
	case t.Before(c.hours.Open.on(day)):
		return PreOpen
	case t.Before(c.hours.ClosingAuction.on(day)):
		return Continuous
	case t.Before(c.hours.Close.on(day)):
		return ClosingAuction
	}
	return Closed
}

// NextOpen returns the first start of continuous trading strictly after t,
// in domain.DubaiTZ.
func (c *Calendar) NextOpen(t time.Time) time.Time {
	// The loop ends: the week has a trading day and closed days are finite.
	for d := dateOf(t); ; d = dateOf(d.start().AddDate(0, 0, 1)) {
		if !c.isTradingDate(d) {
			continue
		}
		if open := c.hours.Open.on(d.start()); open.After(t) {
			return open
		}
	}
}
