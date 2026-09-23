package material

import (
	"github.com/PuerkitoBio/purell"
	"net/url"
	"strings"
)

func NormalizeURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	parsed.Fragment = ""
	query := parsed.Query()
	for key := range query {
		lower := strings.ToLower(strings.TrimSpace(key))
		if strings.HasPrefix(lower, "utm_") || lower == "fbclid" || lower == "gclid" || lower == "spm" {
			query.Del(key)
		}
	}
	parsed.RawQuery = query.Encode()
	return purell.NormalizeURLString(parsed.String(), purell.FlagsSafe|purell.FlagRemoveDotSegments)
}
