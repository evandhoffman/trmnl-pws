package solar

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

// The time helpers format t as given. The caller converts to local time.

// compactTime formats a table time: "1:05p".
func compactTime(t time.Time) string {
	return strings.TrimSuffix(strings.ToLower(t.Format("3:04PM")), "m")
}

// longTime formats a header time: "12:50 PM".
func longTime(t time.Time) string { return t.Format("3:04 PM") }

// clockTime formats a chart marker time without am or pm: "7:21".
func clockTime(t time.Time) string { return t.Format("3:04") }

// dayLabel formats a day: "Fri 10/30".
func dayLabel(t time.Time) string { return t.Format("Mon 1/2") }

// longDate formats the header date: "Friday, October 30".
func longDate(t time.Time) string { return t.Format("Monday, January 2") }

// f1 formats a value with one decimal.
func f1(v float64) string { return noNegZero(fmt.Sprintf("%.1f", v)) }

// f0 formats a value as a whole number.
func f0(v float64) string { return noNegZero(fmt.Sprintf("%.0f", v)) }

// noNegZero turns "-0.0" and "-0" into "0.0" and "0".
func noNegZero(s string) string {
	if strings.HasPrefix(s, "-") && strings.Trim(s[1:], "0.") == "" {
		return s[1:]
	}
	return s
}

// round rounds v to the given number of decimals. It never returns -0.
func round(v float64, decimals int) float64 {
	scale := math.Pow10(decimals)
	r := math.Round(v*scale) / scale
	if r == 0 {
		return 0
	}
	return r
}

// strp returns a pointer to s.
func strp(s string) *string { return &s }

// jsonString encodes v as compact JSON text. The templates print it into
// script blocks.
func jsonString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		// Only numbers and strings reach here, so this cannot happen.
		panic(fmt.Sprintf("solar: encode %T: %v", v, err))
	}
	return string(b)
}
