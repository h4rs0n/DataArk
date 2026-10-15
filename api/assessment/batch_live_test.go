package assessment

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Free official test model verifies transport only, not assessment quality.
func TestBailianBatchLiveTransport(t *testing.T) {
	key := os.Getenv("DATAARK_BAILIAN_TEST_API_KEY")
	if key == "" {
		t.Skip("set DATAARK_BAILIAN_TEST_API_KEY for opt-in free transport smoke test")
	}
	base := os.Getenv("DATAARK_BAILIAN_TEST_BASE_URL")
	if base == "" {
		base = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	}
	p := BailianBatchProvider{BaseURL: base, APIKey: key, Endpoint: "/v1/chat/ds-test", Client: &http.Client{Timeout: 60 * time.Second}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	input := []byte(`{"custom_id":"transport-test","method":"POST","url":"/v1/chat/ds-test","body":{"model":"batch-test-model","messages":[{"role":"user","content":"hello"}]}}` + "\n")
	f, err := p.Upload(ctx, "dataark-free-transport-test.jsonl", input)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Delete(context.Background(), f.ID) }()
	b, err := p.Create(ctx, f.ID, "dataark-free-transport-test")
	if err != nil {
		t.Fatal(err)
	}
	for {
		b, err = p.Retrieve(ctx, b.ID)
		if err != nil {
			t.Fatal(err)
		}
		switch b.Status {
		case "completed":
			reader, err := p.Download(ctx, b.OutputFileID)
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(reader)
			reader.Close()
			_ = p.Delete(context.Background(), b.OutputFileID)
			if err != nil || !strings.Contains(string(data), "transport-test") {
				t.Fatal("test result missing")
			}
			return
		case "failed", "expired", "cancelled":
			t.Fatalf("test batch ended %s", b.Status)
		}
		select {
		case <-ctx.Done():
			t.Fatal("test timed out; inspect remote task before retry")
		case <-time.After(10 * time.Second):
		}
	}
}
