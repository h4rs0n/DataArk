package discovery

import (
	"strings"
)

// truncateValidUTF8Bytes keeps a byte limit without splitting a UTF-8 rune.
func truncateValidUTF8Bytes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	value = strings.ToValidUTF8(value, "\uFFFD")
	if len(value) <= limit {
		return value
	}
	cut := 0
	for index := range value {
		if index > limit {
			break
		}
		cut = index
	}
	return value[:cut]
}
