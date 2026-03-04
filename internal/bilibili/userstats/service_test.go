package userstats

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shirenchuang/bilibili-mcp/internal/bilibili/api"
)

func TestGetRelationStat_WithSpecifiedMid(t *testing.T) {
	const expectedMid int64 = 123456

	navCalled := 0
	relationCalled := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/web-interface/nav":
			navCalled++
			_, _ = w.Write([]byte(`{"code":0,"message":"0","data":{"isLogin":true,"mid":999}}`))
		case "/x/relation/stat":
			relationCalled++
			if got := r.URL.Query().Get("vmid"); got != fmt.Sprintf("%d", expectedMid) {
				t.Fatalf("vmid 参数错误: got=%s want=%d", got, expectedMid)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"0","data":{"following":88,"follower":99}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := api.NewClientWithEndpoints(
		map[string]string{},
		server.URL+"/x/web-interface/nav",
		server.URL+"/x/relation/stat",
	)
	svc := NewService(client)

	mid := expectedMid
	stat, err := svc.GetRelationStat(context.Background(), &mid)
	if err != nil {
		t.Fatalf("GetRelationStat failed: %v", err)
	}

	if navCalled != 0 {
		t.Fatalf("指定 mid 场景不应调用 nav，实际调用次数: %d", navCalled)
	}
	if relationCalled != 1 {
		t.Fatalf("relation 调用次数错误: got=%d want=1", relationCalled)
	}
	if stat.Mid != expectedMid {
		t.Fatalf("Mid 错误: got=%d want=%d", stat.Mid, expectedMid)
	}
	if stat.Following != 88 || stat.Follower != 99 {
		t.Fatalf("关注/粉丝解析错误: following=%d follower=%d", stat.Following, stat.Follower)
	}
}

func TestGetRelationStat_ResolveCurrentMid(t *testing.T) {
	const navMid int64 = 98765

	navCalled := 0
	relationCalled := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/web-interface/nav":
			navCalled++
			_, _ = w.Write([]byte(fmt.Sprintf(`{"code":0,"message":"0","data":{"isLogin":true,"mid":%d}}`, navMid)))
		case "/x/relation/stat":
			relationCalled++
			if got := r.URL.Query().Get("vmid"); got != fmt.Sprintf("%d", navMid) {
				t.Fatalf("vmid 参数错误: got=%s want=%d", got, navMid)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"0","data":{"following":20,"follower":30}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := api.NewClientWithEndpoints(
		map[string]string{},
		server.URL+"/x/web-interface/nav",
		server.URL+"/x/relation/stat",
	)
	svc := NewService(client)

	stat, err := svc.GetRelationStat(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetRelationStat failed: %v", err)
	}

	if navCalled != 1 {
		t.Fatalf("nav 调用次数错误: got=%d want=1", navCalled)
	}
	if relationCalled != 1 {
		t.Fatalf("relation 调用次数错误: got=%d want=1", relationCalled)
	}
	if stat.Mid != navMid {
		t.Fatalf("Mid 错误: got=%d want=%d", stat.Mid, navMid)
	}
	if stat.Following != 20 || stat.Follower != 30 {
		t.Fatalf("关注/粉丝解析错误: following=%d follower=%d", stat.Following, stat.Follower)
	}
}

func TestGetRelationStat_ErrorCases(t *testing.T) {
	tests := []struct {
		name          string
		navBody       string
		relationBody  string
		expectErrPart string
	}{
		{
			name:          "nav code non-zero",
			navBody:       `{"code":-101,"message":"账号未登录","data":{"isLogin":false,"mid":0}}`,
			expectErrPart: "账号未登录 (code: -101)",
		},
		{
			name:          "not logged in",
			navBody:       `{"code":0,"message":"0","data":{"isLogin":false,"mid":0}}`,
			expectErrPart: "未登录",
		},
		{
			name:          "relation code non-zero",
			navBody:       `{"code":0,"message":"0","data":{"isLogin":true,"mid":100}}`,
			relationBody:  `{"code":22001,"message":"请求受限","data":{}}`,
			expectErrPart: "请求受限 (code: 22001)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/x/web-interface/nav":
					_, _ = w.Write([]byte(tt.navBody))
				case "/x/relation/stat":
					body := tt.relationBody
					if body == "" {
						body = `{"code":0,"message":"0","data":{"following":1,"follower":2}}`
					}
					_, _ = w.Write([]byte(body))
				default:
					t.Fatalf("unexpected path: %s", r.URL.Path)
				}
			}))
			defer server.Close()

			client := api.NewClientWithEndpoints(
				map[string]string{},
				server.URL+"/x/web-interface/nav",
				server.URL+"/x/relation/stat",
			)
			svc := NewService(client)

			_, err := svc.GetRelationStat(context.Background(), nil)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if got := err.Error(); !strings.Contains(got, tt.expectErrPart) {
				t.Fatalf("错误信息不符合预期: got=%q want contains %q", got, tt.expectErrPart)
			}
		})
	}
}
