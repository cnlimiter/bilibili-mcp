package article

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shirenchuang/bilibili-mcp/internal/bilibili/api"
)

func TestCreateDraft_Success(t *testing.T) {
	const (
		expectedDraftID int64 = 123456
		title                 = "测试专栏标题"
		markdownContent       = "# 测试标题\n\n这是正文内容"
		categoryID            = 17
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/x/article/creative/draft/add" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method 错误: got=%s want=%s", r.Method, http.MethodPost)
		}
		if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
			t.Fatalf("Content-Type 错误: got=%q want=%q", contentType, "application/json")
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("读取请求体失败: %v", err)
		}

		var req struct {
			Title    string `json:"title"`
			Content  string `json:"content"`
			Category int    `json:"category"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("解析请求体失败: %v", err)
		}

		if req.Title != title {
			t.Fatalf("title 错误: got=%q want=%q", req.Title, title)
		}
		if req.Category != categoryID {
			t.Fatalf("category 错误: got=%d want=%d", req.Category, categoryID)
		}
		if !strings.Contains(req.Content, "<h1>测试标题</h1>") {
			t.Fatalf("content 未正确渲染 markdown: %s", req.Content)
		}

		_, _ = w.Write([]byte(`{"code":0,"message":"0","data":{"draft_id":123456}}`))
	}))
	defer server.Close()

	client := api.NewClientWithOptions(map[string]string{}, api.WithAPIBaseURL(server.URL))
	svc := NewService(client)

	draftID, err := svc.CreateDraft(title, markdownContent, categoryID)
	if err != nil {
		t.Fatalf("CreateDraft failed: %v", err)
	}
	if draftID != expectedDraftID {
		t.Fatalf("draftID 错误: got=%d want=%d", draftID, expectedDraftID)
	}
}

func TestCreateDraft_MarkdownRenderFail(t *testing.T) {
	originalRenderer := renderMarkdownToHTML
	renderMarkdownToHTML = func(string) (string, error) {
		return "", errors.New("mock render error")
	}
	defer func() {
		renderMarkdownToHTML = originalRenderer
	}()

	client := api.NewClientWithOptions(map[string]string{}, api.WithAPIBaseURL("http://127.0.0.1:0"))
	svc := NewService(client)

	_, err := svc.CreateDraft("title", "content", 1)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "渲染Markdown失败") {
		t.Fatalf("错误信息不符合预期: got=%q", got)
	}
}

func TestCreateDraft_APICodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/x/article/creative/draft/add" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
			t.Fatalf("Content-Type 错误: got=%q want=%q", contentType, "application/json")
		}

		var req struct {
			Title   string `json:"title"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("解析请求体失败: %v", err)
		}
		if req.Title == "" || req.Content == "" {
			t.Fatalf("请求体字段不能为空: %+v", req)
		}

		_, _ = w.Write([]byte(`{"code":22013,"message":"参数错误","data":{"draft_id":0}}`))
	}))
	defer server.Close()

	client := api.NewClientWithOptions(map[string]string{}, api.WithAPIBaseURL(server.URL))
	svc := NewService(client)

	_, err := svc.CreateDraft("title", "content", 1)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "参数错误 (code: 22013)") {
		t.Fatalf("错误信息不符合预期: got=%q", got)
	}
}

func TestPublishDraft_Success(t *testing.T) {
	const (
		draftID           int64 = 98765
		expectedArticleID int64 = 54321
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/x/article/creative/publish" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method 错误: got=%s want=%s", r.Method, http.MethodPost)
		}
		if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
			t.Fatalf("Content-Type 错误: got=%q want=%q", contentType, "application/json")
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("读取请求体失败: %v", err)
		}

		var req struct {
			DraftID int64 `json:"draft_id"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("解析请求体失败: %v", err)
		}
		if req.DraftID != draftID {
			t.Fatalf("draft_id 错误: got=%d want=%d", req.DraftID, draftID)
		}

		_, _ = w.Write([]byte(`{"code":0,"message":"0","data":{"article_id":54321}}`))
	}))
	defer server.Close()

	client := api.NewClientWithOptions(map[string]string{}, api.WithAPIBaseURL(server.URL))
	svc := NewService(client)

	articleID, err := svc.PublishDraft(draftID)
	if err != nil {
		t.Fatalf("PublishDraft failed: %v", err)
	}
	if articleID != expectedArticleID {
		t.Fatalf("articleID 错误: got=%d want=%d", articleID, expectedArticleID)
	}
}

func TestPublishDraft_APICodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/x/article/creative/publish" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
			t.Fatalf("Content-Type 错误: got=%q want=%q", contentType, "application/json")
		}

		var req struct {
			DraftID int64 `json:"draft_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("解析请求体失败: %v", err)
		}
		if req.DraftID <= 0 {
			t.Fatalf("draft_id 不合法: %d", req.DraftID)
		}

		_, _ = w.Write([]byte(`{"code":22015,"message":"草稿不存在","data":{"article_id":0}}`))
	}))
	defer server.Close()

	client := api.NewClientWithOptions(map[string]string{}, api.WithAPIBaseURL(server.URL))
	svc := NewService(client)

	_, err := svc.PublishDraft(1)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "草稿不存在 (code: 22015)") {
		t.Fatalf("错误信息不符合预期: got=%q", got)
	}
}
