package xiaohongshu

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/proto"
	"github.com/pkg/errors"
	"github.com/xpzouying/xiaohongshu-mcp/humanize"
)

// PublishLongArticleContent 是“写长文”页面的输入。
type PublishLongArticleContent struct {
	Title        string
	Content      string
	Description  string
	Template     string
	ImagePaths   []string
	ScheduleTime *time.Time
	Visibility   string
	Products     []string
}

type PublishLongArticleAction struct{ page *rod.Page }

const (
	longArticleTitle   = `textarea[placeholder="输入标题"]`
	longArticleEditor  = `div.ProseMirror[contenteditable="true"], div.tiptap[contenteditable="true"], div[role="textbox"][contenteditable="true"]`
	longArticleButtons = `button, [role="button"], .d-button, .d-button-content, [class*="btn"], .creator-tab, .new-creation, .center span, .center a`
)

func NewPublishLongArticleAction(page *rod.Page) *PublishLongArticleAction {
	return &PublishLongArticleAction{page: page}
}

// Publish 的超时覆盖导航、填写和发布，不自动重试最终提交。
func (p *PublishLongArticleAction) Publish(ctx context.Context, content PublishLongArticleContent) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	page := p.page.Context(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ensureCreatorSession(page); err != nil {
		return err
	}
	if err := page.Navigate(urlOfPublic); err != nil {
		return errors.Wrap(err, "打开长文发布页失败")
	}
	if err := clickLongArticleText(page, "写长文"); err != nil {
		return err
	}
	if err := clickLongArticleText(page, "新的创作"); err != nil {
		return err
	}
	title, err := waitLongArticleElement(page, longArticleTitle, "")
	if err != nil {
		return errors.Wrap(err, "查找长文标题失败")
	}
	if err := replaceLongArticleText(title, content.Title); err != nil {
		return errors.Wrap(err, "输入长文标题失败")
	}
	value, err := title.Property("value")
	if err != nil || value.Str() != content.Title {
		return errors.New("长文标题未完整写入，请检查页面标题长度限制")
	}
	editor, err := waitLongArticleElement(page, longArticleEditor, "")
	if err != nil {
		return errors.Wrap(err, "查找长文正文失败")
	}
	if err := replaceLongArticleText(editor, content.Content); err != nil {
		return errors.Wrap(err, "输入长文正文失败")
	}
	if err := uploadLongArticleImages(page, editor, content.ImagePaths); err != nil {
		return err
	}
	if err := clickLongArticleText(page, "一键排版"); err != nil {
		return err
	}
	if err := selectLongArticleTemplate(page, content.Template); err != nil {
		return err
	}
	if err := clickLongArticleText(page, "下一步"); err != nil {
		return err
	}

	err = pollLongArticle(page, 30*time.Second, func(probe *rod.Page) (bool, error) {
		oldTitle, err := visibleLongArticleElement(probe, longArticleTitle, "")
		if err != nil || oldTitle != nil {
			return false, err
		}
		button, err := visibleLongArticleElement(probe, longArticleButtons+", xhs-publish-btn", "发布")
		return button != nil, err
	})
	if err != nil {
		return errors.Wrap(err, "等待长文发布页失败")
	}
	description, err := waitLongArticleElement(page, longArticleEditor, "")
	if err != nil {
		return errors.Wrap(err, "查找发布页简介失败")
	}
	if err := replaceLongArticleText(description, content.Description); err != nil {
		return errors.Wrap(err, "填写发布页简介失败")
	}
	if err := checkContentMaxLength(page); err != nil {
		return err
	}
	if content.ScheduleTime != nil {
		if err := setSchedulePublish(ctx, page, *content.ScheduleTime); err != nil {
			return errors.Wrap(err, "设置定时发布失败")
		}
	}
	if err := setVisibility(page, content.Visibility); err != nil {
		return errors.Wrap(err, "设置可见范围失败")
	}
	if err := bindProducts(ctx, page, content.Products); err != nil {
		return errors.Wrap(err, "绑定商品失败")
	}
	if err := clickLongArticlePublish(page); err != nil {
		return err
	}
	return confirmLongArticlePublished(page)
}

func pollLongArticle(page *rod.Page, timeout time.Duration, check func(*rod.Page) (bool, error)) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := page.GetContext().Err(); err != nil {
			return err
		}
		if time.Now().After(deadline) {
			return context.DeadlineExceeded
		}
		ok, err := check(page)
		if err != nil || ok {
			return err
		}
		select {
		case <-page.GetContext().Done():
			return page.GetContext().Err()
		case <-ticker.C:
		}
	}
}

func visibleLongArticleElement(page *rod.Page, selector, text string) (*rod.Element, error) {
	elems, err := page.Elements(selector)
	if err != nil {
		return nil, err
	}
	for _, elem := range elems {
		visible, err := elem.Visible()
		if err != nil || !visible || !humanize.Visible(elem) {
			continue
		}
		if text != "" {
			value, err := elem.Text()
			if err != nil || strings.TrimSpace(value) != text {
				continue
			}
		}
		return elem, nil
	}
	return nil, nil
}

func waitLongArticleElement(page *rod.Page, selector, text string) (*rod.Element, error) {
	var elem *rod.Element
	err := pollLongArticle(page, 20*time.Second, func(probe *rod.Page) (bool, error) {
		var err error
		elem, err = visibleLongArticleElement(probe, selector, text)
		return elem != nil, err
	})
	if err != nil {
		return nil, err
	}
	return elem, nil
}

func clickLongArticleText(page *rod.Page, text string) error {
	elem, err := waitLongArticleElement(page, longArticleButtons, text)
	if err != nil {
		return errors.Wrapf(err, "查找“%s”失败", text)
	}
	withTimeout := elem.Timeout(10 * time.Second)
	defer withTimeout.CancelTimeout()
	return errors.Wrapf(withTimeout.Click(proto.InputMouseButtonLeft, 1), "点击“%s”失败", text)
}

// 原生输入整段文本，换行用 Enter；不注入 HTML 或逐字等待。
func replaceLongArticleText(elem *rod.Element, text string) error {
	if err := elem.Focus(); err != nil {
		return err
	}
	isField, err := elem.Eval(`() => this.matches('input, textarea')`)
	if err != nil {
		return err
	}
	if isField.Value.Bool() {
		if err := elem.SelectAllText(); err != nil {
			return err
		}
	} else {
		if err := elem.Page().KeyActions().Press(input.ControlLeft).Type(input.KeyA).Release(input.ControlLeft).Do(); err != nil {
			return err
		}
		if err := elem.Page().Keyboard.Type(input.Backspace); err != nil {
			return err
		}
	}
	if err := elem.WaitWritable(); err != nil {
		return err
	}
	page := elem.Page()
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	for i, paragraph := range strings.Split(text, "\n") {
		if i > 0 {
			if err := page.InsertText("\n"); err != nil {
				return err
			}
		}
		if paragraph != "" {
			if err := page.InsertText(paragraph); err != nil {
				return err
			}
		}
	}
	return nil
}

func selectLongArticleTemplate(page *rod.Page, template string) error {
	var available []string
	var chosen *rod.Element
	err := pollLongArticle(page, 20*time.Second, func(probe *rod.Page) (bool, error) {
		cards, err := probe.Elements(".template-card")
		if err != nil {
			return false, err
		}
		available, chosen = nil, nil
		for _, card := range cards {
			visible, _ := card.Visible()
			if !visible {
				continue
			}
			names, err := card.Elements(".template-title")
			if err != nil {
				return false, err
			}
			if len(names) != 1 {
				continue
			}
			name, err := names[0].Text()
			if err != nil {
				return false, err
			}
			name = strings.TrimSpace(name)
			available = append(available, name)
			if name == template {
				chosen = card
			}
		}
		return chosen != nil, nil
	})
	if err != nil {
		return fmt.Errorf("未找到长文模板 %q，可选模板: %s: %w", template, strings.Join(available, ", "), err)
	}
	chosen = chosen.Context(page.GetContext()).Timeout(10 * time.Second)
	defer chosen.CancelTimeout()
	if err := chosen.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return errors.Wrap(err, "选择长文模板失败")
	}
	err = pollLongArticle(page, 10*time.Second, func(probe *rod.Page) (bool, error) {
		elem, err := visibleLongArticleElement(probe, ".template-card.selected .template-title", template)
		return elem != nil, err
	})
	return errors.Wrap(err, "未确认长文模板选中")
}

func uploadLongArticleImages(page *rod.Page, editor *rod.Element, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	inputs, err := page.Elements(`input[type="file"]`)
	if err != nil {
		return err
	}
	var upload *rod.Element
	for _, candidate := range inputs {
		accept, err := candidate.Attribute("accept")
		if err != nil {
			return err
		}
		if accept != nil && acceptsImage(*accept) {
			if upload != nil {
				return errors.New("长文页面存在多个图片上传入口，无法确定正文图片入口")
			}
			upload = candidate
		}
	}
	if upload == nil {
		return errors.New("长文编辑器没有图片上传入口")
	}
	if err := upload.SetFiles(paths); err != nil {
		return errors.Wrap(err, "上传长文图片失败")
	}
	err = pollLongArticle(page, time.Minute, func(probe *rod.Page) (bool, error) {
		res, err := editor.Eval(`() => Array.from(this.querySelectorAll('img')).filter(img => /^https?:\/\//.test(img.src)).length`)
		if err != nil {
			return false, err
		}
		return res.Value.Int() >= len(paths), nil
	})
	if err != nil {
		return errors.Wrapf(err, "未确认长文图片上传完成（需要%d张）", len(paths))
	}
	return nil
}

func clickLongArticlePublish(page *rod.Page) error {
	probe := page.Timeout(20 * time.Second)
	defer probe.CancelTimeout()
	btn, disabled, err := findPublishButton(probe)
	if err != nil {
		return err
	}
	if btn != nil {
		if disabled != "" {
			return errors.New(disabled)
		}
		if btn.isWidget {
			return clickPublishWidget(probe, btn.elem)
		}
		return btn.elem.Click(proto.InputMouseButtonLeft, 1)
	}
	return clickLongArticleText(probe, "发布")
}

func confirmLongArticlePublished(page *rod.Page) error {
	err := pollLongArticle(page, 15*time.Second, func(probe *rod.Page) (bool, error) {
		info, err := probe.Info()
		if err != nil {
			return false, err
		}
		u, err := url.Parse(info.URL)
		if err != nil {
			return false, err
		}
		if u.Hostname() != "creator.xiaohongshu.com" || strings.Contains(u.Path, "login") {
			return false, errors.New("发布后跳转到登录页或非创作者中心页面")
		}
		if strings.TrimRight(u.Path, "/") == "/publish/success" {
			return true, nil
		}
		elem, err := visibleLongArticleElement(probe, `.publish-success, .success-title, [role="status"], [role="alert"]`, "发布成功")
		return elem != nil, err
	})
	return errors.Wrap(err, "长文发布未确认成功，请检查创作者中心后再决定是否重试")
}
