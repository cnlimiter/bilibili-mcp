package article

import (
	"github.com/pkg/errors"
	"github.com/shirenchuang/bilibili-mcp/internal/bilibili/api"
	"github.com/shirenchuang/bilibili-mcp/internal/markdown"
)

// Service 专栏服务
type Service struct {
	apiClient *api.Client
}

var renderMarkdownToHTML = markdown.RenderMarkdownToHTML

// NewService 创建专栏服务
func NewService(apiClient *api.Client) *Service {
	return &Service{apiClient: apiClient}
}

// CreateDraft 创建专栏草稿。
func (s *Service) CreateDraft(title, contentMarkdown string, categoryID int) (int64, error) {
	contentHTML, err := renderMarkdownToHTML(contentMarkdown)
	if err != nil {
		return 0, errors.Wrap(err, "渲染Markdown失败")
	}

	resp, err := s.apiClient.CreateArticleDraft(title, contentHTML, categoryID, "")
	if err != nil {
		return 0, errors.Wrap(err, "调用专栏草稿创建API失败")
	}

	if resp.Code != 0 {
		return 0, errors.Errorf("创建专栏草稿失败: %s (code: %d)", resp.Message, resp.Code)
	}

	return resp.Data.DraftID, nil
}

// PublishDraft 发布专栏草稿。
func (s *Service) PublishDraft(draftID int64) (int64, error) {
	resp, err := s.apiClient.PublishArticleDraft(draftID)
	if err != nil {
		return 0, errors.Wrap(err, "调用专栏发布API失败")
	}

	if resp.Code != 0 {
		return 0, errors.Errorf("发布专栏草稿失败: %s (code: %d)", resp.Message, resp.Code)
	}

	return resp.Data.ArticleID, nil
}
