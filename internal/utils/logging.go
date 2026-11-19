package utils

import (
	"log"
	"spuri/internal/sysconfig"
	"strings"
)

// Debugf emits diagnostics only when explicitly enabled. It keeps sensitive
// educational data out of production logs by default.
func Debugf(format string, args ...interface{}) {
	if strings.EqualFold(strings.TrimSpace(sysconfig.StringEmCache("LOG_LEVEL")), "debug") {
		log.Printf(format, args...)
	}
}
