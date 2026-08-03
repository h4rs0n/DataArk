package discovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	neturl "net/url"
	"strings"
	"time"

	"golang.org/x/net/idna"
	"gorm.io/gorm"
)

var (
	ErrInvalidDiscoveryBlacklistDomain   = errors.New("invalid discovery blacklist domain")
	ErrDuplicateDiscoveryBlacklistDomain = errors.New("discovery blacklist domain already exists")
	ErrDiscoveryDomainBlacklisted        = errors.New("discovery crawl domain is blacklisted")
)

const processingErrorDomainBlacklist = "domain_blacklist"

func discoveryCandidateBlacklistPredicate(candidateAlias string) (string, error) {
	hostColumn := "discovery_candidates.crawl_host"
	switch strings.TrimSpace(candidateAlias) {
	case "", "discovery_candidates":
	case "candidate":
		hostColumn = "candidate.crawl_host"
	default:
		return "", fmt.Errorf("invalid discovery candidate table alias %q", candidateAlias)
	}
	return `EXISTS (
SELECT 1
FROM discovery_domain_blacklist_entries AS domain_blacklist
WHERE ` + hostColumn + ` = domain_blacklist.domain
   OR ` + hostColumn + ` LIKE '%.' || domain_blacklist.domain
)`, nil
}

// ExcludeBlacklistedCandidateDomains applies the active domain blacklist to a
// discovery-candidate query without deleting or rewriting retained evidence.
func ExcludeBlacklistedCandidateDomains(query *gorm.DB, candidateAlias string) *gorm.DB {
	if query == nil {
		return query
	}
	predicate, err := discoveryCandidateBlacklistPredicate(candidateAlias)
	if err != nil {
		_ = query.AddError(err)
		return query
	}
	return query.Where("NOT " + predicate)
}

// OnlyBlacklistedCandidateDomains selects candidates hidden by an active rule.
func OnlyBlacklistedCandidateDomains(query *gorm.DB, candidateAlias string) *gorm.DB {
	if query == nil {
		return query
	}
	predicate, err := discoveryCandidateBlacklistPredicate(candidateAlias)
	if err != nil {
		_ = query.AddError(err)
		return query
	}
	return query.Where(predicate)
}

func NormalizeDiscoveryBlacklistDomain(value string) (string, error) {
	value = strings.TrimSpace(strings.TrimSuffix(value, "."))
	if value == "" || strings.ContainsAny(value, "/:@*?#[]") || net.ParseIP(value) != nil || strings.EqualFold(value, "localhost") {
		return "", ErrInvalidDiscoveryBlacklistDomain
	}
	ascii, err := idna.Lookup.ToASCII(value)
	if err != nil {
		return "", ErrInvalidDiscoveryBlacklistDomain
	}
	ascii = strings.ToLower(strings.TrimSuffix(ascii, "."))
	if len(ascii) == 0 || len(ascii) > 253 || !strings.Contains(ascii, ".") {
		return "", ErrInvalidDiscoveryBlacklistDomain
	}
	for _, label := range strings.Split(ascii, ".") {
		if len(label) == 0 || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", ErrInvalidDiscoveryBlacklistDomain
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return "", ErrInvalidDiscoveryBlacklistDomain
			}
		}
	}
	return ascii, nil
}

func crawlHostForURL(rawURL string) string {
	parsed, err := neturl.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "" {
		return ""
	}
	if ascii, err := idna.Lookup.ToASCII(host); err == nil {
		return strings.ToLower(ascii)
	}
	return host
}

func domainRuleMatchesHost(rule string, host string) bool {
	rule = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(rule), "."))
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	return rule != "" && (host == rule || strings.HasSuffix(host, "."+rule))
}

func domainBlacklistMatchesURL(entries []DiscoveryDomainBlacklistEntry, rawURL string) bool {
	host := crawlHostForURL(rawURL)
	for _, entry := range entries {
		if domainRuleMatchesHost(entry.Domain, host) {
			return true
		}
	}
	return false
}

func loadDiscoveryDomainBlacklist() ([]DiscoveryDomainBlacklistEntry, error) {
	entries := make([]DiscoveryDomainBlacklistEntry, 0)
	if db == nil {
		return entries, nil
	}
	if err := db.Select("id", "domain").Order("id").Find(&entries).Error; err != nil {
		return nil, err
	}
	return entries, nil
}

func ListDiscoveryDomainBlacklist() ([]DiscoveryDomainBlacklistEntry, error) {
	entries := make([]DiscoveryDomainBlacklistEntry, 0)
	if db == nil {
		return entries, nil
	}
	if err := db.Order("created_at DESC, id DESC").Find(&entries).Error; err != nil {
		return nil, err
	}
	return entries, nil
}

func CreateDiscoveryDomainBlacklist(domain string, reason string) (*DiscoveryDomainBlacklistMutation, error) {
	if db == nil {
		return nil, errors.New("discovery database is unavailable")
	}
	normalized, err := NormalizeDiscoveryBlacklistDomain(domain)
	if err != nil {
		return nil, err
	}
	entry := DiscoveryDomainBlacklistEntry{Domain: normalized, Reason: strings.TrimSpace(reason)}
	var affected int64
	err = db.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&DiscoveryDomainBlacklistEntry{}).Where("domain = ?", normalized).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrDuplicateDiscoveryBlacklistDomain
		}
		newlyHidden := ExcludeBlacklistedCandidateDomains(tx.Model(&DiscoveryCandidate{}), "")
		if err := newlyHidden.
			Where("crawl_host = ? OR crawl_host LIKE ?", normalized, "%."+normalized).
			Count(&affected).Error; err != nil {
			return err
		}
		if err := tx.Create(&entry).Error; err != nil {
			return err
		}
		result := tx.Model(&DiscoveryCandidate{}).
			Where("crawl_host = ? OR crawl_host LIKE ?", normalized, "%."+normalized).
			Where("processing_state IN ?", []string{DiscoveryProcessingDiscovered, DiscoveryProcessingFetchPending, DiscoveryProcessingFetching, "extract_pending"}).
			Updates(map[string]interface{}{
				"processing_state":      DiscoveryProcessingDomainBlocked,
				"processing_error_type": processingErrorDomainBlacklist,
				"processing_error":      fmt.Sprintf("crawl domain is blacklisted: %s", normalized),
				"eligibility_state":     DiscoveryEligibilityUnknown,
				"eligibility_reasons":   processingErrorDomainBlacklist,
				"next_processing_at":    nil,
				"updated_at":            time.Now(),
			})
		return result.Error
	})
	if err != nil {
		return nil, err
	}
	return &DiscoveryDomainBlacklistMutation{Entry: entry, AffectedCandidates: affected}, nil
}

func DeleteDiscoveryDomainBlacklist(id uint) (*DiscoveryDomainBlacklistMutation, error) {
	if db == nil || id == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	mutation := &DiscoveryDomainBlacklistMutation{}
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&mutation.Entry, id).Error; err != nil {
			return err
		}
		if err := tx.Delete(&mutation.Entry).Error; err != nil {
			return err
		}
		var remaining []DiscoveryDomainBlacklistEntry
		if err := tx.Order("id").Find(&remaining).Error; err != nil {
			return err
		}
		var candidates []DiscoveryCandidate
		if err := tx.Select("id", "crawl_host", "processing_state", "processing_error_type").
			Where("crawl_host = ? OR crawl_host LIKE ?", mutation.Entry.Domain, "%."+mutation.Entry.Domain).
			Find(&candidates).Error; err != nil {
			return err
		}
		unblocked := make([]uint, 0)
		resumable := make([]uint, 0)
		for _, candidate := range candidates {
			blocked := false
			for _, entry := range remaining {
				if domainRuleMatchesHost(entry.Domain, candidate.CrawlHost) {
					blocked = true
					break
				}
			}
			if !blocked {
				unblocked = append(unblocked, candidate.ID)
				if candidate.ProcessingState == DiscoveryProcessingDomainBlocked && candidate.ProcessingErrorType == processingErrorDomainBlacklist {
					resumable = append(resumable, candidate.ID)
				}
			}
		}
		mutation.AffectedCandidates = int64(len(unblocked))
		if len(resumable) == 0 {
			return nil
		}
		now := time.Now()
		result := tx.Model(&DiscoveryCandidate{}).Where("id IN ?", resumable).Updates(map[string]interface{}{
			"processing_state":      DiscoveryProcessingFetchPending,
			"processing_error_type": "",
			"processing_error":      "",
			"eligibility_state":     DiscoveryEligibilityUnknown,
			"eligibility_reasons":   "",
			"next_processing_at":    &now,
			"updated_at":            now,
		})
		return result.Error
	})
	if err != nil {
		return nil, err
	}
	return mutation, nil
}

func IsDiscoveryURLBlacklisted(_ context.Context, rawURL string) (bool, error) {
	return IsDiscoveryHostBlacklisted(crawlHostForURL(rawURL))
}

func IsDiscoveryHostBlacklisted(host string) (bool, error) {
	return isDiscoveryHostBlacklistedDB(db, host)
}

func isDiscoveryHostBlacklistedDB(database *gorm.DB, host string) (bool, error) {
	if database == nil || strings.TrimSpace(host) == "" {
		return false, nil
	}
	var entries []DiscoveryDomainBlacklistEntry
	if err := database.Select("domain").Find(&entries).Error; err != nil {
		return false, err
	}
	for _, entry := range entries {
		if domainRuleMatchesHost(entry.Domain, host) {
			return true, nil
		}
	}
	return false, nil
}

func ensureDiscoveryURLNotBlacklisted(ctx context.Context, rawURL string) error {
	blocked, err := IsDiscoveryURLBlacklisted(ctx, rawURL)
	if err != nil {
		return err
	}
	if blocked {
		return fmt.Errorf("%w: %s", ErrDiscoveryDomainBlacklisted, crawlHostForURL(rawURL))
	}
	return nil
}
