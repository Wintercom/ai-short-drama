package services

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// TestSayTTSConsistentSampleRate 验证男声（变调）与女声（原声）输出采样率一致。
//
// 回归测试：此前男声变调滤镜硬编码 asetrate=44100，使男声输出 44100Hz，
// 而女声为 22050Hz；concat demuxer 拼接采样率不一致的片段时会丢音，
// 导致男角色（如陈默）在成片中没有声音。修复后两者必须一致。
func TestSayTTSConsistentSampleRate(t *testing.T) {
	if !hasBin("say") || !hasBin("ffmpeg") {
		t.Skip("缺少 say 或 ffmpeg，跳过")
	}
	tts := NewSayTTS("ffmpeg")
	ctx := context.Background()

	femalePath := t.TempDir() + "/f.m4a"
	malePath := t.TempDir() + "/m.m4a"
	if err := tts.Speak(ctx, "测试一句话", "female-1", femalePath); err != nil {
		t.Fatalf("女声合成失败: %v", err)
	}
	if err := tts.Speak(ctx, "测试一句话", "male-1", malePath); err != nil {
		t.Fatalf("男声合成失败: %v", err)
	}

	fRate := sampleRate(t, femalePath)
	mRate := sampleRate(t, malePath)
	if fRate != mRate {
		t.Errorf("男女声采样率不一致会导致拼接丢音：女=%s 男=%s", fRate, mRate)
	}
	if vol := meanVolume(t, malePath); vol <= -80 {
		t.Errorf("男声变调后疑似静音：mean_volume=%.1fdB", vol)
	}
}

func hasBin(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func sampleRate(t *testing.T, path string) string {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "a",
		"-show_entries", "stream=sample_rate", "-of", "csv=p=0", path).Output()
	if err != nil {
		t.Fatalf("ffprobe 采样率失败: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func meanVolume(t *testing.T, path string) float64 {
	t.Helper()
	out, _ := exec.Command("ffmpeg", "-hide_banner", "-i", path,
		"-af", "volumedetect", "-f", "null", "-").CombinedOutput()
	for _, line := range strings.Split(string(out), "\n") {
		if i := strings.Index(line, "mean_volume:"); i >= 0 {
			var v float64
			if _, err := fmt.Sscanf(strings.TrimSpace(line[i+len("mean_volume:"):]), "%f", &v); err == nil {
				return v
			}
		}
	}
	return -91
}
