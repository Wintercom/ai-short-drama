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
	// 续跑前先校验产物：DONE 但产物缺失的节点（及其下游）降级重跑，
	// 避免「状态说完成、文件却不在」导致的空跑（如删了成片仍被跳过）。
	r.verifyAndDemote(p, st)

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

// verifyAndDemote 续跑校验：对 DONE 且实现了 ArtifactVerifier 的节点核验产物，
// 缺失则降级为 PENDING，并级联降级其所有下游依赖节点（直接 + 间接）。
//
// 为何要级联：若分镜片段被删而重跑，依赖它的后期合成必须一并重跑，
// 否则成片仍由旧/缺失的片段拼成。全新任务时所有状态本为 PENDING，此函数无副作用。
func (r *Runner) verifyAndDemote(p *Pipeline, st *models.ProjectState) {
	demote := map[string]bool{}

	// 第一遍：找出产物校验失败的 DONE 节点。
	for _, node := range p.Nodes {
		name := node.Agent.Name()
		if st.GetStatus(name) != models.StatusDone {
			continue
		}
		v, ok := node.Agent.(ArtifactVerifier)
		if !ok {
			continue // 未实现校验的节点维持原状态语义
		}
		if !v.Verify(st) {
			demote[name] = true
		}
	}

	if len(demote) == 0 {
		return
	}

	// 第二遍：沿依赖闭包扩散——任一前驱被降级，则本节点也降级。
	// 用不动点循环传播，不依赖 p.Nodes 的拓扑排序，直到一轮无新增为止。
	for {
		changed := false
		for _, node := range p.Nodes {
			name := node.Agent.Name()
			if demote[name] {
				continue
			}
			for _, dep := range node.DependsOn {
				if demote[dep] {
					demote[name] = true
					changed = true
					break
				}
			}
		}
		if !changed {
			break
		}
	}

	// 落实降级。
	for _, node := range p.Nodes {
		name := node.Agent.Name()
		if demote[name] {
			st.SetStatus(name, models.StatusPending)
			logx.Step("续跑校验：节点[%s]产物缺失或受上游影响，将重跑", name)
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
