package search

import (
	"DataArk/config"
	"DataArk/database"
	"encoding/json"
	"html"
	"log"
	"strconv"
	"strings"

	"github.com/meilisearch/meilisearch-go"
)

type Result struct {
	Id          string `json:"id"`
	MaterialID  uint   `json:"materialId"`
	Title       string `json:"title"`
	Filename    string `json:"filename"`
	Link        string `json:"link"`
	Content     string `json:"content"`
	Domain      string `json:"domain"`
	Archived    bool   `json:"archived"`
	ArchivePath string `json:"archivePath"`
}

const highlightStart = "\u0001"
const highlightEnd = "\u0002"

func safeSnippet(raw string) string {
	escaped := html.EscapeString(raw)
	escaped = strings.ReplaceAll(escaped, highlightStart, `<span class="search-highlight" style="color: red;">`)
	return strings.ReplaceAll(escaped, highlightEnd, "</span>")
}

func QueryByKeyword(keyword string, pageNum int64) (string, map[string]int) {
	if pageNum < 1 {
		return "Error", nil
	}
	hitsPerPage := int64(10)
	req := &meilisearch.SearchRequest{
		Page: pageNum, HitsPerPage: &hitsPerPage,
		AttributesToHighlight: []string{"content", "summary"},
		HighlightPreTag:       highlightStart, HighlightPostTag: highlightEnd,
		AttributesToCrop: []string{"content", "summary"}, CropLength: 150,
	}
	response, err := searchClient().Index(config.MEILIBlogsIndex).Search(keyword, req)
	if err != nil {
		log.Printf("search query: %v", err)
		return "Error", nil
	}
	results := make([]Result, 0, len(response.Hits))
	ids := []uint{}
	for _, hit := range response.Hits {
		if documentString(hit, "id") == generationMarker {
			continue
		}
		rawID := documentString(hit, "materialId")
		id, _ := strconv.ParseUint(rawID, 10, 64)
		formatted := map[string]interface{}{}
		if raw, ok := hit["_formatted"]; ok {
			_ = json.Unmarshal(raw, &formatted)
		}
		content, _ := formatted["content"].(string)
		summary, _ := formatted["summary"].(string)
		if content == "" || (!strings.Contains(content, highlightStart) && strings.Contains(summary, highlightStart)) {
			content = summary
		}
		if content == "" {
			content = documentString(hit, "summary")
		}
		if content == "" {
			content = documentString(hit, "content")
			content = truncateSnippet(content)
		}
		results = append(results, Result{Id: documentString(hit, "id"), MaterialID: uint(id), Title: documentString(hit, "title"), Filename: documentString(hit, "filename"), Link: documentString(hit, "link"), Domain: documentString(hit, "domain"), Content: safeSnippet(content)})
		if id != 0 {
			ids = append(ids, uint(id))
		}
	}
	db := database.DB()
	if db != nil && len(ids) > 0 {
		archives, err := loadArchives(db, ids)
		if err != nil {
			log.Printf("search archive hydration: %v", err)
			return "Error", nil
		}
		for i := range results {
			result := &results[i]
			if result.MaterialID == 0 {
				continue
			}
			result.Filename = ""
			for _, row := range archives[result.MaterialID] {
				path := existingArchive(row.Domain, row.FileName)
				if path == "" {
					continue
				}
				result.Archived, result.ArchivePath, result.Filename, result.Domain = true, path, row.FileName, row.Domain
				break
			}
		}
	}
	// Legacy documents remain readable while the first full index is building.
	for i := range results {
		result := &results[i]
		if result.MaterialID == 0 || db == nil {
			if path := existingArchive(result.Domain, result.Filename); path != "" {
				result.Archived, result.ArchivePath = true, path
			} else {
				result.Filename = ""
			}
		}
	}
	encoded, _ := json.Marshal(results)
	return string(encoded), map[string]int{"TotalHits": int(response.TotalHits), "TotalPages": int(response.TotalPages)}
}

func truncateSnippet(raw string) string {
	runes := []rune(raw)
	if len(runes) > 600 {
		return string(runes[:600]) + "…"
	}
	return raw
}
