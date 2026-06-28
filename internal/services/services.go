// Package services 是能力服务层：把外部 AI 模型与多媒体工具统一封装为接口。
//
// 设计原则——可插拔：业务智能体只依赖这里的接口，不关心背后是
// DeepSeek 还是 Ollama、是真实视频大模型还是 ffmpeg 占位实现。
// 想换供应商只改 config，不动编排与智能体代码。
package services

import "context"

// LLM 文本生成能力（驱动剧本引擎）。
type LLM interface {
	// Complete 给定系统提示与用户提示，返回模型文本输出。
	Complete(ctx context.Context, system, user string) (string, error)
}

// T2I 文生图能力（生成镜头关键帧）。
type T2I interface {
	// Generate 依据提示词生成图片到 outPath。
	// refImage 为角色锁定参考图（可空），seed 为锁定种子——二者共同保证角色一致性。
	Generate(ctx context.Context, prompt, refImage string, seed int, outPath string) error
}

// I2V 图生视频能力（让关键帧动起来）。
type I2V interface {
	// Animate 把关键帧 keyframe 转为时长 duration 秒的视频片段。
	//   - camera 描述运镜方式（推/拉/摇/移），用于本地 zoompan 运镜；
	//   - motion 描述画面中人物/主体的动作（如"抱拳回礼，神色激动"），
	//     真实 I2V 模型据此驱动肢体与表情动作；本地实现忽略该参数。
	Animate(ctx context.Context, keyframe, camera, motion string, duration float64, outPath string) error
}

// TTS 语音合成能力（角色配音）。
type TTS interface {
	// Speak 把文本合成为语音到 outPath；voiceID 为角色锁定音色，保证配音一致。
	Speak(ctx context.Context, text, voiceID string, outPath string) error
}

// Editor 音视频剪辑能力（后期合成）。
type Editor interface {
	// MuxClipAudio 把无声片段与配音合成为有声片段（音画对齐）。
	MuxClipAudio(ctx context.Context, clip, audio, outPath string) error
	// Concat 按顺序拼接多个片段为最终成片。
	Concat(ctx context.Context, clips []string, outPath string) error
	// SilentAudio 生成指定时长的静音轨（无对白镜头兜底）。
	SilentAudio(ctx context.Context, duration float64, outPath string) error
	// ProbeDuration 探测媒体文件时长（秒），用于音画对齐。
	ProbeDuration(ctx context.Context, path string) (float64, error)
	// FitDuration 把已有视频片段适配到目标时长：过短则冻结末帧补长，
	// 过长则裁剪。纯 ffmpeg 操作（廉价），避免为对齐时长而重复调用昂贵的 I2V。
	FitDuration(ctx context.Context, clip string, target float64, outPath string) error
}

// Bundle 聚合全部能力，便于注入各智能体。
type Bundle struct {
	LLM    LLM
	T2I    T2I
	I2V    I2V
	TTS    TTS
	Editor Editor
}
