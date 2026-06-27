// Command drama 是 AI 短剧创作智能体的命令行入口。
//
// 用法示例：
//
//	drama -idea "一个程序员重拾儿时画家梦想的故事" -genre 治愈
//	drama -idea "..." -resume <project_id>   # 断点续跑已有项目
package main

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"time"

	"github.com/cuiwenyang/ai-short-drama/internal/agents"
	"github.com/cuiwenyang/ai-short-drama/internal/config"
	"github.com/cuiwenyang/ai-short-drama/internal/fsx"
	"github.com/cuiwenyang/ai-short-drama/internal/logx"
	"github.com/cuiwenyang/ai-short-drama/internal/memory"
	"github.com/cuiwenyang/ai-short-drama/internal/models"
	"github.com/cuiwenyang/ai-short-drama/internal/orchestrator"
	"github.com/cuiwenyang/ai-short-drama/internal/services"
)

func main() {
	idea := flag.String("idea", "", "短剧创意/主题（必填，新建项目时）")
	genre := flag.String("genre", "都市", "题材，如 都市/悬疑/古风/治愈")
	style := flag.String("style", "写实", "视觉风格")
	resume := flag.String("resume", "", "续跑已有项目 ID（workspace 下的目录名）")
	flag.Parse()

	cfg := config.Load()
	ctx := context.Background()

	// 组装项目状态：续跑则从检查点加载，否则新建。
	st, projectDir, err := setupProject(cfg, *idea, *genre, *style, *resume)
	if err != nil {
		logx.Fatal(err)
	}

	// 校验外部依赖。
	if !fsx.HasBinary(cfg.FFmpegBin) || !fsx.HasBinary(cfg.FFprobeBin) {
		logx.Fatal(fmt.Errorf("未找到 ffmpeg/ffprobe，请先安装（brew install ffmpeg）"))
	}

	logx.Stage("🎬", "AI 短剧创作智能体启动")
	logx.Info("项目 ID：%s", st.Project.ID)
	logx.Info("创意：%s（%s/%s）", st.Project.Idea, st.Project.Genre, st.Project.Style)

	// 组装能力服务（可插拔）。
	svc := services.Build(cfg)

	// 组装五大智能体。
	bank := memory.NewCharacterBank()
	script := agents.NewScriptEngine(cfg, svc.LLM)
	asset := agents.NewAssetManager(cfg, svc.T2I, bank)
	storyboard := agents.NewStoryboard(cfg, svc.T2I, svc.I2V)
	audio := agents.NewAudioSynth(cfg, svc.TTS)
	compositor := agents.NewCompositor(cfg, svc.I2V, svc.Editor)

	// 组装流水线并执行（总控调度层）。
	pipeline := orchestrator.NewDefaultPipeline(script, asset, storyboard, audio, compositor)
	cp := orchestrator.NewCheckpoint(projectDir)
	runner := orchestrator.NewRunner(cp)

	start := time.Now()
	if err := runner.Run(ctx, pipeline, st); err != nil {
		_ = cp.Save(st)
		logx.Fatal(fmt.Errorf("%w\n（已保存进度，可用 -resume %s 续跑）", err, st.Project.ID))
	}
	_ = cp.Save(st)

	logx.Stage("✅", "创作完成")
	logx.Info("耗时：%s", time.Since(start).Round(time.Second))
	logx.Info("成片：%s", st.FinalVideo)
	logx.Info("状态：%s", filepath.Join(projectDir, "project.json"))
}

// setupProject 准备项目状态：续跑加载 / 新建初始化。
func setupProject(cfg *config.Config, idea, genre, style, resume string) (*models.ProjectState, string, error) {
	if resume != "" {
		dir := filepath.Join(cfg.WorkspaceDir, resume)
		cp := orchestrator.NewCheckpoint(dir)
		st, err := cp.Load()
		if err != nil {
			return nil, "", fmt.Errorf("加载续跑项目失败: %w", err)
		}
		if st == nil {
			return nil, "", fmt.Errorf("未找到可续跑的项目：%s", resume)
		}
		return st, dir, nil
	}

	if idea == "" {
		flag.Usage()
		return nil, "", fmt.Errorf("请用 -idea 提供短剧创意")
	}

	id := newProjectID(idea)
	p := models.Project{
		ID:       id,
		Idea:     idea,
		Genre:    genre,
		Style:    style,
		Episodes: 1,
		Created:  time.Now().Format(time.RFC3339),
	}
	dir := filepath.Join(cfg.WorkspaceDir, id)
	if err := fsx.EnsureDir(dir); err != nil {
		return nil, "", err
	}
	return models.NewProjectState(p), dir, nil
}

// newProjectID 生成形如 20060102_150405_<hash> 的项目 ID（时间前缀便于排序）。
func newProjectID(idea string) string {
	ts := time.Now().Format("20060102_150405")
	return ts + "_" + fsx.Hash(idea)[:6]
}
