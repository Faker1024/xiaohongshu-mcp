//go:build integration

package xiaohongshu

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/stretchr/testify/require"
)

// 使用独立浏览器和拦截页面；不读取真实 cookies、不访问小红书。
func TestLongArticleBrowserFixture(t *testing.T) {
	bin := os.Getenv("XHS_TEST_BROWSER_BIN")
	if bin == "" {
		t.Skip("设置 XHS_TEST_BROWSER_BIN 后运行本地浏览器测试")
	}
	l := launcher.New().Bin(bin).Headless(true).Leakless(false).UserDataDir(t.TempDir())
	controlURL, err := l.Launch()
	require.NoError(t, err)
	b := rod.New().ControlURL(controlURL)
	require.NoError(t, b.Connect())
	t.Cleanup(func() { _ = b.Close(); l.Cleanup() })
	require.NoError(t, b.SetCookies([]*proto.NetworkCookieParam{{
		Name: creatorSessionCookie, Value: "fixture-only", Domain: "creator.xiaohongshu.com", Path: "/",
	}}))
	fixture, err := os.ReadFile("testdata/long_article.html")
	require.NoError(t, err)
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=")
	require.NoError(t, err)
	imagePath := filepath.Join(t.TempDir(), "image.png")
	require.NoError(t, os.WriteFile(imagePath, png, 0600))
	imagePath2 := filepath.Join(t.TempDir(), "image-2.png")
	require.NoError(t, os.WriteFile(imagePath2, png, 0600))

	for _, tt := range []struct {
		scenario string
		images   bool
		want     string
	}{
		{scenario: "success"},
		{scenario: "success-images", images: true},
		{scenario: "missing-template", want: "其他模板"},
		{scenario: "selection-fails", want: "模板"},
		{scenario: "no-transition", want: "发布页"},
		{scenario: "truncated-title", want: "标题"},
		{scenario: "upload-stalls", images: true, want: "图片"},
		{scenario: "login", want: "未确认成功"},
	} {
		t.Run(tt.scenario, func(t *testing.T) {
			require.NoError(t, b.SetCookies([]*proto.NetworkCookieParam{{
				Name: creatorSessionCookie, Value: "fixture-only", Domain: "creator.xiaohongshu.com", Path: "/",
			}}))
			page, err := b.Page(proto.TargetCreateTarget{URL: "about:blank"})
			require.NoError(t, err)
			defer page.Close()
			router := page.HijackRequests()
			require.NoError(t, router.Add("*", "", func(h *rod.Hijack) {
				if h.Request.Type() == proto.NetworkResourceTypeDocument {
					h.Response.SetHeader("Content-Type", "text/html; charset=utf-8").SetBody(strings.ReplaceAll(string(fixture), "{{SCENARIO}}", tt.scenario))
				} else {
					h.Response.SetHeader("Content-Type", "image/svg+xml").SetBody(`<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"><rect width="1" height="1"/></svg>`)
				}
			}))
			go router.Run()
			defer router.Stop()
			content := PublishLongArticleContent{
				Title: "测试标题", Content: strings.Repeat("长正文", 800) + "\r\n第二段 <b>原样文本</b> & 😀\n\n最后一段",
				Template: "简约", Description: "简介第一行\n简介第二行",
			}
			if tt.images {
				content.ImagePaths = []string{imagePath, imagePath2}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err = NewPublishLongArticleAction(page).Publish(ctx, content)
			if tt.want != "" {
				require.ErrorContains(t, err, tt.want)
				if tt.scenario != "login" {
					require.True(t, page.MustEval(`() => window.submitted === null`).Bool(), "失败后不得点击发布")
				}
				return
			}
			require.NoError(t, err)
			var actual struct {
				Title, Content, Description, Template string
				Images                                int
			}
			result, err := page.Eval(`() => window.submitted`)
			require.NoError(t, err)
			require.NoError(t, result.Value.Unmarshal(&actual))
			require.Equal(t, content.Title, actual.Title)
			expectedContent := strings.ReplaceAll(strings.ReplaceAll(content.Content, "\r", ""), "\n", "")
			actualContent := strings.ReplaceAll(strings.TrimRight(actual.Content, "\n"), "\n", "")
			require.Equal(t, expectedContent, actualContent)
			require.Equal(t, content.Description, actual.Description)
			require.Equal(t, content.Template, actual.Template)
			require.Equal(t, len(content.ImagePaths), actual.Images)
		})
	}
}
