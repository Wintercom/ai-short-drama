package services

import (
	"strings"
	"testing"
)

// TestParsePromptAppearance 验证编码串能解出角色外貌锚点。
func TestParsePromptAppearance(t *testing.T) {
	in := "深夜办公室，林夏对着屏幕||gender:female||appearance:二十多岁女性，短发，简约风衣||dialogue:还要改到几点||scene:办公室-夜-内||shottype:近景"
	m := parsePrompt(in)

	if m.description != "深夜办公室，林夏对着屏幕" {
		t.Errorf("description 解析错误: %q", m.description)
	}
	if m.appearance != "二十多岁女性，短发，简约风衣" {
		t.Errorf("appearance 解析错误: %q", m.appearance)
	}
	if m.gender != "female" {
		t.Errorf("gender 解析错误: %q", m.gender)
	}
	if m.shotType != "近景" {
		t.Errorf("shotType 解析错误: %q", m.shotType)
	}
}

// TestParsePromptNoAppearance 验证缺 appearance 时为空、不报错。
func TestParsePromptNoAppearance(t *testing.T) {
	m := parsePrompt("纯画面描述")
	if m.description != "纯画面描述" || m.appearance != "" {
		t.Errorf("无 appearance 时解析异常: desc=%q app=%q", m.description, m.appearance)
	}
}

// TestBuildAPIPromptLocksConsistency 验证 API prompt 包含外貌锚点 + 统一画风后缀，
// 且外貌出现在画面描述之后、画风后缀之前（靠前 token 权重更高）。
func TestBuildAPIPromptLocksConsistency(t *testing.T) {
	tt := &PollinationsT2I{Width: 1280, Height: 720}
	meta := shotMeta{
		description: "office at night",
		appearance:  "young woman, short hair, trench coat",
		gender:      "female",
		scene:       "办公室-夜-内",
		shotType:    "近景",
	}
	got := tt.buildAPIPrompt(meta)

	// 必须含外貌锚点
	if !strings.Contains(got, "young woman, short hair, trench coat") {
		t.Errorf("API prompt 缺少角色外貌锚点：%s", got)
	}
	// 必须含统一画风后缀
	if !strings.Contains(got, consistentStyleSuffix) {
		t.Errorf("API prompt 缺少统一画风后缀：%s", got)
	}
	// 外貌应在画风后缀之前（前置锚定）
	iApp := strings.Index(got, "young woman")
	iStyle := strings.Index(got, consistentStyleSuffix)
	if iApp < 0 || iStyle < 0 || iApp > iStyle {
		t.Errorf("外貌锚点应排在画风后缀之前：app@%d style@%d", iApp, iStyle)
	}
}

// TestBuildAPIPromptStyleStableAcrossShots 验证不同镜头共用同一固定画风后缀
// （这是跨镜头画风一致的基础）。
func TestBuildAPIPromptStyleStableAcrossShots(t *testing.T) {
	tt := &PollinationsT2I{Width: 1280, Height: 720}
	shotA := tt.buildAPIPrompt(shotMeta{description: "office", appearance: "woman A", shotType: "近景"})
	shotB := tt.buildAPIPrompt(shotMeta{description: "studio", appearance: "woman A", shotType: "全景"})

	// 两个镜头都应以同一画风后缀结尾
	if !strings.HasSuffix(shotA, consistentStyleSuffix) || !strings.HasSuffix(shotB, consistentStyleSuffix) {
		t.Error("不同镜头未共用同一固定画风后缀")
	}
}
