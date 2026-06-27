// Package orchestrator 是总控调度层。
//
// 职责：把各 Agent 组织成有向无环图（DAG），按依赖顺序驱动执行，
// 统一管理状态、断点续跑与产物持久化。Agent 之间不互相感知，
// 全部通过 ProjectState 黑板交换数据。
package orchestrator

import (
	"context"

	"github.com/cuiwenyang/ai-short-drama/internal/models"
)

// Agent 是流水线节点的统一抽象。
//
// 所有智能体都实现这一个方法：读入全局状态、产出新状态。
// 输入输出都是同一份 ProjectState 契约，使节点天然可组合、可缓存、可续跑。
type Agent interface {
	Name() string                                           // 节点名（用于状态记录与日志）
	Run(ctx context.Context, st *models.ProjectState) error // 执行逻辑：读写黑板
}

// Node 是 DAG 中的一个节点：一个 Agent 加上它依赖的前驱节点名。
type Node struct {
	Agent     Agent
	DependsOn []string
}

// Pipeline 是节点集合，定义了完整的"文本→视频"工业化流程。
type Pipeline struct {
	Nodes []Node
}

// NewDefaultPipeline 按标准短剧生产顺序组装五大智能体。
//
//	剧本引擎 → 资产/角色管理 → (视觉分镜 ∥ 音频合成) → 后期合成
//
// 分镜与音频都依赖角色资产就绪，因此排在资产管理之后；
// 后期合成依赖两者全部完成。
func NewDefaultPipeline(
	script, asset, storyboard, audio, compositor Agent,
) *Pipeline {
	return &Pipeline{
		Nodes: []Node{
			{Agent: script},
			{Agent: asset, DependsOn: []string{script.Name()}},
			{Agent: storyboard, DependsOn: []string{asset.Name()}},
			{Agent: audio, DependsOn: []string{asset.Name()}},
			{Agent: compositor, DependsOn: []string{storyboard.Name(), audio.Name()}},
		},
	}
}
