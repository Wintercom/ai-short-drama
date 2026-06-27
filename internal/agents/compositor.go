package agents

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/cuiwenyang/ai-short-drama/internal/config"
	"github.com/cuiwenyang/ai-short-drama/internal/fsx"
	"github.com/cuiwenyang/ai-short-drama/internal/logx"
	"github.com/cuiwenyang/ai-short-drama/internal/models"
	"github.com/cuiwenyang/ai-short-drama/internal/services"
)

// Compositor 是后期合成器（流水线最后一个智能体）。
//
// 把分散的镜头产物合成为完整成片：
//  1. 对每个镜头，按配音时长重新生成等长运镜片段（音画对齐的关键）；
//  2. 合成有声片段（无对白镜头补静音轨）；
//  3. 按镜号顺序拼接所有有声片段 → 最终 mp4。
//
// 之所以在此处按音频时长重做片段：配音时长在音频合成后才确定，
// 让画面时长服从配音，才能保证每句台词说完再切镜，音画不错位。
type Compositor struct {
	cfg    *config.Config
	i2v    services.I2V
	editor services.Editor
}

// NewCompositor 构造合成器。
func NewCompositor(cfg *config.Config, i2v services.I2V, editor services.Editor) *Compositor {
	return &Compositor{cfg: cfg, i2v: i2v, editor: editor}
}

// Name 节点名。
func (a *Compositor) Name() string { return "compositor" }

// Run 合成最终成片。
func (a *Compositor) Run(ctx context.Context, st *models.ProjectState) error {
	logx.Stage("🎞️", "后期合成：音画对齐并拼接成片")

	dir := projectDir(a.cfg, st)
	finalDir := filepath.Join(dir, "final")
	var muxedClips []string

	for i := range st.Shots {
		shot := &st.Shots[i]

		// 1) 确定本镜头时长：以配音时长为准（音画对齐）。
		duration, err := a.shotDuration(ctx, shot)
		if err != nil {
			return err
		}
		st.UpdateShot(shot.ID, func(s *models.Shot) { s.Duration = duration })

		// 2) 按配音时长重做等长运镜片段，使画面与台词时长一致。
		alignedClip := filepath.Join(finalDir, shot.ID+"_aligned.mp4")
		if err := a.i2v.Animate(ctx, shot.KeyframePath, shot.Camera, duration, alignedClip); err != nil {
			return fmt.Errorf("镜头[%s]对齐片段生成失败: %w", shot.ID, err)
		}

		// 3) 准备音轨：有对白用配音，无对白补等长静音。
		audioPath := shot.AudioPath
		if audioPath == "" || !fsx.Exists(audioPath) {
			audioPath = filepath.Join(finalDir, shot.ID+"_silent.m4a")
			if err := a.editor.SilentAudio(ctx, duration, audioPath); err != nil {
				return fmt.Errorf("镜头[%s]静音轨生成失败: %w", shot.ID, err)
			}
		}

		// 4) 合成有声片段。
		muxed := filepath.Join(finalDir, shot.ID+"_muxed.mp4")
		if err := a.editor.MuxClipAudio(ctx, alignedClip, audioPath, muxed); err != nil {
			return fmt.Errorf("镜头[%s]音画合成失败: %w", shot.ID, err)
		}
		muxedClips = append(muxedClips, muxed)
		logx.Step("镜头 %s 合成完成（%.1fs）", shot.ID, duration)
	}

	// 5) 拼接所有片段为最终成片。
	output := filepath.Join(finalDir, "output.mp4")
	if err := a.editor.Concat(ctx, muxedClips, output); err != nil {
		return fmt.Errorf("拼接成片失败: %w", err)
	}

	st.FinalVideo = output
	st.AddAsset(models.Asset{Kind: "final", Ref: st.Project.ID, Path: output})

	total, _ := a.editor.ProbeDuration(ctx, output)
	logx.Done("成片已输出：%s（总时长 %.1fs）", output, total)
	return nil
}

// shotDuration 确定镜头时长：优先用配音实际时长，否则按对白字数估算，再否则默认 3 秒。
func (a *Compositor) shotDuration(ctx context.Context, shot *models.Shot) (float64, error) {
	if shot.AudioPath != "" && fsx.Exists(shot.AudioPath) {
		d, err := a.editor.ProbeDuration(ctx, shot.AudioPath)
		if err != nil {
			return 0, fmt.Errorf("探测镜头[%s]配音时长失败: %w", shot.ID, err)
		}
		return d + 0.5, nil // 尾部留 0.5s 余白，避免切镜过急
	}
	if n := len([]rune(shot.Dialogue)); n > 0 {
		return float64(n)/5.0 + 0.5, nil
	}
	return 3.0, nil
}
