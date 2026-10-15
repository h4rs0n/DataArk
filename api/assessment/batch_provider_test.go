package assessment

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBailianProviderWireProtocolAndSafeErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-secret" {
			t.Error("missing auth")
		}
		switch {
		case r.Method == "POST" && r.URL.Path == "/files":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
			}
			if r.FormValue("purpose") != "batch" {
				t.Error("wrong purpose")
			}
			f, h, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				return
			}
			data, _ := io.ReadAll(f)
			f.Close()
			if string(data) != "request\n" || h.Filename != "unique.jsonl" {
				t.Error("file payload changed")
			}
			io.WriteString(w, `{"id":"input","status":"processed"}`)
		case r.Method == "POST" && r.URL.Path == "/batches":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["endpoint"] != "/v1/chat/completions" || body["completion_window"] != "24h" || body["input_file_id"] != "input" {
				t.Error(body)
			}
			io.WriteString(w, `{"id":"remote","status":"validating"}`)
		case r.URL.Path == "/batches":
			io.WriteString(w, `{"data":[{"id":"remote","input_file_id":"input","metadata":{"ds_name":"unique"}}],"has_more":false}`)
		case r.URL.Path == "/files":
			io.WriteString(w, `{"data":[{"id":"input","filename":"unique.jsonl"}],"has_more":false}`)
		case r.Method == "DELETE":
			io.WriteString(w, `{"deleted":true}`)
		case strings.HasSuffix(r.URL.Path, "/content"):
			io.WriteString(w, "output\n")
		default:
			w.WriteHeader(401)
			io.WriteString(w, `{"error":"secret prompt fixture-secret"}`)
		}
	}))
	defer server.Close()
	p := BailianBatchProvider{BaseURL: server.URL, APIKey: "fixture-secret", Client: server.Client()}
	ctx := context.Background()
	file, err := p.Upload(ctx, "unique.jsonl", []byte("request\n"))
	if err != nil || file.ID != "input" {
		t.Fatal(file, err)
	}
	if _, err = p.Create(ctx, "input", "unique"); err != nil {
		t.Fatal(err)
	}
	remote, err := p.FindBatch(ctx, "input", "unique")
	if err != nil || remote == nil || remote.ID != "remote" {
		t.Fatal(remote, err)
	}
	f, err := p.FindFile(ctx, "unique.jsonl")
	if err != nil || f == nil {
		t.Fatal(f, err)
	}
	stream, err := p.Download(ctx, "output")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(stream)
	stream.Close()
	if string(data) != "output\n" {
		t.Fatal(string(data))
	}
	if err = p.Delete(ctx, "input"); err != nil {
		t.Fatal(err)
	}
	_, err = p.Retrieve(ctx, "missing")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("unsafe provider error", err)
	}
}
