package fireworks

import (
	"math"
	"testing"
	"time"
)

func TestParseDateValueMatchesPythonUtilityCases(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  time.Time
	}{
		{name: "timestamp string", value: "1494012444.883309", want: dateUTC(2017, 5, 5)},
		{name: "timestamp bytes", value: []byte("1494012444.883309"), want: dateUTC(2017, 5, 5)},
		{name: "timestamp float", value: 1_494_012_444.883_309, want: dateUTC(2017, 5, 5)},
		{name: "timestamp integer string", value: "1494012444", want: dateUTC(2017, 5, 5)},
		{name: "timestamp int", value: 1_494_012_444, want: dateUTC(2017, 5, 5)},
		{name: "epoch", value: 0, want: dateUTC(1970, 1, 1)},
		{name: "date", value: "2012-04-23", want: dateUTC(2012, 4, 23)},
		{name: "date bytes", value: []byte("2012-04-23"), want: dateUTC(2012, 4, 23)},
		{name: "unpadded date", value: "2012-4-9", want: dateUTC(2012, 4, 9)},
		{name: "time value", value: time.Date(2012, 4, 9, 12, 15, 0, 0, time.UTC), want: dateUTC(2012, 4, 9)},
		{name: "just before watershed", value: 19_999_999_999, want: dateUTC(2603, 10, 11)},
		{name: "just after watershed", value: 20_000_000_001, want: dateUTC(1970, 8, 20)},
		{name: "milliseconds", value: int64(1_549_316_052_104), want: dateUTC(2019, 2, 4)},
		{name: "microseconds", value: int64(1_549_316_052_104_324), want: dateUTC(2019, 2, 4)},
		{name: "nanoseconds", value: int64(1_549_316_052_104_324_096), want: dateUTC(2019, 2, 4)},
		{name: "infinity", value: "infinity ", want: dateUTC(9999, 12, 31)},
		{name: "positive inf", value: math.Inf(1), want: dateUTC(9999, 12, 31)},
		{name: "huge numeric string", value: "10000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000", want: dateUTC(9999, 12, 31)},
		{name: "huge float", value: 1e50, want: dateUTC(9999, 12, 31)},
		{name: "negative inf", value: "-inf", want: dateUTC(1, 1, 1)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseDateValue(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if !got.Equal(tc.want) {
				t.Fatalf("date = %s, want %s", got.Format(time.RFC3339Nano), tc.want.Format(time.RFC3339Nano))
			}
		})
	}
}

func TestParseDateValueRejectsPythonInvalidCases(t *testing.T) {
	for _, value := range []any{"x20120423", "2012-04-56", "nan"} {
		t.Run(stringValue(value), func(t *testing.T) {
			if _, err := parseDateValue(value); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestParseDateTimeValueMatchesPythonUtilityCases(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  time.Time
	}{
		{name: "timestamp string", value: "1494012444.883309", want: time.Date(2017, 5, 5, 19, 27, 24, 883309000, time.UTC)},
		{name: "timestamp float", value: 1_494_012_444.883_309, want: time.Date(2017, 5, 5, 19, 27, 24, 883309000, time.UTC)},
		{name: "timestamp integer string", value: "1494012444", want: time.Date(2017, 5, 5, 19, 27, 24, 0, time.UTC)},
		{name: "timestamp integer bytes", value: []byte("1494012444"), want: time.Date(2017, 5, 5, 19, 27, 24, 0, time.UTC)},
		{name: "timestamp int", value: 1_494_012_444, want: time.Date(2017, 5, 5, 19, 27, 24, 0, time.UTC)},
		{name: "milliseconds with fraction", value: "1494012444000.883309", want: time.Date(2017, 5, 5, 19, 27, 24, 883000, time.UTC)},
		{name: "negative milliseconds with fraction", value: "-1494012444000.883309", want: time.Date(1922, 8, 29, 4, 32, 35, 999117000, time.UTC)},
		{name: "milliseconds", value: int64(1_494_012_444_000), want: time.Date(2017, 5, 5, 19, 27, 24, 0, time.UTC)},
		{name: "datetime", value: "2012-04-23T09:15:00", want: time.Date(2012, 4, 23, 9, 15, 0, 0, time.Local)},
		{name: "unpadded datetime", value: "2012-4-9 4:8:16", want: time.Date(2012, 4, 9, 4, 8, 16, 0, time.Local)},
		{name: "zulu", value: "2012-04-23T09:15:00Z", want: time.Date(2012, 4, 23, 9, 15, 0, 0, time.UTC)},
		{name: "offset compact", value: "2012-4-9 4:8:16-0320", want: time.Date(2012, 4, 9, 4, 8, 16, 0, time.FixedZone("-0320", -200*60))},
		{name: "offset colon", value: "2012-04-23T10:20:30.400+02:30", want: time.Date(2012, 4, 23, 10, 20, 30, 400000000, time.FixedZone("+02:30", 150*60))},
		{name: "offset hour", value: []byte("2012-04-23T10:20:30.400-02"), want: time.Date(2012, 4, 23, 10, 20, 30, 400000000, time.FixedZone("-02", -120*60))},
		{name: "large seconds", value: 19_999_999_999, want: time.Date(2603, 10, 11, 11, 33, 19, 0, time.UTC)},
		{name: "large milliseconds", value: int64(1_549_316_052_104), want: time.Date(2019, 2, 4, 21, 34, 12, 104000000, time.UTC)},
		{name: "large microseconds", value: int64(1_549_316_052_104_324), want: time.Date(2019, 2, 4, 21, 34, 12, 104324000, time.UTC)},
		{name: "large nanoseconds", value: int64(1_549_316_052_104_324_096), want: time.Date(2019, 2, 4, 21, 34, 12, 104324000, time.UTC)},
		{name: "infinity", value: "inf ", want: time.Date(9999, 12, 31, 23, 59, 59, 999999000, time.UTC)},
		{name: "huge float", value: 1e50, want: time.Date(9999, 12, 31, 23, 59, 59, 999999000, time.UTC)},
		{name: "negative infinity", value: "-infinity", want: time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseDateTimeValue(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if !got.Equal(tc.want) {
				t.Fatalf("datetime = %s, want %s", got.Format(time.RFC3339Nano), tc.want.Format(time.RFC3339Nano))
			}
		})
	}
}

func TestParseDateTimeValueRejectsPythonInvalidCases(t *testing.T) {
	for _, value := range []any{"x20120423091500", "2012-04-56T09:15:90", "2012-04-23T11:05:00-25:00", "nan", math.NaN()} {
		t.Run(stringValue(value), func(t *testing.T) {
			if _, err := parseDateTimeValue(value); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func dateUTC(year, month, day int) time.Time {
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return "value"
	}
}
