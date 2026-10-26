package tasks

import (
	"math/big"
	"time"
)

type occurrences struct {
	Count                 uint64
	First, Last, Previous time.Time
}

func (b *occurrences) add(t time.Time) {
	b.Count++
	b.ends(t, time.Time{})
	if b.First.IsZero() || t.Before(b.First) {
		b.First = t
	}
}
func (b *occurrences) ends(last, previous time.Time) {
	for _, t := range []time.Time{last, previous} {
		if t.IsZero() || t.Equal(b.Last) || t.Equal(b.Previous) {
			continue
		}
		if b.Last.IsZero() || t.After(b.Last) {
			b.Previous = b.Last
			b.Last = t
		} else if b.Previous.IsZero() || t.After(b.Previous) {
			b.Previous = t
		}
	}
}
func (b *occurrences) merge(other occurrences) {
	if other.Count == 0 {
		return
	}
	b.Count += other.Count
	if b.First.IsZero() || other.First.Before(b.First) {
		b.First = other.First
	}
	b.ends(other.Last, other.Previous)
}

func (s Schedule) batch(after, until time.Time, anchor *time.Time) occurrences {
	var b occurrences
	if !until.After(after) {
		return b
	}
	if s.Every != nil {
		if anchor == nil {
			return b
		}
		d := s.interval
		first := max(int64(1), intervalIndex(after, *anchor, d)+1)
		last := intervalIndex(until, *anchor, d)
		if last < first {
			return b
		}
		b.Count = uint64(last - first + 1)
		b.First = intervalAt(*anchor, first, d)
		b.Last = intervalAt(*anchor, last, d)
		if b.Count > 1 {
			b.Previous = b.Last.Add(-d)
		}
		return b
	}
	start, end := civil(after.In(s.location)), civil(until.In(s.location))
	if end.Before(start) {
		start, end = end, start
	}
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		if !s.compiled.dateMatches(day) {
			continue
		}
		offsets := offsetsOn(day, s.location)
		hours, minutes := s.compiled.hoursAndMinutes()
		if len(offsets) == 1 {
			first := wallInstant(day, hours[0], minutes[0], offsets[0])
			last := wallInstant(day, hours[len(hours)-1], minutes[len(minutes)-1], offsets[0])
			if first.After(after) && !last.After(until) {
				part := occurrences{Count: uint64(len(hours) * len(minutes)), First: first, Last: last}
				if len(minutes) > 1 {
					part.Previous = wallInstant(day, hours[len(hours)-1], minutes[len(minutes)-2], offsets[0])
				} else if len(hours) > 1 {
					part.Previous = wallInstant(day, hours[len(hours)-2], minutes[0], offsets[0])
				}
				b.merge(part)
				continue
			}
		}
		for _, h := range hours {
			for _, m := range minutes {
				t, ok := localMinute(day, h, m, offsets, s.location)
				if ok && t.After(after) && !t.After(until) {
					b.add(t)
				}
			}
		}
	}
	return b
}

func (s Schedule) next(after time.Time, anchor *time.Time) time.Time {
	if s.Every != nil {
		if anchor == nil {
			return time.Time{}
		}
		n := max(int64(1), intervalIndex(after, *anchor, s.interval)+1)
		return intervalAt(*anchor, n, s.interval)
	}
	start := civil(after.In(s.location))
	limit := start.AddDate(400, 0, 1)
	for day := start; day.Before(limit); day = day.AddDate(0, 0, 1) {
		if !s.compiled.dateMatches(day) {
			continue
		}
		offsets := offsetsOn(day, s.location)
		hours, minutes := s.compiled.hoursAndMinutes()
		var next time.Time
		for _, h := range hours {
			for _, m := range minutes {
				t, ok := localMinute(day, h, m, offsets, s.location)
				if ok && t.After(after) && (next.IsZero() || t.Before(next)) {
					next = t
				}
			}
		}
		if !next.IsZero() {
			return next
		}
	}
	return time.Time{}
}

func intervalIndex(t, anchor time.Time, d time.Duration) int64 {
	if t.Before(anchor) {
		return -1
	}
	var delta big.Int
	delta.SetInt64(t.Unix() - anchor.Unix())
	delta.Mul(&delta, big.NewInt(1e9))
	delta.Add(&delta, big.NewInt(int64(t.Nanosecond()-anchor.Nanosecond())))
	delta.Quo(&delta, big.NewInt(int64(d)))
	return delta.Int64()
}
func intervalAt(anchor time.Time, n int64, d time.Duration) time.Time {
	var delta, seconds, remainder big.Int
	delta.Mul(big.NewInt(n), big.NewInt(int64(d)))
	seconds.QuoRem(&delta, big.NewInt(1e9), &remainder)
	return time.Unix(anchor.Unix()+seconds.Int64(), int64(anchor.Nanosecond())+remainder.Int64()).UTC()
}

func (c cronSpec) hoursAndMinutes() ([]int, []int) {
	var h, m []int
	for i := 0; i < 24; i++ {
		if c.fields[1][i] {
			h = append(h, i)
		}
	}
	for i := 0; i < 60; i++ {
		if c.fields[0][i] {
			m = append(m, i)
		}
	}
	return h, m
}
func civil(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
func wallInstant(day time.Time, h, m, offset int) time.Time {
	return day.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute - time.Duration(offset)*time.Second)
}
func offsetsOn(day time.Time, location *time.Location) []int {
	var offsets []int
	for t, end := day.Add(-48*time.Hour), day.Add(72*time.Hour); !t.After(end); {
		z := t.In(location)
		_, offset := z.Zone()
		found := false
		for _, n := range offsets {
			if n == offset {
				found = true
			}
		}
		if !found {
			offsets = append(offsets, offset)
		}
		_, edge := z.ZoneBounds()
		if edge.IsZero() || edge.After(end) {
			break
		}
		if !edge.After(t) {
			t = t.Add(24 * time.Hour)
		} else {
			t = edge
		}
	}
	return offsets
}

// Select the first UTC occurrence of a wall minute. time.Date's choice for an
// ambiguous local time is intentionally unspecified, so validate each offset.
func localMinute(day time.Time, h, m int, offsets []int, location *time.Location) (time.Time, bool) {
	var earliest time.Time
	for _, offset := range offsets {
		candidate := wallInstant(day, h, m, offset)
		local := candidate.In(location)
		if local.Year() != day.Year() || local.Month() != day.Month() || local.Day() != day.Day() || local.Hour() != h || local.Minute() != m || local.Second() != 0 {
			continue
		}
		if earliest.IsZero() || candidate.Before(earliest) {
			earliest = candidate
		}
	}
	return earliest, !earliest.IsZero()
}
