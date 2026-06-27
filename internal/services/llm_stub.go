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
//
// 题材感知：根据 GENRE 选取对应的题材模板（治愈/悬疑/古风/科幻/喜剧/都市…），
// 让离线模式下不同题材也能产出差异化的主题、角色与分镜；未匹配的题材
// 回退到通用模板。idea 始终注入到剧名/梗概，保证与用户创意相关。
type StubLLM struct{}

// NewStubLLM 构造兜底 LLM。
func NewStubLLM() *StubLLM { return &StubLLM{} }

// Complete 依据任务标记返回对应的结构化 JSON。
func (s *StubLLM) Complete(ctx context.Context, system, user string) (string, error) {
	idea := extractField(user, "IDEA:")
	genre := orDefault(extractField(user, "GENRE:"), "都市")
	tpl := pickTemplate(genre)

	switch {
	case strings.Contains(user, "[[TASK:outline]]"):
		return stubOutline(idea, tpl), nil
	case strings.Contains(user, "[[TASK:characters]]"):
		return stubCharacters(tpl), nil
	case strings.Contains(user, "[[TASK:shots]]"):
		return stubShots(tpl), nil
	default:
		return `{"text":"` + jsonEscape(idea) + `"}`, nil
	}
}

func stubOutline(idea string, tpl genreTemplate) string {
	o := map[string]any{
		"title":    truncRunes(idea, 12),
		"logline":  "一个关于「" + truncRunes(idea, 16) + "」的" + tpl.tone + "故事。",
		"theme":    tpl.theme,
		"synopsis": "围绕「" + idea + "」展开：" + tpl.synopsis,
		"beats":    tpl.beats,
	}
	b, _ := json.Marshal(o)
	return string(b)
}

func stubCharacters(tpl genreTemplate) string {
	chars := []map[string]any{
		{
			"id":         "char_lead",
			"name":       tpl.lead.name,
			"persona":    tpl.lead.persona,
			"appearance": tpl.lead.appearance,
			"gender":     tpl.lead.gender,
		},
		{
			"id":         "char_support",
			"name":       tpl.support.name,
			"persona":    tpl.support.persona,
			"appearance": tpl.support.appearance,
			"gender":     tpl.support.gender,
		},
	}
	b, _ := json.Marshal(chars)
	return string(b)
}

func stubShots(tpl genreTemplate) string {
	// 取模板预设的 4 个镜头，覆盖起承转合。
	shots := make([]map[string]any, 0, len(tpl.shots))
	for _, sh := range tpl.shots {
		shots = append(shots, map[string]any{
			"scene_heading":   sh.heading,
			"location":        sh.location,
			"time_of_day":     sh.timeOfDay,
			"char_id":         sh.charID,
			"shot_type":       sh.shotType,
			"camera":          sh.camera,
			"keyframe_prompt": sh.keyframe,
			"dialogue":        sh.dialogue,
		})
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
