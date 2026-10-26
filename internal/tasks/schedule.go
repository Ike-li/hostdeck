package tasks

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type cronSpec struct {
	fields           [5]map[int]bool
	daysAny, weekAny bool
}

func (s Schedule) validate() error {
	if (s.Cron == nil) == (s.Every == nil) {
		return errors.New("choose exactly one of cron and every")
	}
	if s.Every != nil {
		d, e := time.ParseDuration(*s.Every)
		if e != nil || d < time.Minute || d > 365*24*time.Hour || s.Timezone != nil {
			return errors.New("every requires 1m to 8760h and no timezone")
		}
		return nil
	}
	if s.Timezone == nil || *s.Timezone == "" || *s.Timezone == "Local" {
		return errors.New("cron requires an explicit IANA timezone")
	}
	if _, e := time.LoadLocation(*s.Timezone); e != nil {
		return errors.New("unknown timezone")
	}
	_, e := parseCron(*s.Cron)
	return e
}

func parseCron(text string) (cronSpec, error) {
	var c cronSpec
	parts := strings.Fields(text)
	if len(parts) != 5 {
		return c, errors.New("cron needs five numeric fields")
	}
	bounds := [5][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}
	for i, part := range parts {
		c.fields[i] = map[int]bool{}
		for _, term := range strings.Split(part, ",") {
			step := 1
			base := term
			if strings.Contains(term, "/") {
				p := strings.Split(term, "/")
				if len(p) != 2 {
					return c, errors.New("invalid cron step")
				}
				base = p[0]
				var e error
				step, e = numeric(p[1])
				if e != nil || step < 1 || !(base == "*" || strings.Contains(base, "-")) {
					return c, errors.New("invalid cron step")
				}
			}
			lo, hi := bounds[i][0], bounds[i][1]
			if base != "*" {
				p := strings.Split(base, "-")
				var e error
				lo, e = numeric(p[0])
				if e != nil {
					return c, e
				}
				hi = lo
				if len(p) == 2 {
					hi, e = numeric(p[1])
					if e != nil {
						return c, e
					}
				} else if len(p) != 1 {
					return c, errors.New("invalid cron range")
				}
			}
			if lo < bounds[i][0] || hi > bounds[i][1] || lo > hi {
				return c, errors.New("cron range out of bounds")
			}
			for n := lo; n <= hi; {
				v := n
				if i == 4 && v == 7 {
					v = 0
				}
				c.fields[i][v] = true
				if step > hi-n {
					break
				}
				n += step
			}
		}
	}
	c.daysAny = parts[2] == "*"
	c.weekAny = parts[4] == "*"
	for day := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC); day.Year() < 2400; day = day.AddDate(0, 0, 1) {
		if c.dateMatches(day) {
			return c, nil
		}
	}
	return c, errors.New("cron has no matching date in 400 years")
}

func numeric(s string) (int, error) {
	if s == "" {
		return 0, errors.New("empty cron number")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("cron number %q is not numeric", s)
		}
	}
	return strconv.Atoi(s)
}
func (c cronSpec) dateMatches(t time.Time) bool {
	if !c.fields[3][int(t.Month())] {
		return false
	}
	d, w := c.fields[2][t.Day()], c.fields[4][int(t.Weekday())]
	if !c.daysAny && !c.weekAny {
		return d || w
	}
	return d && w
}
