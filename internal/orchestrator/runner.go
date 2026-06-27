package orchestrator

import (
	"context"
	"fmt"

	"github.com/cuiwenyang/ai-short-drama/internal/logx"
	"github.com/cuiwenyang/ai-short-drama/internal/models"
)

// Runner 是 DAG 执行引擎。
//
// 按依赖拓扑顺序驱动各节点：
//   - 前驱未全部 DONE 的节点暂不执行；
//   - 已 DONE 的节点在续跑时直接跳过（断点续跑）；
//   - 每个节点结束后立即落盘检查点；
//   - 任一节点失败则标记 FAILED 并中止（保留现场供续跑）。
//
// 说明：节点级采用顺序拓扑驱动以保证可预测性与可续跑；
// 真正的高并发发生在节点内部——分镜/音频智能体对"镜头"做 goroutine 级并行。
type Runner struct {
	cp *Checkpoint
}

// NewRunner 创建执行引擎。
func NewRunner(cp *Checkpoint) *Runner {
	return &Runner{cp: cp}
}

// Run 执行整条流水线。
func (r *Runner) Run(ctx context.Context, p *Pipeline, st *models.ProjectState) error {
	done := func(name string) bool { return st.GetStatus(name) == models.StatusDone }

	for {
		progressed := false
		allDone := true

		for _, node := range p.Nodes {
			name := node.Agent.Name()

			if done(name) {
				continue
			}
			allDone = false

			// 依赖未满足则本轮跳过，等待前驱完成。
			if !depsSatisfied(node, done) {
				continue
			}

			// —— 执行节点 ——
			st.SetStatus(name, models.StatusRunning)
			if err := node.Agent.Run(ctx, st); err != nil {
				st.SetStatus(name, models.StatusFailed)
				_ = r.cp.Save(st)
				return fmt.Errorf("节点[%s]执行失败: %w", name, err)
			}
			st.SetStatus(name, models.StatusDone)

			// 节点完成立即落盘——这是断点续跑的基础。
			if err := r.cp.Save(st); err != nil {
				logx.Warn("检查点保存失败: %v", err)
			}
			progressed = true
		}

		if allDone {
			return nil
		}
		if !progressed {
			// 还有未完成节点，但无任何节点可推进 → 依赖死锁或前驱失败。
			return fmt.Errorf("流水线停滞：存在无法满足依赖的节点（检查 DAG 定义）")
		}
	}
}

func depsSatisfied(node Node, done func(string) bool) bool {
	for _, dep := range node.DependsOn {
		if !done(dep) {
			return false
		}
	}
	return true
}
