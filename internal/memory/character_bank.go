// Package memory 提供"角色一致性"与"叙事连贯性"两大记忆能力——
// 直接对应 AI 视频生成的两大行业痛点，是本系统工程化的核心价值所在。
package memory

import "github.com/cuiwenyang/ai-short-drama/internal/models"

// 可用的系统中文音色池（macOS say）。为不同角色分配不同音色，
// 一经分配即写入角色档案、全程锁定，保证同角色配音始终一致。
var voicePool = []string{"Tingting", "Sinji", "Meijia"}

// CharacterBank 是角色注册表——"角色一致性"的工程化载体。
//
// 痛点：AI 视频里同一角色在不同镜头中长相、画风、声音飘忽不定。
// 解法：为每个角色一次性锁定 (参考图 + 随机种子 + 音色ID)，
// 之后所有镜头的 T2I/I2V/TTS 调用都强制复用这三者——
// 一致性由注册表强制保证，而非靠每次生成碰运气。
type CharacterBank struct{}

// NewCharacterBank 构造角色注册表。
func NewCharacterBank() *CharacterBank { return &CharacterBank{} }

// Lock 为单个角色分配并锁定一致性三要素。
//   - Seed：由角色 ID 稳定派生，保证可复现、跨镜头画风统一；
//   - VoiceID：从音色池按序分配，不同角色音色相互区分；
//   - RefImage：由资产管理器生成参考图后回填（此处仅占位说明数据流）。
//
// index 为角色序号，用于在音色池中轮转分配。
func (b *CharacterBank) Lock(c *models.Character, index int) {
	if c.Seed == 0 {
		c.Seed = seedFromID(c.ID)
	}
	if c.VoiceID == "" {
		c.VoiceID = voicePool[index%len(voicePool)]
	}
}

// seedFromID 由角色 ID 稳定派生一个正整数种子（同一角色永远同一种子）。
func seedFromID(id string) int {
	h := 0
	for _, r := range id {
		h = h*31 + int(r)
	}
	if h < 0 {
		h = -h
	}
	if h == 0 {
		h = 1
	}
	return h % 1000000
}
