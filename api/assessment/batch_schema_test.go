package assessment

import (
	"DataArk/jobqueue"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// Exercise actual admission, persisted JSONL, multipart upload and batch
// creation so the schema is verified at the provider boundary.
func TestBatchProviderReceivesStrictAssessmentJSONSchema(t *testing.T) {
	_, _, _ = setupBatchFixture(t)
	uploads, creates := 0, 0
	var uploaded []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/files":
			uploads++
			if err := r.ParseMultipartForm(2 << 20); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			defer r.MultipartForm.RemoveAll()
			if r.FormValue("purpose") != "batch" {
				t.Error("invalid file purpose")
			}
			file, _, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			defer file.Close()
			data, _ := io.ReadAll(file)
			uploaded = append(uploaded, data...)
			io.WriteString(w, `{"id":"schema-input","status":"processed"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/files/schema-input":
			io.WriteString(w, `{"id":"schema-input","status":"processed"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/batches":
			creates++
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				t.Error("invalid create body")
			}
			if body["input_file_id"] != "schema-input" || body["endpoint"] != "/v1/chat/completions" {
				t.Error("invalid batch endpoint or file")
			}
			if _, present := body["response_format"]; present {
				t.Error("schema belongs inside JSONL body")
			}
			io.WriteString(w, `{"id":"schema-remote","status":"validating"}`)
		default:
			t.Errorf("unexpected provider request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(400)
		}
	}))
	defer server.Close()
	batchProviderFactory = func(string) BatchProvider {
		return BailianBatchProvider{BaseURL: server.URL + "/v1", APIKey: "fixture", Client: server.Client()}
	}
	if err := StartBatchRun(context.Background()); err != nil && !errors.Is(err, jobqueue.ErrHandlerUnavailable) {
		t.Fatal(err)
	}
	for n := 0; n < 8 && creates == 0; n++ {
		batchStep(t)
	}
	if uploads != 1 || creates != 1 {
		t.Fatal("schema batch not submitted", uploads, creates)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(uploaded)), "\n") {
		assertBatchAssessmentSchema(t, []byte(line))
	}
	var batch Batch
	if err := db.First(&batch).Error; err != nil {
		t.Fatal(err)
	}
	if batch.ResponseMode != ResponseSchemaMode || batch.RemoteID != "schema-remote" {
		t.Fatal(batch.ResponseMode, batch.RemoteID)
	}
}

func assertBatchAssessmentSchema(t *testing.T, line []byte) {
	t.Helper()
	var request struct {
		Method string `json:"method"`
		URL    string `json:"url"`
		Body   struct {
			Thinking bool `json:"enable_thinking"`
			Format   struct {
				Type   string `json:"type"`
				Schema struct {
					Strict     bool   `json:"strict"`
					Name       string `json:"name"`
					Definition struct {
						Type       string                     `json:"type"`
						Additional bool                       `json:"additionalProperties"`
						Required   []string                   `json:"required"`
						Properties map[string]json.RawMessage `json:"properties"`
					} `json:"schema"`
				} `json:"json_schema"`
			} `json:"response_format"`
		} `json:"body"`
	}
	if err := json.Unmarshal(line, &request); err != nil {
		t.Fatal(err)
	}
	format := request.Body.Format
	schema := format.Schema.Definition
	if request.Method != "POST" || request.URL != "/v1/chat/completions" || format.Type != ResponseSchemaMode || !format.Schema.Strict || format.Schema.Name != "article_assessment" || schema.Type != "object" || schema.Additional || request.Body.Thinking {
		t.Fatal("invalid strict schema envelope")
	}
	if !reflect.DeepEqual(schema.Required, []string{"qualityScore", "depthScore", "evergreenScore", "reasons", "summary", "keywords"}) || len(schema.Properties) != 6 {
		t.Fatal("missing or unexpected assessment fields")
	}
	for _, field := range []string{"qualityScore", "depthScore", "evergreenScore"} {
		var p map[string]any
		_ = json.Unmarshal(schema.Properties[field], &p)
		if p["type"] != "integer" || p["minimum"] != float64(0) || p["maximum"] != float64(100) {
			t.Fatal("invalid score constraint", field)
		}
	}
	for _, field := range []struct {
		name             string
		min, max, length int
	}{{"reasons", 2, 2, 120}, {"keywords", 3, 8, 20}} {
		var p struct {
			Type               string
			MinItems, MaxItems int
			Items              struct {
				Type                 string
				MinLength, MaxLength int
			}
		}
		if err := json.Unmarshal(schema.Properties[field.name], &p); err != nil {
			t.Fatal(err)
		}
		if p.Type != "array" || p.MinItems != field.min || p.MaxItems != field.max || p.Items.Type != "string" || p.Items.MinLength != 1 || p.Items.MaxLength != field.length {
			t.Fatal("invalid array constraint", field.name, p)
		}
	}
	var summary struct {
		Type                 string
		MinLength, MaxLength int
	}
	_ = json.Unmarshal(schema.Properties["summary"], &summary)
	if summary.Type != "string" || summary.MinLength != 1 || summary.MaxLength != 200 {
		t.Fatal("invalid summary constraint")
	}
	var envelope struct{ Body map[string]json.RawMessage }
	_ = json.Unmarshal(line, &envelope)
	if string(envelope.Body["enable_thinking"]) != "false" || string(envelope.Body["stream"]) != "false" {
		t.Fatal("batch must explicitly disable thinking and streaming")
	}
	var rawFormat struct {
		JSONSchema struct{ Schema map[string]json.RawMessage } `json:"json_schema"`
	}
	_ = json.Unmarshal(envelope.Body["response_format"], &rawFormat)
	if string(rawFormat.JSONSchema.Schema["additionalProperties"]) != "false" {
		t.Fatal("extra output fields must be explicitly forbidden")
	}
	if _, present := envelope.Body["max_tokens"]; present {
		t.Fatal("schema response can be truncated by max_tokens")
	}
}
