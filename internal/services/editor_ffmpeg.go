package services

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cuiwenyang/ai-short-drama/internal/fsx"
)

// FFmpegEditor 用 ffmpeg/ffprobe 实现后期合成能力（零成本、跨平台）。
type FFmpegEditor struct {
	FFmpeg  string
	FFprobe string
	Width   int
	Height  int
	FPS     int
}

// NewFFmpegEditor 构造剪辑器。
func NewFFmpegEditor(ffmpeg, ffprobe string, w, h, fps int) *FFmpegEditor {
	return &FFmpegEditor{FFmpeg: ffmpeg, FFprobe: ffprobe, Width: w, Height: h, FPS: fps}
}

// MuxClipAudio 把无声片段与配音合成为有声片段。
// 以视频时长为准（-shortest），保证音画对齐、不出现黑屏拖尾。
func (e *FFmpegEditor) MuxClipAudio(ctx context.Context, clip, audio, outPath string) error {
	if err := fsx.EnsureDir(filepath.Dir(outPath)); err != nil {
		return err
	}
	return e.run(ctx,
		"-y",
		"-i", clip,
		"-i", audio,
		"-c:v", "copy",
		"-c:a", "aac",
		"-map", "0:v:0",
		"-map", "1:a:0",
		"-shortest",
		outPath,
	)
}

// Concat 按顺序拼接多个有声片段为最终成片。
// 统一重编码（而非 -c copy）以兼容各片段参数差异，保证拼接稳定。
func (e *FFmpegEditor) Concat(ctx context.Context, clips []string, outPath string) error {
	if len(clips) == 0 {
		return fmt.Errorf("没有可拼接的片段")
	}
	if err := fsx.EnsureDir(filepath.Dir(outPath)); err != nil {
		return err
	}

	// 生成 concat 清单文件。
	listPath := filepath.Join(filepath.Dir(outPath), "concat_list.txt")
	var sb strings.Builder
	for _, c := range clips {
		abs, _ := filepath.Abs(c)
		sb.WriteString("file '" + abs + "'\n")
	}
	if err := fsx.WriteFile(listPath, sb.String()); err != nil {
		return err
	}

	return e.run(ctx,
		"-y",
		"-f", "concat",
		"-safe", "0",
		"-i", listPath,
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-ar", "44100", // 强制统一音频采样率，兼容变调男声/edge 等不同来源，防拼接丢音
		"-ac", "2", // 强制统一声道，进一步保证拼接稳定
		"-r", strconv.Itoa(e.FPS),
		outPath,
	)
}

// SilentAudio 生成指定时长的静音 AAC 轨（无对白镜头兜底）。
func (e *FFmpegEditor) SilentAudio(ctx context.Context, duration float64, outPath string) error {
	if err := fsx.EnsureDir(filepath.Dir(outPath)); err != nil {
		return err
	}
	return e.run(ctx,
		"-y",
		"-f", "lavfi",
		"-i", "anullsrc=channel_layout=stereo:sample_rate=44100",
		"-t", ftoa(duration),
		"-c:a", "aac",
		outPath,
	)
}

// ProbeDuration 用 ffprobe 探测媒体时长（秒）。
func (e *FFmpegEditor) ProbeDuration(ctx context.Context, path string) (float64, error) {
	cmd := exec.CommandContext(ctx, e.FFprobe,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe 探测时长失败: %w", err)
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return 0, fmt.Errorf("解析时长失败: %w", err)
	}
	return d, nil
}

// FitDuration 把已有视频片段适配到目标时长。
//   - 过短：用 tpad 冻结末帧补足（画面定格，不黑屏）；
//   - 过长或相近：用 -t 裁剪到目标时长。
//
// 纯 ffmpeg 重编码，廉价且对所有 I2V 来源通用——这样真实 I2V（如 Wan）
// 每镜只需生成一次，无需为对齐配音时长而二次调用昂贵接口。
func (e *FFmpegEditor) FitDuration(ctx context.Context, clip string, target float64, outPath string) error {
	if err := fsx.EnsureDir(filepath.Dir(outPath)); err != nil {
		return err
	}
	if target <= 0 {
		target = 1
	}

	cur, err := e.ProbeDuration(ctx, clip)
	if err != nil {
		return err
	}

	args := []string{"-y", "-i", clip}
	if cur+0.05 < target {
		// 过短：冻结末帧补长到 target（tpad 在视频尾部克隆最后一帧）。
		vf := fmt.Sprintf("tpad=stop_mode=clone:stop_duration=%s", ftoa(target-cur))
		args = append(args, "-vf", vf)
	}
	// 统一以 -t 截到精确目标时长（补长后裁掉多余、过长则直接裁短）。
	args = append(args,
		"-t", ftoa(target),
		"-c:v", "libx264", "-pix_fmt", "yuv420p",
		"-r", strconv.Itoa(e.FPS),
		outPath,
	)
	return e.run(ctx, args...)
}

// run 执行 ffmpeg 命令。
func (e *FFmpegEditor) run(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, e.FFmpeg, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg 失败: %w\n%s", err, tail(string(out), 600))
	}
	return nil
}

func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', 3, 64) }

func tail(s string, n int) string {
	if len(s) > n {
		return "..." + s[len(s)-n:]
	}
	return s
}
