// Package models 定义贯穿全流程的强类型数据契约。
//
// 设计要点：所有 Agent 都读写同一份 ProjectState（黑板模式），
// 而不互相直接调用。剧本、角色、分镜、产物路径全部沉淀为强类型字段，
// 这样"剧本 → 分镜 → 片段 → 成片"的每一环都有契约约束，
// 从根本上消灭"流程割裂"。
package models

// Project 是一次创作任务的元信息。
//
// 支持两种创作入口（二选一）：
//   - 创意模式：仅给 Idea，由剧本引擎用 LLM 分层生成剧本；
//   - 剧本模式：给定 ScriptInput（完整文本剧本），由剧本引擎离线解析为分镜。
//
// 两种入口都汇入同一份 ProjectState 黑板，下游分镜/配音/合成流程完全一致——
// 这正是"文本→视频"闭环不中断的体现。
type Project struct {
	ID       string `json:"id"`       // 唯一项目号，同时作为 workspace 子目录名
	Source   string `json:"source"`   // 创作入口：idea（创意）| script（文本剧本）
	Idea     string `json:"idea"`     // 用户原始创意/主题（创意模式）
	Script   string `json:"script"`   // 文本剧本原文（剧本模式）
	Genre    string `json:"genre"`    // 题材，如 都市/悬疑/古风
	Style    string `json:"style"`    // 视觉风格，如 写实/动漫/赛博朋克
	Episodes int    `json:"episodes"` // 集数（基础闭环固定为 1）
	Created  string `json:"created"`  // 创建时间（RFC3339）
}

// Outline 是剧本引擎产出的顶层叙事结构。
// 分层递进生成的最上层：大纲约束下层的分场与分镜，保证长篇叙事不断裂。
type Outline struct {
	Title    string   `json:"title"`    // 剧名
	Logline  string   `json:"logline"`  // 一句话故事
	Theme    string   `json:"theme"`    // 主题
	Synopsis string   `json:"synopsis"` // 剧情梗概
	Beats    []string `json:"beats"`    // 关键剧情节拍（起承转合）
}
