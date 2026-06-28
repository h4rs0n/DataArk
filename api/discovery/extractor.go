package discovery

import (
	"bytes"
	"net/url"
	"strings"

	distiller "github.com/markusmobius/go-domdistiller"
	"golang.org/x/net/html"
)

type ExtractedArticle struct {
	Title        string
	Text         string
	WordCount    int
	Description  string
	CanonicalURL string
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
		Title:     strings.TrimSpace(result.Title),
		Text:      strings.Join(strings.Fields(result.Text), " "),
		WordCount: result.WordCount,
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
	return article, nil
}

type htmlMetadata struct {
	title        string
	description  string
	canonicalURL string
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
