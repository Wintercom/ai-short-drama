package memory

import (
	"fmt"
	"strings"

	"github.com/cuiwenyang/ai-short-drama/internal/models"
)

// StoryContext 是叙事记忆——"长篇叙事连贯性"的工程化解法。
//
// 痛点：长视频里 AI 容易"失忆"，后段剧情与前段脱节、人物行为前后矛盾。
// 解法：把全局大纲 + 已发生剧情的滚动摘要，注入每一次下层生成的提示词，
// 让分镜/对白生成时始终"记得"故事全局与此前发生了什么，从而保持连贯。
//
// 这是"分层递进生成"的关键一环：大纲(上层) → 注入 → 分镜(下层)。
type StoryContext struct {
	outline models.Outline
	chars   []models.Character
	recap   []string // 已生成内容的滚动摘要
}

// NewStoryContext 基于大纲与角色表初始化叙事记忆。
func NewStoryContext(outline models.Outline, chars []models.Character) *StoryContext {
	return &StoryContext{outline: outline, chars: chars}
}

// Remember 追加一条剧情摘要（每生成一场/一镜后调用）。
func (s *StoryContext) Remember(summary string) {
	if strings.TrimSpace(summary) != "" {
		s.recap = append(s.recap, summary)
	}
}

// Prompt 生成注入下层 LLM 的"故事记忆"上下文块。
// 包含：剧名/主题/梗概（全局约束）+ 角色设定（人物一致）+ 已发生剧情（连贯）。
func (s *StoryContext) Prompt() string {
	var b strings.Builder
	b.WriteString("【故事全局】\n")
	b.WriteString(fmt.Sprintf("剧名：%s\n主题：%s\n梗概：%s\n",
		s.outline.Title, s.outline.Theme, s.outline.Synopsis))

	if len(s.outline.Beats) > 0 {
		b.WriteString("剧情节拍：" + strings.Join(s.outline.Beats, " → ") + "\n")
	}

	b.WriteString("\n【人物设定（务必保持一致）】\n")
	for _, c := range s.chars {
		b.WriteString(fmt.Sprintf("- %s（%s）：%s\n", c.Name, c.ID, c.Persona))
	}

	if len(s.recap) > 0 {
		b.WriteString("\n【已发生剧情（保持连贯，勿矛盾）】\n")
		for i, r := range s.recap {
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, r))
		}
	}
	return b.String()
}
