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
		T2I:    buildT2I(cfg),
		I2V:    buildI2V(cfg),
		TTS:    buildTTS(cfg, editor),
		Editor: editor,
	}
}

// buildT2I 选择 T2I 实现。
//   - pollinations：Pollinations.AI 免费在线 API（真实图片，无需 Key），本地 SVG 兜底；
//   - local（默认）：SVG 矢量人物剪影（零成本、离线）。
func buildT2I(cfg *config.Config) T2I {
	if cfg.T2IProvider == "pollinations" {
		logx.Info("T2I：使用 Pollinations AI（免费在线真实图片），本地 SVG 兜底")
		return NewPollinationsT2I(cfg.FFmpegBin, cfg.VideoWidth, cfg.VideoHeight)
	}
	logx.Info("T2I：使用本地 SVG 关键帧（人物剪影 + 场景背景，零成本离线）")
	return NewLocalT2I(cfg.FFmpegBin, cfg.VideoWidth, cfg.VideoHeight)
}

// buildI2V 选择 I2V 实现。
//   - wan：通义万相图生视频（真人级动作，关键帧作首帧保角色一致），本地 zoompan 兜底；
//     缺 API Key 时自动回退 local（保证零配置仍可离线跑通）。
//   - local（默认）：ffmpeg zoompan 运镜（推/拉/摇/移，仅镜头运动，人物静止）。
func buildI2V(cfg *config.Config) I2V {
	if cfg.I2VProvider == "wan" {
		if cfg.I2VAPIKey != "" {
			logx.Info("I2V：使用通义万相图生视频（真人级动作），本地运镜兜底")
			return NewWanI2V(cfg.I2VAPIKey, cfg.I2VModel, cfg.I2VBaseURL,
				cfg.FFmpegBin, cfg.VideoWidth, cfg.VideoHeight, cfg.VideoFPS)
		}
		logx.Warn("I2V_PROVIDER=wan 但未配置 I2V_API_KEY，降级到本地运镜")
	}
	logx.Info("I2V：使用本地 ffmpeg 运镜（推/拉/摇/移，零成本离线）")
	return NewLocalI2V(cfg.FFmpegBin, cfg.VideoWidth, cfg.VideoHeight, cfg.VideoFPS)
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

// buildTTS 选择 TTS 实现。
//   - edge：真人级在线男/女声，包装本地 say 兜底（限速/断网自动降级，流程不中断）；
//   - say：macOS 系统女声 + 男声变调（离线零成本）；
//   - 其他/不可用：静音兜底。
func buildTTS(cfg *config.Config, editor *FFmpegEditor) TTS {
	switch cfg.TTSProvider {
	case "edge":
		edge := NewEdgeTTS(cfg.FFmpegBin)
		if edge.Available() && fsx.HasBinary("say") {
			logx.Info("TTS：使用 edge-tts 真人配音（含男声），本地 say 变调兜底")
			return NewFallbackTTS(edge, NewSayTTS(cfg.FFmpegBin), "edge-tts")
		}
		if edge.Available() {
			logx.Info("TTS：使用 edge-tts 真人配音（含男声），静音兜底")
			return NewFallbackTTS(edge, NewSilentTTS(editor), "edge-tts")
		}
		logx.Warn("edge-tts 不可用，降级到本地 say")
		fallthrough
	case "say":
		if fsx.HasBinary("say") {
			logx.Info("TTS：使用 macOS say（女声原声 + 男声变调）")
			return NewSayTTS(cfg.FFmpegBin)
		}
		fallthrough
	default:
		logx.Info("TTS：使用静音兜底轨（无 say 或已禁用）")
		return NewSilentTTS(editor)
	}
}
