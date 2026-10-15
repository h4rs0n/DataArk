package assessment

import (
	"DataArk/config"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type batchProviderProblem struct {
	Code    string
	Message string
	Line    string
}

var batchSecretPattern = regexp.MustCompile(`(?i)\bsk-[a-z0-9_-]+\b|\b(?:authorization|api[_-]?key|access[_-]?token|password)\s*[:=]\s*(?:bearer\s+)?[^\s,;]+`)
var batchURLPattern = regexp.MustCompile(`https?://[^\s]+`)

func safeBatchMessage(message string, extraKeys ...string) string {
	for _, key := range append(extraKeys, config.LLMBATCHAPIKEY, config.LLMAPIKEY) {
		if key != "" {
			message = strings.ReplaceAll(message, key, "[redacted]")
		}
	}
	message = batchSecretPattern.ReplaceAllString(message, "[redacted]")
	message = batchURLPattern.ReplaceAllString(message, "[url]")
	return compactAssessmentError(message)
}

// Only documented code/message/line fields are exposed. Raw responses can
// contain request bodies or credentials and are never copied into logs.
func batchProviderProblems(raw json.RawMessage) []batchProviderProblem {
	var result []batchProviderProblem
	var visit func(json.RawMessage, int)
	visit = func(data json.RawMessage, depth int) {
		if depth > 4 || len(result) >= 128 || len(data) == 0 || string(data) == "null" {
			return
		}
		var list []json.RawMessage
		if json.Unmarshal(data, &list) == nil {
			for _, item := range list {
				visit(item, depth+1)
			}
			return
		}
		var object map[string]json.RawMessage
		if json.Unmarshal(data, &object) != nil {
			return
		}
		var problem batchProviderProblem
		_ = json.Unmarshal(object["code"], &problem.Code)
		if problem.Code == "" {
			var number json.Number
			if json.Unmarshal(object["code"], &number) == nil {
				problem.Code = number.String()
			}
		}
		_ = json.Unmarshal(object["message"], &problem.Message)
		if problem.Code != "" || problem.Message != "" {
			if line, ok := object["line"]; ok {
				var n json.Number
				if json.Unmarshal(line, &n) == nil {
					problem.Line = n.String()
				}
			}
			result = append(result, problem)
		}
		for _, key := range []string{"error", "errors", "data"} {
			visit(object[key], depth+1)
		}
	}
	visit(raw, 0)
	return result
}

func batchProblemStatus(code string) int {
	code = strings.ToLower(strings.TrimSpace(code))
	if status, err := strconv.Atoi(code); err == nil && status >= 400 && status < 600 {
		return status
	}
	switch code {
	case "invalidapikey", "invalid_api_key", "invalidaccesstoken", "invalid_access_token", "missing_api_key", "token_expired", "invalid_token", "unauthorized", "authentication_error":
		return 401
	case "limitrequests", "limit_requests", "limit_burst_rate", "rate_limit_exceeded", "rate_limit_error", "insufficient_quota", "resourceexhausted", "too many requests", "budgetlimitexceeded", "commoditynotpurchased", "prepaidbilloverdue", "postpaidbilloverdue":
		return 429
	}
	if code == "throttling" || strings.HasPrefix(code, "throttling.") {
		return 429
	}
	return 0
}

func batchProblemSummary(problems []batchProviderProblem) string {
	parts := make([]string, 0, len(problems))
	for _, problem := range problems {
		message := problem.Code
		if problem.Message != "" {
			if message != "" {
				message += ": "
			}
			message += problem.Message
		}
		if problem.Line != "" {
			message = "line " + problem.Line + ": " + message
		}
		parts = append(parts, message)
	}
	return safeBatchMessage(strings.Join(parts, "; "))
}

func batchProblemPause(problems []batchProviderProblem) error {
	for _, problem := range problems {
		if status := batchProblemStatus(problem.Code); status == 401 || status == 429 {
			return batchHTTPError{Status: status, Code: problem.Code, Message: safeBatchMessage(problem.Message)}
		}
	}
	return nil
}

// batchStopError preserves an uncertain outcome or exhausted lookup for
// explicit owner intervention; resuming never automatically resubmits it.
type batchStopError struct {
	Status  int
	Message string
}

func (e batchStopError) Error() string { return e.Message }

func isBatchPauseError(err error) bool {
	var stop batchStopError
	return batchPauseCode(err) != 0 || errors.As(err, &stop)
}

func batchHTTPStatus(err error) int {
	var h batchHTTPError
	if errors.As(err, &h) {
		return h.Status
	}
	return 0
}

func boundedBatchError(b *Batch, err error) error {
	if err == nil || isBatchPauseError(err) {
		return err
	}
	status := batchHTTPStatus(err)
	limit := 8
	if status >= 400 && status < 500 && status != 408 {
		limit = 3
	}
	if b.FailureCount+1 >= limit {
		return batchStopError{Status: status, Message: fmt.Sprintf("%s; stopped after %d consecutive errors; check the provider task before manually resuming", safeBatchError(err), limit)}
	}
	return err
}
