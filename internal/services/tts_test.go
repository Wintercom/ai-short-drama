package services

import (
	"context"
	"fmt"
	"testing"
)

// TestResolveSayVoice 验证逻辑音色→say 声音映射：女声不变调，男声降调，旧名兼容。
func TestResolveSayVoice(t *testing.T) {
	if v := resolveSayVoice("female-1"); v.system != "Tingting" || v.pitch != 1.0 {
		t.Errorf("female-1 映射错误：%+v", v)
	}
	if v := resolveSayVoice("male-1"); v.pitch >= 1.0 {
		t.Errorf("male-1 应降调（pitch<1），实得 %+v", v)
	}
	// 兼容旧档案里的系统语音名
	if v := resolveSayVoice("Tingting"); v.system != "Tingting" || v.pitch != 1.0 {
		t.Errorf("旧语音名兼容失败：%+v", v)
	}
}

// TestResolveEdgeVoice 验证逻辑音色→edge 声音映射：男声为男声、女声为女声、未知回退。
func TestResolveEdgeVoice(t *testing.T) {
	if v := resolveEdgeVoice("male-1"); v != "zh-CN-YunxiNeural" {
		t.Errorf("male-1 应映射云希，实得 %s", v)
	}
	if v := resolveEdgeVoice("female-1"); v != "zh-CN-XiaoxiaoNeural" {
		t.Errorf("female-1 应映射晓晓，实得 %s", v)
	}
	if v := resolveEdgeVoice("male-9"); v != "zh-CN-YunxiNeural" {
		t.Errorf("未知男声应回退男声，实得 %s", v)
	}
}

// stubTTS 是测试用桩：可配置成功或失败，并记录是否被调用。
type stubTTS struct {
	fail   bool
	called bool
}

func (s *stubTTS) Speak(ctx context.Context, text, voiceID, outPath string) error {
	s.called = true
	if s.fail {
		return fmt.Errorf("stub 故意失败")
	}
	return nil
}

// TestFallbackPrimarySuccess 验证主引擎成功时不触发备引擎。
func TestFallbackPrimarySuccess(t *testing.T) {
	primary := &stubTTS{fail: false}
	backup := &stubTTS{fail: false}
	f := NewFallbackTTS(primary, backup, "test")

	if err := f.Speak(context.Background(), "hi", "male-1", "/tmp/x.m4a"); err != nil {
		t.Fatalf("主成功不应报错：%v", err)
	}
	if !primary.called {
		t.Error("主引擎未被调用")
	}
	if backup.called {
		t.Error("主成功时不应调用备引擎")
	}
}

// TestFallbackDegrades 验证主引擎失败时自动降级到备引擎（流程不中断）。
func TestFallbackDegrades(t *testing.T) {
	primary := &stubTTS{fail: true}
	backup := &stubTTS{fail: false}
	f := NewFallbackTTS(primary, backup, "test")

	if err := f.Speak(context.Background(), "hi", "male-1", "/tmp/x.m4a"); err != nil {
		t.Fatalf("备引擎成功时整体应成功：%v", err)
	}
	if !primary.called || !backup.called {
		t.Errorf("应先试主再降级到备：primary=%v backup=%v", primary.called, backup.called)
	}
}
