package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPublishLongArticleValidatesRequestBeforeOpeningBrowser(t *testing.T) {
	service := NewXiaohongshuService()

	tests := []struct {
		name string
		req  *PublishLongArticleRequest
		want string
	}{
		{name: "nil request", want: "request is required"},
		{
			name: "missing title",
			req:  &PublishLongArticleRequest{Content: "正文", Template: "模板"},
			want: "标题不能为空",
		},
		{
			name: "missing content",
			req:  &PublishLongArticleRequest{Title: "标题", Template: "模板"},
			want: "正文不能为空",
		},
		{
			name: "missing template",
			req:  &PublishLongArticleRequest{Title: "标题", Content: "正文"},
			want: "排版模板不能为空",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.PublishLongArticle(context.Background(), tt.req)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestPublishLongArticleRejectsInvalidOptionsBeforeDownloading(t *testing.T) {
	var downloads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	tests := []struct {
		name string
		edit func(*PublishLongArticleRequest)
		want string
	}{
		{"blank title", func(r *PublishLongArticleRequest) { r.Title = " \n " }, "标题不能为空"},
		{"long title", func(r *PublishLongArticleRequest) { r.Title = strings.Repeat("字", 21) }, "标题长度"},
		{"blank content", func(r *PublishLongArticleRequest) { r.Content = "\t\n" }, "正文不能为空"},
		{"blank template", func(r *PublishLongArticleRequest) { r.Template = "  " }, "排版模板不能为空"},
		{"long description", func(r *PublishLongArticleRequest) { r.Description = strings.Repeat("字", 1001) }, "description"},
		{"long content needs summary", func(r *PublishLongArticleRequest) { r.Content = strings.Repeat("字", 1001) }, "description"},
		{"blank description", func(r *PublishLongArticleRequest) { r.Description = "  " }, "description"},
		{"invalid visibility", func(r *PublishLongArticleRequest) { r.Visibility = "private" }, "可见范围"},
		{"bad schedule", func(r *PublishLongArticleRequest) { r.ScheduleAt = "tomorrow" }, "格式错误"},
		{"early schedule", func(r *PublishLongArticleRequest) {
			r.ScheduleAt = time.Now().Add(30 * time.Minute).Format(time.RFC3339)
		}, "1小时至14天"},
		{"late schedule", func(r *PublishLongArticleRequest) {
			r.ScheduleAt = time.Now().Add(15 * 24 * time.Hour).Format(time.RFC3339)
		}, "1小时至14天"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &PublishLongArticleRequest{Title: "标题", Content: "正文", Template: "模板", Images: []string{server.URL + "/image.png"}}
			tt.edit(req)
			_, err := NewXiaohongshuService().PublishLongArticle(context.Background(), req)
			require.ErrorContains(t, err, tt.want)
		})
	}
	require.Zero(t, downloads.Load(), "无效请求不应下载图片")
}

func TestPublishLongArticleHonorsCanceledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewXiaohongshuService().PublishLongArticle(ctx, &PublishLongArticleRequest{
		Title: "标题", Content: strings.Repeat("正文", 1000), Template: "模板", Description: "简介",
		Images: []string{"missing-image.png"},
	})
	require.ErrorIs(t, err, context.Canceled)
}
