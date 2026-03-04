package userstats

import (
	"context"

	"github.com/pkg/errors"
	"github.com/shirenchuang/bilibili-mcp/internal/bilibili/api"
)

// RelationStat 统一的关注/粉丝统计结构
type RelationStat struct {
	Mid       int64
	Following int64
	Follower  int64
}

// Service 用户统计服务
type Service struct {
	apiClient *api.Client
}

// NewService 创建用户统计服务
func NewService(apiClient *api.Client) *Service {
	return &Service{apiClient: apiClient}
}

// GetRelationStat 获取关注/粉丝统计。mid 为 nil 或 0 时，自动查询当前账号 mid。
func (s *Service) GetRelationStat(ctx context.Context, mid *int64) (*RelationStat, error) {
	_ = ctx

	targetMid, err := s.resolveMid(mid)
	if err != nil {
		return nil, err
	}

	resp, err := s.apiClient.GetRelationStat(targetMid)
	if err != nil {
		return nil, errors.Wrap(err, "调用关系统计API失败")
	}

	if resp.Code != 0 {
		return nil, errors.Errorf("获取关系统计失败: %s (code: %d)", resp.Message, resp.Code)
	}

	return &RelationStat{
		Mid:       targetMid,
		Following: resp.Data.Following,
		Follower:  resp.Data.Follower,
	}, nil
}

func (s *Service) resolveMid(mid *int64) (int64, error) {
	if mid != nil && *mid > 0 {
		return *mid, nil
	}

	navResp, err := s.apiClient.GetNavInfo()
	if err != nil {
		return 0, errors.Wrap(err, "获取当前账号信息失败")
	}

	if navResp.Code != 0 {
		return 0, errors.Errorf("获取当前账号信息失败: %s (code: %d)", navResp.Message, navResp.Code)
	}

	if !navResp.Data.IsLogin || navResp.Data.Mid <= 0 {
		return 0, errors.New("获取当前账号信息失败: 未登录")
	}

	return navResp.Data.Mid, nil
}
