package orchestrator

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/cuiwenyang/ai-short-drama/internal/fsx"
	"github.com/cuiwenyang/ai-short-drama/internal/models"
)

// Checkpoint 负责把 ProjectState 持久化到 workspace/{id}/project.json。
//
// 视频生成既慢又贵，断点续跑是关键工程能力：每个节点跑完即落盘，
// 重跑时已完成（DONE）的节点直接跳过，只补做失败/未完成的部分。
type Checkpoint struct {
	Path string // project.json 路径
}

// NewCheckpoint 根据项目工作目录构造检查点。
func NewCheckpoint(projectDir string) *Checkpoint {
	return &Checkpoint{Path: filepath.Join(projectDir, "project.json")}
}

// Save 把当前状态写盘（每个节点结束后调用）。
func (c *Checkpoint) Save(st *models.ProjectState) error {
	if err := fsx.EnsureDir(filepath.Dir(c.Path)); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.Path, b, 0o644)
}

// Load 读取已有状态用于续跑；文件不存在时返回 (nil,nil) 表示全新任务。
func (c *Checkpoint) Load() (*models.ProjectState, error) {
	b, err := os.ReadFile(c.Path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var st models.ProjectState
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, err
	}
	if st.Status == nil {
		st.Status = map[string]models.NodeStatus{}
	}
	return &st, nil
}
