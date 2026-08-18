// Package articlerules 集中文章 URL 与正文闸门，便于按表调整，避免规则散落在发现各处。
package articlerules

import (
	"net/url"
	"strings"
)

const (
	// CategoryNotArticle 表示页面是导航、列表、登录或其他非正文。
	CategoryNotArticle = "not_article"
	// CategoryMissingTitle 表示抽不到标题。
	CategoryMissingTitle = "missing_title"
	// CategoryBodyTooShort 表示正文短于配置下限。
	CategoryBodyTooShort = "body_too_short"
	// CategoryLanguageUnknown 表示无法识别语言，送人工复核而不是直接丢弃。
	CategoryLanguageUnknown = "language_unknown"
	// CategoryExtractionFailed 表示抽取器没有返回结果。
	CategoryExtractionFailed = "extraction_failed"
)

// directoryPrefixes 作为目录前缀出现时视为列表/导航，避免 /tag 误伤 /tagged-notes 这类文章路径。
var directoryPrefixes = []string{
	"/archive/", "/archives/", "/tag/", "/tags/", "/category/", "/categories/",
	"/page/", "/login/", "/signin/", "/signup/", "/register/", "/search/",
	"/author/", "/authors/", "/comments/", "/blogroll/", "/friends/", "/links/",
	"/about/",
}

// exactPaths 整段路径（去尾斜杠后）是站点入口而不是文章。
var exactPaths = []string{
	"/", "/archive", "/archives", "/blogroll", "/friends", "/links", "/about",
	"/login", "/signin", "/signup", "/register", "/search", "/feed", "/rss",
	"/atom", "/sitemap",
}

// feedSuffixes 常见 Feed/Sitemap 文件名，只按后缀判断以免误伤 /rss-post 这种文章 slug。
var feedSuffixes = []string{
	"/feed.xml", "/rss.xml", "/atom.xml", "/index.xml", "/sitemap.xml",
}

// queryMarkers 查询键表明这是列表、分页或会话页。
var queryMarkers = []string{"tag", "tags", "category", "cat", "page", "paged", "s"}

// exactAnchors 锚文本完全等于这些词时视为导航，而不是文章标题。
var exactAnchors = []string{
	"archive", "archives", "blogroll", "friends", "links", "about",
	"sign in", "login", "rss", "atom", "feed", "sitemap", "older posts",
	"next page", "previous page",
}

// Page 是正文闸门输入。只使用已抽取的干净文本和少量页面结构标记。
type Page struct {
	URL              string
	Title            string
	Text             string
	Language         string
	IsArticle        bool
	HasPasswordInput bool
}

// RejectURL 入池闸门：明显的导航/列表/登录 URL 在创建候选或抓正文前被拒绝。
func RejectURL(rawURL, anchorText string) (reason string, rejected bool) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "url is not a usable article locator", true
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "url scheme is not http(s)", true
	}
	path := strings.ToLower(parsed.Path)
	if path == "" {
		path = "/"
	}
	trimmed := strings.TrimRight(path, "/")
	if trimmed == "" {
		trimmed = "/"
	}
	for _, marker := range exactPaths {
		if trimmed == marker {
			return "url path is a site navigation or listing page", true
		}
	}
	for _, marker := range directoryPrefixes {
		if strings.HasPrefix(path, marker) || strings.Contains(path, marker) {
			return "url path looks like navigation, listing, login, feed, or pagination", true
		}
	}
	for _, suffix := range feedSuffixes {
		if strings.HasSuffix(path, suffix) {
			return "url path is a feed or sitemap document", true
		}
	}
	query := strings.ToLower(parsed.RawQuery)
	if query != "" {
		values := parsed.Query()
		for _, marker := range queryMarkers {
			if _, ok := values[marker]; ok {
				return "url query identifies a listing, search, or pagination page", true
			}
		}
	}
	anchor := strings.ToLower(strings.TrimSpace(anchorText))
	for _, marker := range exactAnchors {
		if anchor == marker {
			return "anchor text is a navigation label rather than an article title", true
		}
	}
	return "", false
}

// RejectPage 正文闸门：抓取并抽取之后再次确认这是可评估的文章。
func RejectPage(page Page, minimumCharacters int) (reason, category string, review bool) {
	if strings.TrimSpace(page.URL) == "" && strings.TrimSpace(page.Title) == "" && strings.TrimSpace(page.Text) == "" {
		return "article extraction returned no result", CategoryExtractionFailed, true
	}
	if reason, rejected := RejectURL(page.URL, page.Title); rejected {
		return reason, CategoryNotArticle, false
	}
	if page.HasPasswordInput {
		return "page is a login or account form", CategoryNotArticle, false
	}
	if !page.IsArticle {
		return "page is navigation, listing, login, tag, or another non-article type", CategoryNotArticle, false
	}
	if strings.TrimSpace(page.Title) == "" {
		return "article title is missing", CategoryMissingTitle, false
	}
	if minimumCharacters <= 0 {
		minimumCharacters = 120
	}
	if len([]rune(strings.TrimSpace(page.Text))) < minimumCharacters {
		return "extracted article text is shorter than the configured minimum", CategoryBodyTooShort, false
	}
	if strings.TrimSpace(page.Language) == "" {
		return "article language could not be identified", CategoryLanguageUnknown, true
	}
	return "", "", false
}
