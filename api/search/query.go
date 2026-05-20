package search

import (
	"DataArk/common"
	"encoding/json"
	"log"
	"strings"

	"github.com/meilisearch/meilisearch-go"
)

type Result struct {
	Id       string `json:"id"`
	Title    string `json:"title"`
	Filename string `json:"filename"`
	Link     string `json:"link"`
	Content  string `json:"content"`
	Domain   string `json:"domain"`
}

func QueryByKeyword(keyword string, pageNum int64) (string, map[string]int) {
	pageAndHits := make(map[string]int)
	QueryResults := make([]Result, 0, 10)
	preTag := "<span style=\"color: red;\">"
	postTag := "</span>"

	client := meilisearch.New(common.MEILIHOST, meilisearch.WithAPIKey(common.MEILIAPIKey))

	meiliReqOpt := &meilisearch.SearchRequest{
		Page:                  pageNum,
		HitsPerPage:           10,
		AttributesToHighlight: []string{"content"},
		ShowMatchesPosition:   true,
		HighlightPreTag:       preTag,
		HighlightPostTag:      postTag,
		AttributesToCrop:      []string{"content"},
		CropLength:            150,
	}

	meiliResp, err := client.Index("blogs").Search(keyword, meiliReqOpt)
	if err != nil {
		log.Println("Error Occur: " + err.Error())
		return "Error", nil
	}

	TotalHits := meiliResp.TotalHits
	TotalPages := meiliResp.TotalPages
	pageAndHits["TotalHits"] = int(TotalHits)
	pageAndHits["TotalPages"] = int(TotalPages)

	hits := meiliResp.Hits

	for _, hit := range hits {
		var result Result
		singleContent, _ := hit.(map[string]interface{})

		// 获取高亮内容
		formattedContent, _ := singleContent["_formatted"].(map[string]interface{})
		formattedContentStr, _ := formattedContent["content"].(string)

		result.Id = documentString(formattedContent, "id")
		if result.Id == "" {
			result.Id = documentString(singleContent, "id")
		}
		result.Filename = documentString(singleContent, "filename")
		result.Domain = documentString(singleContent, "domain")
		result.Title = documentString(singleContent, "title")
		result.Link = documentString(singleContent, "link")
		result.Content = formattedContentStr

		QueryResults = append(QueryResults, result)
	}
	resultJson, _ := json.MarshalIndent(QueryResults, "", "    ")
	// fmt.Println(string(resultJson))
	resultJsonString := strings.ReplaceAll(string(resultJson), "\n", "")

	return resultJsonString, pageAndHits
}
