package upload

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/shirenchuang/bilibili-mcp/internal/bilibili/api"
)

func TestCreateVideoDraft_Success(t *testing.T) {
	videoPath := createTempVideoFile(t, videoChunkSize+123)

	var uploadedParts []int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/preupload":
			_, _ = io.WriteString(w, `{"code":0,"message":"0","upos_uri":"upos://bucket/test.mp4","auth":"token-abc","biz_id":1001,"chunk_size":4194304,"endpoint":"`+serverURL(r)+`"}`)
			return

		case r.Method == http.MethodPut && r.URL.Path == "/bucket/test.mp4":
			partNumberValue := r.URL.Query().Get("partNumber")
			partNumber, err := strconv.Atoi(partNumberValue)
			if err != nil {
				t.Fatalf("partNumber 解析失败: %v", err)
			}
			uploadedParts = append(uploadedParts, partNumber)

			if auth := r.Header.Get("X-Upos-Auth"); auth != "token-abc" {
				t.Fatalf("X-Upos-Auth 错误: got=%q want=%q", auth, "token-abc")
			}

			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("读取分片请求体失败: %v", err)
			}
			if len(body) == 0 {
				t.Fatalf("分片内容为空")
			}

			w.WriteHeader(http.StatusNoContent)
			return

		case r.Method == http.MethodPost && r.URL.Path == "/bucket/test.mp4":
			if output := r.URL.Query().Get("output"); output != "json" {
				t.Fatalf("output 参数错误: got=%q want=%q", output, "json")
			}

			_, _ = io.WriteString(w, `{"code":0,"cid":987654321}`)
			return
		}

		t.Fatalf("unexpected request: method=%s path=%s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	client := api.NewClientWithOptions(map[string]string{}, api.WithMemberBaseURL(server.URL))
	service := NewService(client)

	info, err := service.CreateVideoDraft(context.Background(), videoPath, 21, "测试视频")
	if err != nil {
		t.Fatalf("CreateVideoDraft 返回错误: %v", err)
	}

	if info.CID != 987654321 {
		t.Fatalf("CID 错误: got=%d want=%d", info.CID, 987654321)
	}

	if len(uploadedParts) != 2 || uploadedParts[0] != 1 || uploadedParts[1] != 2 {
		t.Fatalf("分片上传次数或顺序错误: got=%v want=[1 2]", uploadedParts)
	}

	decodedToken, err := base64.RawURLEncoding.DecodeString(info.DraftToken)
	if err != nil {
		t.Fatalf("token base64 解码失败: %v", err)
	}

	var token VideoDraftToken
	if err := json.Unmarshal(decodedToken, &token); err != nil {
		t.Fatalf("token json 解析失败: %v", err)
	}

	expectedMD5, err := calculateFileMD5(videoPath)
	if err != nil {
		t.Fatalf("计算期望MD5失败: %v", err)
	}

	if token.Version != videoTokenSchema {
		t.Fatalf("token version 错误: got=%d want=%d", token.Version, videoTokenSchema)
	}
	if token.FilePath != videoPath {
		t.Fatalf("token file_path 错误: got=%q want=%q", token.FilePath, videoPath)
	}
	if token.FileSize != int64(videoChunkSize+123) {
		t.Fatalf("token file_size 错误: got=%d want=%d", token.FileSize, int64(videoChunkSize+123))
	}
	if token.FileName != filepath.Base(videoPath) {
		t.Fatalf("token file_name 错误: got=%q want=%q", token.FileName, filepath.Base(videoPath))
	}
	if token.FileMD5 != expectedMD5 {
		t.Fatalf("token file_md5 错误: got=%q want=%q", token.FileMD5, expectedMD5)
	}
}

func TestCreateVideoDraft_PreuploadError(t *testing.T) {
	videoPath := createTempVideoFile(t, 128)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/preupload" {
			_, _ = io.WriteString(w, `{"code":10001,"message":"preupload failed"}`)
			return
		}

		t.Fatalf("unexpected request: method=%s path=%s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	client := api.NewClientWithOptions(map[string]string{}, api.WithMemberBaseURL(server.URL))
	service := NewService(client)

	_, err := service.CreateVideoDraft(context.Background(), videoPath, 1, "标题")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "预上传失败") {
		t.Fatalf("错误信息不符合预期: %v", err)
	}
}

func TestCreateVideoDraft_UploadChunkError(t *testing.T) {
	videoPath := createTempVideoFile(t, videoChunkSize+10)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/preupload":
			_, _ = io.WriteString(w, `{"code":0,"message":"0","upos_uri":"upos://bucket/test.mp4","auth":"token-abc","biz_id":1001,"chunk_size":4194304,"endpoint":"`+serverURL(r)+`"}`)
			return

		case r.Method == http.MethodPut && r.URL.Path == "/bucket/test.mp4":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `upload failed`)
			return
		}

		t.Fatalf("unexpected request: method=%s path=%s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	client := api.NewClientWithOptions(map[string]string{}, api.WithMemberBaseURL(server.URL))
	service := NewService(client)

	_, err := service.CreateVideoDraft(context.Background(), videoPath, 21, "测试视频")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "上传分片失败") {
		t.Fatalf("错误信息不符合预期: %v", err)
	}
}

func TestPublishVideo_Success(t *testing.T) {
	draftToken := mustBuildDraftToken(t, 55667788)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/x/vu/web/add/v3" {
			t.Fatalf("unexpected request: method=%s path=%s", r.Method, r.URL.Path)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("读取请求体失败: %v", err)
		}

		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("解析请求体失败: %v", err)
		}

		if got := int(payload["copyright"].(float64)); got != 1 {
			t.Fatalf("copyright 错误: got=%d want=%d", got, 1)
		}
		if got := int(payload["tid"].(float64)); got != 171 {
			t.Fatalf("tid 错误: got=%d want=%d", got, 171)
		}
		if got := payload["title"].(string); got != "投稿标题" {
			t.Fatalf("title 错误: got=%q want=%q", got, "投稿标题")
		}
		if got := payload["tag"].(string); got != "测试,自动化" {
			t.Fatalf("tag 错误: got=%q want=%q", got, "测试,自动化")
		}
		if got := payload["desc"].(string); got != "投稿简介" {
			t.Fatalf("desc 错误: got=%q want=%q", got, "投稿简介")
		}

		videos, ok := payload["videos"].([]any)
		if !ok || len(videos) != 1 {
			t.Fatalf("videos 字段错误: %#v", payload["videos"])
		}
		videoItem, ok := videos[0].(map[string]any)
		if !ok {
			t.Fatalf("videos[0] 类型错误: %#v", videos[0])
		}
		if got := videoItem["filename"].(string); got != "55667788" {
			t.Fatalf("videos[0].filename 错误: got=%q want=%q", got, "55667788")
		}

		_, _ = io.WriteString(w, `{"code":0,"message":"0","data":{"bvid":"BV1xx411c7mD"}}`)
	}))
	defer server.Close()

	client := api.NewClientWithOptions(map[string]string{}, api.WithMemberBaseURL(server.URL))
	service := NewService(client)

	bvid, err := service.PublishVideo(context.Background(), draftToken, VideoPublishInfo{
		Copyright: 1,
		Tid:       171,
		Title:     "投稿标题",
		Tag:       "测试,自动化",
		Desc:      "投稿简介",
	})
	if err != nil {
		t.Fatalf("PublishVideo 返回错误: %v", err)
	}
	if bvid != "BV1xx411c7mD" {
		t.Fatalf("bvid 错误: got=%q want=%q", bvid, "BV1xx411c7mD")
	}
}

func TestPublishVideo_InvalidDraftToken(t *testing.T) {
	client := api.NewClientWithOptions(map[string]string{})
	service := NewService(client)

	_, err := service.PublishVideo(context.Background(), "invalid-@@token", VideoPublishInfo{
		Copyright: 1,
		Tid:       171,
		Title:     "投稿标题",
		Tag:       "测试",
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "draftToken base64 解析失败") {
		t.Fatalf("错误信息不符合预期: %v", err)
	}
}

func TestPublishVideo_APICodeError(t *testing.T) {
	draftToken := mustBuildDraftToken(t, 44556677)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/x/vu/web/add/v3" {
			t.Fatalf("unexpected request: method=%s path=%s", r.Method, r.URL.Path)
		}

		_, _ = io.WriteString(w, `{"code":21012,"message":"publish failed"}`)
	}))
	defer server.Close()

	client := api.NewClientWithOptions(map[string]string{}, api.WithMemberBaseURL(server.URL))
	service := NewService(client)

	_, err := service.PublishVideo(context.Background(), draftToken, VideoPublishInfo{
		Copyright: 1,
		Tid:       171,
		Title:     "投稿标题",
		Tag:       "测试",
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "投稿提交失败") || !strings.Contains(err.Error(), "21012") {
		t.Fatalf("错误信息不符合预期: %v", err)
	}
}

func mustBuildDraftToken(t *testing.T, cid int64) string {
	t.Helper()

	tokenBytes, err := json.Marshal(VideoDraftToken{
		Version: videoTokenSchema,
		CID:     cid,
	})
	if err != nil {
		t.Fatalf("序列化 draft token 失败: %v", err)
	}

	return base64.RawURLEncoding.EncodeToString(tokenBytes)
}

func createTempVideoFile(t *testing.T, size int) string {
	t.Helper()

	tempDir := t.TempDir()
	videoPath := filepath.Join(tempDir, "video.mp4")
	content := make([]byte, size)
	for index := range content {
		content[index] = byte(index % 251)
	}

	if err := os.WriteFile(videoPath, content, 0o644); err != nil {
		t.Fatalf("创建临时视频文件失败: %v", err)
	}

	return videoPath
}

func serverURL(request *http.Request) string {
	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	}

	return scheme + "://" + request.Host
}
