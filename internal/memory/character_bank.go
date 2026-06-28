// Package memory 提供"角色一致性"与"叙事连贯性"两大记忆能力——
// 直接对应 AI 视频生成的两大行业痛点，是本系统工程化的核心价值所在。
package memory

import (
	"fmt"
	"strings"

	"github.com/cuiwenyang/ai-short-drama/internal/models"
)

// 逻辑音色池：与具体 TTS 引擎解耦。角色注册表只分配逻辑音色（male-N/female-N），
// 由 TTS 层（say 变调 / edge 真人声）各自映射到具体声音。
// 这样切换配音引擎、增删声音都不影响角色档案，男女声也有了统一抽象。
const (
	maleVoiceCount   = 2 // 逻辑男声数量：male-1, male-2
	femaleVoiceCount = 3 // 逻辑女声数量：female-1, female-2, female-3
)

// CharacterBank 是角色注册表——"角色一致性"的工程化载体。
//
// 痛点：AI 视频里同一角色在不同镜头中长相、画风、声音飘忽不定。
// 解法：为每个角色一次性锁定 (参考图 + 随机种子 + 音色ID)，
// 之后所有镜头的 T2I/I2V/TTS 调用都强制复用这三者——
// 一致性由注册表强制保证，而非靠每次生成碰运气。
type CharacterBank struct {
	maleIdx   int // 男声轮转计数
	femaleIdx int // 女声轮转计数
}

// NewCharacterBank 构造角色注册表。
func NewCharacterBank() *CharacterBank { return &CharacterBank{} }

// Lock 为单个角色分配并锁定一致性三要素。
//   - Seed：由角色 ID 稳定派生，保证可复现、跨镜头画风统一；
//   - Gender：未显式指定时由名字猜测，决定男/女声；
//   - VoiceID：按性别从对应逻辑音色池轮流分配，同性别角色音色相互区分。
func (b *CharacterBank) Lock(c *models.Character, index int) {
	if c.Seed == 0 {
		c.Seed = seedFromID(c.ID)
	}
	// 性别：显式优先，否则按名字猜测（用户已确认的策略）。
	if c.Gender == "" {
		c.Gender = guessGender(c.Name)
	}
	// 逻辑音色：仅在未分配时按性别分池轮流分配；已有值（含旧档案的系统语音名）一律保留，保证续跑一致。
	if c.VoiceID == "" {
		c.VoiceID = b.assignVoice(c.Gender)
	}
}

// assignVoice 按性别从对应池轮流分配逻辑音色。
func (b *CharacterBank) assignVoice(gender string) string {
	if gender == "male" {
		v := fmt.Sprintf("male-%d", b.maleIdx%maleVoiceCount+1)
		b.maleIdx++
		return v
	}
	v := fmt.Sprintf("female-%d", b.femaleIdx%femaleVoiceCount+1)
	b.femaleIdx++
	return v
}

// maleNameHints 是明显偏男性的名字用字，用于在未标性别时启发式猜测。
// 刻意只保留辨识度高的字（避免「林/海/子」等中性字造成误判）；猜不准时回退 female。
var maleNameHints = []rune("伟强刚军磊涛勇毅斌鹏杰浩轩晨建宏志振雄豪栋")

// guessGender 按名字用字启发式猜测性别；命中男性用字→male，否则 female。
// 仅作兜底，剧本/模板显式标注的性别优先级更高。
func guessGender(name string) string {
	rs := []rune(strings.TrimSpace(name))
	if len(rs) == 0 {
		return "female"
	}
	// 取末字（中文名常以性别色彩字结尾），并扫描全名。
	for _, r := range rs {
		for _, h := range maleNameHints {
			if r == h {
				return "male"
			}
		}
	}
	return "female"
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
