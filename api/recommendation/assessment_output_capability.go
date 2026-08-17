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

// currentMode 短锁读取已缓存的响应格式，调用方不得在 HTTP 期间持有这把锁。
func (capability *assessmentOutputCapability) currentMode() string {
	capability.mu.Lock()
	defer capability.mu.Unlock()
	return capability.mode
}

// rememberMode 记录本进程对该 endpoint+model 可用的响应格式。
func (capability *assessmentOutputCapability) rememberMode(mode string) {
	capability.mu.Lock()
	defer capability.mu.Unlock()
	capability.mode = mode
}

// isArticleAssessmentOutputRetryable 判定模型输出本身坏了，可用具体校验根因再打 json_schema。
func isArticleAssessmentOutputRetryable(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "invalid chat json") || strings.Contains(message, "empty chat completion")
}

// isArticleAssessmentUnsupportedSchema 判定网关拒绝 json_schema，应立刻改 json_object 且不必把 HTTP 正文喂给模型。
func isArticleAssessmentUnsupportedSchema(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
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
