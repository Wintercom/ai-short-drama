package agents

import (
	"testing"

	"github.com/cuiwenyang/ai-short-drama/internal/models"
)

// TestJSONBlock 验证从 LLM 文本输出中提取 JSON 的容错能力。
// 真实 LLM 常夹带 ```json 围栏或解释文字，必须能稳定抽取，否则解析失败。
func TestJSONBlock(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"纯JSON对象", `{"a":1}`, `{"a":1}`},
		{"代码块围栏", "```json\n{\"a\":1}\n```", `{"a":1}`},
		{"夹带前后文字", "这是结果：\n{\"a\":1}\n以上。", `{"a":1}`},
		{"JSON数组", `[{"x":1}]`, `[{"x":1}]`},
		{"数组带围栏", "```\n[1,2,3]\n```", `[1,2,3]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := jsonBlock(c.in); got != c.want {
				t.Errorf("jsonBlock(%q) = %q, 期望 %q", c.in, got, c.want)
			}
		})
	}
}

// TestAssembleScenes 验证分镜组装：场景去重、非法角色回退、镜号递增。
func TestAssembleScenes(t *testing.T) {
	chars := []models.Character{{ID: "char_lead", Name: "林夏"}, {ID: "char_b", Name: "陈默"}}
	dtos := []shotDTO{
		{SceneHeading: "咖啡馆-日-内", CharID: "char_lead", KeyframePrompt: "p1"},
		{SceneHeading: "咖啡馆-日-内", CharID: "char_b", KeyframePrompt: "p2"}, // 同场景
		{SceneHeading: "天台-夜-外", CharID: "不存在的角色", KeyframePrompt: "p3"},  // 非法角色
	}

	scenes, shots := assembleScenes(dtos, chars)

	if len(scenes) != 2 {
		t.Fatalf("场景去重失败：期望 2 场，实得 %d 场", len(scenes))
	}
	if len(shots) != 3 {
		t.Fatalf("镜头数错误：期望 3 镜，实得 %d 镜", len(shots))
	}
	// 前两镜应归入同一场景
	if shots[0].SceneID != shots[1].SceneID {
		t.Errorf("相同 heading 未归入同一场景：%s vs %s", shots[0].SceneID, shots[1].SceneID)
	}
	// 非法角色应回退到首个角色
	if shots[2].CharID != "char_lead" {
		t.Errorf("非法角色未回退：实得 %s，期望 char_lead", shots[2].CharID)
	}
	// 镜号递增
	for i, s := range shots {
		if s.Index != i+1 {
			t.Errorf("镜号错误：第 %d 镜 Index=%d", i, s.Index)
		}
	}
}
