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
