package services

import (
	"bytes"
	"context"
	"encoding/base64"
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

// WanI2V 调用阿里云百炼（DashScope）通义万相图生视频，让关键帧中的人物真正动起来。
//
// 这是真实 I2V 模型：以关键帧为首帧锚点（保证角色形象一致），由模型推理后续帧，
// 驱动人物肢体与表情动作——解决"画面静止"的根本问题。
//
// API 为异步两段式：
//  1. POST 创建任务（X-DashScope-Async: enable）→ 返回 task_id；
//  2. 轮询 GET /tasks/{task_id} 直到 SUCCEEDED → 拿 video_url 下载 mp4。
//
// 关键帧用 Base64 data URI 内联传入（img_url 字段），无需把本地图片托管到公网。
//
// 容错设计（与 Pollinations/edge-tts 同构）：任何失败（提交、轮询超时、下载）
// 都降级到本地 LocalI2V（ffmpeg 运镜），保证流程不中断、不阻塞成片。
type WanI2V struct {
	Local      *LocalI2V // 降级兜底
	APIKey     string
	Model      string // wan2.2-i2v-plus / wanx2.1-i2v-turbo 等
	BaseURL    string
	Resolution string // 480P / 1080P（wan2.2-i2v-plus 不支持 720P）
	HTTP       *http.Client

	SubmitTimeout time.Duration // 单次提交请求超时
	PollInterval  time.Duration // 轮询间隔
	PollTimeout   time.Duration // 轮询总封顶（防止任务卡死）
	MaxRetries    int           // 提交/下载阶段重试次数

	submitMu sync.Mutex // 仅串行化"提交"请求，避开并发提交触发 RateQuota 限流；轮询/下载仍并发
}

// NewWanI2V 构造通义万相 I2V，以 LocalI2V 作降级兜底。
// resolution 取模型支持的档位（wan2.2-i2v-plus 支持 480P / 1080P，不支持 720P）；
// 留空默认 480P（出片快、成本低）。
func NewWanI2V(apiKey, model, baseURL, resolution string, ffmpeg string, w, h, fps int) *WanI2V {
	if model == "" {
		model = "wan2.2-i2v-plus"
	}
	if baseURL == "" {
		baseURL = "https://dashscope.aliyuncs.com"
	}
	if resolution == "" {
		resolution = "480P"
	}
	return &WanI2V{
		Local:         NewLocalI2V(ffmpeg, w, h, fps),
		APIKey:        apiKey,
		Model:         model,
		BaseURL:       strings.TrimRight(baseURL, "/"),
		Resolution:    resolution,
		HTTP:          &http.Client{},
		SubmitTimeout: 30 * time.Second,
		PollInterval:  15 * time.Second,
		PollTimeout:   10 * time.Minute,
		MaxRetries:    3,
	}
}

// Available 报告是否具备调用条件（有 API Key）。
func (v *WanI2V) Available() bool { return v.APIKey != "" }

// Animate 用通义万相把关键帧生成会动的视频片段；任何环节失败降级到本地运镜。
func (v *WanI2V) Animate(ctx context.Context, keyframe, camera, motion string, duration float64, outPath string) error {
	if err := fsx.EnsureDir(filepath.Dir(outPath)); err != nil {
		return err
	}

	videoURL, err := v.generate(ctx, keyframe, camera, motion, duration)
	if err != nil {
		logx.Warn("Wan I2V 失败，降级到本地运镜：%v", err)
		return v.Local.Animate(ctx, keyframe, camera, motion, duration, outPath)
	}
	if err := v.download(ctx, videoURL, outPath); err != nil {
		logx.Warn("Wan I2V 视频下载失败，降级到本地运镜：%v", err)
		return v.Local.Animate(ctx, keyframe, camera, motion, duration, outPath)
	}
	return nil
}

// generate 执行"提交任务 → 轮询结果"，返回成片视频 URL。
func (v *WanI2V) generate(ctx context.Context, keyframe, camera, motion string, duration float64) (string, error) {
	imgURI, err := encodeImageDataURI(keyframe)
	if err != nil {
		return "", fmt.Errorf("关键帧编码失败: %w", err)
	}

	taskID, err := v.submit(ctx, imgURI, buildMotionPrompt(camera, motion), duration)
	if err != nil {
		return "", err
	}
	return v.poll(ctx, taskID)
}

// submit 创建图生视频异步任务，返回 task_id（带限流重试退避）。
// 全程持有 submitMu：并发镜头的提交被串行化，避开 RateQuota 限流；
// 提交只是秒级请求，串行代价小，真正耗时的轮询/下载仍并发进行。
func (v *WanI2V) submit(ctx context.Context, imgURI, prompt string, duration float64) (string, error) {
	v.submitMu.Lock()
	defer v.submitMu.Unlock()

	body := map[string]any{
		"model": v.Model,
		"input": map[string]any{
			"img_url": imgURI,
			"prompt":  prompt,
		},
		"parameters": map[string]any{
			"resolution": v.Resolution,
			"duration":   wanDuration(duration),
		},
	}
	raw, _ := json.Marshal(body)

	var lastErr error
	for attempt := 0; attempt <= v.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt) * 3 * time.Second
			logx.Warn("Wan I2V 提交第 %d 次重试（退避 %s）：%v", attempt, backoff, lastErr)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}

		reqCtx, cancel := context.WithTimeout(ctx, v.SubmitTimeout)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodPost,
			v.BaseURL+"/api/v1/services/aigc/video-generation/video-synthesis",
			bytes.NewReader(raw))
		if err != nil {
			cancel()
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+v.APIKey)
		req.Header.Set("X-DashScope-Async", "enable")

		resp, err := v.HTTP.Do(req)
		if err != nil {
			cancel()
			lastErr = err
			continue
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("提交返回 HTTP %d: %s", resp.StatusCode, tail(string(data), 200))
			continue // 限流/服务端错误，重试
		}
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("提交失败 HTTP %d: %s", resp.StatusCode, tail(string(data), 300))
		}

		var r struct {
			Output struct {
				TaskID     string `json:"task_id"`
				TaskStatus string `json:"task_status"`
			} `json:"output"`
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(data, &r); err != nil {
			return "", fmt.Errorf("解析提交响应失败: %w", err)
		}
		if r.Output.TaskID == "" {
			return "", fmt.Errorf("提交未返回 task_id（code=%s msg=%s）", r.Code, r.Message)
		}
		return r.Output.TaskID, nil
	}
	return "", fmt.Errorf("提交多次失败: %w", lastErr)
}

// poll 轮询任务直到完成，返回视频 URL；超时或失败返回错误。
func (v *WanI2V) poll(ctx context.Context, taskID string) (string, error) {
	deadline := time.Now().Add(v.PollTimeout)
	for {
		if time.Now().After(deadline) {
			return "", fmt.Errorf("任务 %s 轮询超时（%s）", taskID, v.PollTimeout)
		}

		st, err := v.queryTask(ctx, taskID)
		if err != nil {
			return "", err
		}
		switch st.status {
		case "SUCCEEDED":
			if st.videoURL == "" {
				return "", fmt.Errorf("任务成功但无 video_url")
			}
			return st.videoURL, nil
		case "FAILED", "CANCELED", "UNKNOWN":
			// 带上 API 返回的 code/message，便于排障（如分辨率不支持等）。
			if st.message != "" {
				return "", fmt.Errorf("任务 %s 状态 %s：%s（%s）", taskID, st.status, st.message, st.code)
			}
			return "", fmt.Errorf("任务 %s 状态 %s", taskID, st.status)
		}
		// PENDING / RUNNING：等待后继续轮询。
		select {
		case <-time.After(v.PollInterval):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

// taskResult 是一次任务查询的结构化结果。
type taskResult struct {
	status   string
	videoURL string
	code     string
	message  string
}

// queryTask 查询单次任务状态。
func (v *WanI2V) queryTask(ctx context.Context, taskID string) (taskResult, error) {
	reqCtx, cancel := context.WithTimeout(ctx, v.SubmitTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet,
		v.BaseURL+"/api/v1/tasks/"+taskID, nil)
	if err != nil {
		return taskResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+v.APIKey)

	resp, err := v.HTTP.Do(req)
	if err != nil {
		return taskResult{}, err
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return taskResult{}, fmt.Errorf("查询任务 HTTP %d: %s", resp.StatusCode, tail(string(data), 200))
	}

	var r struct {
		Output struct {
			TaskStatus string `json:"task_status"`
			VideoURL   string `json:"video_url"`
			Code       string `json:"code"`
			Message    string `json:"message"`
		} `json:"output"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return taskResult{}, fmt.Errorf("解析任务响应失败: %w", err)
	}
	return taskResult{
		status:   r.Output.TaskStatus,
		videoURL: r.Output.VideoURL,
		code:     r.Output.Code,
		message:  r.Output.Message,
	}, nil
}

// download 下载视频 URL 到本地文件（带重试退避）。
// 任务已生成成功，视频很贵，不能因 OSS 偶发 TLS/超时就丢弃——故下载失败也重试。
func (v *WanI2V) download(ctx context.Context, rawURL, outPath string) error {
	var lastErr error
	for attempt := 0; attempt <= v.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt) * 2 * time.Second
			logx.Warn("Wan I2V 视频下载第 %d 次重试（退避 %s）：%v", attempt, backoff, lastErr)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if err := v.downloadOnce(ctx, rawURL, outPath); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("下载多次失败: %w", lastErr)
}

// downloadOnce 执行一次下载尝试。
func (v *WanI2V) downloadOnce(ctx context.Context, rawURL, outPath string) error {
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := v.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载视频 HTTP %d", resp.StatusCode)
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

// encodeImageDataURI 把本地图片读为 data:{mime};base64,{...} 形式。
func encodeImageDataURI(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	mime := "image/png"
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		mime = "image/jpeg"
	case ".webp":
		mime = "image/webp"
	case ".bmp":
		mime = "image/bmp"
	}
	return fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(raw)), nil
}

// buildMotionPrompt 组合运镜与动作为模型提示词。
// 动作（motion）是主体，运镜（camera）作为镜头语言补充。
func buildMotionPrompt(camera, motion string) string {
	motion = strings.TrimSpace(motion)
	var cam string
	switch camera {
	case "推":
		cam = "镜头缓缓推近"
	case "拉":
		cam = "镜头缓缓拉远"
	case "摇":
		cam = "镜头横向摇移"
	case "移":
		cam = "镜头平稳移动"
	}
	switch {
	case motion != "" && cam != "":
		return motion + "，" + cam
	case motion != "":
		return motion
	default:
		return cam
	}
}

// wanDuration 把任意时长归一到通义万相支持的离散秒数（当前支持约 5s）。
// 模型输出固定时长，最终由 compositor 的 FitDuration 适配到配音时长。
func wanDuration(d float64) int {
	if d <= 0 {
		return 5
	}
	// wan2.2-i2v-plus 支持 5s；预留对未来更长档位的就近取整。
	switch {
	case d <= 5:
		return 5
	default:
		return 5
	}
}
