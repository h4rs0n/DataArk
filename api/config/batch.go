package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

const DefaultBatchBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"

var LLMBATCHPROVIDER = ""
var LLMBATCHBASEURL = DefaultBatchBaseURL
var LLMBATCHAPIKEY = ""
var LLMBATCHMODEL = ""
var LLMBATCHSIZE = 100
var LLMBATCHCONCURRENCY = 10
var LLMBATCHPOLLINTERVAL = "90s"
var LLMBATCHREQUESTTIMEOUT = "60s"

func BatchEnabled() bool { return strings.TrimSpace(LLMBATCHPROVIDER) == "bailian" }

// ArticleAssessmentIdentity is shared with dedupe without importing assessment.
func ArticleAssessmentIdentity() (string, string) {
	if BatchEnabled() {
		return "bailian_batch", strings.TrimSpace(LLMBATCHMODEL)
	}
	model := strings.TrimSpace(LLMCHATMODEL)
	if model == "" {
		model = "configured"
	}
	return "openai_compatible", model
}

func ValidateBatchLLM() error {
	if LLMBATCHPROVIDER != "" && LLMBATCHPROVIDER != "bailian" {
		return fmt.Errorf("-llm-batch-provider supports bailian only")
	}
	if BatchEnabled() && (strings.TrimSpace(LLMBATCHAPIKEY) == "" || strings.TrimSpace(LLMBATCHMODEL) == "") {
		return fmt.Errorf("-llm-batch-api-key and -llm-batch-model are required when Batch is enabled")
	}
	u, err := url.Parse(LLMBATCHBASEURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("-llm-batch-base-url must be an HTTP(S) base URL without credentials, query or fragment")
	}
	if LLMBATCHSIZE < 1 || LLMBATCHSIZE > 50000 {
		return fmt.Errorf("-llm-batch-size must be 1 through 50000")
	}
	if LLMBATCHCONCURRENCY < 1 || LLMBATCHCONCURRENCY > 100 {
		return fmt.Errorf("-llm-batch-concurrency must be 1 through 100")
	}
	for _, p := range []struct{ name, value string }{{"poll-interval", LLMBATCHPOLLINTERVAL}, {"request-timeout", LLMBATCHREQUESTTIMEOUT}} {
		d, err := time.ParseDuration(p.value)
		if err != nil || d <= 0 {
			return fmt.Errorf("-llm-batch-%s must be a positive duration", p.name)
		}
	}
	return nil
}
