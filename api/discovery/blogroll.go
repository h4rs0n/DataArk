package discovery

import (
	"bytes"
	neturl "net/url"
	"sort"
	"strings"

	"golang.org/x/net/html"
)

type BlogrollLink struct {
	TargetURL      string  `json:"targetUrl"`
	SourcePageURL  string  `json:"sourcePageUrl"`
	AnchorText     string  `json:"anchorText"`
	ContextSummary string  `json:"contextSummary"`
	DetectionRule  string  `json:"detectionRule"`
	RelationType   string  `json:"relationType"`
	Confidence     float64 `json:"confidence"`
}

type BlogrollDiscovery struct {
	Links         []BlogrollLink
	DedicatedURLs []string
}

type BlogrollDiscoverer struct{}

func (BlogrollDiscoverer) Discover(body []byte, sourcePageURL string, siteRootURL string) (BlogrollDiscovery, error) {
	document, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return BlogrollDiscovery{}, err
	}
	pageURL, err := neturl.Parse(sourcePageURL)
	if err != nil {
		return BlogrollDiscovery{}, err
	}
	rootURL, err := neturl.Parse(siteRootURL)
	if err != nil {
		return BlogrollDiscovery{}, err
	}
	pageContext := pageSemanticContext(document)
	pageIsDedicated := isBlogrollContext(pageContext) || isCommonBlogrollPath(pageURL.Path)
	blockExternalCounts := externalLinkCounts(document, rootURL)

	links := make(map[string]BlogrollLink)
	dedicated := make(map[string]struct{})
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && (node.Data == "a" || node.Data == "link") {
			href := strings.TrimSpace(attrValue(node, "href"))
			resolved, ok := resolveWebURL(href, pageURL)
			if ok {
				if sameLogicalHost(resolved, rootURL) {
					anchor := compactNodeText(node, 120)
					if isCommonBlogrollPath(resolved.Path) || isBlogrollContext(anchor+" "+attrValue(node, "rel")) {
						dedicated[resolved.String()] = struct{}{}
					}
				} else if candidate, accepted := classifyBlogrollNode(node, resolved, sourcePageURL, pageIsDedicated, blockExternalCounts); accepted {
					existing, found := links[candidate.TargetURL]
					if !found || candidate.Confidence > existing.Confidence {
						links[candidate.TargetURL] = candidate
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)

	result := BlogrollDiscovery{Links: make([]BlogrollLink, 0, len(links)), DedicatedURLs: sortedKeys(dedicated)}
	for _, link := range links {
		result.Links = append(result.Links, link)
	}
	sort.SliceStable(result.Links, func(i, j int) bool {
		if result.Links[i].Confidence == result.Links[j].Confidence {
			return result.Links[i].TargetURL < result.Links[j].TargetURL
		}
		return result.Links[i].Confidence > result.Links[j].Confidence
	})
	return result, nil
}

func classifyBlogrollNode(node *html.Node, target *neturl.URL, sourcePageURL string, pageIsDedicated bool, blockCounts map[*html.Node]int) (BlogrollLink, bool) {
	rel := strings.ToLower(attrValue(node, "rel"))
	anchor := compactNodeText(node, 160)
	block := nearestEvidenceBlock(node)
	contextSummary := compactNodeText(block, 240)
	contextText := strings.Join([]string{rel, anchor, contextSummary, attrValue(block, "aria-label"), attrValue(block, "id"), attrValue(block, "class")}, " ")
	rule := ""
	relation := "blogroll"
	confidence := 0.0
	switch {
	case containsRelation(rel, "friend", "me", "blogroll"):
		rule, confidence = "explicit_rel", 0.98
		if strings.Contains(rel, "friend") || strings.Contains(rel, "me") {
			relation = "friend"
		}
	case isBlogrollContext(contextText) && !isNegatedBlogrollContext(contextText):
		rule, confidence = "context_heading", 0.90
	case pageIsDedicated && !isExcludedLinkContext(contextText):
		rule, confidence = "dedicated_page", 0.85
	case blockCounts[block] >= 3 && !isExcludedLinkContext(contextText):
		rule, confidence = "stable_external_list", 0.72
	default:
		return BlogrollLink{}, false
	}
	return BlogrollLink{
		TargetURL: target.String(), SourcePageURL: sourcePageURL, AnchorText: anchor,
		ContextSummary: contextSummary, DetectionRule: rule, RelationType: relation, Confidence: confidence,
	}, true
}

func externalLinkCounts(document *html.Node, rootURL *neturl.URL) map[*html.Node]int {
	counts := make(map[*html.Node]int)
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "a" {
			if target, ok := resolveWebURL(attrValue(node, "href"), rootURL); ok && !sameLogicalHost(target, rootURL) {
				counts[nearestEvidenceBlock(node)]++
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	return counts
}

func nearestEvidenceBlock(node *html.Node) *html.Node {
	var fallback *html.Node
	for ancestor := node.Parent; ancestor != nil; ancestor = ancestor.Parent {
		if !isEvidenceBlock(ancestor) {
			continue
		}
		if fallback == nil {
			fallback = ancestor
		}
		switch ancestor.Data {
		case "section", "nav", "aside", "footer", "main", "div":
			return ancestor
		}
	}
	if fallback != nil {
		return fallback
	}
	return node
}

func pageSemanticContext(document *html.Node) string {
	parts := make([]string, 0, 3)
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && (node.Data == "title" || node.Data == "h1") {
			parts = append(parts, compactNodeText(node, 120))
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	return strings.Join(parts, " ")
}

func isEvidenceBlock(node *html.Node) bool {
	if node == nil || node.Type != html.ElementNode {
		return false
	}
	switch node.Data {
	case "li", "ul", "ol", "section", "nav", "aside", "main", "footer", "div", "body":
		return true
	default:
		return false
	}
}

func compactNodeText(node *html.Node, limit int) string {
	if node == nil {
		return ""
	}
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
			builder.WriteByte(' ')
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	text := strings.Join(strings.Fields(builder.String()), " ")
	if limit > 0 {
		runes := []rune(text)
		if len(runes) > limit {
			return strings.TrimSpace(string(runes[:limit]))
		}
	}
	return text
}

func resolveWebURL(rawURL string, baseURL *neturl.URL) (*neturl.URL, bool) {
	parsed, err := neturl.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.String() == "" {
		return nil, false
	}
	resolved := baseURL.ResolveReference(parsed)
	if resolved.Scheme != "http" && resolved.Scheme != "https" || resolved.Hostname() == "" {
		return nil, false
	}
	resolved.Fragment = ""
	return resolved, true
}

func sameLogicalHost(left *neturl.URL, right *neturl.URL) bool {
	leftHost := strings.TrimPrefix(strings.ToLower(strings.TrimSuffix(left.Hostname(), ".")), "www.")
	rightHost := strings.TrimPrefix(strings.ToLower(strings.TrimSuffix(right.Hostname(), ".")), "www.")
	return leftHost == rightHost && left.Port() == right.Port()
}

func isBlogrollContext(value string) bool {
	value = strings.ToLower(strings.Join(strings.Fields(value), " "))
	for _, marker := range []string{"友情链接", "友链", "邻居", "朋友", "推荐博客", "blogroll", "friends", "friend links", "people i read", "blogs i read", "recommended blogs"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func isNegatedBlogrollContext(value string) bool {
	value = strings.ToLower(strings.Join(strings.Fields(value), " "))
	for _, marker := range []string{"not a blogroll", "not blogroll", "not a friend link", "不是友链", "并非友链", "不是友情链接"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func isExcludedLinkContext(value string) bool {
	value = strings.ToLower(value)
	for _, marker := range []string{"advert", "sponsor", "shopping", "social", "sign in", "login"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func isCommonBlogrollPath(value string) bool {
	value = strings.ToLower(strings.TrimSuffix(value, "/"))
	base := value[strings.LastIndex(value, "/")+1:]
	base = strings.TrimSuffix(strings.TrimSuffix(base, ".html"), ".htm")
	switch base {
	case "links", "friends", "blogroll", "roll", "neighbors", "neighbours":
		return true
	default:
		return false
	}
}

func containsRelation(value string, relations ...string) bool {
	fields := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool { return r == ' ' || r == ',' })
	for _, field := range fields {
		for _, relation := range relations {
			if field == relation {
				return true
			}
		}
	}
	return false
}
