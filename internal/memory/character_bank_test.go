package memory

import (
	"testing"

	"github.com/cuiwenyang/ai-short-drama/internal/models"
)

// TestLockDeterministic 验证角色一致性锁定的确定性与稳定性：
// 同一角色多次锁定得到相同 seed（保证跨镜头、跨次运行画风一致）。
func TestLockDeterministic(t *testing.T) {
	bank := NewCharacterBank()

	c1 := models.Character{ID: "char_lead", Name: "林夏"}
	c2 := models.Character{ID: "char_lead", Name: "林夏"}
	bank.Lock(&c1, 0)
	bank.Lock(&c2, 0)

	if c1.Seed != c2.Seed {
		t.Errorf("同一角色 seed 不稳定：%d vs %d", c1.Seed, c2.Seed)
	}
	if c1.Seed == 0 {
		t.Error("seed 未被分配")
	}
	if c1.VoiceID == "" {
		t.Error("音色未被分配")
	}
}

// TestLockVoiceRotation 验证不同角色分配到不同音色（音色区分）。
func TestLockVoiceRotation(t *testing.T) {
	bank := NewCharacterBank()
	a := models.Character{ID: "char_a"}
	b := models.Character{ID: "char_b"}
	bank.Lock(&a, 0)
	bank.Lock(&b, 1)

	if a.VoiceID == b.VoiceID {
		t.Errorf("不同角色应分配不同音色，却都是 %s", a.VoiceID)
	}
}

// TestLockPreservesExisting 验证已锁定的值不被覆盖（续跑时保持一致）。
func TestLockPreservesExisting(t *testing.T) {
	bank := NewCharacterBank()
	c := models.Character{ID: "char_lead", Seed: 12345, VoiceID: "CustomVoice"}
	bank.Lock(&c, 0)

	if c.Seed != 12345 || c.VoiceID != "CustomVoice" {
		t.Errorf("已有锁定值被覆盖：seed=%d voice=%s", c.Seed, c.VoiceID)
	}
}

// TestLockGenderVoice 验证按性别分配逻辑音色：男角色拿 male-*，女角色拿 female-*。
func TestLockGenderVoice(t *testing.T) {
	bank := NewCharacterBank()
	male := models.Character{ID: "c1", Name: "陈默", Gender: "male"}
	female := models.Character{ID: "c2", Name: "林夏", Gender: "female"}
	bank.Lock(&male, 0)
	bank.Lock(&female, 1)

	if male.VoiceID[:5] != "male-" {
		t.Errorf("男角色未分配男声：%s", male.VoiceID)
	}
	if female.VoiceID[:7] != "female-" {
		t.Errorf("女角色未分配女声：%s", female.VoiceID)
	}
}

// TestLockSameGenderDistinct 验证同性别多角色音色相互区分。
func TestLockSameGenderDistinct(t *testing.T) {
	bank := NewCharacterBank()
	a := models.Character{ID: "a", Gender: "male"}
	b := models.Character{ID: "b", Gender: "male"}
	bank.Lock(&a, 0)
	bank.Lock(&b, 1)
	if a.VoiceID == b.VoiceID {
		t.Errorf("同性别角色音色应区分，却都是 %s", a.VoiceID)
	}
}

// TestGuessGender 验证按名字猜测性别：男性化用字→male，否则 female。
func TestGuessGender(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"李伟", "male"},   // 含「伟」
		{"张强", "male"},   // 含「强」
		{"王磊", "male"},   // 含「磊」
		{"林夏", "female"}, // 无男性化用字
		{"苏挽", "female"},
		{"", "female"}, // 空名兜底
	}
	for _, c := range cases {
		if got := guessGender(c.name); got != c.want {
			t.Errorf("guessGender(%q) = %q, 期望 %q", c.name, got, c.want)
		}
	}
}

// TestLockGuessesWhenNoGender 验证未标性别时由名字猜测并据此配音。
func TestLockGuessesWhenNoGender(t *testing.T) {
	bank := NewCharacterBank()
	c := models.Character{ID: "x", Name: "李强"} // 含「强」→ 应猜 male
	bank.Lock(&c, 0)
	if c.Gender != "male" {
		t.Errorf("未标性别应猜测为 male，实得 %q", c.Gender)
	}
	if c.VoiceID[:5] != "male-" {
		t.Errorf("猜测男性后应配男声，实得 %s", c.VoiceID)
	}
}
