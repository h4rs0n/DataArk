package config

import "testing"

func TestValidateRequiredLLM(t *testing.T) {
	oldBase := LLMBASEURL
	oldModel := LLMCHATMODEL
	oldMode := ARTICLEASSESSMENTMODE
	t.Cleanup(func() {
		LLMBASEURL = oldBase
		LLMCHATMODEL = oldModel
		ARTICLEASSESSMENTMODE = oldMode
	})

	LLMBASEURL = ""
	LLMCHATMODEL = "qwen"
	ARTICLEASSESSMENTMODE = "active"
	if err := ValidateRequiredLLM(); err == nil || err.Error() != "-llm-base-url is required" {
		t.Fatalf("missing base url: %v", err)
	}

	LLMBASEURL = "http://ollama:11434"
	LLMCHATMODEL = ""
	if err := ValidateRequiredLLM(); err == nil || err.Error() != "-llm-chat-model is required" {
		t.Fatalf("missing chat model: %v", err)
	}

	LLMCHATMODEL = "qwen"
	ARTICLEASSESSMENTMODE = "observe"
	if err := ValidateRequiredLLM(); err == nil || err.Error() != "-article-assessment-mode must be active" {
		t.Fatalf("observe mode: %v", err)
	}

	ARTICLEASSESSMENTMODE = "active"
	if err := ValidateRequiredLLM(); err != nil {
		t.Fatalf("valid config: %v", err)
	}
}
