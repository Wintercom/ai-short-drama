// Package agents 实现五大智能体，每个智能体是流水线中的一个节点。
//
// 统一约定：所有智能体只通过 ProjectState 黑板交换数据，彼此不直接调用；
// 各自实现 orchestrator.Agent 接口（Name + Run）。
package agents

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cuiwenyang/ai-short-drama/internal/config"
	"github.com/cuiwenyang/ai-short-drama/internal/models"
)

// projectDir 返回某项目的工作目录 workspace/{id}。
func projectDir(cfg *config.Config, st *models.ProjectState) string {
	return filepath.Join(cfg.WorkspaceDir, st.Project.ID)
}

// jsonBlock 从 LLM 文本输出中提取首个 JSON 片段。
// 兼容模型可能输出的 ```json 代码块或夹带解释文字的情况，提升真实 LLM 兼容性。
func jsonBlock(s string) string {
	s = strings.TrimSpace(s)

	// 去除 ```json ... ``` 代码块围栏
	if i := strings.Index(s, "```"); i >= 0 {
		s = s[i+3:]
		s = strings.TrimPrefix(s, "json")
		s = strings.TrimPrefix(s, "JSON")
		if j := strings.Index(s, "```"); j >= 0 {
			s = s[:j]
		}
	}

	// 截取第一个 { 或 [ 到对应的最后一个 } 或 ]
	start := strings.IndexAny(s, "{[")
	if start < 0 {
		return strings.TrimSpace(s)
	}
	open := s[start]
	close := byte('}')
	if open == '[' {
		close = ']'
	}
	end := strings.LastIndexByte(s, close)
	if end < start {
		return strings.TrimSpace(s[start:])
	}
	return s[start : end+1]
}

// unmarshalLoose 宽松解析 JSON：先抽取 JSON 块再反序列化。
func unmarshalLoose(raw string, v any) error {
	return json.Unmarshal([]byte(jsonBlock(raw)), v)
}

var nonWord = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// slug 把任意文本规整为安全的文件名片段。
func slug(s string, max int) string {
	s = nonWord.ReplaceAllString(strings.TrimSpace(s), "_")
	s = strings.Trim(s, "_")
	r := []rune(s)
	if len(r) > max {
		r = r[:max]
	}
	if len(r) == 0 {
		return "x"
	}
	return string(r)
}
