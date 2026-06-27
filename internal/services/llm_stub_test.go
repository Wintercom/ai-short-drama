package services

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestPickTemplate 验证题材匹配：精确、模糊包含、未命中回退默认。
func TestPickTemplate(t *testing.T) {
	cases := []struct {
		genre     string
		wantTheme string
	}{
		{"治愈", "勇气与自我和解"},
		{"治愈系", "勇气与自我和解"}, // 模糊包含
		{"悬疑", "真相与抉择"},
		{"古风", "情义与抉择"},
		{"科幻", "人性与边界"},
		{"喜剧", "乐观与成长"},
		{"都市", "选择与成长"},
		{"赛博朋克", "选择与成长"}, // 未命中 → 回退默认（都市）
		{"", "选择与成长"},     // 空 → 回退默认
	}
	for _, c := range cases {
		got := pickTemplate(c.genre).theme
		if got != c.wantTheme {
			t.Errorf("pickTemplate(%q).theme = %q, 期望 %q", c.genre, got, c.wantTheme)
		}
	}
}

// TestStubGenreAware 验证不同题材产出差异化的大纲、角色与分镜。
func TestStubGenreAware(t *testing.T) {
	llm := NewStubLLM()
	ctx := context.Background()

	outlineOf := func(genre string) map[string]any {
		raw, err := llm.Complete(ctx, "", "[[TASK:outline]]\nIDEA: 测试创意\nGENRE: "+genre)
		if err != nil {
			t.Fatalf("genre=%s outline 失败: %v", genre, err)
		}
		var o map[string]any
		if err := json.Unmarshal([]byte(raw), &o); err != nil {
			t.Fatalf("genre=%s 解析失败: %v\n%s", genre, err, raw)
		}
		return o
	}

	heal := outlineOf("治愈")
	susp := outlineOf("悬疑")

	// 不同题材主题应不同
	if heal["theme"] == susp["theme"] {
		t.Errorf("治愈与悬疑主题相同，未体现题材差异：%v", heal["theme"])
	}

	// idea 应注入到剧名（保证与用户创意相关）
	if title, _ := heal["title"].(string); !strings.Contains(title, "测试创意") {
		t.Errorf("剧名未注入 idea：%q", title)
	}

	// 悬疑角色应不同于治愈角色（验证角色差异化）
	charsRaw, _ := llm.Complete(ctx, "", "[[TASK:characters]]\nGENRE: 悬疑")
	if !strings.Contains(charsRaw, "沈宁") {
		t.Errorf("悬疑题材未产出对应角色，实得：%s", charsRaw)
	}

	// 古风分镜应出现古风场景词
	shotsRaw, _ := llm.Complete(ctx, "", "[[TASK:shots]]\nGENRE: 古风")
	if !strings.Contains(shotsRaw, "客栈") && !strings.Contains(shotsRaw, "江畔") {
		t.Errorf("古风题材未产出对应场景，实得：%s", shotsRaw)
	}
}
