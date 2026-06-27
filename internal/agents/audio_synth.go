package agents

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/cuiwenyang/ai-short-drama/internal/config"
	"github.com/cuiwenyang/ai-short-drama/internal/fsx"
	"github.com/cuiwenyang/ai-short-drama/internal/logx"
	"github.com/cuiwenyang/ai-short-drama/internal/models"
	"github.com/cuiwenyang/ai-short-drama/internal/services"
)

// AudioSynth 是音频合成器（流水线第四个智能体）。
//
// 为每个有对白的镜头合成配音，使用该镜头主角的锁定音色(VoiceID)——
// 保证同一角色在所有镜头中声音一致。镜头间独立，故同样做镜头级并发。
//
// 与视觉分镜并列：二者都只依赖角色资产就绪，可并行推进
// （由调度层的 DAG 依赖关系保证）。
type AudioSynth struct {
	cfg *config.Config
	tts services.TTS
}

// NewAudioSynth 构造音频合成器。
func NewAudioSynth(cfg *config.Config, tts services.TTS) *AudioSynth {
	return &AudioSynth{cfg: cfg, tts: tts}
}

// Name 节点名。
func (a *AudioSynth) Name() string { return "audio_synth" }

// Run 并发为各镜头合成配音。
func (a *AudioSynth) Run(ctx context.Context, st *models.ProjectState) error {
	logx.Stage("🔊", "音频合成：按角色锁定音色并发配音")

	dir := projectDir(a.cfg, st)
	sem := make(chan struct{}, max1(a.cfg.ShotConcurrency))
	var wg sync.WaitGroup
	errs := make([]error, len(st.Shots))

	for i := range st.Shots {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			errs[idx] = a.synthShot(ctx, st, &st.Shots[idx], dir)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			return fmt.Errorf("镜头[%s]配音失败: %w", st.Shots[i].ID, err)
		}
	}
	logx.Done("全部配音完成")
	return nil
}

// synthShot 为单个镜头合成配音。
func (a *AudioSynth) synthShot(ctx context.Context, st *models.ProjectState, shot *models.Shot, dir string) error {
	if shot.Dialogue == "" {
		return nil // 无对白镜头，留待合成器补静音轨
	}

	// 取主角锁定音色
	voiceID := ""
	if c, ok := st.CharByID(shot.CharID); ok {
		voiceID = c.VoiceID
	}

	audioPath := filepath.Join(dir, "audio", shot.ID+".m4a")
	if !fsx.Exists(audioPath) {
		if err := a.tts.Speak(ctx, shot.Dialogue, voiceID, audioPath); err != nil {
			return fmt.Errorf("配音合成失败: %w", err)
		}
	}
	st.UpdateShot(shot.ID, func(s *models.Shot) { s.AudioPath = audioPath })

	logx.Step("镜头 %s 配音完成（音色 %s）", shot.ID, voiceID)
	return nil
}
