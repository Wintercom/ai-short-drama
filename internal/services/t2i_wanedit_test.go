package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildRequestText2Image：无参考图 → 文生图模式，content 仅 text、用文生图模型。
func TestBuildRequestText2Image(t *testing.T) {
	w := NewWanEditT2I("sk-x", "qwen-image", "qwen-image-edit-plus", "", nil)
	body, err := w.buildRequest("一位女性角色||appearance:短发", "")
	if err != nil {
		t.Fatal(err)
	}
	if body["model"] != "qwen-image" {
		t.Errorf("无参考图应用文生图模型，实际 %v", body["model"])
	}
	content := extractContent(t, body)
	for _, c := range content {
		if _, hasImg := c["image"]; hasImg {
			t.Error("文生图模式不应含 image content")
		}
	}
}

// TestBuildRequestImageEdit：有参考图 → 图生图模式，content 含 image + text、用编辑模型。
func TestBuildRequestImageEdit(t *testing.T) {
	dir := t.TempDir()
	ref := filepath.Join(dir, "char.png")
	if err := os.WriteFile(ref, []byte("fake-png-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	w := NewWanEditT2I("sk-x", "qwen-image", "qwen-image-edit-plus", "", nil)
	body, err := w.buildRequest("深夜办公室画面||appearance:短发女性||shottype:近景", ref)
	if err != nil {
		t.Fatal(err)
	}
	if body["model"] != "qwen-image-edit-plus" {
		t.Errorf("有参考图应用编辑模型，实际 %v", body["model"])
	}
	content := extractContent(t, body)
	var hasImage, hasText bool
	for _, c := range content {
		if v, ok := c["image"].(string); ok && strings.HasPrefix(v, "data:image/") {
			hasImage = true
		}
		if _, ok := c["text"].(string); ok {
			hasText = true
		}
	}
	if !hasImage {
		t.Error("图生图模式应含 image(data URI) content")
	}
	if !hasText {
		t.Error("图生图模式应含 text 指令")
	}
}

// TestBuildRequestMissingRefFallsBackToText：参考图路径不存在 → 退回文生图，不报错。
func TestBuildRequestMissingRefFallsBackToText(t *testing.T) {
	w := NewWanEditT2I("sk-x", "qwen-image", "qwen-image-edit-plus", "", nil)
	body, err := w.buildRequest("画面||appearance:短发", "/no/such/ref.png")
	if err != nil {
		t.Fatal(err)
	}
	if body["model"] != "qwen-image" {
		t.Errorf("参考图不存在应退回文生图模型，实际 %v", body["model"])
	}
}

// TestParseImageURL：从真实响应结构提取图片 URL。
func TestParseImageURL(t *testing.T) {
	resp := `{"output":{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":[{"image":"https://oss.example.com/a.png?sig=x"}]}}]}}`
	got := parseImageURL([]byte(resp))
	if got != "https://oss.example.com/a.png?sig=x" {
		t.Errorf("URL 解析错误: %q", got)
	}

	// 无图片内容返回空
	if parseImageURL([]byte(`{"output":{"choices":[]}}`)) != "" {
		t.Error("无图片时应返回空")
	}
	// 非法 JSON 返回空、不 panic
	if parseImageURL([]byte("not json")) != "" {
		t.Error("非法 JSON 应返回空")
	}
}

// TestWanEditDefaults：构造器默认值兜底。
func TestWanEditDefaults(t *testing.T) {
	w := NewWanEditT2I("sk-x", "", "", "", nil)
	if w.Model != "qwen-image" {
		t.Errorf("默认文生图模型错误: %q", w.Model)
	}
	if w.EditModel != "qwen-image-edit-plus" {
		t.Errorf("默认编辑模型错误: %q", w.EditModel)
	}
	if w.BaseURL != "https://dashscope.aliyuncs.com" {
		t.Errorf("默认地址错误: %q", w.BaseURL)
	}
	if !w.Available() {
		t.Error("有 Key 应可用")
	}
	if NewWanEditT2I("", "", "", "", nil).Available() {
		t.Error("无 Key 应不可用")
	}
}

// extractContent 取出 messages[0].content（测试辅助）。
func extractContent(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	// 经 JSON round-trip 规整类型，避免 map[string]any 断言层层嵌套出错
	raw, _ := json.Marshal(body)
	var parsed struct {
		Input struct {
			Messages []struct {
				Content []map[string]any `json:"content"`
			} `json:"messages"`
		} `json:"input"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("解析请求体失败: %v", err)
	}
	if len(parsed.Input.Messages) == 0 {
		t.Fatal("请求体缺 messages")
	}
	return parsed.Input.Messages[0].Content
}
