package upload

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/shirenchuang/bilibili-mcp/internal/bilibili/api"
)

const (
	videoChunkSize   = 4 * 1024 * 1024
	videoTokenSchema = 1
)

// Service 视频上传服务。
type Service struct {
	apiClient *api.Client
}

// NewService 创建视频上传服务。
func NewService(apiClient *api.Client) *Service {
	return &Service{apiClient: apiClient}
}

// VideoDraftToken 视频草稿 token。
type VideoDraftToken struct {
	Version  int    `json:"version"`
	CID      int64  `json:"cid"`
	FilePath string `json:"file_path"`
	FileSize int64  `json:"file_size"`
	FileMD5  string `json:"file_md5"`
	FileName string `json:"file_name"`
}

// VideoDraftInfo 视频草稿信息。
type VideoDraftInfo struct {
	CID        int64  `json:"cid"`
	DraftToken string `json:"draft_token"`
}

// VideoPublishInfo 视频投稿参数。
type VideoPublishInfo struct {
	Copyright int    `json:"copyright"`
	Source    string `json:"source,omitempty"`
	Tid       int    `json:"tid"`
	Title     string `json:"title"`
	Tag       string `json:"tag"`
	Desc      string `json:"desc,omitempty"`
}

type preuploadResponse struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	UposURI   string `json:"upos_uri"`
	Auth      string `json:"auth"`
	BizID     int64  `json:"biz_id"`
	ChunkSize int    `json:"chunk_size"`
	Endpoint  string `json:"endpoint"`
}

type uploadedPart struct {
	PartNumber int    `json:"partNumber"`
	ETag       string `json:"eTag,omitempty"`
}

// CreateVideoDraft 创建视频草稿：预上传 + 顺序分片上传 + 完成确认。
func (s *Service) CreateVideoDraft(ctx context.Context, videoPath string, tid int, title string) (*VideoDraftInfo, error) {
	if s == nil || s.apiClient == nil {
		return nil, errors.New("api client 未初始化")
	}

	fileInfo, err := os.Stat(videoPath)
	if err != nil {
		return nil, errors.Wrap(err, "读取视频文件信息失败")
	}
	if fileInfo.IsDir() {
		return nil, errors.New("videoPath 不能是目录")
	}

	fileMD5, err := calculateFileMD5(videoPath)
	if err != nil {
		return nil, errors.Wrap(err, "计算视频文件MD5失败")
	}

	videoName := filepath.Base(videoPath)
	preuploadInfo, err := s.preupload(videoName, fileInfo.Size(), tid, title)
	if err != nil {
		return nil, err
	}

	uploadURL, err := buildUploadURL(preuploadInfo.Endpoint, preuploadInfo.UposURI)
	if err != nil {
		return nil, err
	}

	if err := s.uploadChunksSequentially(ctx, uploadURL, preuploadInfo.Auth, videoPath, fileInfo.Size()); err != nil {
		return nil, err
	}

	cid, err := s.completeUpload(uploadURL, preuploadInfo.Auth, videoName, fileInfo.Size())
	if err != nil {
		return nil, err
	}

	tokenPayload := VideoDraftToken{
		Version:  videoTokenSchema,
		CID:      cid,
		FilePath: videoPath,
		FileSize: fileInfo.Size(),
		FileMD5:  fileMD5,
		FileName: videoName,
	}

	tokenBytes, err := json.Marshal(tokenPayload)
	if err != nil {
		return nil, errors.Wrap(err, "序列化草稿token失败")
	}

	return &VideoDraftInfo{
		CID:        cid,
		DraftToken: base64.RawURLEncoding.EncodeToString(tokenBytes),
	}, nil
}

// PublishVideo 提交视频投稿。
func (s *Service) PublishVideo(ctx context.Context, draftToken string, info VideoPublishInfo) (string, error) {
	if s == nil || s.apiClient == nil {
		return "", errors.New("api client 未初始化")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	cid, err := parseCIDFromDraftToken(draftToken)
	if err != nil {
		return "", err
	}

	if info.Copyright == 2 && strings.TrimSpace(info.Source) == "" {
		return "", errors.New("转载视频必须提供 source")
	}

	requestURL := strings.TrimRight(s.apiClient.MemberBaseURL(), "/") + "/x/vu/web/add/v3"
	headers := s.apiClient.GetHeaders("https://member.bilibili.com/platform/upload/video/frame")
	payload := map[string]any{
		"copyright": info.Copyright,
		"source":    strings.TrimSpace(info.Source),
		"tid":       info.Tid,
		"title":     info.Title,
		"tag":       info.Tag,
		"desc":      info.Desc,
		"videos": []map[string]string{
			{
				"filename": strconv.FormatInt(cid, 10),
				"title":    "",
				"desc":     "",
			},
		},
	}

	responseBody, err := s.apiClient.MakeJSONRequest(http.MethodPost, requestURL, payload, headers)
	if err != nil {
		return "", errors.Wrap(err, "调用投稿提交接口失败")
	}

	bvid, err := parseBVIDFromPublishResponse(responseBody)
	if err != nil {
		return "", err
	}

	return bvid, nil
}

func parseCIDFromDraftToken(draftToken string) (int64, error) {
	decodedToken, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(draftToken))
	if err != nil {
		return 0, errors.Wrap(err, "draftToken base64 解析失败")
	}

	var token VideoDraftToken
	if err := json.Unmarshal(decodedToken, &token); err != nil {
		return 0, errors.Wrap(err, "draftToken JSON 解析失败")
	}

	if token.CID <= 0 {
		return 0, errors.New("draftToken 缺少有效 cid")
	}

	return token.CID, nil
}

func parseBVIDFromPublishResponse(responseBody []byte) (string, error) {
	var responseMap map[string]any
	if err := json.Unmarshal(responseBody, &responseMap); err != nil {
		return "", errors.Wrap(err, "解析投稿提交响应失败")
	}

	codeNumber, err := toInt64(responseMap["code"])
	if err != nil {
		return "", errors.Wrap(err, "解析投稿提交 code 失败")
	}
	if codeNumber != 0 {
		message := ""
		if messageValue, ok := responseMap["message"].(string); ok {
			message = messageValue
		}
		return "", errors.Errorf("投稿提交失败: %s (code: %d)", message, codeNumber)
	}

	dataMap, ok := responseMap["data"].(map[string]any)
	if !ok {
		return "", errors.New("投稿提交响应缺少 data")
	}

	bvidValue, ok := dataMap["bvid"]
	if !ok {
		return "", errors.New("投稿提交响应缺少 bvid")
	}

	bvid, ok := bvidValue.(string)
	if !ok || strings.TrimSpace(bvid) == "" {
		return "", errors.New("投稿提交响应 bvid 无效")
	}

	return bvid, nil
}

func (s *Service) preupload(fileName string, fileSize int64, tid int, title string) (*preuploadResponse, error) {
	requestURL := strings.TrimRight(s.apiClient.MemberBaseURL(), "/") + "/preupload"
	headers := s.apiClient.GetHeaders("https://member.bilibili.com/platform/upload/video/frame")
	payload := map[string]any{
		"name":    fileName,
		"size":    fileSize,
		"r":       "upos",
		"profile": "ugcfr/pc",
		"tid":     tid,
		"title":   title,
	}

	responseBody, err := s.apiClient.MakeJSONRequest(http.MethodPost, requestURL, payload, headers)
	if err != nil {
		return nil, errors.Wrap(err, "调用预上传接口失败")
	}

	var response preuploadResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, errors.Wrap(err, "解析预上传响应失败")
	}

	if response.Code != 0 {
		return nil, errors.Errorf("预上传失败: %s (code: %d)", response.Message, response.Code)
	}
	if strings.TrimSpace(response.UposURI) == "" {
		return nil, errors.New("预上传响应缺少 upos_uri")
	}

	return &response, nil
}

func buildUploadURL(endpoint, uposURI string) (string, error) {
	trimmedURI := strings.TrimSpace(uposURI)
	if strings.HasPrefix(trimmedURI, "http://") || strings.HasPrefix(trimmedURI, "https://") {
		return trimmedURI, nil
	}

	if !strings.HasPrefix(trimmedURI, "upos://") {
		return "", errors.Errorf("不支持的 upos_uri: %s", uposURI)
	}

	pathPart := strings.TrimPrefix(trimmedURI, "upos://")
	pathPart = strings.TrimPrefix(pathPart, "/")

	trimmedEndpoint := strings.TrimSpace(endpoint)
	if trimmedEndpoint == "" {
		return "", errors.New("预上传响应缺少 endpoint")
	}
	if !strings.HasPrefix(trimmedEndpoint, "http://") && !strings.HasPrefix(trimmedEndpoint, "https://") {
		trimmedEndpoint = "https://" + strings.TrimPrefix(trimmedEndpoint, "//")
	}

	return strings.TrimRight(trimmedEndpoint, "/") + "/" + pathPart, nil
}

func (s *Service) uploadChunksSequentially(ctx context.Context, uploadURL, authHeader, videoPath string, totalSize int64) error {
	videoFile, err := os.Open(videoPath)
	if err != nil {
		return errors.Wrap(err, "打开视频文件失败")
	}
	defer videoFile.Close()

	partNumber := 1
	buffer := make([]byte, videoChunkSize)
	var uploadedBytes int64

	for {
		readBytes, readErr := videoFile.Read(buffer)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return errors.Wrap(readErr, "读取分片数据失败")
		}
		if readBytes == 0 {
			break
		}

		partURL := uploadURL + "?partNumber=" + strconv.Itoa(partNumber)
		request, err := http.NewRequestWithContext(ctx, http.MethodPut, partURL, bytes.NewReader(buffer[:readBytes]))
		if err != nil {
			return errors.Wrap(err, "创建分片上传请求失败")
		}

		request.Header.Set("Content-Type", "application/octet-stream")
		if strings.TrimSpace(authHeader) != "" {
			request.Header.Set("X-Upos-Auth", authHeader)
		}
		request.Header.Set("Cookie", s.apiClient.GetCookieString())

		response, err := s.apiClient.DoRequest(request)
		if err != nil {
			return errors.Wrapf(err, "上传分片失败(part=%d)", partNumber)
		}

		responseBody, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()

		if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNoContent {
			return errors.Errorf("上传分片失败(part=%d): status=%d body=%s", partNumber, response.StatusCode, strings.TrimSpace(string(responseBody)))
		}

		uploadedBytes += int64(readBytes)
		partNumber++

		if uploadedBytes >= totalSize {
			break
		}
	}

	return nil
}

func (s *Service) completeUpload(uploadURL, authHeader, fileName string, fileSize int64) (int64, error) {
	totalChunks := int((fileSize + videoChunkSize - 1) / videoChunkSize)
	parts := make([]uploadedPart, 0, totalChunks)
	for index := 0; index < totalChunks; index++ {
		parts = append(parts, uploadedPart{PartNumber: index + 1})
	}

	completeURL := uploadURL + "?output=json&name=" + url.QueryEscape(fileName)
	headers := s.apiClient.GetHeaders("https://member.bilibili.com/platform/upload/video/frame")
	if strings.TrimSpace(authHeader) != "" {
		headers["X-Upos-Auth"] = authHeader
	}

	payload := map[string]any{
		"name":     fileName,
		"filesize": fileSize,
		"chunks":   totalChunks,
		"parts":    parts,
	}

	responseBody, err := s.apiClient.MakeJSONRequest(http.MethodPost, completeURL, payload, headers)
	if err != nil {
		return 0, errors.Wrap(err, "调用上传完成确认接口失败")
	}

	cid, err := parseCIDFromCompleteResponse(responseBody)
	if err != nil {
		return 0, err
	}

	return cid, nil
}

func parseCIDFromCompleteResponse(responseBody []byte) (int64, error) {
	var responseMap map[string]any
	if err := json.Unmarshal(responseBody, &responseMap); err != nil {
		return 0, errors.Wrap(err, "解析上传完成确认响应失败")
	}

	if codeValue, exists := responseMap["code"]; exists {
		codeNumber, convertErr := toInt64(codeValue)
		if convertErr != nil {
			return 0, errors.Wrap(convertErr, "解析上传完成确认 code 失败")
		}
		if codeNumber != 0 {
			message := ""
			if messageValue, ok := responseMap["message"].(string); ok {
				message = messageValue
			}
			return 0, errors.Errorf("上传完成确认失败: %s (code: %d)", message, codeNumber)
		}
	}

	if cidValue, exists := responseMap["cid"]; exists {
		cidNumber, convertErr := toInt64(cidValue)
		if convertErr == nil {
			return cidNumber, nil
		}
	}

	if dataValue, exists := responseMap["data"]; exists {
		if dataMap, ok := dataValue.(map[string]any); ok {
			if cidValue, hasCID := dataMap["cid"]; hasCID {
				cidNumber, convertErr := toInt64(cidValue)
				if convertErr == nil {
					return cidNumber, nil
				}
			}
		}
	}

	return 0, errors.New("上传完成确认响应缺少 cid")
}

func toInt64(value any) (int64, error) {
	switch numericValue := value.(type) {
	case float64:
		return int64(numericValue), nil
	case int64:
		return numericValue, nil
	case int:
		return int64(numericValue), nil
	case json.Number:
		return numericValue.Int64()
	case string:
		parsedNumber, err := strconv.ParseInt(strings.TrimSpace(numericValue), 10, 64)
		if err != nil {
			return 0, err
		}
		return parsedNumber, nil
	default:
		return 0, errors.Errorf("未知数字类型: %T", value)
	}
}

func calculateFileMD5(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}
