// pkg/plugin/occ.go
package plugin

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// OccParts is the parsed canonical option instrument_id. Mirrors
// oc-plugin-core-app/src/lib/import/occ.ts:OccParts (the fields the chain
// lookup needs).
type OccParts struct {
	Underlying string
	Expiry     time.Time // UTC midnight of expiry day
	Strike     float64
	Right      string // "C" | "P"
}

var occMonths = map[string]time.Month{
	"JAN": time.January, "FEB": time.February, "MAR": time.March,
	"APR": time.April, "MAY": time.May, "JUN": time.June,
	"JUL": time.July, "AUG": time.August, "SEP": time.September,
	"OCT": time.October, "NOV": time.November, "DEC": time.December,
}

// occRe mirrors OCC_RE in occ.ts: {UND} {DD}{MON}{YY} {STRIKE} {C|P}.
var occRe = regexp.MustCompile(`^\s*([A-Z][A-Z0-9.]*)\s+(\d{1,2})([A-Z]{3})(\d{2})\s+(\d+(?:\.\d+)?)\s+([CP])\s*$`)

// ParseOcc parses a canonical option instrument_id. Returns an error for any
// string that is not in canonical OCC form (e.g. plain equity tickers).
func ParseOcc(id string) (OccParts, error) {
	m := occRe.FindStringSubmatch(strings.ToUpper(id))
	if m == nil {
		return OccParts{}, fmt.Errorf("not an OCC ticker: %q", id)
	}
	mon, ok := occMonths[m[3]]
	if !ok {
		return OccParts{}, fmt.Errorf("invalid OCC month %q in %q", m[3], id)
	}
	day, _ := strconv.Atoi(m[2])
	yy, _ := strconv.Atoi(m[4])
	strike, err := strconv.ParseFloat(m[5], 64)
	if err != nil {
		return OccParts{}, fmt.Errorf("invalid OCC strike in %q: %w", id, err)
	}
	expiry := time.Date(2000+yy, mon, day, 0, 0, 0, 0, time.UTC)
	if expiry.Day() != day || expiry.Month() != mon {
		return OccParts{}, fmt.Errorf("invalid OCC date in %q", id)
	}
	return OccParts{Underlying: m[1], Expiry: expiry, Strike: strike, Right: m[6]}, nil
}
