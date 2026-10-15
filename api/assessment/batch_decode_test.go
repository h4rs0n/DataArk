package assessment

import (
	"DataArk/llm"
	"encoding/json"
	"strings"
	"testing"
)

func TestBatchCompatibleAssessmentPreservesStringEvidence(t *testing.T) {
	for _, sample := range []struct {
		name, reasons, keywords string
		reasonCount             int
	}{
		{"paragraph", "文章提供具体证据，解释机制。局限在于未提供对照实验。", "队列、调度、评估", 1},
		{"semicolon", "文章提供具体证据；缺少对照实验", "队列，调度,评估", 2},
		{"lines", "文章提供具体证据\n缺少对照实验", "队列;调度；评估", 2},
		{"encoded-arrays", `["文章提供具体证据","缺少对照实验"]`, `["队列","调度","评估"]`, 2},
		{"english-term", "文章提供具体证据", "machine learning,调度,评估", 1},
	} {
		t.Run(sample.name, func(t *testing.T) {
			var fields map[string]any
			_ = json.Unmarshal([]byte(validBatchAssessment), &fields)
			fields["reasons"], fields["keywords"], fields["provider_note"] = sample.reasons, sample.keywords, "metadata"
			content, _ := json.Marshal(fields)
			result, err := decodeBatchAssessment(string(content))
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Reasons) != sample.reasonCount || len(result.Keywords) != 3 || result.QualityScore != 85 {
				t.Fatal(result)
			}
			if sample.reasonCount == 1 && result.Reasons[0] != sample.reasons {
				t.Fatal("reason text changed", result.Reasons)
			}
			if sample.name == "english-term" && result.Keywords[0] != "machine learning" {
				t.Fatal(result.Keywords)
			}
			var strict articleAssessmentOutput
			if llm.DecodeChatJSON(string(content), &strict, true) == nil {
				t.Fatal("shared strict decoder unexpectedly accepts compatibility fields")
			}
		})
	}
}

func TestBatchCompatibleAssessmentStillRejectsInvalidData(t *testing.T) {
	for _, sample := range []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{"fractional-score", func(m map[string]any) { m["qualityScore"] = 85.5 }, "qualityScore has an invalid type"},
		{"out-of-range", func(m map[string]any) { m["qualityScore"] = 101 }, "scores must be integers"},
		{"missing-summary", func(m map[string]any) { delete(m, "summary") }, "Missing required fields: summary"},
		{"null-reasons", func(m map[string]any) { m["reasons"] = nil }, "Missing required fields: reasons"},
		{"object-reasons", func(m map[string]any) { m["reasons"] = map[string]string{"one": "证据"} }, "reasons must be a string or string array"},
		{"mixed-array", func(m map[string]any) { m["keywords"] = []any{"队列", 1, "调度"} }, "keywords must be a string or string array"},
		{"empty-reason", func(m map[string]any) { m["reasons"] = " " }, "reasons must be 1 to 240"},
		{"oversized-reason", func(m map[string]any) { m["reasons"] = strings.Repeat("证", 241) }, "reasons must be 1 to 240"},
		{"single-array-reason", func(m map[string]any) { m["reasons"] = []string{"证据"} }, "requires exactly two reasons"},
		{"too-few-keywords", func(m map[string]any) { m["keywords"] = "队列，调度" }, "requires 3 to 8 keywords"},
		{"long-summary", func(m map[string]any) { m["summary"] = strings.Repeat("摘", 201) }, "summary must be 1 to 200"},
	} {
		t.Run(sample.name, func(t *testing.T) {
			var fields map[string]any
			_ = json.Unmarshal([]byte(validBatchAssessment), &fields)
			sample.mutate(fields)
			content, _ := json.Marshal(fields)
			_, err := decodeBatchAssessment(string(content))
			if err == nil || !strings.Contains(err.Error(), sample.want) {
				t.Fatal(err)
			}
		})
	}
	for _, content := range []string{`{`, validBatchAssessment + ` {}`, `null`, `[]`} {
		if _, err := decodeBatchAssessment(content); err == nil || err.Error() != "model output is invalid JSON" {
			t.Fatal(content, err)
		}
	}
}

func TestBatchOutputSeparatesTruncationFromTypeAndProviderErrors(t *testing.T) {
	var out batchOutput
	_ = json.Unmarshal([]byte(batchResult("fixture", validBatchAssessment)), &out)
	out.Response.Body.Choices[0].FinishReason = "length"
	if result, message := decodeBatchOutput(out); result != nil || !strings.Contains(message, "finish_reason=length") {
		t.Fatal(result, message)
	}
	out.Response.Body.Choices[0].FinishReason = "stop"
	out.Response.Body.Error = json.RawMessage(`{"code":"InvalidApiKey"}`)
	if result, _ := decodeBatchOutput(out); result != nil {
		t.Fatal("provider error activated")
	}
}
