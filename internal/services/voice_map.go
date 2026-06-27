package services

// 逻辑音色 → 具体引擎声音的映射。
//
// 角色注册表只分配逻辑音色（male-N/female-N），各 TTS 引擎在此把逻辑音色
// 翻译成自己的具体声音。这层间接让"换引擎不动角色档案、男女声统一抽象"成立。

// sayVoice 描述 macOS say 的一个声音及其男声变调参数。
type sayVoice struct {
	system string  // 系统语音名（say -v）
	pitch  float64 // 变调系数：<1 降调变男声，1=不变调
}

// sayVoiceMap 把逻辑音色映射到 say 声音。
// macOS 无中文男声，故男声用系统女声 + ffmpeg 降调模拟（已实测有效）。
var sayVoiceMap = map[string]sayVoice{
	"female-1": {"Tingting", 1.0},
	"female-2": {"Sinji", 1.0},
	"female-3": {"Meijia", 1.0},
	"male-1":   {"Tingting", 0.82}, // 婷婷降调 → 男声
	"male-2":   {"Sinji", 0.80},    // 善怡降调 → 另一种男声
}

// edgeVoiceMap 把逻辑音色映射到 edge-tts 真人中文声音。
var edgeVoiceMap = map[string]string{
	"female-1": "zh-CN-XiaoxiaoNeural", // 晓晓，温暖
	"female-2": "zh-CN-XiaoyiNeural",   // 晓伊，活泼
	"female-3": "zh-CN-XiaoxiaoNeural",
	"male-1":   "zh-CN-YunxiNeural",   // 云希，阳光
	"male-2":   "zh-CN-YunyangNeural", // 云扬，专业可靠
}

// resolveSayVoice 解析逻辑音色到 say 声音；兼容旧档案里的系统语音名（直接当女声用）。
func resolveSayVoice(voiceID string) sayVoice {
	if v, ok := sayVoiceMap[voiceID]; ok {
		return v
	}
	// 兼容：旧 project.json 里直接存了系统语音名（如 "Tingting"）。
	if voiceID != "" {
		return sayVoice{system: voiceID, pitch: 1.0}
	}
	return sayVoice{system: "Tingting", pitch: 1.0}
}

// resolveEdgeVoice 解析逻辑音色到 edge 声音；未知则回退到一个男/女默认声。
func resolveEdgeVoice(voiceID string) string {
	if v, ok := edgeVoiceMap[voiceID]; ok {
		return v
	}
	if len(voiceID) >= 4 && voiceID[:4] == "male" {
		return "zh-CN-YunxiNeural"
	}
	return "zh-CN-XiaoxiaoNeural"
}
