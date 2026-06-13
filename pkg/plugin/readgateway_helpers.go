package plugin

import (
	"github.com/ignacioballester/oc-plugin-sdk/pluginclient"
)

// colIndex maps a read-gateway result's column names to their positions.
func colIndex(cols []pluginclient.ReadGatewayColumn) map[string]int {
	idx := make(map[string]int, len(cols))
	for i, c := range cols {
		idx[c.Name] = i
	}
	return idx
}

// asString coerces a read-gateway cell to a string ("" for nil/non-string).
func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// asInt coerces a read-gateway scalar cell (JSON number) to an int.
func asInt(v any) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}

// asMicros coerces a read-gateway int-microsecond bigint cell (arrives as
// float64 from JSON) to int64 epoch micros; 0 when absent or wrong type.
func asMicros(v any) int64 {
	if f, ok := v.(float64); ok {
		return int64(f)
	}
	return 0
}
