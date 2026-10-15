package config

import "testing"

func TestBatchConfigurationAndIndependentIdentity(t *testing.T) {
	oldProvider, oldKey, oldModel, oldURL := LLMBATCHPROVIDER, LLMBATCHAPIKEY, LLMBATCHMODEL, LLMBATCHBASEURL
	defer func() {
		LLMBATCHPROVIDER, LLMBATCHAPIKEY, LLMBATCHMODEL, LLMBATCHBASEURL = oldProvider, oldKey, oldModel, oldURL
	}()
	LLMBATCHPROVIDER = ""
	LLMBATCHAPIKEY = ""
	LLMBATCHMODEL = ""
	LLMBATCHBASEURL = DefaultBatchBaseURL
	if BatchEnabled() || ValidateBatchLLM() != nil {
		t.Fatal("default must be realtime")
	}
	LLMBATCHPROVIDER = "bailian"
	if ValidateBatchLLM() == nil {
		t.Fatal("missing credentials accepted")
	}
	LLMBATCHAPIKEY = "fixture"
	LLMBATCHMODEL = "qwen-plus"
	if err := ValidateBatchLLM(); err != nil {
		t.Fatal(err)
	}
	name, model := ArticleAssessmentIdentity()
	if name != "bailian_batch" || model != "qwen-plus" {
		t.Fatal(name, model)
	}
	LLMBATCHBASEURL = "https://user:secret@example.com/v1"
	if ValidateBatchLLM() == nil {
		t.Fatal("URL credential accepted")
	}
	LLMBATCHBASEURL = DefaultBatchBaseURL
	LLMBATCHPROVIDER = "other"
	if ValidateBatchLLM() == nil {
		t.Fatal("unsupported provider accepted")
	}
	LLMBATCHPROVIDER = ""
	if err := ValidateBatchLLM(); err != nil {
		t.Fatal("recovery credentials may remain", err)
	}
}
