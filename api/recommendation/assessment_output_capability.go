package recommendation

import (
	"fmt"
	"strings"
	"sync"
)

type assessmentOutputCapability struct {
	mu   sync.Mutex
	mode string
}

var assessmentOutputCapabilities = struct {
	sync.Mutex
	items map[string]*assessmentOutputCapability
}{items: make(map[string]*assessmentOutputCapability)}

func assessmentOutputCapabilityFor(provider OpenAICompatibleProvider) *assessmentOutputCapability {
	key := strings.ToLower(strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")) + "\x00" + strings.ToLower(strings.TrimSpace(provider.ChatModel))
	assessmentOutputCapabilities.Lock()
	defer assessmentOutputCapabilities.Unlock()
	if existing := assessmentOutputCapabilities.items[key]; existing != nil {
		return existing
	}
	created := &assessmentOutputCapability{}
	assessmentOutputCapabilities.items[key] = created
	return created
}

func shouldRetryArticleAssessmentAsJSONObject(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "invalid chat json") || strings.Contains(message, "empty chat completion") {
		return true
	}
	statusAllowsFallback := false
	for _, status := range []int{400, 404, 415, 422} {
		if strings.Contains(message, fmt.Sprintf("status %d", status)) {
			statusAllowsFallback = true
			break
		}
	}
	if !statusAllowsFallback {
		return false
	}
	for _, marker := range []string{"response_format", "response format", "json_schema", "json schema", "structured output", "unsupported schema"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func resetAssessmentOutputCapabilitiesForTest() {
	assessmentOutputCapabilities.Lock()
	assessmentOutputCapabilities.items = make(map[string]*assessmentOutputCapability)
	assessmentOutputCapabilities.Unlock()
}
