package agents

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/cuiwenyang/ai-short-drama/internal/config"
	"github.com/cuiwenyang/ai-short-drama/internal/fsx"
	"github.com/cuiwenyang/ai-short-drama/internal/logx"
	"github.com/cuiwenyang/ai-short-drama/internal/memory"
	"github.com/cuiwenyang/ai-short-drama/internal/models"
	"github.com/cuiwenyang/ai-short-drama/internal/services"
)

// AssetManager 是资产与角色管理器（流水线第二个智能体）。
//
// 核心职责——锁定角色一致性三要素：为每个角色生成并固定
// (参考图 RefImage + 种子 Seed + 音色 VoiceID)，写回角色档案。
// 后续所有镜头的画面与配音都复用这三者，从架构上保证：
// 同一角色跨镜头长相统一、画风统一、声音统一。
type AssetManager struct {
	cfg  *config.Config
	t2i  services.T2I
	bank *memory.CharacterBank
}

// NewAssetManager 构造资产管理器。
func NewAssetManager(cfg *config.Config, t2i services.T2I, bank *memory.CharacterBank) *AssetManager {
	return &AssetManager{cfg: cfg, t2i: t2i, bank: bank}
}

// Name 节点名。
func (a *AssetManager) Name() string { return "asset_manager" }

// Verify 报告角色资产是否完整：每个角色都已锁定并生成参考图文件。
func (a *AssetManager) Verify(st *models.ProjectState) bool {
	if len(st.Characters) == 0 {
		return false
	}
	for i := range st.Characters {
		if !fsx.Exists(st.Characters[i].RefImage) {
			return false
		}
	}
	return true
}

// Run 为每个角色锁定一致性要素并生成参考图。
func (a *AssetManager) Run(ctx context.Context, st *models.ProjectState) error {
	logx.Stage("🎭", "资产管理器：锁定角色一致性（参考图 + 种子 + 音色）")

	dir := filepath.Join(projectDir(a.cfg, st), "characters")

	for i := range st.Characters {
		c := &st.Characters[i]

		// 1) 锁定种子与音色（确定性，可复现）
		a.bank.Lock(c, i)

		// 2) 生成角色参考图（一次生成，全程复用）
		if c.RefImage == "" || !fsx.Exists(c.RefImage) {
			refPath := filepath.Join(dir, fmt.Sprintf("%s_%s.png", c.ID, slug(c.Name, 12)))
			prompt := fmt.Sprintf("角色形象设定：%s。外貌：%s", c.Name, c.Appearance)
			if err := a.t2i.Generate(ctx, prompt, "", c.Seed, refPath); err != nil {
				return fmt.Errorf("生成角色[%s]参考图失败: %w", c.Name, err)
			}
			c.RefImage = refPath
			st.AddAsset(models.Asset{Kind: "character", Ref: c.ID, Path: refPath})
		}

		logx.Done("%s：seed=%d 音色=%s 参考图=%s",
			c.Name, c.Seed, c.VoiceID, filepath.Base(c.RefImage))
	}
	return nil
}
