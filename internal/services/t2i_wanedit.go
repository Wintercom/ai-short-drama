package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cuiwenyang/ai-short-drama/internal/fsx"
	"github.com/cuiwenyang/ai-short-drama/internal/logx"
)

// WanEditT2I 用阿里云百炼通义千问图像模型实现「参考图驱动」的文生图，
// 是角色一致性的第二层——让同一角色在不同镜头里是「同一张脸」。
//
// 两种模式（同一端点、同步返回，无需轮询）：
//   - 无参考图（refImage 空，如生成角色锚点图）：纯文生图，model=qwen-image；
//   - 有参考图（每个镜头传入角色锚点图）：图生图编辑，model=qwen-image-edit-plus，
//     把锚点人物「放进」新的镜头画面 → 跨镜头同一张脸。
//
// 端点：POST {base}/api/v1/services/aigc/multimodal-generation/generation
// 请求：input.messages[].content = [{image: dataURI}?, {text: 指令}]
// 响应：output.choices[0].message.content[0].image = 生成图 OSS URL
//
// 容错（三级降级）：失败降级到 Pollinations（再失败由其降级 SVG），流程不中断。
type WanEditT2I struct {
	Fallback  T2I // 降级兜底（Pollinations）
	APIKey    string
	Model     string // 文生图模型，如 qwen-image
	EditModel string // 图生图编辑模型，如 qwen-image-edit-plus
	BaseURL   string
	HTTP      *http.Client

	Timeout    time.Duration
	MaxRetries int
	mu         sync.Mutex // 串行化请求，避开并发限流（与 WanI2V/Pollinations 同构）
}

// NewWanEditT2I 构造参考图驱动 T2I，以 fallback 作降级兜底。
func NewWanEditT2I(apiKey, model, editModel, baseURL string, fallback T2I) *WanEditT2I {
	if model == "" {
		model = "qwen-image"
	}
	if editModel == "" {
		editModel = "qwen-image-edit-plus"
	}
	if baseURL == "" {
		baseURL = "https://dashscope.aliyuncs.com"
	}
	return &WanEditT2I{
		Fallback:   fallback,
		APIKey:     apiKey,
		Model:      model,
		EditModel:  editModel,
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTP:       &http.Client{},
		Timeout:    120 * time.Second,
		MaxRetries: 2,
	}
}

// Available 报告是否具备调用条件（有 API Key）。
func (t *WanEditT2I) Available() bool { return t.APIKey != "" }

// Generate 生成关键帧：有参考图走图生图（保持人物），否则走文生图；失败降级 fallback。
func (t *WanEditT2I) Generate(ctx context.Context, prompt, refImage string, seed int, outPath string) error {
	if err := fsx.EnsureDir(filepath.Dir(outPath)); err != nil {
		return err
	}

	imageURL, err := t.generate(ctx, prompt, refImage)
	if err != nil {
		logx.Warn("WanEdit T2I 失败，降级到兜底：%v", err)
		return t.Fallback.Generate(ctx, prompt, refImage, seed, outPath)
	}
	if err := downloadTo(ctx, t.HTTP, imageURL, outPath, t.Timeout); err != nil {
		logx.Warn("WanEdit T2I 图片下载失败，降级到兜底：%v", err)
		return t.Fallback.Generate(ctx, prompt, refImage, seed, outPath)
	}
	return nil
}

// generate 调用模型生成图片，返回 OSS URL（串行 + 重试退避）。
func (t *WanEditT2I) generate(ctx context.Context, prompt, refImage string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	body, err := t.buildRequest(prompt, refImage)
	if err != nil {
		return "", err
	}
	raw, _ := json.Marshal(body)

	var lastErr error
	for attempt := 0; attempt <= t.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt) * 3 * time.Second
			logx.Warn("WanEdit T2I 第 %d 次重试（退避 %s）：%v", attempt, backoff, lastErr)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}

		url, retryable, err := t.doRequest(ctx, raw)
		if err != nil {
			lastErr = err
			if retryable {
				continue
			}
			return "", err
		}
		return url, nil
	}
	return "", fmt.Errorf("生成多次失败: %w", lastErr)
}

// buildRequest 构造请求体：有参考图则 content 含 image + text，否则仅 text。
func (t *WanEditT2I) buildRequest(prompt, refImage string) (map[string]any, error) {
	meta := parsePrompt(prompt)
	instruction := buildEditInstruction(meta)

	model := t.Model
	var content []map[string]any

	if refImage != "" && fsx.Exists(refImage) {
		// 图生图：参考图 + 指令 → 保持人物进入新画面
		model = t.EditModel
		dataURI, err := encodeImageDataURI(refImage)
		if err != nil {
			return nil, fmt.Errorf("参考图编码失败: %w", err)
		}
		content = []map[string]any{
			{"image": dataURI},
			{"text": instruction},
		}
	} else {
		// 文生图：仅指令（用于生成角色锚点图）
		content = []map[string]any{
			{"text": instruction},
		}
	}

	return map[string]any{
		"model": model,
		"input": map[string]any{
			"messages": []map[string]any{
				{"role": "user", "content": content},
			},
		},
	}, nil
}

// doRequest 执行一次请求，返回图片 URL；retryable 标记是否可重试（限流/5xx）。
func (t *WanEditT2I) doRequest(ctx context.Context, raw []byte) (url string, retryable bool, err error) {
	reqCtx, cancel := context.WithTimeout(ctx, t.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost,
		t.BaseURL+"/api/v1/services/aigc/multimodal-generation/generation",
		bytes.NewReader(raw))
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.APIKey)

	resp, err := t.HTTP.Do(req)
	if err != nil {
		return "", true, err // 网络错误可重试
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return "", true, fmt.Errorf("HTTP %d: %s", resp.StatusCode, tail(string(data), 200))
	}
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("HTTP %d: %s", resp.StatusCode, tail(string(data), 300))
	}

	imageURL := parseImageURL(data)
	if imageURL == "" {
		return "", false, fmt.Errorf("响应未含图片 URL: %s", tail(string(data), 300))
	}
	return imageURL, false, nil
}

// parseImageURL 从响应中提取 output.choices[0].message.content[].image。
func parseImageURL(data []byte) string {
	var r struct {
		Output struct {
			Choices []struct {
				Message struct {
					Content []struct {
						Image string `json:"image"`
					} `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		} `json:"output"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return ""
	}
	for _, ch := range r.Output.Choices {
		for _, c := range ch.Message.Content {
			if c.Image != "" {
				return c.Image
			}
		}
	}
	return ""
}

// buildEditInstruction 由结构化 meta 组合给图像模型的指令。
// 图生图时这是「把参考人物放进这个画面」的描述；文生图时是角色/画面描述本身。
func buildEditInstruction(meta shotMeta) string {
	var parts []string
	if meta.description != "" {
		parts = append(parts, meta.description)
	}
	if meta.appearance != "" {
		parts = append(parts, "人物外貌："+meta.appearance)
	}
	// 景别
	switch meta.shotType {
	case "特写":
		parts = append(parts, "面部特写")
	case "近景":
		parts = append(parts, "近景")
	case "中景":
		parts = append(parts, "中景半身")
	case "全景", "远景":
		parts = append(parts, "全景")
	}
	// 场景氛围
	if strings.Contains(meta.scene, "夜") {
		parts = append(parts, "夜晚")
	} else if strings.Contains(meta.scene, "日") {
		parts = append(parts, "白天")
	}
	parts = append(parts, "保持人物面部与身份一致，电影感，写实")
	return strings.Join(parts, "，")
}

// downloadTo 下载 URL 到本地文件（供 T2I/I2V 复用的通用下载）。
func downloadTo(ctx context.Context, client *http.Client, rawURL, outPath string, timeout time.Duration) error {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载 HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(outPath)
		return err
	}
	return nil
}
