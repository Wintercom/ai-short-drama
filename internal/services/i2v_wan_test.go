package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildMotionPrompt 验证运镜与人物动作的提示词组合。
func TestBuildMotionPrompt(t *testing.T) {
	cases := []struct {
		name   string
		camera string
		motion string
		want   string
	}{
		{"动作+运镜", "推", "抱拳回礼，神色激动", "抱拳回礼，神色激动，镜头缓缓推近"},
		{"仅动作", "固定", "缓缓抬头", "缓缓抬头"},         // 固定无运镜词
		{"仅运镜", "拉", "", "镜头缓缓拉远"},            // 无动作
		{"都为空", "固定", "", ""},                 // 都空
		{"动作含空格", "摇", "  挥手  ", "挥手，镜头横向摇移"}, // 两端空格被裁
	}
	for _, c := range cases {
		got := buildMotionPrompt(c.camera, c.motion)
		if got != c.want {
			t.Errorf("%s: buildMotionPrompt(%q,%q)=%q，期望 %q", c.name, c.camera, c.motion, got, c.want)
		}
	}
}

// TestWanDuration 验证时长归一到模型支持的离散秒数。
func TestWanDuration(t *testing.T) {
	cases := []struct {
		in   float64
		want int
	}{
		{0, 5}, {-1, 5}, {2.3, 5}, {5, 5}, {8.9, 5},
	}
	for _, c := range cases {
		if got := wanDuration(c.in); got != c.want {
			t.Errorf("wanDuration(%.1f)=%d，期望 %d", c.in, got, c.want)
		}
	}
}

// TestEncodeImageDataURI 验证本地图片编码为 data URI，且 MIME 随扩展名变化。
func TestEncodeImageDataURI(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		ext      string
		wantMime string
	}{
		{".png", "data:image/png;base64,"},
		{".jpg", "data:image/jpeg;base64,"},
		{".jpeg", "data:image/jpeg;base64,"},
		{".webp", "data:image/webp;base64,"},
	}
	for _, c := range cases {
		p := filepath.Join(dir, "img"+c.ext)
		if err := os.WriteFile(p, []byte("fake-image-bytes"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := encodeImageDataURI(p)
		if err != nil {
			t.Fatalf("encodeImageDataURI(%s) 报错: %v", c.ext, err)
		}
		if !strings.HasPrefix(got, c.wantMime) {
			n := len(got)
			if n > 30 {
				n = 30
			}
			t.Errorf("扩展名 %s：前缀=%q，期望以 %q 开头", c.ext, got[:n], c.wantMime)
		}
	}

	// 文件不存在应返回错误
	if _, err := encodeImageDataURI(filepath.Join(dir, "nope.png")); err == nil {
		t.Error("不存在的文件应返回错误")
	}
}

// TestWanI2VFallbackWhenNoKey 验证缺 Key 时 Available 报告不可用。
func TestWanI2VAvailable(t *testing.T) {
	withKey := NewWanI2V("sk-xxx", "", "", "ffmpeg", 1280, 720, 25)
	if !withKey.Available() {
		t.Error("有 Key 时应可用")
	}
	noKey := NewWanI2V("", "", "", "ffmpeg", 1280, 720, 25)
	if noKey.Available() {
		t.Error("无 Key 时应不可用")
	}
	// 默认模型与地址兜底
	if withKey.Model != "wan2.2-i2v-plus" {
		t.Errorf("默认模型应为 wan2.2-i2v-plus，实际 %q", withKey.Model)
	}
	if withKey.BaseURL != "https://dashscope.aliyuncs.com" {
		t.Errorf("默认地址兜底错误：%q", withKey.BaseURL)
	}
}
