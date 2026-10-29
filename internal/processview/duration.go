package processview

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// ps prints elapsed and CPU times as [[days-]hours:]minutes:seconds[.fraction].
func parseProcessDuration(value string) *time.Duration {
	var days uint64
	clock := value
	if before, after, ok := strings.Cut(value, "-"); ok {
		var err error
		days, err = strconv.ParseUint(before, 10, 64)
		if err != nil {
			return nil
		}
		clock = after
	}
	parts := strings.Split(clock, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return nil
	}
	secondsPart := parts[len(parts)-1]
	for _, r := range secondsPart {
		if (r < '0' || r > '9') && r != '.' {
			return nil
		}
	}
	seconds, err := time.ParseDuration(secondsPart + "s")
	if err != nil || seconds < 0 || seconds >= time.Minute {
		return nil
	}
	minutes, err := strconv.ParseUint(parts[len(parts)-2], 10, 64)
	if err != nil {
		return nil
	}
	var hours uint64
	if len(parts) == 3 {
		hours, err = strconv.ParseUint(parts[0], 10, 64)
		if err != nil || minutes >= 60 {
			return nil
		}
	}
	if strings.Contains(value, "-") && (len(parts) != 3 || hours >= 24) {
		return nil
	}
	const maxSeconds = uint64(math.MaxInt64 / int64(time.Second))
	if days > maxSeconds/86400 || hours > maxSeconds/3600 || minutes > maxSeconds/60 {
		return nil
	}
	total := days*86400 + hours*3600 + minutes*60
	if total > maxSeconds || time.Duration(total)*time.Second > time.Duration(math.MaxInt64)-seconds {
		return nil
	}
	d := time.Duration(total)*time.Second + seconds
	return &d
}
