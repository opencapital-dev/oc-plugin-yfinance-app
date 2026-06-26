package plugin

import (
	"time"

	"github.com/google/uuid"
)

func nowMicros() int64 {
	return time.Now().UTC().UnixMicro()
}

func genID() string {
	return uuid.NewString()
}
