package assessment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type BatchFile struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	Status   string `json:"status"`
}
type RemoteBatch struct {
	ID           string            `json:"id"`
	InputFileID  string            `json:"input_file_id"`
	OutputFileID string            `json:"output_file_id"`
	ErrorFileID  string            `json:"error_file_id"`
	Status       string            `json:"status"`
	CreatedAt    int64             `json:"created_at"`
	ExpiresAt    int64             `json:"expires_at"`
	Metadata     map[string]string `json:"metadata"`
	Counts       struct {
		Total     int `json:"total"`
		Completed int `json:"completed"`
		Failed    int `json:"failed"`
	} `json:"request_counts"`
	Errors json.RawMessage `json:"errors"`
}
type BatchProvider interface {
	Upload(context.Context, string, []byte) (BatchFile, error)
	File(context.Context, string) (BatchFile, error)
	FindFile(context.Context, string) (*BatchFile, error)
	Create(context.Context, string, string) (RemoteBatch, error)
	Retrieve(context.Context, string) (RemoteBatch, error)
	FindBatch(context.Context, string, string) (*RemoteBatch, error)
	Download(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
}
type BailianBatchProvider struct {
	BaseURL, APIKey string
	Endpoint        string
	Client          *http.Client
}
type batchHTTPError struct {
	Status        int
	Code, Message string
}

func (e batchHTTPError) Error() string {
	message := fmt.Sprintf("Batch provider HTTP %d", e.Status)
	if e.Code != "" {
		message += " (" + e.Code + ")"
	}
	if e.Message != "" {
		message += ": " + e.Message
	}
	return message
}
func (p BailianBatchProvider) request(ctx context.Context, method, path string, body io.Reader, contentType string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(p.BaseURL, "/")+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Batch transport failed")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
		res.Body.Close()
		problems := batchProviderProblems(data)
		failure := batchHTTPError{Status: res.StatusCode}
		if len(problems) > 0 {
			failure.Code = safeBatchMessage(problems[0].Code, p.APIKey)
			failure.Message = safeBatchMessage(problems[0].Message, p.APIKey)
		}
		return nil, failure
	}
	return res, nil
}
func (p BailianBatchProvider) json(ctx context.Context, method, path string, body any, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	res, err := p.request(ctx, method, path, r, "application/json")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(out); err != nil {
		return fmt.Errorf("invalid Batch provider JSON response")
	}
	return nil
}
func (p BailianBatchProvider) Upload(ctx context.Context, name string, data []byte) (BatchFile, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("purpose", "batch")
	f, err := w.CreateFormFile("file", name)
	if err != nil {
		return BatchFile{}, err
	}
	if _, err = f.Write(data); err != nil {
		return BatchFile{}, err
	}
	if err = w.Close(); err != nil {
		return BatchFile{}, err
	}
	res, err := p.request(ctx, "POST", "/files", &body, w.FormDataContentType())
	if err != nil {
		return BatchFile{}, err
	}
	defer res.Body.Close()
	var file BatchFile
	if err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&file); err != nil || file.ID == "" {
		return file, fmt.Errorf("invalid upload response")
	}
	return file, nil
}
func (p BailianBatchProvider) File(ctx context.Context, id string) (BatchFile, error) {
	var f BatchFile
	err := p.json(ctx, "GET", "/files/"+url.PathEscape(id), nil, &f)
	return f, err
}
func (p BailianBatchProvider) FindFile(ctx context.Context, name string) (*BatchFile, error) {
	after := ""
	for {
		var page struct {
			Data    []BatchFile `json:"data"`
			HasMore bool        `json:"has_more"`
		}
		err := p.json(ctx, "GET", "/files?purpose=batch&limit=100&after="+url.QueryEscape(after), nil, &page)
		if err != nil {
			return nil, err
		}
		for _, f := range page.Data {
			if f.Filename == name {
				return &f, nil
			}
		}
		if !page.HasMore || len(page.Data) == 0 {
			return nil, nil
		}
		last := page.Data[len(page.Data)-1].ID
		if last == after {
			return nil, fmt.Errorf("invalid file pagination")
		}
		after = last
	}
}
func (p BailianBatchProvider) Create(ctx context.Context, fileID, name string) (RemoteBatch, error) {
	var b RemoteBatch
	err := p.json(ctx, "POST", "/batches", map[string]any{"input_file_id": fileID, "endpoint": batchEndpoint(p.Endpoint), "completion_window": "24h", "metadata": map[string]string{"ds_name": name}}, &b)
	if err == nil && b.ID == "" {
		err = fmt.Errorf("invalid create response")
	}
	return b, err
}
func (p BailianBatchProvider) Retrieve(ctx context.Context, id string) (RemoteBatch, error) {
	var b RemoteBatch
	err := p.json(ctx, "GET", "/batches/"+url.PathEscape(id), nil, &b)
	return b, err
}
func (p BailianBatchProvider) FindBatch(ctx context.Context, fileID, name string) (*RemoteBatch, error) {
	after := ""
	for {
		var page struct {
			Data    []RemoteBatch `json:"data"`
			HasMore bool          `json:"has_more"`
		}
		err := p.json(ctx, "GET", "/batches?limit=100&input_file_ids="+url.QueryEscape(fileID)+"&ds_name="+url.QueryEscape(name)+"&after="+url.QueryEscape(after), nil, &page)
		if err != nil {
			return nil, err
		}
		for _, b := range page.Data {
			if b.InputFileID == fileID && b.Metadata["ds_name"] == name {
				return &b, nil
			}
		}
		if !page.HasMore || len(page.Data) == 0 {
			return nil, nil
		}
		last := page.Data[len(page.Data)-1].ID
		if last == after {
			return nil, fmt.Errorf("invalid Batch pagination")
		}
		after = last
	}
}
func (p BailianBatchProvider) Download(ctx context.Context, id string) (io.ReadCloser, error) {
	res, err := p.request(ctx, "GET", "/files/"+url.PathEscape(id)+"/content", nil, "")
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}
func (p BailianBatchProvider) Delete(ctx context.Context, id string) error {
	return p.json(ctx, "DELETE", "/files/"+url.PathEscape(id), nil, nil)
}

func batchEndpoint(endpoint string) string {
	if endpoint != "" {
		return endpoint
	}
	return "/v1/chat/completions"
}
