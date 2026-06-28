package orchestrator

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cuiwenyang/ai-short-drama/internal/models"
)

// fakeAgent 是可控的测试桩：记录是否被 Run，Verify 返回值可配置。
type fakeAgent struct {
	name      string
	ran       bool
	verifyVal bool
	verifies  bool // 是否实现 ArtifactVerifier（false 则不参与产物校验）
}

func (f *fakeAgent) Name() string { return f.name }

func (f *fakeAgent) Run(ctx context.Context, st *models.ProjectState) error {
	f.ran = true
	st.SetStatus(f.name, models.StatusDone)
	return nil
}

// verifyingAgent 实现 ArtifactVerifier；普通 fakeAgent 不实现。
type verifyingAgent struct{ *fakeAgent }

func (f verifyingAgent) Verify(st *models.ProjectState) bool { return f.verifyVal }

// wrap 按 verifies 决定是否暴露 Verify 能力。
func (f *fakeAgent) wrap() Agent {
	if f.verifies {
		return verifyingAgent{f}
	}
	return f
}

// buildTestPipeline 造一条 a→b→c 线性依赖的流水线，全部置 DONE。
func buildTestPipeline(a, b, c *fakeAgent) (*Pipeline, *models.ProjectState) {
	p := &Pipeline{Nodes: []Node{
		{Agent: a.wrap()},
		{Agent: b.wrap(), DependsOn: []string{"a"}},
		{Agent: c.wrap(), DependsOn: []string{"b"}},
	}}
	st := models.NewProjectState(models.Project{ID: "test"})
	st.SetStatus("a", models.StatusDone)
	st.SetStatus("b", models.StatusDone)
	st.SetStatus("c", models.StatusDone)
	return p, st
}

func runWith(t *testing.T, p *Pipeline, st *models.ProjectState) {
	t.Helper()
	cp := NewCheckpoint(t.TempDir())
	r := NewRunner(cp)
	if err := r.Run(context.Background(), p, st); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
}

// TestResumeAllArtifactsPresent：产物齐全 → 所有 DONE 节点都不重跑。
func TestResumeAllArtifactsPresent(t *testing.T) {
	a := &fakeAgent{name: "a", verifies: true, verifyVal: true}
	b := &fakeAgent{name: "b", verifies: true, verifyVal: true}
	c := &fakeAgent{name: "c", verifies: true, verifyVal: true}
	p, st := buildTestPipeline(a, b, c)

	runWith(t, p, st)

	for _, f := range []*fakeAgent{a, b, c} {
		if f.ran {
			t.Errorf("节点[%s]产物齐全却被重跑", f.name)
		}
	}
}

// TestResumeMissingArtifactCascades：中间节点 b 产物缺失 →
// b 和其下游 c 都重跑，上游 a 不动。
func TestResumeMissingArtifactCascades(t *testing.T) {
	a := &fakeAgent{name: "a", verifies: true, verifyVal: true}
	b := &fakeAgent{name: "b", verifies: true, verifyVal: false} // 产物缺失
	c := &fakeAgent{name: "c", verifies: true, verifyVal: true}
	p, st := buildTestPipeline(a, b, c)

	runWith(t, p, st)

	if a.ran {
		t.Error("上游 a 产物齐全，不应重跑")
	}
	if !b.ran {
		t.Error("b 产物缺失，应重跑")
	}
	if !c.ran {
		t.Error("c 是 b 的下游，b 重跑后 c 也应重跑")
	}
}

// TestResumeLeafMissingOnlyRerunsLeaf：仅末节点 c 产物缺失 →
// 只重跑 c，a、b 不动（验证不波及上游）。
func TestResumeLeafMissingOnlyRerunsLeaf(t *testing.T) {
	a := &fakeAgent{name: "a", verifies: true, verifyVal: true}
	b := &fakeAgent{name: "b", verifies: true, verifyVal: true}
	c := &fakeAgent{name: "c", verifies: true, verifyVal: false} // 成片被删
	p, st := buildTestPipeline(a, b, c)

	runWith(t, p, st)

	if a.ran || b.ran {
		t.Error("仅末节点产物缺失，不应波及上游 a/b")
	}
	if !c.ran {
		t.Error("c 产物缺失，应重跑")
	}
}

// TestResumeNonVerifierUntouched：未实现 ArtifactVerifier 的 DONE 节点
// 维持原「按状态跳过」语义，不被校验降级。
func TestResumeNonVerifierUntouched(t *testing.T) {
	a := &fakeAgent{name: "a", verifies: false} // 不实现 Verify
	b := &fakeAgent{name: "b", verifies: true, verifyVal: true}
	c := &fakeAgent{name: "c", verifies: true, verifyVal: true}
	p, st := buildTestPipeline(a, b, c)

	runWith(t, p, st)

	for _, f := range []*fakeAgent{a, b, c} {
		if f.ran {
			t.Errorf("节点[%s]不应重跑", f.name)
		}
	}
}

// TestFreshRunExecutesAll：全新任务（无 DONE 状态）→ 所有节点都执行。
func TestFreshRunExecutesAll(t *testing.T) {
	a := &fakeAgent{name: "a", verifies: true, verifyVal: true}
	b := &fakeAgent{name: "b", verifies: true, verifyVal: true}
	c := &fakeAgent{name: "c", verifies: true, verifyVal: true}
	p := &Pipeline{Nodes: []Node{
		{Agent: a.wrap()},
		{Agent: b.wrap(), DependsOn: []string{"a"}},
		{Agent: c.wrap(), DependsOn: []string{"b"}},
	}}
	st := models.NewProjectState(models.Project{ID: "fresh"}) // 全 PENDING

	runWith(t, p, st)

	for _, f := range []*fakeAgent{a, b, c} {
		if !f.ran {
			t.Errorf("全新任务节点[%s]应执行", f.name)
		}
	}
}

// 确保 NewCheckpoint 在临时目录可用（防御路径拼接回归）。
func TestCheckpointPathInTemp(t *testing.T) {
	dir := t.TempDir()
	cp := NewCheckpoint(dir)
	if cp.Path != filepath.Join(dir, "project.json") {
		t.Errorf("checkpoint 路径错误: %s", cp.Path)
	}
}
