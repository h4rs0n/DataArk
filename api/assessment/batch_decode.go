package assessment

import (
	"DataArk/llm"
	"encoding/json"
	"fmt"
	"strings"
)

// Batch JSON Object mode guarantees JSON syntax, not field types. Normalize
// the two observed string/list variations without inventing missing evidence.
func decodeBatchAssessment(content string) (ChatAssessmentResult, error) {
	var fields map[string]json.RawMessage
	if err := llm.DecodeChatJSON(content, &fields, true); err != nil || fields == nil {
		return ChatAssessmentResult{}, fmt.Errorf("model output is invalid JSON")
	}
	var output articleAssessmentOutput
	for _, field := range []struct {
		name string
		dest any
	}{
		{"qualityScore", &output.QualityScore}, {"depthScore", &output.DepthScore},
		{"evergreenScore", &output.EvergreenScore}, {"summary", &output.Summary},
	} {
		if raw, exists := fields[field.name]; exists {
			if err := json.Unmarshal(raw, field.dest); err != nil {
				return ChatAssessmentResult{}, fmt.Errorf("model output field %s has an invalid type", field.name)
			}
		}
	}
	stringReason := false
	for _, field := range []struct {
		name string
		dest **[]string
	}{{"reasons", &output.Reasons}, {"keywords", &output.Keywords}} {
		raw, exists := fields[field.name]
		if !exists || string(raw) == "null" {
			continue
		}
		values, wasString, err := batchStringList(raw, field.name)
		if err != nil {
			return ChatAssessmentResult{}, err
		}
		*field.dest = &values
		if field.name == "reasons" {
			stringReason = wasString
		}
	}
	// Additional provider fields do not change the six assessment fields.
	// Array outputs retain strict two-reason validation. A string can contain
	// both reasons as prose, so preserve it in one element (at most 2*120 runes).
	minReasons, maxReasonRunes := 2, 120
	if stringReason && output.Reasons != nil && len(*output.Reasons) == 1 {
		minReasons, maxReasonRunes = 1, 240
	}
	result, err := output.resultWithReasonLimits(minReasons, maxReasonRunes)
	if err != nil {
		return ChatAssessmentResult{}, fmt.Errorf("model output failed assessment field validation: %s", err)
	}
	return result, nil
}

func batchStringList(raw json.RawMessage, field string) ([]string, bool, error) {
	var values []string
	if json.Unmarshal(raw, &values) == nil {
		return values, false, nil
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return nil, false, fmt.Errorf("model output field %s must be a string or string array", field)
	}
	text = strings.TrimSpace(text)
	// Some providers serialize an array inside a JSON string.
	if strings.HasPrefix(text, "[") {
		if json.Unmarshal([]byte(text), &values) == nil {
			return values, true, nil
		}
	}
	if field == "keywords" {
		values = strings.FieldsFunc(text, func(r rune) bool {
			return strings.ContainsRune(",，、;；\n\r", r)
		})
	} else {
		// Commas and full stops belong to reason prose. Split only unambiguous
		// two-part line/semicolon lists; otherwise preserve the entire text.
		values = strings.FieldsFunc(text, func(r rune) bool {
			return strings.ContainsRune(";；\n\r", r)
		})
		if len(values) != 2 {
			values = []string{text}
		} else if len([]rune(strings.TrimSpace(values[0]))) > 120 || len([]rune(strings.TrimSpace(values[1]))) > 120 {
			values = []string{text}
		}
	}
	for i := range values {
		values[i] = strings.TrimSpace(values[i])
	}
	return values, true, nil
}

func decodeBatchOutput(out batchOutput) (*ArticleAssessmentResult, string) {
	if out.Response == nil || out.Response.Status != 200 || hasBatchError(out.Error) || hasBatchError(out.Response.Body.Error) {
		return nil, "provider request failed"
	}
	if len(out.Response.Body.Choices) == 0 {
		return nil, "provider returned an empty completion"
	}
	choice := out.Response.Body.Choices[0]
	if choice.FinishReason == "length" {
		return nil, "model output was truncated (finish_reason=length)"
	}
	result, err := decodeBatchAssessment(choice.Message.Content)
	if err != nil {
		return nil, err.Error()
	}
	normalized := normalizeChatAssessment(result)
	return &normalized, ""
}

func hasBatchError(raw json.RawMessage) bool {
	return len(raw) > 0 && strings.TrimSpace(string(raw)) != "null"
}
