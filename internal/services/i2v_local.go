package services

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/cuiwenyang/ai-short-drama/internal/fsx"
)

// LocalI2V 是零成本的图生视频实现：用 ffmpeg zoompan 滤镜对关键帧
// 施加"推/拉/摇/移"运镜，把静态图变成有镜头语言的动态片段。
// 无需任何视频大模型，是"跑通闭环"最经济的方案；后续可替换为
// 可灵/即梦/SVD 等真实 I2V 模型，接口不变。
type LocalI2V struct {
	FFmpeg string
	Width  int
	Height int
	FPS    int
}

// NewLocalI2V 构造本地图生视频。
func NewLocalI2V(ffmpeg string, w, h, fps int) *LocalI2V {
	return &LocalI2V{FFmpeg: ffmpeg, Width: w, Height: h, FPS: fps}
}

// Animate 把关键帧渲染为 duration 秒的运镜片段。
func (v *LocalI2V) Animate(ctx context.Context, keyframe, camera string, duration float64, outPath string) error {
	if err := fsx.EnsureDir(filepath.Dir(outPath)); err != nil {
		return err
	}
	if duration <= 0 {
		duration = 2
	}
	frames := int(duration * float64(v.FPS))
	if frames < 1 {
		frames = 1
	}

	// 先放大 2 倍再 zoompan，留出推拉摇移的运动空间，避免边缘穿帮。
	zoom := v.zoomExpr(camera, frames)
	vf := fmt.Sprintf("scale=%d:%d,zoompan=%s:d=%d:s=%dx%d:fps=%d",
		v.Width*2, v.Height*2, zoom, frames, v.Width, v.Height, v.FPS)

	cmd := exec.CommandContext(ctx, v.FFmpeg, "-y", "-hide_banner", "-loglevel", "error",
		"-loop", "1", "-i", keyframe,
		"-vf", vf,
		"-t", ftoa(duration),
		"-c:v", "libx264", "-pix_fmt", "yuv420p",
		"-r", strconv.Itoa(v.FPS),
		outPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("I2V 运镜失败: %w\n%s", err, tail(string(out), 400))
	}
	return nil
}

// zoomExpr 根据运镜类型返回 zoompan 的 z/x/y 表达式。
func (v *LocalI2V) zoomExpr(camera string, frames int) string {
	switch camera {
	case "推": // 缓慢放大
		return "z='min(zoom+0.0015,1.3)':x='iw/2-(iw/zoom/2)':y='ih/2-(ih/zoom/2)'"
	case "拉": // 由放大回到原始
		return "z='if(eq(on,1),1.3,max(zoom-0.0015,1.0))':x='iw/2-(iw/zoom/2)':y='ih/2-(ih/zoom/2)'"
	case "摇": // 横向移动
		return fmt.Sprintf("z='1.15':x='(iw-iw/zoom)*on/%d':y='ih/2-(ih/zoom/2)'", frames)
	case "移": // 纵向移动
		return fmt.Sprintf("z='1.15':x='iw/2-(iw/zoom/2)':y='(ih-ih/zoom)*on/%d'", frames)
	default: // 固定：轻微呼吸感，避免完全静止的呆板
		return "z='min(zoom+0.0005,1.08)':x='iw/2-(iw/zoom/2)':y='ih/2-(ih/zoom/2)'"
	}
}
