package services

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/cuiwenyang/ai-short-drama/internal/fsx"
	"github.com/cuiwenyang/ai-short-drama/internal/logx"
)

// SayTTS 用 macOS 内置 `say` 命令做语音合成（零成本、离线）。
//
// voiceID 为逻辑音色（male-N/female-N）：女声直接用系统女声；男声因 macOS
// 无中文男声，用系统女声 + ffmpeg 降调变声模拟（已实测有效、低沉可辨）。
// 流程：say 生成 AIFF →（男声则降调）→ ffmpeg 转 AAC。
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
	v := resolveSayVoice(voiceID)

	aiff := outPath + ".aiff"
	defer os.Remove(aiff)

	// 1) say 合成 AIFF（用系统女声）
	args := []string{}
	if v.system != "" {
		args = append(args, "-v", v.system)
	}
	args = append(args, "-o", aiff, text)
	cmd := exec.CommandContext(ctx, "say", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("say 合成失败: %w\n%s", err, tail(string(out), 300))
	}

	// 2) ffmpeg 转 AAC；男声（pitch<1）施加降调变声滤镜。
	convArgs := []string{"-y", "-hide_banner", "-loglevel", "error", "-i", aiff}
	if v.pitch > 0 && v.pitch < 1.0 {
		// asetrate 降调使音色低沉（男声），atempo 补回时长保持语速不变。
		af := fmt.Sprintf("asetrate=44100*%.3f,aresample=44100,atempo=%.3f", v.pitch, 1.0/v.pitch)
		convArgs = append(convArgs, "-af", af)
	}
	convArgs = append(convArgs, "-c:a", "aac", outPath)
	conv := exec.CommandContext(ctx, t.FFmpeg, convArgs...)
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

// FallbackTTS 包装"主 + 备"两个 TTS：主失败时自动降级到备，保证配音绝不中断。
//
// 典型用法（edge 模式）：主 = EdgeTTS（真人男声，但可能限速），
// 备 = SayTTS（本地变调男声，永远可用）。这把"要真男声"与"流程不中断"
// 两个目标解耦：能联网就用真人声，失败就无缝回落本地。
type FallbackTTS struct {
	Primary TTS
	Backup  TTS
	label   string // 主引擎名，用于日志
}

// NewFallbackTTS 构造降级包装。
func NewFallbackTTS(primary, backup TTS, label string) *FallbackTTS {
	return &FallbackTTS{Primary: primary, Backup: backup, label: label}
}

// Speak 先试主引擎，失败则降级到备引擎。
func (t *FallbackTTS) Speak(ctx context.Context, text, voiceID, outPath string) error {
	if err := t.Primary.Speak(ctx, text, voiceID, outPath); err != nil {
		logx.Warn("%s 配音失败，降级到本地兜底：%v", t.label, err)
		return t.Backup.Speak(ctx, text, voiceID, outPath)
	}
	return nil
}
