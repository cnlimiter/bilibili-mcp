package api

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewClientWithOptions_BaseURLInjection(t *testing.T) {
	client := NewClientWithOptions(
		nil,
		WithAPIBaseURL("http://127.0.0.1:18080/"),
		WithMemberBaseURL("http://127.0.0.1:19090/"),
	)

	if client.apiBaseURL != "http://127.0.0.1:18080" {
		t.Fatalf("apiBaseURL 不符合预期: %s", client.apiBaseURL)
	}

	if client.memberBaseURL != "http://127.0.0.1:19090" {
		t.Fatalf("memberBaseURL 不符合预期: %s", client.memberBaseURL)
	}

	if client.navURL != "http://127.0.0.1:18080/x/web-interface/nav" {
		t.Fatalf("navURL 不符合预期: %s", client.navURL)
	}

	if client.relationStatURL != "http://127.0.0.1:18080/x/relation/stat" {
		t.Fatalf("relationStatURL 不符合预期: %s", client.relationStatURL)
	}
}

func TestMakeJSONRequest_SetsContentTypeAndBody(t *testing.T) {
	type jsonPayload struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	payload := jsonPayload{Name: "alice", Age: 18}
	requestErr := make(chan error, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			requestErr <- fmt.Errorf("请求方法不符合预期: %s", r.Method)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
			requestErr <- fmt.Errorf("Content-Type 不符合预期: %s", contentType)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if cookie := r.Header.Get("Cookie"); cookie != "bili_jct=testcsrf" {
			requestErr <- fmt.Errorf("Cookie 不符合预期: %s", cookie)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if xTest := r.Header.Get("X-Test"); xTest != "1" {
			requestErr <- fmt.Errorf("X-Test 头不符合预期: %s", xTest)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			requestErr <- fmt.Errorf("读取请求体失败: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if got := string(bodyBytes); got != `{"name":"alice","age":18}` {
			requestErr <- fmt.Errorf("JSON body 不符合预期: %s", got)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		requestErr <- nil

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := NewClientWithOptions(map[string]string{"bili_jct": "testcsrf"}, WithAPIBaseURL(server.URL))
	body, err := client.makeJSONRequest(http.MethodPost, client.apiBaseURL+"/x/test/json", payload, map[string]string{"X-Test": "1"})
	if err != nil {
		t.Fatalf("makeJSONRequest 返回错误: %v", err)
	}

	if string(body) != `{"ok":true}` {
		t.Fatalf("响应体不符合预期: %s", string(body))
	}

	if err := <-requestErr; err != nil {
		t.Fatalf("请求断言失败: %v", err)
	}
}
