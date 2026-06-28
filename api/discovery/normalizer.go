package discovery

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/purell"
)

var trackingQueryPrefixes = []string{"utm_"}
var trackingQueryKeys = map[string]struct{}{
	"fbclid": {},
	"gclid":  {},
	"spm":    {},
}

func NormalizeArticleURL(rawURL string) (string, error) {
	parsedURL, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", err
	}
	parsedURL.Fragment = ""
	query := parsedURL.Query()
	for key := range query {
		if isTrackingQueryKey(key) {
			query.Del(key)
		}
	}
	parsedURL.RawQuery = query.Encode()
	return purell.NormalizeURLString(parsedURL.String(), purell.FlagsSafe|purell.FlagRemoveDotSegments)
}

func ContentHash(text string) string {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

func isTrackingQueryKey(key string) bool {
	normalizedKey := strings.ToLower(strings.TrimSpace(key))
	if _, ok := trackingQueryKeys[normalizedKey]; ok {
		return true
	}
	for _, prefix := range trackingQueryPrefixes {
		if strings.HasPrefix(normalizedKey, prefix) {
			return true
		}
	}
	return false
}
