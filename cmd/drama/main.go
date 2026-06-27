// Command drama 是 AI 短剧创作智能体的命令行入口。
//
// 两种创作入口：
//
//	# 文本剧本 → 视频（基础闭环，离线零成本）
//	drama -script examples/screenplay.txt
//	cat screenplay.txt | drama -script -
//
//	# 创意 → AI 生成剧本 → 视频
//	drama -idea "一个程序员重拾儿时画家梦想的故事" -genre 治愈
//
//	# 断点续跑已有项目
//	drama -resume <project_id>
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	idea := flag.String("idea", "", "创意模式：一句短剧创意/主题，由 AI 生成剧本")
	script := flag.String("script", "", "剧本模式：文本剧本文件路径（传 - 从标准输入读取）")
	genre := flag.String("genre", "都市", "题材，如 都市/悬疑/古风/治愈")
	style := flag.String("style", "写实", "视觉风格")
	resume := flag.String("resume", "", "续跑已有项目 ID（workspace 下的目录名）")
	flag.Parse()

	cfg := config.Load()
	ctx := context.Background()

	// 组装项目状态：续跑则从检查点加载，否则按入口（剧本/创意）新建。
	st, projectDir, err := setupProject(cfg, *idea, *script, *genre, *style, *resume)
	if err != nil {
		logx.Fatal(err)
	}

	// 校验外部依赖。
	if !fsx.HasBinary(cfg.FFmpegBin) || !fsx.HasBinary(cfg.FFprobeBin) {
		logx.Fatal(fmt.Errorf("未找到 ffmpeg/ffprobe，请先安装（brew install ffmpeg）"))
	}

	logx.Stage("🎬", "AI 短剧创作智能体启动")
	logx.Info("项目 ID：%s", st.Project.ID)
	if st.Project.Source == "script" {
		logx.Info("入口：文本剧本（%d 字）题材 %s / 风格 %s",
			len([]rune(st.Project.Script)), st.Project.Genre, st.Project.Style)
	} else {
		logx.Info("入口：创意「%s」（%s/%s）", st.Project.Idea, st.Project.Genre, st.Project.Style)
	}

	// 组装能力服务（可插拔）。
	svc := services.Build(cfg)

	// 组装五大智能体。
	bank := memory.NewCharacterBank()
	scriptEngine := agents.NewScriptEngine(cfg, svc.LLM)
	asset := agents.NewAssetManager(cfg, svc.T2I, bank)
	storyboard := agents.NewStoryboard(cfg, svc.T2I, svc.I2V)
	audio := agents.NewAudioSynth(cfg, svc.TTS)
	compositor := agents.NewCompositor(cfg, svc.I2V, svc.Editor)

	// 组装流水线并执行（总控调度层）。
	pipeline := orchestrator.NewDefaultPipeline(scriptEngine, asset, storyboard, audio, compositor)
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

// setupProject 准备项目状态：续跑加载 / 新建初始化（剧本模式或创意模式）。
func setupProject(cfg *config.Config, idea, script, genre, style, resume string) (*models.ProjectState, string, error) {
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

	p := models.Project{
		Genre:    genre,
		Style:    style,
		Episodes: 1,
		Created:  time.Now().Format(time.RFC3339),
	}

	switch {
	case script != "": // 剧本模式：文本剧本 → 视频
		content, err := readScript(script)
		if err != nil {
			return nil, "", err
		}
		p.Source = "script"
		p.Script = content
		p.ID = newProjectID(content)

	case idea != "": // 创意模式：AI 生成剧本 → 视频
		p.Source = "idea"
		p.Idea = idea
		p.ID = newProjectID(idea)

	default:
		flag.Usage()
		return nil, "", fmt.Errorf("请用 -script 提供文本剧本，或用 -idea 提供创意")
	}

	dir := filepath.Join(cfg.WorkspaceDir, p.ID)
	if err := fsx.EnsureDir(dir); err != nil {
		return nil, "", err
	}
	return models.NewProjectState(p), dir, nil
}

// readScript 读取文本剧本：path 为 "-" 时读标准输入，否则读文件。
func readScript(path string) (string, error) {
	var b []byte
	var err error
	if path == "-" {
		b, err = io.ReadAll(os.Stdin)
	} else {
		b, err = os.ReadFile(path)
	}
	if err != nil {
		return "", fmt.Errorf("读取剧本失败: %w", err)
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return "", fmt.Errorf("剧本内容为空：%s", path)
	}
	return string(b), nil
}

// newProjectID 生成形如 20060102_150405_<hash> 的项目 ID（时间前缀便于排序）。
func newProjectID(seed string) string {
	ts := time.Now().Format("20060102_150405")
	return ts + "_" + fsx.Hash(seed)[:6]
}
