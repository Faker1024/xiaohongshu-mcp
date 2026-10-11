package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xpzouying/xiaohongshu-mcp/pkg/xhsutil"
	"github.com/xpzouying/xiaohongshu-mcp/xiaohongshu"
)

// PublishLongArticleRequest 是写长文请求。
type PublishLongArticleRequest struct {
	Title       string   `json:"title"`
	Content     string   `json:"content"`
	Template    string   `json:"template"`
	Description string   `json:"description,omitempty"`
	Images      []string `json:"images,omitempty"`
	ScheduleAt  string   `json:"schedule_at,omitempty"`
	Visibility  string   `json:"visibility,omitempty"`
	Products    []string `json:"products,omitempty"`
}

// PublishLongArticleResponse 是写长文结果。
type PublishLongArticleResponse struct {
	Title    string `json:"title"`
	Template string `json:"template"`
	Images   int    `json:"images"`
	Status   string `json:"status"`
}

// PublishLongArticle 执行小红书“写长文”发布流程。
func (s *XiaohongshuService) PublishLongArticle(ctx context.Context, req *PublishLongArticleRequest) (*PublishLongArticleResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateLongArticleRequest(req); err != nil {
		return nil, err
	}
	scheduleTime, err := parseLongArticleScheduleAt(req.ScheduleAt)
	if err != nil {
		return nil, err
	}

	var imagePaths []string
	if len(req.Images) > 0 {
		imagePaths, err = s.processImages(req.Images)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}

	for _, path := range imagePaths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("图片文件不可访问: %w", err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("图片路径不是普通文件: %s", path)
		}
	}

	description := req.Description
	if description == "" {
		description = req.Content
	}

	content := xiaohongshu.PublishLongArticleContent{
		Title:        req.Title,
		Content:      req.Content,
		Description:  description,
		Template:     req.Template,
		ImagePaths:   imagePaths,
		ScheduleTime: scheduleTime,
		Visibility:   req.Visibility,
		Products:     req.Products,
	}

	if err := s.publishLongArticle(ctx, content); err != nil {
		return nil, err
	}

	return &PublishLongArticleResponse{
		Title:    req.Title,
		Template: req.Template,
		Images:   len(imagePaths),
		Status:   "发布完成",
	}, nil
}

func validateLongArticleRequest(req *PublishLongArticleRequest) error {
	if req == nil {
		return fmt.Errorf("request is required")
	}
	if strings.TrimSpace(req.Title) == "" {
		return fmt.Errorf("标题不能为空")
	}
	// 与现有图文发布的标题长度策略保持一致。
	if xhsutil.CalcTitleLength(req.Title) > 20 {
		return fmt.Errorf("标题长度超过限制")
	}
	if strings.TrimSpace(req.Content) == "" {
		return fmt.Errorf("正文不能为空")
	}
	if strings.TrimSpace(req.Template) == "" {
		return fmt.Errorf("排版模板不能为空")
	}
	description := req.Description
	if description == "" {
		description = req.Content
	}
	if strings.TrimSpace(description) == "" || utf8.RuneCountInString(description) > 1000 {
		return fmt.Errorf("请提供非空且不超过1000字的 description（发布页简介），长文正文不会自动截断")
	}
	switch req.Visibility {
	case "", "公开可见", "仅自己可见", "仅互关好友可见":
	default:
		return fmt.Errorf("不支持的可见范围: %s", req.Visibility)
	}
	return nil
}

func parseLongArticleScheduleAt(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}

	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, fmt.Errorf("定时发布时间格式错误，请使用 ISO8601 格式: %v", err)
	}

	now := time.Now()
	minTime := now.Add(time.Hour)
	maxTime := now.Add(14 * 24 * time.Hour)
	if t.Before(minTime) || t.After(maxTime) {
		return nil, fmt.Errorf("定时发布时间必须在1小时至14天内")
	}

	return &t, nil
}

func (s *XiaohongshuService) publishLongArticle(ctx context.Context, content xiaohongshu.PublishLongArticleContent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b := newBrowser()
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	return xiaohongshu.NewPublishLongArticleAction(page).Publish(ctx, content)
}
