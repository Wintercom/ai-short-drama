package services

import (
	"github.com/cuiwenyang/ai-short-drama/internal/config"
	"github.com/cuiwenyang/ai-short-drama/internal/fsx"
	"github.com/cuiwenyang/ai-short-drama/internal/logx"
)

// Build 依据配置组装能力服务集合（可插拔的核心：只看 config 决定用哪个实现）。
func Build(cfg *config.Config) *Bundle {
	editor := NewFFmpegEditor(cfg.FFmpegBin, cfg.FFprobeBin, cfg.VideoWidth, cfg.VideoHeight, cfg.VideoFPS)

	return &Bundle{
		LLM:    buildLLM(cfg),
		T2I:    NewLocalT2I(cfg.FFmpegBin, cfg.VideoWidth, cfg.VideoHeight),
		I2V:    NewLocalI2V(cfg.FFmpegBin, cfg.VideoWidth, cfg.VideoHeight, cfg.VideoFPS),
		TTS:    buildTTS(cfg, editor),
		Editor: editor,
	}
}

// buildLLM 选择 LLM 实现：有 key 用 OpenAI 兼容端点，否则用离线 Stub。
func buildLLM(cfg *config.Config) LLM {
	if cfg.LLMProvider == "openai-compatible" && cfg.LLMAPIKey != "" {
		logx.Info("LLM：使用 OpenAI 兼容端点 %s（%s）", cfg.LLMBaseURL, cfg.LLMModel)
		return NewOpenAILLM(cfg.LLMBaseURL, cfg.LLMModel, cfg.LLMAPIKey)
	}
	logx.Info("LLM：使用内置离线 Stub（零成本，可设置 LLM_API_KEY 切换真实模型）")
	return NewStubLLM()
}

// buildTTS 选择 TTS 实现：say 可用且配置为 say 时用系统语音，否则静音兜底。
func buildTTS(cfg *config.Config, editor *FFmpegEditor) TTS {
	if cfg.TTSProvider == "say" && fsx.HasBinary("say") {
		logx.Info("TTS：使用 macOS say 系统语音")
		return NewSayTTS(cfg.FFmpegBin)
	}
	logx.Info("TTS：使用静音兜底轨（无 say 或已禁用）")
	return NewSilentTTS(editor)
}
