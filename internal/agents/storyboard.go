package agents

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/cuiwenyang/ai-short-drama/internal/config"
	"github.com/cuiwenyang/ai-short-drama/internal/fsx"
	"github.com/cuiwenyang/ai-short-drama/internal/logx"
	"github.com/cuiwenyang/ai-short-drama/internal/models"
	"github.com/cuiwenyang/ai-short-drama/internal/services"
)

// Storyboard 是视觉化分镜模块（流水线第三个智能体）。
//
// 对每个镜头执行：关键帧生成(T2I) → 运镜成片(I2V)。
// 镜头之间相互独立，因此用 goroutine 做镜头级并发（Go 的核心优势），
// 并发度由 config.ShotConcurrency 控制。
//
// 一致性保证：每个镜头按其主角的锁定 (RefImage + Seed) 生成画面，
// 使同一角色跨镜头视觉统一。
//
// 工程化：已生成且文件就绪的镜头直接跳过（产物缓存），续跑时不重复烧算力。
type Storyboard struct {
	cfg *config.Config
	t2i services.T2I
	i2v services.I2V
}

// NewStoryboard 构造分镜模块。
func NewStoryboard(cfg *config.Config, t2i services.T2I, i2v services.I2V) *Storyboard {
	return &Storyboard{cfg: cfg, t2i: t2i, i2v: i2v}
}

// Name 节点名。
func (a *Storyboard) Name() string { return "storyboard" }

// Verify 报告分镜产物是否完整：每个镜头的关键帧与运镜片段文件都在。
func (a *Storyboard) Verify(st *models.ProjectState) bool {
	if len(st.Shots) == 0 {
		return false
	}
	for i := range st.Shots {
		s := &st.Shots[i]
		if !fsx.Exists(s.KeyframePath) || !fsx.Exists(s.ClipPath) {
			return false
		}
	}
	return true
}

// Run 并发生成所有镜头的关键帧与视频片段。
func (a *Storyboard) Run(ctx context.Context, st *models.ProjectState) error {
	logx.Stage("🎬", "视觉分镜：并发生成关键帧与运镜片段")

	dir := projectDir(a.cfg, st)
	sem := make(chan struct{}, max1(a.cfg.ShotConcurrency))
	var wg sync.WaitGroup
	errs := make([]error, len(st.Shots))

	for i := range st.Shots {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}        // 获取并发额度
			defer func() { <-sem }() // 释放
			errs[idx] = a.renderShot(ctx, st, &st.Shots[idx], dir)
		}(i)
	}
	wg.Wait()

	// 汇总错误：任一镜头失败即整体失败（保留已成功产物供续跑）。
	for i, err := range errs {
		if err != nil {
			return fmt.Errorf("镜头[%s]渲染失败: %w", st.Shots[i].ID, err)
		}
	}
	logx.Done("全部 %d 个镜头渲染完成", len(st.Shots))
	return nil
}

// renderShot 渲染单个镜头：关键帧 → 片段。
func (a *Storyboard) renderShot(ctx context.Context, st *models.ProjectState, shot *models.Shot, dir string) error {
	// 注入主角的锁定一致性要素
	var refImage, gender, appearance string
	var seed int
	if c, ok := st.CharByID(shot.CharID); ok {
		refImage = c.RefImage
		seed = c.Seed
		gender = c.Gender
		appearance = c.Appearance // 外貌锚点：跨镜头锁定角色形象
	}

	// 查找本镜头所属 scene，提取场景信息
	sceneHeading := ""
	for _, sc := range st.Scenes {
		if sc.ID == shot.SceneID {
			sceneHeading = sc.Heading
			break
		}
	}

	keyframePath := filepath.Join(dir, "shots", shot.ID+"_key.png")
	clipPath := filepath.Join(dir, "shots", shot.ID+"_clip.mp4")

	// 1) 关键帧（缓存命中则跳过）
	if !fsx.Exists(keyframePath) {
		// 用 "||key:val" 编码格式把结构化元信息传给 T2I（LocalT2I 解析，真实模型直接用 description）
		prompt := fmt.Sprintf("%s||gender:%s||appearance:%s||dialogue:%s||scene:%s||shottype:%s",
			shot.KeyframePrompt, gender, appearance, shot.Dialogue, sceneHeading, shot.ShotType)
		if err := a.t2i.Generate(ctx, prompt, refImage, seed, keyframePath); err != nil {
			return fmt.Errorf("关键帧生成失败: %w", err)
		}
	}
	st.UpdateShot(shot.ID, func(s *models.Shot) { s.KeyframePath = keyframePath })

	// 2) 运镜成片（时长先用默认值，后续按配音时长由合成器对齐）
	dur := shot.Duration
	if dur <= 0 {
		dur = 3.0
	}
	if !fsx.Exists(clipPath) {
		// shot.KeyframePrompt 既是关键帧画面描述，也是人物动作描述，
		// 传给 I2V 驱动真实模型的肢体/表情动作（本地实现忽略）。
		if err := a.i2v.Animate(ctx, keyframePath, shot.Camera, shot.KeyframePrompt, dur, clipPath); err != nil {
			return fmt.Errorf("运镜成片失败: %w", err)
		}
	}
	st.UpdateShot(shot.ID, func(s *models.Shot) { s.ClipPath = clipPath })

	logx.Step("镜头 %s 完成（%s/%s）", shot.ID, shot.ShotType, shot.Camera)
	return nil
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
