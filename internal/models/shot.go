package models

// Shot 是最小生产单元——一个镜头。
// 视觉分镜与音频合成都以 Shot 为粒度并行处理，产物（关键帧/片段/配音）回填到本结构。
type Shot struct {
	ID       string `json:"id"`
	SceneID  string `json:"scene_id"`
	Index    int    `json:"index"`     // 全局镜号（决定最终拼接顺序）
	ShotType string `json:"shot_type"` // 景别：远景/全景/中景/近景/特写
	Camera   string `json:"camera"`    // 运镜：推/拉/摇/移/固定
	CharID   string `json:"char_id"`   // 主体角色 ID（用于注入角色参考图，保证一致性）

	KeyframePrompt string  `json:"keyframe_prompt"` // 关键帧画面描述
	Dialogue       string  `json:"dialogue"`        // 该镜头对白（驱动 TTS）
	Duration       float64 `json:"duration"`        // 时长（秒）

	// —— 产物回填字段 ——
	KeyframePath string `json:"keyframe_path"` // T2I 产物：关键帧图
	ClipPath     string `json:"clip_path"`     // I2V 产物：无声视频片段
	AudioPath    string `json:"audio_path"`    // TTS 产物：配音音轨
}

// Asset 记录一项生成产物，便于缓存复用与溯源（同输入不重复生成，省钱省时）。
type Asset struct {
	Kind string `json:"kind"` // keyframe | clip | audio | final
	Ref  string `json:"ref"`  // 关联的 Shot/Character ID
	Path string `json:"path"` // 文件路径
	Hash string `json:"hash"` // 输入指纹，用于产物缓存命中判断
}
