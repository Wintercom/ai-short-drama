package models

// Character 是角色定义，也是"角色一致性"的载体。
//
// 一致性的工程化关键：为每个角色锁定 (RefImage + Seed + VoiceID)，
// 之后所有 T2I / I2V / TTS 调用都强制注入这三者——
// 形象一致靠 RefImage（参考图/IP-Adapter），画风一致靠固定 Seed，
// 配音一致靠固定 VoiceID。一致性由架构强制保证，而非靠运气。
type Character struct {
	ID         string `json:"id"`         // 稳定 ID，分镜中以此引用角色
	Name       string `json:"name"`       // 角色名
	Persona    string `json:"persona"`    // 性格/背景设定
	Appearance string `json:"appearance"` // 外貌描述（生成参考图的 prompt）

	// —— 以下为"锁定"字段，由资产管理器写入后全程不变 ——
	RefImage string `json:"ref_image"` // 锁定的参考形象图路径
	Seed     int    `json:"seed"`      // 锁定的随机种子（控画风一致）
	VoiceID  string `json:"voice_id"`  // 锁定的音色 ID（控配音一致）
}

// Scene 是一场戏，介于大纲与分镜之间的中间层。
type Scene struct {
	ID        string   `json:"id"`
	Index     int      `json:"index"`       // 场序
	Heading   string   `json:"heading"`     // 场景标题，如「咖啡馆-日-内」
	Location  string   `json:"location"`    // 地点
	TimeOfDay string   `json:"time_of_day"` // 时间（日/夜）
	Summary   string   `json:"summary"`     // 本场剧情摘要（注入叙事记忆，保证连贯）
	CharIDs   []string `json:"char_ids"`    // 出场角色 ID
}
