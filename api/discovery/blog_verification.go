package discovery

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"golang.org/x/net/html"
	"gorm.io/gorm"
)

const (
	BlogVerificationDeclaredFeed      = "declared_feed"
	BlogVerificationGenerator         = "blog_generator"
	BlogVerificationStructuredData    = "blog_structured_data"
	BlogVerificationArticleCollection = "article_collection"
	BlogVerificationSemanticLinks     = "blog_semantic_links"
	BlogVerificationNoEvidence        = "no_blog_evidence"
)

var ErrBlogVerificationUnavailable = errors.New("blog verification requires a complete homepage response")

type BlogVerification struct {
	IsBlog   bool
	Evidence string
}

func VerifyBlogHomepage(body []byte, pageURL string, siteRootURL string) (BlogVerification, error) {
	document, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return BlogVerification{}, err
	}
	links, err := discoverHomepageEndpoints(body, pageURL, siteRootURL)
	if err != nil {
		return BlogVerification{}, err
	}
	if len(links.feeds) > 0 {
		return BlogVerification{IsBlog: true, Evidence: BlogVerificationDeclaredFeed}, nil
	}
	articleCount := 0
	generator := ""
	hasBlogStructuredData := false
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			switch node.Data {
			case "article":
				articleCount++
			case "meta":
				if strings.EqualFold(attrValue(node, "name"), "generator") {
					generator = strings.ToLower(attrValue(node, "content"))
				}
			case "script":
				if strings.Contains(strings.ToLower(attrValue(node, "type")), "ld+json") && structuredDataContainsBlog(nodeText(node)) {
					hasBlogStructuredData = true
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	for _, marker := range []string{"wordpress", "ghost", "hugo", "jekyll", "hexo", "typecho", "blogger"} {
		if strings.Contains(generator, marker) {
			return BlogVerification{IsBlog: true, Evidence: BlogVerificationGenerator}, nil
		}
	}
	if hasBlogStructuredData {
		return BlogVerification{IsBlog: true, Evidence: BlogVerificationStructuredData}, nil
	}
	if articleCount >= 2 {
		return BlogVerification{IsBlog: true, Evidence: BlogVerificationArticleCollection}, nil
	}
	semantic := strings.ToLower(pageSemanticContext(document))
	if len(links.candidates) > 0 && containsAny(semantic, "blog", "weblog", "博客", "文章") {
		return BlogVerification{IsBlog: true, Evidence: BlogVerificationSemanticLinks}, nil
	}
	return BlogVerification{Evidence: BlogVerificationNoEvidence}, nil
}

func structuredDataContainsBlog(raw string) bool {
	var value interface{}
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &value) != nil {
		return false
	}
	var inspect func(interface{}) bool
	inspect = func(current interface{}) bool {
		switch typed := current.(type) {
		case map[string]interface{}:
			for key, nested := range typed {
				if strings.EqualFold(key, "@type") {
					switch typeValue := nested.(type) {
					case string:
						if typeValue == "Blog" || typeValue == "BlogPosting" {
							return true
						}
					case []interface{}:
						for _, item := range typeValue {
							if name, ok := item.(string); ok && (name == "Blog" || name == "BlogPosting") {
								return true
							}
						}
					}
				}
				if inspect(nested) {
					return true
				}
			}
		case []interface{}:
			for _, nested := range typed {
				if inspect(nested) {
					return true
				}
			}
		}
		return false
	}
	return inspect(value)
}

func persistObservedSiteVerification(site DiscoverySite, verification BlogVerification, now time.Time) error {
	if db == nil || site.ID == 0 || site.Status != DiscoverySiteStatusObserving {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if verification.IsBlog {
			return tx.Model(&DiscoverySite{}).Where("id = ? AND status = ?", site.ID, DiscoverySiteStatusObserving).Updates(map[string]interface{}{
				"status": DiscoverySiteStatusActive, "crawl_allowed": true, "last_validated_at": &now,
				"operational_pause": "", "operational_details": "", "next_graph_scan_at": &now,
			}).Error
		}
		if err := tx.Model(&DiscoverySite{}).Where("id = ? AND status = ?", site.ID, DiscoverySiteStatusObserving).Updates(map[string]interface{}{
			"status": DiscoverySiteStatusNonBlog, "crawl_allowed": false, "last_validated_at": &now,
			"operational_pause": DiscoveryPauseNonBlog, "operational_details": "blog_verification:" + verification.Evidence,
			"next_graph_scan_at": nil,
		}).Error; err != nil {
			return err
		}
		return tx.Model(&DiscoverySource{}).Where("site_id = ?", site.ID).Updates(map[string]interface{}{
			"enabled": false, "next_due_at": nil, "next_fetch_at": nil,
		}).Error
	})
}

func nodeText(node *html.Node) string {
	if node == nil {
		return ""
	}
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return builder.String()
}

func containsAny(value string, markers ...string) bool {
	for _, marker := range markers {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}
