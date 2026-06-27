package models

import (
	"sync"
)

// NodeStatus 表示流水线某节点的执行状态（供断点续跑判断）。
type NodeStatus string

const (
	StatusPending NodeStatus = "PENDING"
	StatusRunning NodeStatus = "RUNNING"
	StatusDone    NodeStatus = "DONE"
	StatusFailed  NodeStatus = "FAILED"
)

// ProjectState 是全局唯一真相源（黑板）。
//
// 整个系统的灵魂：所有 Agent 不互相直接调用，而是统一读写这一份共享状态。
// 剧本引擎写入 Outline/Characters/Scenes/Shots，分镜模块读 Shots 回填片段，
// 合成器读全部产物输出成片。流程不是靠口口相传，而是靠这份强类型、
// 可持久化的契约串起来——这就是消灭"流程割裂"的根本手段。
//
// 内嵌 sync.Mutex：镜头级并发回填产物时保护并发写。
type ProjectState struct {
	mu sync.Mutex `json:"-"`

	Project    Project               `json:"project"`
	Outline    Outline               `json:"outline"`
	Characters []Character           `json:"characters"`
	Scenes     []Scene               `json:"scenes"`
	Shots      []Shot                `json:"shots"`
	Assets     []Asset               `json:"assets"`
	FinalVideo string                `json:"final_video"` // 最终成片路径
	Status     map[string]NodeStatus `json:"status"`      // 各 DAG 节点状态
}

// NewProjectState 初始化一个空状态。
func NewProjectState(p Project) *ProjectState {
	return &ProjectState{
		Project: p,
		Status:  map[string]NodeStatus{},
	}
}

// SetStatus 线程安全地更新节点状态。
func (s *ProjectState) SetStatus(node string, st NodeStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Status == nil {
		s.Status = map[string]NodeStatus{}
	}
	s.Status[node] = st
}

// GetStatus 读取节点状态，未知节点视为 PENDING。
func (s *ProjectState) GetStatus(node string) NodeStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.Status[node]; ok {
		return st
	}
	return StatusPending
}

// CharByID 按 ID 查找角色（注入角色参考图时使用）。
func (s *ProjectState) CharByID(id string) (Character, bool) {
	for _, c := range s.Characters {
		if c.ID == id {
			return c, true
		}
	}
	return Character{}, false
}

// UpdateShot 线程安全地按 ID 回填镜头产物（并发分镜/配音时使用）。
func (s *ProjectState) UpdateShot(id string, fn func(*Shot)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Shots {
		if s.Shots[i].ID == id {
			fn(&s.Shots[i])
			return
		}
	}
}

// AddAsset 线程安全地登记一项产物（用于缓存与溯源）。
func (s *ProjectState) AddAsset(a Asset) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Assets = append(s.Assets, a)
}
