package services

import (
	"context"
	"encoding/json"
	"strings"
)

// StubLLM 是离线、免费、确定性的 LLM 兜底实现。
//
// 不需要任何 API key 即可让剧本引擎产出结构化剧本，从而保证
// "文本→视频"闭环在零成本、可离线环境下也能完整跑通。
// 它通过识别提示词中的 [[TASK:xxx]] 标记决定返回哪种 JSON。
// 真实 LLM 会忽略该标记、按自然语言指令输出，因此二者可无缝互换。
type StubLLM struct{}

// NewStubLLM 构造兜底 LLM。
func NewStubLLM() *StubLLM { return &StubLLM{} }

// Complete 依据任务标记返回对应的结构化 JSON。
func (s *StubLLM) Complete(ctx context.Context, system, user string) (string, error) {
	idea := extractField(user, "IDEA:")
	genre := orDefault(extractField(user, "GENRE:"), "都市")

	switch {
	case strings.Contains(user, "[[TASK:outline]]"):
		return stubOutline(idea, genre), nil
	case strings.Contains(user, "[[TASK:characters]]"):
		return stubCharacters(idea), nil
	case strings.Contains(user, "[[TASK:shots]]"):
		return stubShots(idea), nil
	default:
		return `{"text":"` + jsonEscape(idea) + `"}`, nil
	}
}

func stubOutline(idea, genre string) string {
	o := map[string]any{
		"title":    truncRunes(idea, 12),
		"logline":  "一个关于「" + truncRunes(idea, 16) + "」的短故事。",
		"theme":    "选择与成长",
		"synopsis": "围绕「" + idea + "」展开：主角面临困境，在关键抉择中直面内心，最终完成转变。",
		"beats": []string{
			"开场：主角的日常被打破",
			"冲突：意外事件迫使主角行动",
			"高潮：直面最大的阻碍",
			"结局：主角做出改变并收获成长",
		},
	}
	b, _ := json.Marshal(o)
	return string(b)
}

func stubCharacters(idea string) string {
	chars := []map[string]any{
		{
			"id":         "char_lead",
			"name":       "林夏",
			"persona":    "坚韧而敏感的年轻人，遇事容易自我怀疑但最终勇敢",
			"appearance": "二十多岁女性，短发，简约风衣，神情坚定",
		},
		{
			"id":         "char_support",
			"name":       "陈默",
			"persona":    "沉稳的引路人，话不多但总在关键时刻点醒主角",
			"appearance": "三十多岁男性，眼镜，深色大衣，气质温和",
		},
	}
	b, _ := json.Marshal(chars)
	return string(b)
}

func stubShots(idea string) string {
	// 与 outline 的节拍呼应，生成 4 个镜头，覆盖起承转合。
	shots := []map[string]any{
		{
			"scene_heading": "城市街头-日-外", "location": "城市街头", "time_of_day": "日",
			"char_id": "char_lead", "shot_type": "全景", "camera": "推",
			"keyframe_prompt": "清晨的城市街头，主角独自走在人流中，神情若有所思",
			"dialogue":        "又是一个看似普通的早晨，但我知道，有些事情该改变了。",
		},
		{
			"scene_heading": "咖啡馆-日-内", "location": "咖啡馆", "time_of_day": "日",
			"char_id": "char_support", "shot_type": "中景", "camera": "固定",
			"keyframe_prompt": "安静的咖啡馆里，引路人放下咖啡杯，目光看向主角",
			"dialogue":        "你一直在等一个完美的时机，可时机从不会自己到来。",
		},
		{
			"scene_heading": "天台-黄昏-外", "location": "天台", "time_of_day": "黄昏",
			"char_id": "char_lead", "shot_type": "近景", "camera": "摇",
			"keyframe_prompt": "黄昏天台，主角凭栏远望，晚霞映在脸上，眼神逐渐坚定",
			"dialogue":        "如果连我自己都不相信，那还有谁会相信我呢？",
		},
		{
			"scene_heading": "城市街头-夜-外", "location": "城市街头", "time_of_day": "夜",
			"char_id": "char_lead", "shot_type": "特写", "camera": "拉",
			"keyframe_prompt": "夜晚霓虹下，主角迈步向前，嘴角扬起释然的微笑",
			"dialogue":        "这一次，我选择向前走。",
		},
	}
	b, _ := json.Marshal(shots)
	return string(b)
}

// —— 小工具 ——

func extractField(s, key string) string {
	idx := strings.Index(s, key)
	if idx < 0 {
		return ""
	}
	rest := s[idx+len(key):]
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[:nl]
	}
	return strings.TrimSpace(rest)
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func truncRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) == 0 {
		return "无题"
	}
	if len(r) > n {
		return string(r[:n])
	}
	return string(r)
}

func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return strings.Trim(string(b), `"`)
}
