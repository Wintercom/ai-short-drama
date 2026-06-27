package services

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/cuiwenyang/ai-short-drama/internal/fsx"
	"github.com/cuiwenyang/ai-short-drama/internal/logx"
)

// EdgeTTS 用微软 edge-tts（免费在线 TTS）合成真人级中文配音，含男声。
//
// edge-tts 是非官方接口，存在限速/间歇失败风险，故内置：
//   - 串行调用（由 audio_synth 在 edge 模式下把并发降为 1 保证）；
//   - 失败重试 + 指数退避；
//   - 首次使用自动 pip --user 安装。
//
// 自身仅负责"尽力生成"，最终的不中断由外层 FallbackTTS 降级到本地 say 保证。
type EdgeTTS struct {
	FFmpeg     string
	Python     string // python3 路径
	EdgeBin    string // edge-tts 可执行路径（空则用 python3 -m edge_tts）
	MaxRetries int
	mu         sync.Mutex // 串行化请求，避免对非官方接口并发触发限速
}

var edgeInstallOnce sync.Once

// NewEdgeTTS 构造 edge-tts 合成器并确保依赖就绪。
func NewEdgeTTS(ffmpeg string) *EdgeTTS {
	e := &EdgeTTS{FFmpeg: ffmpeg, Python: "python3", MaxRetries: 2}
	e.EdgeBin = e.locateOrInstall()
	return e
}

// Available 报告 edge-tts 是否可用。
func (t *EdgeTTS) Available() bool { return t.EdgeBin != "" || t.moduleAvailable() }

// Speak 合成语音到 outPath（.m4a）：edge 生成 mp3 → ffmpeg 转 aac，失败重试退避。
// 内部串行化：即便 audio_synth 多 goroutine 并发调用，对 edge 接口仍是串行请求。
func (t *EdgeTTS) Speak(ctx context.Context, text, voiceID, outPath string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := fsx.EnsureDir(filepath.Dir(outPath)); err != nil {
		return err
	}
	voice := resolveEdgeVoice(voiceID)
	mp3 := outPath + ".mp3"
	defer os.Remove(mp3)

	var lastErr error
	for attempt := 0; attempt <= t.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt) * time.Second // 1s, 2s...
			logx.Warn("edge-tts 第 %d 次重试（退避 %s）：%v", attempt, backoff, lastErr)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if err := t.runEdge(ctx, text, voice, mp3); err != nil {
			lastErr = err
			continue
		}
		if !fsx.Exists(mp3) {
			lastErr = fmt.Errorf("edge-tts 未产出音频")
			continue
		}
		// 转 aac
		conv := exec.CommandContext(ctx, t.FFmpeg, "-y", "-hide_banner", "-loglevel", "error",
			"-i", mp3, "-c:a", "aac", outPath)
		if out, err := conv.CombinedOutput(); err != nil {
			lastErr = fmt.Errorf("edge 配音转码失败: %w\n%s", err, tail(string(out), 200))
			continue
		}
		return nil
	}
	return fmt.Errorf("edge-tts 合成失败（已重试 %d 次）: %w", t.MaxRetries, lastErr)
}

// runEdge 执行一次 edge-tts 生成。
func (t *EdgeTTS) runEdge(ctx context.Context, text, voice, mp3 string) error {
	args := t.baseArgs()
	args = append(args, "--voice", voice, "--text", text, "--write-media", mp3)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w\n%s", err, tail(string(out), 200))
	}
	return nil
}

// baseArgs 返回调用 edge-tts 的基础命令（可执行优先，否则 python3 -m）。
func (t *EdgeTTS) baseArgs() []string {
	if t.EdgeBin != "" {
		return []string{t.EdgeBin}
	}
	return []string{t.Python, "-m", "edge_tts"}
}

// locateOrInstall 定位 edge-tts，缺失则自动用户级安装（仅尝试一次）。
func (t *EdgeTTS) locateOrInstall() string {
	if p, err := exec.LookPath("edge-tts"); err == nil {
		return p
	}
	// 常见用户级安装路径
	for _, p := range edgeUserPaths() {
		if fsx.Exists(p) {
			return p
		}
	}
	if t.moduleAvailable() {
		return "" // 用 python3 -m edge_tts
	}
	// 自动安装（用户已确认的策略）
	edgeInstallOnce.Do(func() {
		logx.Info("TTS：未检测到 edge-tts，正在自动安装（pip install --user edge-tts）……")
		cmd := exec.Command(t.Python, "-m", "pip", "install", "--user", "--quiet", "edge-tts")
		if out, err := cmd.CombinedOutput(); err != nil {
			logx.Warn("edge-tts 自动安装失败：%v\n%s", err, tail(string(out), 200))
		} else {
			logx.Done("edge-tts 安装完成")
		}
	})
	if p, err := exec.LookPath("edge-tts"); err == nil {
		return p
	}
	for _, p := range edgeUserPaths() {
		if fsx.Exists(p) {
			return p
		}
	}
	return ""
}

// moduleAvailable 检查 python3 -m edge_tts 是否可用。
func (t *EdgeTTS) moduleAvailable() bool {
	cmd := exec.Command(t.Python, "-c", "import edge_tts")
	return cmd.Run() == nil
}

// edgeUserPaths 返回 edge-tts 可能的用户级安装路径。
func edgeUserPaths() []string {
	home, _ := os.UserHomeDir()
	var paths []string
	for _, ver := range []string{"3.13", "3.12", "3.11", "3.10", "3.9"} {
		paths = append(paths, filepath.Join(home, "Library/Python", ver, "bin/edge-tts"))
	}
	paths = append(paths, filepath.Join(home, ".local/bin/edge-tts"))
	return paths
}
