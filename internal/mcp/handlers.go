package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/shirenchuang/bilibili-mcp/internal/bilibili/api"
	"github.com/shirenchuang/bilibili-mcp/internal/bilibili/article"
	"github.com/shirenchuang/bilibili-mcp/internal/bilibili/comment"
	"github.com/shirenchuang/bilibili-mcp/internal/bilibili/download"
	"github.com/shirenchuang/bilibili-mcp/internal/bilibili/upload"
	"github.com/shirenchuang/bilibili-mcp/internal/bilibili/userstats"
	"github.com/shirenchuang/bilibili-mcp/pkg/logger"
)

// 频率限制器
var (
	rateLimiter = make(map[string]time.Time)
	rateMutex   sync.RWMutex
)

// checkRateLimit 检查频率限制
func checkRateLimit(operation string, minInterval time.Duration) error {
	rateMutex.Lock()
	defer rateMutex.Unlock()

	now := time.Now()
	if lastTime, exists := rateLimiter[operation]; exists {
		if elapsed := now.Sub(lastTime); elapsed < minInterval {
			return errors.Errorf("请求过于频繁，请等待 %.1f 秒后再试", (minInterval - elapsed).Seconds())
		}
	}

	rateLimiter[operation] = now
	return nil
}

// 认证相关处理器

// handleCheckLoginStatus 检查登录状态
func (s *Server) handleCheckLoginStatus(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	accountName := s.getAccountName(args)

	isLoggedIn, account, err := s.loginService.CheckLoginStatus(ctx, accountName)
	if err != nil {
		return s.createErrorResult(err)
	}

	if !isLoggedIn {
		if accountName == "" {
			return s.createToolResult("未登录，请先运行登录工具: ./bilibili-login", false)
		} else {
			return s.createToolResult(fmt.Sprintf("账号 '%s' 未登录，请运行: ./bilibili-login -account %s", accountName, accountName), false)
		}
	}

	result := fmt.Sprintf("已登录 - 账号: %s, 昵称: %s, UID: %s",
		account.Name, account.Nickname, account.UID)
	return s.createToolResult(result, false)
}

// handleListAccounts 列出所有账号
func (s *Server) handleListAccounts(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	accounts, err := s.loginService.ListAccounts()
	if err != nil {
		return s.createErrorResult(err)
	}

	if len(accounts) == 0 {
		return s.createToolResult("没有已登录的账号，请先运行登录工具: ./bilibili-login", false)
	}

	// 格式化账号列表
	var result strings.Builder
	result.WriteString("已登录的账号列表:\n")

	for i, account := range accounts {
		status := ""
		if account.IsDefault {
			status += " (默认)"
		}
		if !account.IsActive {
			status += " (未激活)"
		}

		result.WriteString(fmt.Sprintf("%d. %s - %s (UID: %s)%s\n",
			i+1, account.Name, account.Nickname, account.UID, status))
	}

	return s.createToolResult(result.String(), false)
}

// handleSwitchAccount 切换账号
func (s *Server) handleSwitchAccount(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	accountName, ok := args["account_name"].(string)
	if !ok || accountName == "" {
		return s.createToolResult("缺少account_name参数", true)
	}

	if err := s.loginService.SwitchAccount(accountName); err != nil {
		return s.createErrorResult(err)
	}

	return s.createToolResult(fmt.Sprintf("已切换到账号: %s", accountName), false)
}

// 评论相关处理器

// handlePostComment 发表评论 - 使用API优先
func (s *Server) handlePostComment(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	videoID, ok := args["video_id"].(string)
	if !ok || videoID == "" {
		return s.createToolResult("缺少video_id参数", true)
	}

	content, ok := args["content"].(string)
	if !ok || content == "" {
		return s.createToolResult("缺少content参数", true)
	}

	if err := s.validateVideoID(videoID); err != nil {
		return s.createErrorResult(err)
	}

	accountName := s.getAccountName(args)

	// 获取带认证的浏览器页面（仅用于获取cookies）
	page, cleanup, err := s.browserPool.GetWithAuth(accountName)
	if err != nil {
		return s.createErrorResult(err)
	}
	defer cleanup()

	// 创建API评论服务
	apiCommentService, err := comment.NewAPICommentService(page)
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "创建API评论服务失败"))
	}

	// 使用API发表评论
	commentID, err := apiCommentService.PostComment(ctx, videoID, content)
	if err != nil {
		return s.createErrorResult(err)
	}

	// 生成评论链接
	commentURL := fmt.Sprintf("https://www.bilibili.com/video/%s#reply%d", videoID, commentID)

	result := fmt.Sprintf("评论发表成功！\n视频: %s\n评论ID: %d\n评论链接: %s", videoID, commentID, commentURL)
	return s.createToolResult(result, false)
}

// 暂时注释 - handlePostImageComment 发表图片评论功能暂不提供
// func (s *Server) handlePostImageComment(ctx context.Context, args map[string]interface{}) *MCPToolResult {
// 	videoID, ok := args["video_id"].(string)
// 	if !ok || videoID == "" {
// 		return s.createToolResult("缺少video_id参数", true)
// 	}

// 	content, ok := args["content"].(string)
// 	if !ok || content == "" {
// 		return s.createToolResult("缺少content参数", true)
// 	}

// 	imagePath, ok := args["image_path"].(string)
// 	if !ok || imagePath == "" {
// 		return s.createToolResult("缺少image_path参数", true)
// 	}

// 	if err := s.validateVideoID(videoID); err != nil {
// 		return s.createErrorResult(err)
// 	}

// 	// 提醒用户图片评论较慢
// 	logger.Warn("图片评论使用浏览器自动化，可能需要30-60秒，请耐心等待...")

// 	accountName := s.getAccountName(args)

// 	// 获取带认证的浏览器页面，设置更长的超时时间
// 	page, cleanup, err := s.browserPool.GetWithAuth(accountName)
// 	if err != nil {
// 		return s.createErrorResult(err)
// 	}
// 	defer cleanup()

// 	// 创建评论服务
// 	commentService := comment.NewCommentService(page)

// 	// 发表图片评论（这个操作可能需要较长时间）
// 	if err := commentService.PostImageComment(ctx, videoID, content, imagePath); err != nil {
// 		return s.createErrorResult(err)
// 	}

// 	result := fmt.Sprintf("图片评论发表成功！\n视频: %s\n注意: 由于使用浏览器自动化，图片评论无法获取评论ID和链接", videoID)
// 	return s.createToolResult(result, false)
// }

// handleReplyComment 回复评论
func (s *Server) handleReplyComment(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	videoID, ok := args["video_id"].(string)
	if !ok || videoID == "" {
		return s.createToolResult("缺少video_id参数", true)
	}

	parentCommentID, ok := args["parent_comment_id"].(string)
	if !ok || parentCommentID == "" {
		return s.createToolResult("缺少parent_comment_id参数", true)
	}

	content, ok := args["content"].(string)
	if !ok || content == "" {
		return s.createToolResult("缺少content参数", true)
	}

	if err := s.validateVideoID(videoID); err != nil {
		return s.createErrorResult(err)
	}

	accountName := s.getAccountName(args)

	// 检查频率限制
	rateLimitKey := fmt.Sprintf("reply_comment_%s_%s", accountName, videoID)
	if err := checkRateLimit(rateLimitKey, 10*time.Second); err != nil {
		return s.createErrorResult(err)
	}

	// 获取带认证的浏览器页面（仅用于获取cookies）
	page, cleanup, err := s.browserPool.GetWithAuth(accountName)
	if err != nil {
		return s.createErrorResult(err)
	}
	defer cleanup()

	// 获取cookies并创建API客户端
	cookies, err := page.Context().Cookies()
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "获取cookies失败"))
	}

	cookieMap := make(map[string]string)
	for _, cookie := range cookies {
		cookieMap[cookie.Name] = cookie.Value
	}

	apiClient := api.NewClient(cookieMap)

	// 使用API回复评论
	replyResp, err := apiClient.ReplyComment(videoID, parentCommentID, content)
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "回复评论失败"))
	}

	if replyResp.Code != 0 {
		return s.createErrorResult(errors.Errorf("API返回错误: %s (code: %d)", replyResp.Message, replyResp.Code))
	}

	return s.createToolResult(fmt.Sprintf("回复评论成功 - 视频: %s, 回复ID: %s", videoID, replyResp.Data.RPID), false)
}

// 视频相关处理器

// handleGetVideoInfo 获取视频信息 - 使用API优先
func (s *Server) handleGetVideoInfo(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	videoID, ok := args["video_id"].(string)
	if !ok || videoID == "" {
		return s.createToolResult("缺少video_id参数", true)
	}

	if err := s.validateVideoID(videoID); err != nil {
		return s.createErrorResult(err)
	}

	// 创建API客户端（不需要登录cookies获取基本视频信息）
	apiClient := api.NewClient(map[string]string{})

	// 使用API获取视频信息
	videoInfo, err := apiClient.GetVideoInfo(videoID)
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "获取视频信息失败"))
	}

	if videoInfo.Code != 0 {
		return s.createErrorResult(errors.Errorf("API返回错误: %s (code: %d)", videoInfo.Message, videoInfo.Code))
	}

	// 格式化输出
	jsonData, err := json.MarshalIndent(videoInfo.Data, "", "  ")
	if err != nil {
		return s.createErrorResult(err)
	}

	return s.createToolResult(string(jsonData), false)
}

// handleDownloadMedia 下载媒体文件（音频、视频或合并文件）
func (s *Server) handleDownloadMedia(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	videoID, ok := args["video_id"].(string)
	if !ok || videoID == "" {
		return s.createErrorResult(errors.New("缺少必需的参数: video_id"))
	}

	// 获取媒体类型，默认为合并文件
	mediaTypeStr := "merged"
	if mt, ok := args["media_type"].(string); ok && mt != "" {
		mediaTypeStr = mt
	}

	var mediaType download.MediaType
	switch mediaTypeStr {
	case "audio":
		mediaType = download.MediaTypeAudio
	case "video":
		mediaType = download.MediaTypeVideo
	case "merged":
		mediaType = download.MediaTypeMerged
	default:
		return s.createErrorResult(errors.Errorf("不支持的媒体类型: %s，支持的类型: audio, video, merged", mediaTypeStr))
	}

	// 获取清晰度，默认为0（自动选择）
	quality := 0
	if q, ok := args["quality"]; ok {
		if qInt, ok := q.(float64); ok {
			quality = int(qInt)
		}
	}

	// 获取CID
	var cid int64
	if cidValue, ok := args["cid"]; ok {
		switch v := cidValue.(type) {
		case float64:
			cid = int64(v)
		case int:
			cid = int64(v)
		case int64:
			cid = v
		case string:
			parsed, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return s.createToolResult("cid参数格式错误", true)
			}
			cid = parsed
		}
	}

	// 获取输出目录
	outputDir := "./downloads"
	if dir, ok := args["output_dir"].(string); ok && dir != "" {
		outputDir = dir
	}

	accountName := s.getAccountName(args)

	// 获取带认证的浏览器页面（仅用于获取cookies）
	page, cleanup, err := s.browserPool.GetWithAuth(accountName)
	if err != nil {
		return s.createErrorResult(err)
	}
	defer cleanup()

	// 获取cookies并创建API客户端
	cookies, err := page.Context().Cookies()
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "获取cookies失败"))
	}

	cookieMap := make(map[string]string)
	for _, cookie := range cookies {
		cookieMap[cookie.Name] = cookie.Value
	}

	apiClient := api.NewClient(cookieMap)

	// 创建媒体下载服务
	mediaDownloadService := download.NewMediaDownloadService(apiClient, outputDir)

	// 设置下载选项
	opts := download.DownloadOptions{
		MediaType: mediaType,
		Quality:   quality,
		CID:       cid,
	}

	// 下载媒体
	result, err := mediaDownloadService.DownloadMedia(ctx, videoID, opts)
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "下载媒体失败"))
	}

	// 构建格式化的结果信息
	var message strings.Builder
	message.WriteString("🎉 媒体下载完成！\n\n")

	// 基本信息
	message.WriteString("1. 视频信息\n")
	message.WriteString(fmt.Sprintf("   • 标题: %s\n", result.Title))
	message.WriteString(fmt.Sprintf("   • 类型: %s\n", result.MediaType))
	message.WriteString(fmt.Sprintf("   • 时长: %d秒\n\n", result.Duration))

	// 当前下载清晰度信息
	message.WriteString("2. 当前下载清晰度\n")
	message.WriteString(fmt.Sprintf("   • 清晰度: %s", result.CurrentQuality.Description))
	if result.CurrentQuality.Width > 0 && result.CurrentQuality.Height > 0 {
		message.WriteString(fmt.Sprintf(" (%dx%d)", result.CurrentQuality.Width, result.CurrentQuality.Height))
	}
	if result.CurrentQuality.HasAudio {
		message.WriteString(" [包含音频]")
	} else {
		message.WriteString(" [纯视频，需合并音频]")
	}
	message.WriteString("\n\n")

	// 可用清晰度列表
	if len(result.AvailableQualities) > 0 {
		message.WriteString("3. 所有可用清晰度\n")
		for i, quality := range result.AvailableQualities {
			marker := "   "
			if quality.Quality == result.CurrentQuality.Quality {
				marker = " ✓ " // 标记当前选择的清晰度
			}

			message.WriteString(fmt.Sprintf("%s%s", marker, quality.Description))
			if quality.Width > 0 && quality.Height > 0 {
				message.WriteString(fmt.Sprintf(" (%dx%d)", quality.Width, quality.Height))
			}
			if quality.HasAudio {
				message.WriteString(" [完整视频]")
			} else {
				message.WriteString(" [需合并]")
			}
			message.WriteString("\n")

			// 限制显示数量，避免信息过多
			if i >= 9 {
				remaining := len(result.AvailableQualities) - i - 1
				if remaining > 0 {
					message.WriteString(fmt.Sprintf("   ... 还有 %d 个清晰度可选\n", remaining))
				}
				break
			}
		}
		message.WriteString("\n")
	}

	// 文件信息
	sectionNum := 4
	if len(result.AvailableQualities) == 0 {
		sectionNum = 3
	}
	message.WriteString(fmt.Sprintf("%d. 下载文件\n", sectionNum))
	fileCount := 1
	if result.MergedPath != "" {
		message.WriteString(fmt.Sprintf("   %d) 完整视频: %s (%.2f MB)\n",
			fileCount, filepath.Base(result.MergedPath), float64(result.MergedSize)/(1024*1024)))
		fileCount++
	}
	if result.AudioPath != "" && result.MergedPath == "" {
		message.WriteString(fmt.Sprintf("   %d) 音频文件: %s (%.2f MB)\n",
			fileCount, filepath.Base(result.AudioPath), float64(result.AudioSize)/(1024*1024)))
		fileCount++
	}
	if result.VideoPath != "" && result.MergedPath == "" {
		message.WriteString(fmt.Sprintf("   %d) 视频文件: %s (%.2f MB)\n",
			fileCount, filepath.Base(result.VideoPath), float64(result.VideoSize)/(1024*1024)))
		fileCount++
	}

	// 合并提示和高清视频建议
	sectionNum++
	needsSection := result.MergeRequired || (!result.CurrentQuality.HasAudio && result.MediaType == download.MediaTypeMerged)

	if needsSection {
		message.WriteString(fmt.Sprintf("\n%d. 重要提示\n", sectionNum))

		if result.MergeRequired && result.MergeCommand != "" {
			message.WriteString("   ⚠️  当前下载的视频为：纯视频 + 音频，需要手动合并\n")
			message.WriteString(fmt.Sprintf("   请执行：%s\n", result.MergeCommand))
		}

		// 如果下载的是纯视频，提示用户可以下载高清
		if !result.CurrentQuality.HasAudio && result.MediaType == download.MediaTypeMerged {
			message.WriteString("\n   💡 提示：如果需要更高清晰度的视频，可以指定 quality 参数\n")
			message.WriteString("   例如：quality=80 (1080P), quality=112 (1080P+), quality=120 (4K)\n")
			message.WriteString("   高清视频会自动下载音频并提供合并命令\n")
		}
	}

	// 其他提示
	if result.Notes != "" && !result.MergeRequired && !needsSection {
		message.WriteString(fmt.Sprintf("\n%d. 提示信息\n", sectionNum))
		message.WriteString(fmt.Sprintf("   📝 %s\n", result.Notes))
	}

	return s.createToolResult(message.String(), false)
}

// handleGetUserVideos 获取用户视频列表
func (s *Server) handleGetUserVideos(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	userID, ok := args["user_id"].(string)
	if !ok || userID == "" {
		return s.createErrorResult(errors.New("缺少必需的参数: user_id"))
	}

	// 检查频率限制 - 每个用户每20秒最多请求一次
	rateLimitKey := fmt.Sprintf("get_user_videos_%s", userID)
	if err := checkRateLimit(rateLimitKey, 20*time.Second); err != nil {
		return s.createErrorResult(err)
	}

	// 获取页码参数
	page := 1
	if p, ok := args["page"].(float64); ok {
		page = int(p)
	}
	if page < 1 {
		page = 1
	}

	// 获取每页数量参数
	pageSize := 20
	if ps, ok := args["page_size"].(float64); ok {
		pageSize = int(ps)
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 50 {
		pageSize = 50
	}

	logger.Infof("获取用户视频列表 - 用户ID: %s, 页码: %d, 每页数量: %d", userID, page, pageSize)

	// 创建API客户端（获取用户视频列表不需要登录）
	apiClient := api.NewClient(map[string]string{})

	// 获取用户视频列表
	userVideos, err := apiClient.GetUserVideos(userID, page, pageSize)
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "获取用户视频列表失败"))
	}

	if userVideos.Code != 0 {
		return s.createErrorResult(errors.Errorf("API返回错误: %s (code: %d)", userVideos.Message, userVideos.Code))
	}

	// 格式化输出
	result := map[string]interface{}{
		"user_id":     userID,
		"page":        userVideos.Data.Page.Pn,
		"page_size":   userVideos.Data.Page.Ps,
		"total_count": userVideos.Data.Page.Count,
		"videos":      userVideos.Data.List.Vlist,
		"categories":  userVideos.Data.List.Tlist,
	}

	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return s.createErrorResult(err)
	}

	return s.createToolResult(string(jsonData), false)
}

// handleLikeVideo 点赞视频 - 使用API优先
func (s *Server) handleLikeVideo(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	videoID, ok := args["video_id"].(string)
	if !ok || videoID == "" {
		return s.createErrorResult(errors.New("缺少必需的参数: video_id"))
	}

	if err := s.validateVideoID(videoID); err != nil {
		return s.createErrorResult(err)
	}

	// 获取点赞状态，默认为true（点赞）
	like := true
	if likeArg, ok := args["like"].(bool); ok {
		like = likeArg
	}

	accountName := s.getAccountName(args)
	logger.Infof("点赞视频 - 使用账号: '%s' (空表示默认账号)", accountName)

	// 检查频率限制
	rateLimitKey := fmt.Sprintf("like_video_%s_%s", accountName, videoID)
	if err := checkRateLimit(rateLimitKey, 5*time.Second); err != nil {
		return s.createErrorResult(err)
	}

	// 获取带认证的浏览器页面（仅用于获取cookies）
	page, cleanup, err := s.browserPool.GetWithAuth(accountName)
	if err != nil {
		logger.Errorf("获取浏览器页面失败: %v", err)
		return s.createErrorResult(err)
	}
	defer cleanup()

	// 获取cookies并创建API客户端 - 从多个域名获取完整cookie
	allCookies := make(map[string]string)

	// 获取所有相关域名的cookies
	domains := []string{
		"https://www.bilibili.com",
		"https://api.bilibili.com",
		"https://passport.bilibili.com",
		"https://space.bilibili.com",
	}

	for _, domain := range domains {
		cookies, err := page.Context().Cookies(domain)
		if err != nil {
			logger.Warnf("获取%s域名cookies失败: %v", domain, err)
			continue
		}

		for _, cookie := range cookies {
			allCookies[cookie.Name] = cookie.Value
		}
	}

	// 如果还是没有bili_jct，尝试获取所有cookies
	if _, exists := allCookies["bili_jct"]; !exists {
		logger.Warn("从指定域名未获取到bili_jct，尝试获取所有cookies")
		allPageCookies, err := page.Context().Cookies()
		if err == nil {
			for _, cookie := range allPageCookies {
				allCookies[cookie.Name] = cookie.Value
			}
		}
	}

	// 调试：检查bili_jct是否存在
	logger.Infof("调试cookie信息: 总数=%d", len(allCookies))
	if biliJct, exists := allCookies["bili_jct"]; exists {
		logger.Infof("bili_jct存在: %s", biliJct[:8]+"...")
	} else {
		logger.Warnf("bili_jct不存在，可用的cookies: %v", func() []string {
			var names []string
			for name := range allCookies {
				names = append(names, name)
			}
			return names
		}())

		// 如果没有bili_jct，返回错误并提示重新登录
		return s.createErrorResult(errors.New("缺少CSRF token (bili_jct)，请重新登录账号"))
	}

	apiClient := api.NewClient(allCookies)

	// 使用API点赞视频
	action := 1
	if !like {
		action = 2 // 取消点赞
	}

	likeResp, err := apiClient.LikeVideo(videoID, action)
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "点赞视频失败"))
	}

	if likeResp.Code != 0 {
		return s.createErrorResult(errors.Errorf("API返回错误: %s (code: %d)", likeResp.Message, likeResp.Code))
	}

	actionText := "点赞"
	if !like {
		actionText = "取消点赞"
	}

	return s.createToolResult(fmt.Sprintf("%s成功 - 视频: %s", actionText, videoID), false)
}

// handleCoinVideo 投币视频
func (s *Server) handleCoinVideo(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	videoID, ok := args["video_id"].(string)
	if !ok || videoID == "" {
		return s.createToolResult("缺少video_id参数", true)
	}

	if err := s.validateVideoID(videoID); err != nil {
		return s.createErrorResult(err)
	}

	coinCount := 1
	if count, ok := args["coin_count"].(float64); ok {
		coinCount = int(count)
		if coinCount < 1 || coinCount > 2 {
			coinCount = 1
		}
	}

	// 是否同时点赞
	alsoLike := false
	if like, ok := args["also_like"].(bool); ok {
		alsoLike = like
	}

	accountName := s.getAccountName(args)

	// 检查频率限制
	rateLimitKey := fmt.Sprintf("coin_video_%s_%s", accountName, videoID)
	if err := checkRateLimit(rateLimitKey, 10*time.Second); err != nil {
		return s.createErrorResult(err)
	}

	// 获取带认证的浏览器页面（仅用于获取cookies）
	page, cleanup, err := s.browserPool.GetWithAuth(accountName)
	if err != nil {
		return s.createErrorResult(err)
	}
	defer cleanup()

	// 获取cookies并创建API客户端
	cookies, err := page.Context().Cookies()
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "获取cookies失败"))
	}

	cookieMap := make(map[string]string)
	for _, cookie := range cookies {
		cookieMap[cookie.Name] = cookie.Value
	}

	apiClient := api.NewClient(cookieMap)

	// 使用API投币视频
	coinResp, err := apiClient.CoinVideo(videoID, coinCount, alsoLike)
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "投币视频失败"))
	}

	if coinResp.Code != 0 {
		return s.createErrorResult(errors.Errorf("API返回错误: %s (code: %d)", coinResp.Message, coinResp.Code))
	}

	resultMsg := fmt.Sprintf("投币成功 - 视频: %s, 数量: %d", videoID, coinCount)
	if alsoLike && coinResp.Data.Like {
		resultMsg += " (同时点赞)"
	}

	return s.createToolResult(resultMsg, false)
}

// handleFavoriteVideo 收藏视频
func (s *Server) handleFavoriteVideo(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	videoID, ok := args["video_id"].(string)
	if !ok || videoID == "" {
		return s.createToolResult("缺少video_id参数", true)
	}

	if err := s.validateVideoID(videoID); err != nil {
		return s.createErrorResult(err)
	}

	folderID := ""
	if id, ok := args["folder_id"].(string); ok {
		folderID = id
	}

	accountName := s.getAccountName(args)

	// 检查频率限制
	rateLimitKey := fmt.Sprintf("favorite_video_%s_%s", accountName, videoID)
	if err := checkRateLimit(rateLimitKey, 10*time.Second); err != nil {
		return s.createErrorResult(err)
	}

	// 获取带认证的浏览器页面（仅用于获取cookies）
	page, cleanup, err := s.browserPool.GetWithAuth(accountName)
	if err != nil {
		return s.createErrorResult(err)
	}
	defer cleanup()

	// 获取cookies并创建API客户端
	cookies, err := page.Context().Cookies()
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "获取cookies失败"))
	}

	cookieMap := make(map[string]string)
	for _, cookie := range cookies {
		cookieMap[cookie.Name] = cookie.Value
	}

	apiClient := api.NewClient(cookieMap)

	// 使用API收藏视频
	folderIDs := []string{}
	if folderID != "" {
		folderIDs = []string{folderID}
	}

	favResp, err := apiClient.FavoriteVideo(videoID, folderIDs, true)
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "收藏视频失败"))
	}

	if favResp.Code != 0 {
		return s.createErrorResult(errors.Errorf("API返回错误: %s (code: %d)", favResp.Message, favResp.Code))
	}

	return s.createToolResult(fmt.Sprintf("收藏成功 - 视频: %s", videoID), false)
}

// 用户相关处理器

// handleFollowUser 关注用户
func (s *Server) handleFollowUser(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	userID, ok := args["user_id"].(string)
	if !ok || userID == "" {
		return s.createToolResult("缺少user_id参数", true)
	}

	accountName := s.getAccountName(args)

	// 检查频率限制
	rateLimitKey := fmt.Sprintf("follow_user_%s_%s", accountName, userID)
	if err := checkRateLimit(rateLimitKey, 10*time.Second); err != nil {
		return s.createErrorResult(err)
	}

	// 获取带认证的浏览器页面（仅用于获取cookies）
	page, cleanup, err := s.browserPool.GetWithAuth(accountName)
	if err != nil {
		return s.createErrorResult(err)
	}
	defer cleanup()

	// 获取cookies并创建API客户端
	cookies, err := page.Context().Cookies()
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "获取cookies失败"))
	}

	cookieMap := make(map[string]string)
	for _, cookie := range cookies {
		cookieMap[cookie.Name] = cookie.Value
	}

	apiClient := api.NewClient(cookieMap)

	// 使用API关注用户 (1:关注 2:取消关注)
	followResp, err := apiClient.FollowUser(userID, 1)
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "关注用户失败"))
	}

	if followResp.Code != 0 {
		return s.createErrorResult(errors.Errorf("API返回错误: %s (code: %d)", followResp.Message, followResp.Code))
	}

	return s.createToolResult(fmt.Sprintf("关注成功 - 用户: %s", userID), false)
}

// 统计与视频上传相关处理器

// handleGetUserStats 获取关注/粉丝统计
func (s *Server) handleGetUserStats(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	accountName := s.getAccountName(args)

	page, cleanup, err := s.browserPool.GetWithAuth(accountName)
	if err != nil {
		return s.createErrorResult(err)
	}
	defer cleanup()

	cookies, err := page.Context().Cookies()
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "获取cookies失败"))
	}

	cookieMap := make(map[string]string)
	for _, cookie := range cookies {
		cookieMap[cookie.Name] = cookie.Value
	}

	apiClient := api.NewClient(cookieMap)
	statsService := userstats.NewService(apiClient)

	var targetUserID *int64
	if rawUserID, exists := args["user_id"]; exists {
		parsedUserID, parseErr := parseInt64Arg(rawUserID, "user_id")
		if parseErr != nil {
			return s.createToolResult(parseErr.Error(), true)
		}
		if parsedUserID <= 0 {
			return s.createToolResult("user_id参数必须大于0", true)
		}
		targetUserID = &parsedUserID
	}

	stats, err := statsService.GetRelationStat(ctx, targetUserID)
	if err != nil {
		return s.createErrorResult(err)
	}

	resultJSON, err := json.Marshal(map[string]interface{}{
		"user_id":         stats.Mid,
		"following_count": stats.Following,
		"follower_count":  stats.Follower,
	})
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "序列化结果失败"))
	}

	return s.createToolResult(string(resultJSON), false)
}

// handleUploadVideoDraft 上传视频并创建草稿
func (s *Server) handleUploadVideoDraft(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	videoPath, ok := args["video_path"].(string)
	if !ok || strings.TrimSpace(videoPath) == "" {
		return s.createToolResult("缺少video_path参数", true)
	}

	rawTid, exists := args["tid"]
	if !exists {
		return s.createToolResult("缺少tid参数", true)
	}
	tid, err := parseIntArg(rawTid, "tid")
	if err != nil {
		return s.createToolResult(err.Error(), true)
	}
	if tid <= 0 {
		return s.createToolResult("tid参数必须大于0", true)
	}

	title, ok := args["title"].(string)
	if !ok || strings.TrimSpace(title) == "" {
		return s.createToolResult("缺少title参数", true)
	}

	accountName := s.getAccountName(args)

	page, cleanup, err := s.browserPool.GetWithAuth(accountName)
	if err != nil {
		return s.createErrorResult(err)
	}
	defer cleanup()

	cookies, err := page.Context().Cookies()
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "获取cookies失败"))
	}

	cookieMap := make(map[string]string)
	for _, cookie := range cookies {
		cookieMap[cookie.Name] = cookie.Value
	}

	apiClient := api.NewClient(cookieMap)
	uploadService := upload.NewService(apiClient)

	draftInfo, err := uploadService.CreateVideoDraft(ctx, strings.TrimSpace(videoPath), tid, strings.TrimSpace(title))
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "创建视频草稿失败"))
	}

	resultJSON, err := json.Marshal(map[string]interface{}{
		"draft_token": draftInfo.DraftToken,
		"cid":         draftInfo.CID,
	})
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "序列化结果失败"))
	}

	return s.createToolResult(string(resultJSON), false)
}

// handlePublishVideo 发布视频草稿
func (s *Server) handlePublishVideo(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	draftToken, ok := args["draft_token"].(string)
	if !ok || strings.TrimSpace(draftToken) == "" {
		return s.createToolResult("缺少draft_token参数", true)
	}

	rawCopyright, exists := args["copyright"]
	if !exists {
		return s.createToolResult("缺少copyright参数", true)
	}
	copyright, err := parseIntArg(rawCopyright, "copyright")
	if err != nil {
		return s.createToolResult(err.Error(), true)
	}
	if copyright <= 0 {
		return s.createToolResult("copyright参数必须大于0", true)
	}

	rawTid, exists := args["tid"]
	if !exists {
		return s.createToolResult("缺少tid参数", true)
	}
	tid, err := parseIntArg(rawTid, "tid")
	if err != nil {
		return s.createToolResult(err.Error(), true)
	}
	if tid <= 0 {
		return s.createToolResult("tid参数必须大于0", true)
	}

	title, ok := args["title"].(string)
	if !ok || strings.TrimSpace(title) == "" {
		return s.createToolResult("缺少title参数", true)
	}

	tag, ok := args["tag"].(string)
	if !ok || strings.TrimSpace(tag) == "" {
		return s.createToolResult("缺少tag参数", true)
	}

	desc := ""
	if rawDesc, exists := args["desc"]; exists {
		if typedDesc, typeOK := rawDesc.(string); !typeOK {
			return s.createToolResult("desc参数类型错误，应为字符串", true)
		} else {
			desc = strings.TrimSpace(typedDesc)
		}
	}

	if copyright == 2 {
		return s.createToolResult("当前工具未提供source参数，暂不支持copyright=2（转载）", true)
	}

	accountName := s.getAccountName(args)

	page, cleanup, err := s.browserPool.GetWithAuth(accountName)
	if err != nil {
		return s.createErrorResult(err)
	}
	defer cleanup()

	cookies, err := page.Context().Cookies()
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "获取cookies失败"))
	}

	cookieMap := make(map[string]string)
	for _, cookie := range cookies {
		cookieMap[cookie.Name] = cookie.Value
	}

	apiClient := api.NewClient(cookieMap)
	uploadService := upload.NewService(apiClient)

	bvid, err := uploadService.PublishVideo(ctx, strings.TrimSpace(draftToken), upload.VideoPublishInfo{
		Copyright: copyright,
		Tid:       tid,
		Title:     strings.TrimSpace(title),
		Tag:       strings.TrimSpace(tag),
		Desc:      desc,
	})
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "发布视频失败"))
	}

	videoURL := fmt.Sprintf("https://www.bilibili.com/video/%s", bvid)
	resultJSON, err := json.Marshal(map[string]interface{}{
		"bvid":      bvid,
		"video_url": videoURL,
	})
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "序列化结果失败"))
	}

	return s.createToolResult(string(resultJSON), false)
}

// handleUploadVideo 一键上传视频（上传+发布）
func (s *Server) handleUploadVideo(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	videoPath, ok := args["video_path"].(string)
	if !ok || strings.TrimSpace(videoPath) == "" {
		return s.createToolResult("缺少video_path参数", true)
	}

	rawCopyright, exists := args["copyright"]
	if !exists {
		return s.createToolResult("缺少copyright参数", true)
	}
	copyright, err := parseIntArg(rawCopyright, "copyright")
	if err != nil {
		return s.createToolResult(err.Error(), true)
	}
	if copyright <= 0 {
		return s.createToolResult("copyright参数必须大于0", true)
	}

	rawTid, exists := args["tid"]
	if !exists {
		return s.createToolResult("缺少tid参数", true)
	}
	tid, err := parseIntArg(rawTid, "tid")
	if err != nil {
		return s.createToolResult(err.Error(), true)
	}
	if tid <= 0 {
		return s.createToolResult("tid参数必须大于0", true)
	}

	title, ok := args["title"].(string)
	if !ok || strings.TrimSpace(title) == "" {
		return s.createToolResult("缺少title参数", true)
	}

	tag, ok := args["tag"].(string)
	if !ok || strings.TrimSpace(tag) == "" {
		return s.createToolResult("缺少tag参数", true)
	}

	desc := ""
	if rawDesc, exists := args["desc"]; exists {
		if typedDesc, typeOK := rawDesc.(string); !typeOK {
			return s.createToolResult("desc参数类型错误，应为字符串", true)
		} else {
			desc = strings.TrimSpace(typedDesc)
		}
	}

	if copyright == 2 {
		return s.createToolResult("当前工具未提供source参数，暂不支持copyright=2（转载）", true)
	}

	accountName := s.getAccountName(args)

	page, cleanup, err := s.browserPool.GetWithAuth(accountName)
	if err != nil {
		return s.createErrorResult(err)
	}
	defer cleanup()

	cookies, err := page.Context().Cookies()
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "获取cookies失败"))
	}

	cookieMap := make(map[string]string)
	for _, cookie := range cookies {
		cookieMap[cookie.Name] = cookie.Value
	}

	apiClient := api.NewClient(cookieMap)
	uploadService := upload.NewService(apiClient)

	draftInfo, err := uploadService.CreateVideoDraft(ctx, strings.TrimSpace(videoPath), tid, strings.TrimSpace(title))
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "创建视频草稿失败"))
	}

	bvid, err := uploadService.PublishVideo(ctx, draftInfo.DraftToken, upload.VideoPublishInfo{
		Copyright: copyright,
		Tid:       tid,
		Title:     strings.TrimSpace(title),
		Tag:       strings.TrimSpace(tag),
		Desc:      desc,
	})
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "发布视频失败"))
	}

	videoURL := fmt.Sprintf("https://www.bilibili.com/video/%s", bvid)
	resultJSON, err := json.Marshal(map[string]interface{}{
		"bvid":      bvid,
		"video_url": videoURL,
	})
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "序列化结果失败"))
	}

	return s.createToolResult(string(resultJSON), false)
}

// handleCheckVideoUploadStatus 查询上传状态（预留）
func (s *Server) handleCheckVideoUploadStatus(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	_ = ctx

	draftToken, ok := args["draft_token"].(string)
	if !ok || strings.TrimSpace(draftToken) == "" {
		return s.createToolResult("缺少draft_token参数", true)
	}

	resultJSON, err := json.Marshal(map[string]interface{}{
		"status":  "pending",
		"message": "not implemented",
	})
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "序列化结果失败"))
	}

	return s.createToolResult(string(resultJSON), false)
}

func parseIntArg(rawValue interface{}, fieldName string) (int, error) {
	switch value := rawValue.(type) {
	case float64:
		return int(value), nil
	case int:
		return value, nil
	case int64:
		return int(value), nil
	case json.Number:
		parsed, err := value.Int64()
		if err != nil {
			return 0, errors.Errorf("%s参数格式错误", fieldName)
		}
		return int(parsed), nil
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			return 0, errors.Errorf("%s参数格式错误", fieldName)
		}
		return int(parsed), nil
	default:
		return 0, errors.Errorf("%s参数类型错误，应为整数", fieldName)
	}
}

func parseInt64Arg(rawValue interface{}, fieldName string) (int64, error) {
	switch value := rawValue.(type) {
	case float64:
		return int64(value), nil
	case int:
		return int64(value), nil
	case int64:
		return value, nil
	case json.Number:
		parsed, err := value.Int64()
		if err != nil {
			return 0, errors.Errorf("%s参数格式错误", fieldName)
		}
		return parsed, nil
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			return 0, errors.Errorf("%s参数格式错误", fieldName)
		}
		return parsed, nil
	default:
		return 0, errors.Errorf("%s参数类型错误，应为整数", fieldName)
	}
}

// 专栏相关处理器

// handleUploadColumnDraft 创建专栏草稿
func (s *Server) handleUploadColumnDraft(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	title, ok := args["title"].(string)
	if !ok || title == "" {
		return s.createToolResult("缺少title参数", true)
	}

	content, ok := args["content"].(string)
	if !ok || content == "" {
		return s.createToolResult("缺少content参数", true)
	}

	categoryID := 0
	if rawCategoryID, exists := args["category_id"]; exists {
		switch value := rawCategoryID.(type) {
		case float64:
			categoryID = int(value)
		case int:
			categoryID = value
		case int64:
			categoryID = int(value)
		default:
			return s.createToolResult("category_id参数类型错误，应为整数", true)
		}
	}

	accountName := s.getAccountName(args)

	page, cleanup, err := s.browserPool.GetWithAuth(accountName)
	if err != nil {
		return s.createErrorResult(err)
	}
	defer cleanup()

	cookies, err := page.Context().Cookies()
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "获取cookies失败"))
	}

	cookieMap := make(map[string]string)
	for _, cookie := range cookies {
		cookieMap[cookie.Name] = cookie.Value
	}

	apiClient := api.NewClient(cookieMap)
	articleService := article.NewService(apiClient)

	draftID, err := articleService.CreateDraft(title, content, categoryID)
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "创建专栏草稿失败"))
	}

	resultJSON, err := json.Marshal(map[string]interface{}{
		"draft_id": draftID,
	})
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "序列化结果失败"))
	}

	return s.createToolResult(string(resultJSON), false)
}

// handlePublishColumn 发布专栏草稿
func (s *Server) handlePublishColumn(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	rawDraftID, exists := args["draft_id"]
	if !exists {
		return s.createToolResult("缺少draft_id参数", true)
	}

	var draftID int64
	switch value := rawDraftID.(type) {
	case float64:
		draftID = int64(value)
	case int:
		draftID = int64(value)
	case int64:
		draftID = value
	case string:
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return s.createToolResult("draft_id参数格式错误", true)
		}
		draftID = parsed
	default:
		return s.createToolResult("draft_id参数类型错误，应为整数", true)
	}

	if draftID <= 0 {
		return s.createToolResult("draft_id参数必须大于0", true)
	}

	accountName := s.getAccountName(args)

	page, cleanup, err := s.browserPool.GetWithAuth(accountName)
	if err != nil {
		return s.createErrorResult(err)
	}
	defer cleanup()

	cookies, err := page.Context().Cookies()
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "获取cookies失败"))
	}

	cookieMap := make(map[string]string)
	for _, cookie := range cookies {
		cookieMap[cookie.Name] = cookie.Value
	}

	apiClient := api.NewClient(cookieMap)
	articleService := article.NewService(apiClient)

	articleID, err := articleService.PublishDraft(draftID)
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "发布专栏草稿失败"))
	}

	articleURL := fmt.Sprintf("https://www.bilibili.com/read/cv%d", articleID)
	resultJSON, err := json.Marshal(map[string]interface{}{
		"article_id":  articleID,
		"article_url": articleURL,
	})
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "序列化结果失败"))
	}

	return s.createToolResult(string(resultJSON), false)
}

// handleUploadColumn 一键上传专栏（创建草稿并发布）
func (s *Server) handleUploadColumn(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	title, ok := args["title"].(string)
	if !ok || title == "" {
		return s.createToolResult("缺少title参数", true)
	}

	content, ok := args["content"].(string)
	if !ok || content == "" {
		return s.createToolResult("缺少content参数", true)
	}

	categoryID := 0
	if rawCategoryID, exists := args["category_id"]; exists {
		switch value := rawCategoryID.(type) {
		case float64:
			categoryID = int(value)
		case int:
			categoryID = value
		case int64:
			categoryID = int(value)
		default:
			return s.createToolResult("category_id参数类型错误，应为整数", true)
		}
	}

	accountName := s.getAccountName(args)

	page, cleanup, err := s.browserPool.GetWithAuth(accountName)
	if err != nil {
		return s.createErrorResult(err)
	}
	defer cleanup()

	cookies, err := page.Context().Cookies()
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "获取cookies失败"))
	}

	cookieMap := make(map[string]string)
	for _, cookie := range cookies {
		cookieMap[cookie.Name] = cookie.Value
	}

	apiClient := api.NewClient(cookieMap)
	articleService := article.NewService(apiClient)

	draftID, err := articleService.CreateDraft(title, content, categoryID)
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "创建专栏草稿失败"))
	}

	articleID, err := articleService.PublishDraft(draftID)
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "发布专栏草稿失败"))
	}

	articleURL := fmt.Sprintf("https://www.bilibili.com/read/cv%d", articleID)
	resultJSON, err := json.Marshal(map[string]interface{}{
		"article_id":  articleID,
		"article_url": articleURL,
	})
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "序列化结果失败"))
	}

	return s.createToolResult(string(resultJSON), false)
}

// 可选功能处理器

// handleWhisperAudio2Text 使用Whisper.cpp转录音频
func (s *Server) handleWhisperAudio2Text(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	audioPath, ok := args["audio_path"].(string)
	if !ok || audioPath == "" {
		return s.createToolResult("缺少audio_path参数", true)
	}

	// 检查Whisper是否启用
	if !s.config.Features.Whisper.Enabled {
		return s.createToolResult("Whisper功能未启用，请先运行 ./bilibili-whisper-init 进行初始化", true)
	}

	// 获取语言参数
	language := s.config.Features.Whisper.Language
	if lang, ok := args["language"].(string); ok && lang != "" {
		language = lang
	}

	// 获取模型参数（可选）
	requestedModel := s.config.Features.Whisper.DefaultModel
	if m, ok := args["model"].(string); ok && m != "" {
		requestedModel = m
	}

	logger.Infof("开始音频转录 - 文件: %s, 语言: %s, 请求模型: %s", audioPath, language, requestedModel)

	// 创建Whisper服务
	whisperService, err := s.getOrCreateWhisperService()
	if err != nil {
		return s.createErrorResult(err)
	}

	// 如果用户指定了不同的参数，需要创建临时配置
	// 但由于whisper服务现在使用完整配置，我们需要在服务层面处理这些参数
	// 这里暂时保持原有逻辑，在后续优化中可以改进

	// 执行转录
	result, err := whisperService.TranscribeAudio(ctx, audioPath)
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "音频转录失败"))
	}

	// 构建结果消息
	var message strings.Builder
	message.WriteString("🎤 音频转录完成！\n\n")

	message.WriteString("📁 文件信息\n")
	message.WriteString(fmt.Sprintf("   • 音频文件: %s\n", filepath.Base(result.AudioPath)))
	message.WriteString(fmt.Sprintf("   • SRT文件: %s\n", filepath.Base(result.OutputPath)))
	message.WriteString(fmt.Sprintf("   • 处理时间: %.2f秒\n\n", result.ProcessTime))

	message.WriteString("⚙️ 转录配置\n")
	message.WriteString(fmt.Sprintf("   • 模型: %s\n", result.Model))
	message.WriteString(fmt.Sprintf("   • 语言: %s\n", result.Language))
	message.WriteString(fmt.Sprintf("   • 加速类型: %s\n", result.AccelerationType))
	message.WriteString(fmt.Sprintf("   • 创建时间: %s\n\n", result.CreatedAt.Format("2006-01-02 15:04:05")))

	message.WriteString("📝 转录文本\n")
	message.WriteString("=" + strings.Repeat("=", 50) + "\n")
	message.WriteString(result.Text)
	message.WriteString("\n" + strings.Repeat("=", 51) + "\n")

	// 转换为绝对路径
	absOutputPath, err := filepath.Abs(result.OutputPath)
	if err != nil {
		absOutputPath = result.OutputPath // 如果转换失败，使用原路径
	}
	message.WriteString(fmt.Sprintf("\n💾 详细的时间轴信息已保存到: %s", absOutputPath))

	// 添加可用模型信息
	if len(result.AvailableModels) > 0 {
		message.WriteString("\n\n📚 当前可用模型\n")
		for i, model := range result.AvailableModels {
			marker := "   "
			if model.Name == result.Model {
				marker = " ✅ " // 标记当前使用的模型
			}

			// 格式化文件大小
			sizeStr := formatFileSize(model.Size)

			message.WriteString(fmt.Sprintf("%s%s - %s", marker, model.Name, model.Description))
			if model.IsCoreMl {
				message.WriteString(" 🚀")
			}
			message.WriteString(fmt.Sprintf(" [%s]\n", sizeStr))

			// 限制显示数量，避免信息过多
			if i >= 9 {
				remaining := len(result.AvailableModels) - i - 1
				if remaining > 0 {
					message.WriteString(fmt.Sprintf("   ... 还有 %d 个模型可用\n", remaining))
				}
				break
			}
		}
	}

	return s.createToolResult(message.String(), false)
}

// formatFileSize 格式化文件大小
func formatFileSize(size int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case size >= GB:
		return fmt.Sprintf("%.1fGB", float64(size)/GB)
	case size >= MB:
		return fmt.Sprintf("%.1fMB", float64(size)/MB)
	case size >= KB:
		return fmt.Sprintf("%.1fKB", float64(size)/KB)
	default:
		return fmt.Sprintf("%dB", size)
	}
}

// handleGetVideoStream 获取视频流地址
func (s *Server) handleGetVideoStream(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	videoID, ok := args["video_id"].(string)
	if !ok || videoID == "" {
		return s.createToolResult("缺少video_id参数", true)
	}

	// CID现在是可选参数，如果没有提供就自动获取
	var cid int64
	if cidValue, ok := args["cid"]; ok {
		switch v := cidValue.(type) {
		case float64:
			cid = int64(v)
		case int:
			cid = int64(v)
		case int64:
			cid = v
		case string:
			parsed, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return s.createToolResult("cid参数格式错误", true)
			}
			cid = parsed
		default:
			return s.createToolResult("cid参数类型错误", true)
		}

		// 验证CID不能为0
		if cid <= 0 {
			return s.createToolResult("CID参数不能为0", true)
		}
	}

	// 可选参数
	quality := 64 // 默认720P
	if q, ok := args["quality"]; ok {
		if qInt, ok := q.(float64); ok {
			quality = int(qInt)
		}
	}

	fnval := 16 // 默认DASH格式
	if f, ok := args["fnval"]; ok {
		if fInt, ok := f.(float64); ok {
			fnval = int(fInt)
		}
	}

	platform := ""
	if p, ok := args["platform"].(string); ok {
		platform = p
	}

	accountName := s.getAccountName(args)

	// 获取带认证的浏览器页面（用于获取cookies）
	page, cleanup, err := s.browserPool.GetWithAuth(accountName)
	if err != nil {
		return s.createErrorResult(err)
	}
	defer cleanup()

	// 从playwright页面获取cookies
	cookies, err := page.Context().Cookies()
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "获取cookies失败"))
	}

	// 转换为map格式
	cookieMap := make(map[string]string)
	for _, cookie := range cookies {
		cookieMap[cookie.Name] = cookie.Value
	}

	// 创建API客户端
	client := api.NewClient(cookieMap)

	// 如果没有提供CID，自动获取视频信息来获取CID
	if cid == 0 {
		videoInfo, err := client.GetVideoInfo(videoID)
		if err != nil {
			return s.createToolResult(fmt.Sprintf("获取视频信息失败: %v", err), true)
		}

		if videoInfo.Code != 0 {
			return s.createToolResult(fmt.Sprintf("获取视频信息失败: %s (code: %d)", videoInfo.Message, videoInfo.Code), true)
		}

		if len(videoInfo.Data.Pages) == 0 {
			return s.createToolResult("该视频没有可用的分P", true)
		}

		// 使用第一个分P的CID
		cid = videoInfo.Data.Pages[0].Cid
		logger.Infof("自动获取到CID: %d", cid)
	}

	logger.Infof("获取视频流 - 视频ID: %s, CID: %d, 清晰度: %d, 格式: %d, 平台: %s, 账号: %s",
		videoID, cid, quality, fnval, platform, accountName)

	// 调用API获取视频流
	streamResp, err := client.GetVideoStream(videoID, cid, quality, fnval, platform)
	if err != nil {
		return s.createToolResult(fmt.Sprintf("获取视频流失败: %v", err), true)
	}

	// 构建简化的播放地址结果
	result := map[string]interface{}{
		"video_id":   videoID,
		"cid":        cid,
		"quality":    streamResp.Data.Quality,
		"duration":   streamResp.Data.TimeLength / 1000, // 转换为秒
		"usage_note": "注意：播放地址需要正确的Referer和User-Agent才能访问",
	}

	// 提取播放地址
	playUrls := make(map[string]interface{})

	// DASH格式的音视频流
	if streamResp.Data.DASH != nil {
		// 视频流地址
		if len(streamResp.Data.DASH.Video) > 0 {
			videoStreams := make([]map[string]interface{}, 0)
			for _, video := range streamResp.Data.DASH.Video {
				videoStreams = append(videoStreams, map[string]interface{}{
					"quality":    getQualityDescription(video.ID),
					"resolution": fmt.Sprintf("%dx%d", video.Width, video.Height),
					"url":        video.BaseURL,
					"codecs":     video.Codecs,
					"bandwidth":  video.Bandwidth,
				})
			}
			playUrls["video_streams"] = videoStreams
		}

		// 音频流地址
		if len(streamResp.Data.DASH.Audio) > 0 {
			audioStreams := make([]map[string]interface{}, 0)
			for _, audio := range streamResp.Data.DASH.Audio {
				audioStreams = append(audioStreams, map[string]interface{}{
					"quality":   fmt.Sprintf("ID_%d", audio.ID),
					"url":       audio.BaseURL,
					"codecs":    audio.Codecs,
					"bandwidth": audio.Bandwidth,
				})
			}
			playUrls["audio_streams"] = audioStreams
		}

		// 推荐的最佳流
		if len(streamResp.Data.DASH.Video) > 0 && len(streamResp.Data.DASH.Audio) > 0 {
			// 选择最佳视频流（通常是第一个）
			bestVideo := streamResp.Data.DASH.Video[0]
			for _, video := range streamResp.Data.DASH.Video {
				if video.ID == streamResp.Data.Quality {
					bestVideo = video
					break
				}
			}

			// 选择最佳音频流（带宽最高）
			bestAudio := streamResp.Data.DASH.Audio[0]
			for _, audio := range streamResp.Data.DASH.Audio {
				if audio.Bandwidth > bestAudio.Bandwidth {
					bestAudio = audio
				}
			}

			playUrls["recommended"] = map[string]interface{}{
				"video_url": bestVideo.BaseURL,
				"audio_url": bestAudio.BaseURL,
				"note":      "DASH格式需要分别下载音视频后用ffmpeg合并",
			}
		}
	}

	// MP4格式的合并流
	if len(streamResp.Data.DURL) > 0 {
		mp4Streams := make([]map[string]interface{}, 0)
		for i, segment := range streamResp.Data.DURL {
			mp4Streams = append(mp4Streams, map[string]interface{}{
				"segment": i + 1,
				"url":     segment.URL,
				"size":    segment.Size,
				"length":  segment.Length,
			})
		}
		playUrls["mp4_streams"] = mp4Streams

		// 推荐的合并流（第一个分段）
		if len(streamResp.Data.DURL) > 0 {
			playUrls["recommended"] = map[string]interface{}{
				"merged_url": streamResp.Data.DURL[0].URL,
				"note":       "MP4格式已合并音视频，可直接播放",
			}
		}
	}

	result["play_urls"] = playUrls

	// 添加使用示例
	refererURL := fmt.Sprintf("https://www.bilibili.com/video/%s", videoID)
	result["usage_examples"] = map[string]interface{}{
		"curl_download": fmt.Sprintf(`curl "播放地址" -H "Referer: %s" -H "User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36" -o video.mp4`, refererURL),
		"ffmpeg_play":   fmt.Sprintf(`ffmpeg -user_agent "Mozilla/5.0..." -referer "%s" -i "播放地址" -c copy output.mp4`, refererURL),
	}

	// 将结果转换为JSON字符串
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return s.createErrorResult(errors.Wrap(err, "序列化结果失败"))
	}

	return s.createToolResult(string(resultJSON), false)
}

// getQualityDescription 获取清晰度描述
func getQualityDescription(quality int) string {
	qualityMap := map[int]string{
		16:  "360P",
		32:  "480P",
		64:  "720P",
		74:  "720P60",
		80:  "1080P",
		112: "1080P+",
		116: "1080P60",
		120: "4K",
		125: "HDR",
		126: "杜比视界",
		127: "8K",
	}

	if desc, exists := qualityMap[quality]; exists {
		return desc
	}
	return fmt.Sprintf("Q%d", quality)
}
