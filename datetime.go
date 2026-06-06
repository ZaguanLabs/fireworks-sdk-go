package fireworks

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	timestampUnitWatershed = 20_000_000_000
	maxTimestampNumber     = 3e20
)

var (
	dateValueRe     = regexp.MustCompile(`^(\d{4})-(\d{1,2})-(\d{1,2})$`)
	dateTimeValueRe = regexp.MustCompile(`^(\d{4})-(\d{1,2})-(\d{1,2})[T ](\d{1,2}):(\d{1,2}):(\d{1,2})(?:\.(\d+))?(Z|[+-]\d{2}(?::?\d{2})?)?$`)
)

func parseDateValue(value any) (time.Time, error) {
	if dt, ok := value.(time.Time); ok {
		return dateOnly(dt), nil
	}
	dt, err := parseDateTimeValue(value)
	if err == nil {
		return dateOnly(dt), nil
	}
	text, ok := valueAsString(value)
	if !ok {
		return time.Time{}, err
	}
	parts := dateValueRe.FindStringSubmatch(strings.TrimSpace(text))
	if parts == nil {
		return time.Time{}, err
	}
	year, _ := strconv.Atoi(parts[1])
	month, _ := strconv.Atoi(parts[2])
	day, _ := strconv.Atoi(parts[3])
	return checkedDate(year, month, day, 0, 0, 0, 0, time.UTC)
}

func parseDateTimeValue(value any) (time.Time, error) {
	switch v := value.(type) {
	case time.Time:
		return v, nil
	case []byte:
		return parseDateTimeValue(string(v))
	case string:
		text := strings.TrimSpace(v)
		if dt, ok, err := parseSpecialDateTimeValue(text); ok || err != nil {
			return dt, err
		}
		if number, err := strconv.ParseInt(text, 10, 64); err == nil {
			return parseUnixTimestampInt(number), nil
		}
		if number, err := strconv.ParseFloat(text, 64); err == nil {
			return parseUnixTimestamp(number)
		}
		return parseDateTimeString(text)
	case int:
		return parseUnixTimestampInt(int64(v)), nil
	case int64:
		return parseUnixTimestampInt(v), nil
	case float64:
		return parseUnixTimestamp(v)
	default:
		return time.Time{}, fmt.Errorf("fireworks: unsupported datetime value %T", value)
	}
}

func parseSpecialDateTimeValue(text string) (time.Time, bool, error) {
	switch strings.ToLower(text) {
	case "inf", "infinity":
		return time.Date(9999, 12, 31, 23, 59, 59, 999999000, time.UTC), true, nil
	case "-inf", "-infinity":
		return time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC), true, nil
	case "nan":
		return time.Time{}, true, fmt.Errorf("fireworks: invalid datetime value %q", text)
	default:
		return time.Time{}, false, nil
	}
}

func parseUnixTimestamp(value float64) (time.Time, error) {
	if math.IsNaN(value) {
		return time.Time{}, fmt.Errorf("fireworks: invalid datetime value NaN")
	}
	if math.IsInf(value, 1) || value > maxTimestampNumber {
		return time.Date(9999, 12, 31, 23, 59, 59, 999999000, time.UTC), nil
	}
	if math.IsInf(value, -1) || value < -maxTimestampNumber {
		return time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC), nil
	}
	for math.Abs(value) > timestampUnitWatershed {
		value /= 1000
	}
	seconds := int64(value)
	microseconds := int64(math.Round((value - float64(seconds)) * 1_000_000))
	return time.Unix(seconds, microseconds*1000).UTC(), nil
}

func parseUnixTimestampInt(value int64) time.Time {
	divisor := int64(1)
	for math.Abs(float64(value)/float64(divisor)) > timestampUnitWatershed {
		divisor *= 1000
	}
	seconds := value / divisor
	remainder := value % divisor
	microseconds := remainder * 1_000_000 / divisor
	return time.Unix(seconds, microseconds*1000).UTC()
}

func parseDateTimeString(text string) (time.Time, error) {
	parts := dateTimeValueRe.FindStringSubmatch(text)
	if parts == nil {
		return time.Time{}, fmt.Errorf("fireworks: invalid datetime value %q", text)
	}
	year, _ := strconv.Atoi(parts[1])
	month, _ := strconv.Atoi(parts[2])
	day, _ := strconv.Atoi(parts[3])
	hour, _ := strconv.Atoi(parts[4])
	minute, _ := strconv.Atoi(parts[5])
	second, _ := strconv.Atoi(parts[6])
	nanosecond := fractionNanoseconds(parts[7])
	location, err := parseDateTimeLocation(parts[8])
	if err != nil {
		return time.Time{}, err
	}
	return checkedDate(year, month, day, hour, minute, second, nanosecond, location)
}

func parseDateTimeLocation(offset string) (*time.Location, error) {
	if offset == "" {
		return time.Local, nil
	}
	if offset == "Z" {
		return time.UTC, nil
	}
	sign := 1
	if strings.HasPrefix(offset, "-") {
		sign = -1
	}
	raw := strings.TrimPrefix(strings.TrimPrefix(offset, "+"), "-")
	hours := 0
	minutes := 0
	var err error
	if strings.Contains(raw, ":") {
		items := strings.SplitN(raw, ":", 2)
		hours, err = strconv.Atoi(items[0])
		if err != nil {
			return nil, err
		}
		minutes, err = strconv.Atoi(items[1])
		if err != nil {
			return nil, err
		}
	} else if len(raw) == 4 {
		hours, _ = strconv.Atoi(raw[:2])
		minutes, _ = strconv.Atoi(raw[2:])
	} else {
		hours, err = strconv.Atoi(raw)
		if err != nil {
			return nil, err
		}
	}
	if hours > 23 || minutes > 59 {
		return nil, fmt.Errorf("fireworks: invalid timezone offset %q", offset)
	}
	return time.FixedZone(offset, sign*(hours*3600+minutes*60)), nil
}

func fractionNanoseconds(fraction string) int {
	if fraction == "" {
		return 0
	}
	if len(fraction) > 9 {
		fraction = fraction[:9]
	}
	for len(fraction) < 9 {
		fraction += "0"
	}
	nanos, _ := strconv.Atoi(fraction)
	return nanos
}

func checkedDate(year, month, day, hour, minute, second, nanosecond int, location *time.Location) (time.Time, error) {
	if location == nil {
		location = time.UTC
	}
	value := time.Date(year, time.Month(month), day, hour, minute, second, nanosecond, location)
	if value.Year() != year || int(value.Month()) != month || value.Day() != day || value.Hour() != hour || value.Minute() != minute || value.Second() != second {
		return time.Time{}, fmt.Errorf("fireworks: invalid date or datetime value")
	}
	return value, nil
}

func dateOnly(value time.Time) time.Time {
	utc := value.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

func valueAsString(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case []byte:
		return string(v), true
	default:
		return "", false
	}
}
