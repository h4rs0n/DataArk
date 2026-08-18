package discovery

import (
	"bytes"
	"net/url"
	"strings"
	"time"
	"unicode"

	distiller "github.com/markusmobius/go-domdistiller"
	"golang.org/x/net/html"
)

type ExtractedArticle struct {
	Title            string
	Text             string
	WordCount        int
	Description      string
	CanonicalURL     string
	Author           string
	PublishedAt      *time.Time
	Language         string
	IsArticle        bool
	HasPasswordInput bool
}

func ExtractArticle(rawURL string, body []byte) (*ExtractedArticle, error) {
	parsedURL, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, err
	}
	metadata := extractHTMLMetadata(body)
	result, err := distiller.ApplyForReader(bytes.NewReader(body), &distiller.Options{
		OriginalURL:    parsedURL,
		SkipPagination: true,
	})
	if err != nil {
		return nil, err
	}
	article := &ExtractedArticle{
		Title:            strings.TrimSpace(result.Title),
		Text:             strings.Join(strings.Fields(result.Text), " "),
		WordCount:        result.WordCount,
		Author:           strings.TrimSpace(result.MarkupInfo.Author),
		Language:         metadata.language,
		IsArticle:        metadata.hasArticleElement || strings.EqualFold(result.MarkupInfo.Type, "article"),
		HasPasswordInput: metadata.hasPasswordInput,
		PublishedAt:      parseArticleTime(result.MarkupInfo.Article.PublishedTime),
	}
	if article.Title == "" {
		article.Title = metadata.title
	}
	if result.MarkupInfo.Description != "" {
		article.Description = strings.TrimSpace(result.MarkupInfo.Description)
	} else {
		article.Description = metadata.description
	}
	if result.MarkupInfo.URL != "" {
		article.CanonicalURL = strings.TrimSpace(result.MarkupInfo.URL)
	} else {
		article.CanonicalURL = metadata.canonicalURL
	}
	if article.Author == "" {
		article.Author = metadata.author
	}
	if article.PublishedAt == nil {
		article.PublishedAt = parseArticleTime(metadata.publishedAt)
	}
	if article.Language == "" {
		article.Language = detectTextLanguage(article.Text)
	}
	return article, nil
}

type htmlMetadata struct {
	title             string
	description       string
	canonicalURL      string
	author            string
	publishedAt       string
	language          string
	hasArticleElement bool
	hasPasswordInput  bool
}

func extractHTMLMetadata(body []byte) htmlMetadata {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return htmlMetadata{}
	}
	var metadata htmlMetadata
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			switch node.Data {
			case "html":
				if metadata.language == "" {
					metadata.language = normalizeLanguage(attr(node, "lang"))
				}
			case "article":
				metadata.hasArticleElement = true
			case "input":
				if strings.EqualFold(attr(node, "type"), "password") {
					metadata.hasPasswordInput = true
				}
			case "title":
				if metadata.title == "" && node.FirstChild != nil && node.FirstChild.Type == html.TextNode {
					metadata.title = strings.TrimSpace(node.FirstChild.Data)
				}
			case "meta":
				key := strings.ToLower(firstAttr(node, "property", "name"))
				content := strings.TrimSpace(attr(node, "content"))
				switch key {
				case "og:title", "twitter:title":
					if metadata.title == "" {
						metadata.title = content
					}
				case "description", "og:description", "twitter:description":
					if metadata.description == "" {
						metadata.description = content
					}
				case "og:url":
					if metadata.canonicalURL == "" {
						metadata.canonicalURL = content
					}
				case "author", "article:author":
					if metadata.author == "" {
						metadata.author = content
					}
				case "article:published_time", "date", "datepublished", "publishdate", "pubdate":
					if metadata.publishedAt == "" {
						metadata.publishedAt = content
					}
				}
			case "time":
				if metadata.publishedAt == "" {
					metadata.publishedAt = strings.TrimSpace(attr(node, "datetime"))
				}
			case "link":
				if metadata.canonicalURL == "" && strings.Contains(strings.ToLower(attr(node, "rel")), "canonical") {
					metadata.canonicalURL = strings.TrimSpace(attr(node, "href"))
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return metadata
}

func parseArticleTime(value string) *time.Time {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02", time.RFC1123Z, time.RFC1123} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return &parsed
		}
	}
	return nil
}

func normalizeLanguage(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if separator := strings.IndexAny(value, "-_"); separator >= 0 {
		value = value[:separator]
	}
	if len(value) >= 2 && len(value) <= 3 {
		return value
	}
	return ""
}

func detectTextLanguage(value string) string {
	var latin, han int
	for _, character := range value {
		switch {
		case unicode.In(character, unicode.Han):
			han++
		case unicode.In(character, unicode.Latin):
			latin++
		}
	}
	if han >= 4 && han >= latin/4 {
		return "zh"
	}
	if latin >= 8 {
		return "en"
	}
	return ""
}

func firstAttr(node *html.Node, keys ...string) string {
	for _, key := range keys {
		if value := attr(node, key); value != "" {
			return value
		}
	}
	return ""
}

func attr(node *html.Node, key string) string {
	for _, attribute := range node.Attr {
		if strings.EqualFold(attribute.Key, key) {
			return attribute.Val
		}
	}
	return ""
}
