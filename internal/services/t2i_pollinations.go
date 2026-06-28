package services

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cuiwenyang/ai-short-drama/internal/fsx"
	"github.com/cuiwenyang/ai-short-drama/internal/logx"
)

// PollinationsT2I 使用 Pollinations.AI 免费 API 生成真实图片（无需 API Key）。
//
// API：GET https://image.pollinations.ai/prompt/{encoded_prompt}?width=W&height=H&seed=N&nologo=true
// 无需注册，完全免费，每次 HTTP 请求即返回 JPEG 图片。
// 网络不可用或失败时，自动降级到本地 SVG 实现（LocalT2I），保证流程不中断。
//
// prompt 中文友好：Pollinations 模型（FLUX）支持中文 prompt 生成人物场景图。
//
// 防限速设计（与 EdgeTTS 同构）：免费层强制串行——并发请求会触发 HTTP 429。
// 故内置：
//   - 串行锁（mu）：即便 storyboard 多 goroutine 并发调用，对 API 仍是逐个请求；
//   - 429/超时重试 + 指数退避；
//   - 最终失败降级到本地 SVG，流程不中断。
type PollinationsT2I struct {
	Local      *LocalT2I // 降级兜底
	Width      int
	Height     int
	Timeout    time.Duration
	MaxRetries int
	mu         sync.Mutex // 串行化请求，避开免费层 429 限流
}

// NewPollinationsT2I 构造 Pollinations T2I，以 LocalT2I 作降级兜底。
func NewPollinationsT2I(ffmpeg string, w, h int) *PollinationsT2I {
	return &PollinationsT2I{
		Local:      NewLocalT2I(ffmpeg, w, h),
		Width:      w,
		Height:     h,
		Timeout:    60 * time.Second, // 排队时留余量（单图本身约 2s）
		MaxRetries: 3,
	}
}

// Generate 先尝试 Pollinations API（串行 + 重试退避），失败则降级到本地 SVG。
func (t *PollinationsT2I) Generate(ctx context.Context, prompt, refImage string, seed int, outPath string) error {
	if err := fsx.EnsureDir(filepath.Dir(outPath)); err != nil {
		return err
	}

	// 串行化：免费层并发会被 429 限流，逐个请求最稳。
	t.mu.Lock()
	defer t.mu.Unlock()

	// 从结构化 prompt 中提取画面描述，拼英文提示（Pollinations 对英文效果更好）
	meta := parsePrompt(prompt)
	apiPrompt := t.buildAPIPrompt(meta)

	apiURL := fmt.Sprintf(
		"https://image.pollinations.ai/prompt/%s?width=%d&height=%d&seed=%d&nologo=true&model=flux",
		url.PathEscape(apiPrompt), t.Width, t.Height, seed,
	)

	var lastErr error
	for attempt := 0; attempt <= t.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt) * 2 * time.Second // 2s, 4s, 6s
			logx.Warn("Pollinations T2I 第 %d 次重试（退避 %s）：%v", attempt, backoff, lastErr)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if err := t.download(ctx, apiURL, outPath); err != nil {
			lastErr = err
			continue
		}
		return nil // 成功
	}

	logx.Warn("Pollinations T2I 多次失败，降级到本地 SVG：%v", lastErr)
	return t.Local.Generate(ctx, prompt, refImage, seed, outPath)
}

// buildAPIPrompt 把中文结构化 meta 转为适合 Pollinations 的英文描述。
//
// 角色一致性的关键：把「角色外貌锚点」紧跟画面描述前置（扩散模型靠前 token 权重更高），
// 再叠加固定 seed（调用方传入）与统一画风后缀，使同一角色跨镜头长相、画风尽量一致。
func (t *PollinationsT2I) buildAPIPrompt(meta shotMeta) string {
	var parts []string

	// 主画面描述（直接用中文，Pollinations FLUX 支持）
	if meta.description != "" {
		parts = append(parts, meta.description)
	}

	// 角色外貌锚点（一致性核心）：把固定外貌前置，让模型每镜都锚定同一个人。
	if meta.appearance != "" {
		parts = append(parts, meta.appearance)
	}

	// 角色性别提示
	if meta.gender == "male" {
		parts = append(parts, "male character")
	} else if meta.gender == "female" {
		parts = append(parts, "female character")
	}

	// 场景氛围
	if strings.Contains(meta.scene, "夜") {
		parts = append(parts, "night scene, cinematic lighting")
	} else if strings.Contains(meta.scene, "日") || strings.Contains(meta.scene, "黄昏") {
		parts = append(parts, "daytime, warm lighting")
	}
	if strings.Contains(meta.scene, "内") {
		parts = append(parts, "interior")
	} else if strings.Contains(meta.scene, "外") {
		parts = append(parts, "outdoor")
	}

	// 景别提示
	switch meta.shotType {
	case "特写":
		parts = append(parts, "extreme close up portrait")
	case "近景":
		parts = append(parts, "close up shot")
	case "中景":
		parts = append(parts, "medium shot, waist up")
	default:
		parts = append(parts, "full body shot, wide angle")
	}

	// 统一画风后缀（固定不变）：所有镜头共用同一组风格词 + 一致性提示，
	// 配合固定 seed 锁定跨镜头的画风与人物调性，避免忽欧美忽亚洲、忽冷忽暖。
	parts = append(parts, consistentStyleSuffix)

	return strings.Join(parts, ", ")
}

// consistentStyleSuffix 是所有镜头共用的固定画风/一致性后缀。
// 固定不变是关键——任何镜头都长这一种调性，才能跨镜头统一。
const consistentStyleSuffix = "consistent character design, same person across shots, " +
	"east asian features, photorealistic, cinematic film still, " +
	"soft natural lighting, muted color grade, 35mm, high detail"

// download 下载 URL 到 outPath（JPEG 直接存储）。
func (t *PollinationsT2I) download(ctx context.Context, rawURL, outPath string) error {
	ctx, cancel := context.WithTimeout(ctx, t.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ai-short-drama/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// 检查 Content-Type 是否为图片
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") {
		return fmt.Errorf("返回非图片内容: %s", ct)
	}

	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(outPath)
		return fmt.Errorf("写入图片失败: %w", err)
	}
	return nil
}
