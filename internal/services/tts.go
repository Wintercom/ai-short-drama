package services

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/cuiwenyang/ai-short-drama/internal/fsx"
)

// SayTTS 用 macOS 内置 `say` 命令做语音合成（零成本、离线）。
// voiceID 映射到系统语音名（如「Tingting」中文女声），保证角色音色一致。
// 流程：say 生成 AIFF → ffmpeg 转 AAC（统一容器，便于后续合成）。
type SayTTS struct {
	FFmpeg string
}

// NewSayTTS 构造 say 语音合成器。
func NewSayTTS(ffmpeg string) *SayTTS { return &SayTTS{FFmpeg: ffmpeg} }

// Speak 合成语音到 outPath（.m4a）。
func (t *SayTTS) Speak(ctx context.Context, text, voiceID, outPath string) error {
	if err := fsx.EnsureDir(filepath.Dir(outPath)); err != nil {
		return err
	}

	aiff := outPath + ".aiff"
	defer os.Remove(aiff)

	// 1) say 合成 AIFF
	args := []string{}
	if voiceID != "" {
		args = append(args, "-v", voiceID)
	}
	args = append(args, "-o", aiff, text)
	cmd := exec.CommandContext(ctx, "say", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("say 合成失败: %w\n%s", err, tail(string(out), 300))
	}

	// 2) ffmpeg 转 AAC
	conv := exec.CommandContext(ctx, t.FFmpeg, "-y", "-hide_banner", "-loglevel", "error",
		"-i", aiff, "-c:a", "aac", outPath)
	if out, err := conv.CombinedOutput(); err != nil {
		return fmt.Errorf("配音转码失败: %w\n%s", err, tail(string(out), 300))
	}
	return nil
}

// SilentTTS 是跨平台兜底：不依赖 say，直接生成等长静音轨。
// 当无对白或非 macOS 环境时使用，保证流程不中断。
type SilentTTS struct {
	Editor *FFmpegEditor
}

// NewSilentTTS 构造静音兜底 TTS。
func NewSilentTTS(e *FFmpegEditor) *SilentTTS { return &SilentTTS{Editor: e} }

// Speak 按文本长度估算时长，生成对应静音轨。
func (t *SilentTTS) Speak(ctx context.Context, text, voiceID, outPath string) error {
	// 按中文约每秒 5 字估算配音时长，最少 1.5 秒。
	dur := float64(len([]rune(text))) / 5.0
	if dur < 1.5 {
		dur = 1.5
	}
	return t.Editor.SilentAudio(ctx, dur, outPath)
}
