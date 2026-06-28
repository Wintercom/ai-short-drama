package agents

import (
	"fmt"
	"strings"

	"github.com/cuiwenyang/ai-short-drama/internal/models"
)

// 本文件实现"文本剧本 → 结构化分镜"的离线解析，是"文本→视频"闭环的入口之一。
//
// 设计取向：纯文本、规则化、确定性解析，不依赖 LLM——这样"文本剧本→视频"
// 这条主链零成本、可离线、绝不因外部服务波动而中断（对齐基础目标）。
// 解析结果（Outline + Characters + shotDTO）与 AI 生成路径完全同构，
// 因此下游分镜/配音/合成流程无需任何改动。
//
// 剧本格式（中英文标题与字段名均可，全/半角标点均可，键值用「键：值」）：
//   - 区块标题：## 角色 / ## 分镜（亦支持 ## Characters / ## Shots）
//   - 字段名同时支持中英文别名（如 场景/scene、台词/line）
//
//	# 标题：重拾画笔
//	# 题材：治愈
//	# 主题：勇气与梦想
//	# 梗概：程序员深夜重拾儿时画笔的故事
//
//	## 角色
//	- 林夏 | 坚韧敏感的程序员，怀揣画家梦 | 二十多岁，短发，风衣 | 女 | ./faces/林夏.png
//	- 陈默 | 沉稳的画室老师 | 三十多岁，眼镜，深色大衣
//	（角色行第 4 段性别、第 5 段画像路径均可省；指定画像则全程以它为参考图）
//
//	## 分镜
//	### 镜头
//	场景：办公室-夜-内
//	角色：林夏
//	景别：全景
//	运镜：推
//	画面：深夜办公室，林夏独自对着电脑屏幕
//	台词：又是凌晨两点，这真的是我想要的生活吗？
//
//	### 镜头
//	角色：陈默
//	...

// parsedScript 是剧本解析的中间产物。
type parsedScript struct {
	outline    models.Outline
	characters []models.Character
	shots      []shotDTO
}

// 解析阶段。
type section int

const (
	secMeta section = iota
	secCharacters
	secShots
)

// parseScreenplay 把文本剧本解析为结构化数据。
func parseScreenplay(text string) (parsedScript, error) {
	var ps parsedScript
	cur := secMeta
	nameToID := map[string]string{} // 角色名 → 角色 ID（用于分镜引用解析）
	var curShot *shotDTO            // 正在构建的镜头
	flush := func() {               // 收束当前镜头
		if curShot != nil {
			ps.shots = append(ps.shots, *curShot)
			curShot = nil
		}
	}

	for _, raw := range strings.Split(text, "\n") {
		line := normalizeLine(raw)
		if line == "" || isComment(line) {
			continue
		}

		// —— 区块切换 ——
		if h, ok := sectionHeader(line); ok {
			flush()
			cur = h
			continue
		}

		switch cur {
		case secMeta:
			applyMeta(&ps.outline, line)

		case secCharacters:
			if c, ok := parseCharacterLine(line); ok {
				c.ID = fmt.Sprintf("char_%d", len(ps.characters)+1)
				nameToID[c.Name] = c.ID
				ps.characters = append(ps.characters, c)
			}

		case secShots:
			// 「### ...」开启一个新镜头。
			if strings.HasPrefix(line, "###") {
				flush()
				curShot = &shotDTO{}
				continue
			}
			if curShot == nil { // 容错：分镜区出现裸键值时自动开镜
				curShot = &shotDTO{}
			}
			applyShotField(curShot, line)
		}
	}
	flush()

	// 收尾：补全角色与分镜的一致性（角色引用名 → ID）。
	resolveCharacters(&ps, nameToID)

	if len(ps.shots) == 0 {
		return ps, fmt.Errorf("剧本中未解析到任何镜头（请在「## 分镜 / ## Shots」下用「### 镜头 / ### Shot」分隔）")
	}
	if ps.outline.Title == "" {
		ps.outline.Title = "无题短剧"
	}
	return ps, nil
}

// normalizeLine 统一全角标点并去除首尾空白，便于规则匹配。
func normalizeLine(s string) string {
	r := strings.NewReplacer("：", ":", "｜", "|", "　", " ")
	return strings.TrimSpace(r.Replace(s))
}

func isComment(line string) bool {
	return strings.HasPrefix(line, "//")
}

// sectionHeader 识别区块标题（## 角色 / ## 分镜），中英文标题均可。
func sectionHeader(line string) (section, bool) {
	if !strings.HasPrefix(line, "##") || strings.HasPrefix(line, "###") {
		return 0, false
	}
	title := strings.TrimSpace(strings.TrimLeft(line, "#"))
	lower := strings.ToLower(title) // 英文标题大小写不敏感
	switch {
	case containsAny(title, "角色", "人物"),
		containsAny(lower, "character", "cast", "role"):
		return secCharacters, true
	case containsAny(title, "分镜", "镜头", "脚本"),
		containsAny(lower, "shot", "storyboard", "scene", "script"):
		return secShots, true
	}
	return 0, false
}

// containsAny 判断 s 是否包含任一子串。
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// applyMeta 解析元信息行（# 标题：xxx 或 标题：xxx）。
func applyMeta(o *models.Outline, line string) {
	k, v := splitKV(strings.TrimLeft(line, "# "))
	if v == "" {
		return
	}
	switch {
	case matchKey(k, "标题", "title", "剧名"):
		o.Title = v
	case matchKey(k, "一句话", "logline"):
		o.Logline = v
	case matchKey(k, "主题", "theme"):
		o.Theme = v
	case matchKey(k, "梗概", "简介", "synopsis"):
		o.Synopsis = v
	case matchKey(k, "节拍", "beats"):
		o.Beats = splitList(v)
	}
}

// parseCharacterLine 解析角色行：- 名字 | 性格 | 外貌 | 性别 | 画像路径（后两段可省）。
//   - 必须以列表标记（- 或 *）开头，否则视为非角色行（如 # 注释）跳过
//   - 第 4 段性别可省（省略则由名字猜测）
//   - 第 5 段画像路径可省（用户自带"演员"参考图；省略则由 AI 生成锚点）
func parseCharacterLine(line string) (models.Character, bool) {
	// 角色行必须是列表项；非「- / *」开头的（如 # 注释、裸文本）不是角色。
	if !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "*") {
		return models.Character{}, false
	}
	line = strings.TrimLeft(line, "-* ")
	if line == "" {
		return models.Character{}, false
	}
	parts := strings.Split(line, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	c := models.Character{Name: parts[0]}
	if len(parts) > 1 {
		c.Persona = parts[1]
	}
	if len(parts) > 2 {
		c.Appearance = parts[2]
	}
	if len(parts) > 3 {
		c.Gender = normalizeGender(parts[3])
	}
	if len(parts) > 4 && parts[4] != "" {
		// 用户指定的角色画像路径（可为相对路径，后续相对剧本目录解析）。
		c.RefImage = parts[4]
	}
	if c.Name == "" {
		return models.Character{}, false
	}
	return c, true
}

// normalizeGender 归一化性别词：男/male/m → male；女/female/f → female；其余空。
func normalizeGender(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.Contains(s, "男"), s == "male", s == "m", s == "boy":
		return "male"
	case strings.Contains(s, "女"), s == "female", s == "f", s == "girl":
		return "female"
	}
	return ""
}

// applyShotField 解析镜头内的键值行。
func applyShotField(s *shotDTO, line string) {
	k, v := splitKV(line)
	if v == "" {
		return
	}
	switch {
	case matchKey(k, "场景", "scene", "heading"):
		s.SceneHeading = v
	case matchKey(k, "地点", "location"):
		s.Location = v
	case matchKey(k, "时间", "time"):
		s.TimeOfDay = v
	case matchKey(k, "角色", "人物", "role", "char"):
		s.CharID = v // 暂存角色名，后续 resolveCharacters 译为 ID
	case matchKey(k, "景别", "shot", "shottype"):
		s.ShotType = v
	case matchKey(k, "运镜", "camera"):
		s.Camera = v
	case matchKey(k, "画面", "描述", "keyframe", "desc"):
		s.KeyframePrompt = v
	case matchKey(k, "台词", "对白", "dialogue", "line"):
		s.Dialogue = v
	}
}

// resolveCharacters 把镜头里以"角色名"引用的 CharID 译为真正的角色 ID；
// 若引用了未声明的角色，则自动补建，保证人物一致且流程不中断。
func resolveCharacters(ps *parsedScript, nameToID map[string]string) {
	ensure := func(name string) string {
		if id, ok := nameToID[name]; ok {
			return id
		}
		id := fmt.Sprintf("char_%d", len(ps.characters)+1)
		ps.characters = append(ps.characters, models.Character{ID: id, Name: name})
		nameToID[name] = id
		return id
	}
	for i := range ps.shots {
		name := strings.TrimSpace(ps.shots[i].CharID)
		if name == "" {
			continue
		}
		ps.shots[i].CharID = ensure(name)
	}
}

// —— 通用小工具 ——

// splitKV 按首个冒号拆分键值。
func splitKV(line string) (string, string) {
	k, v, ok := strings.Cut(line, ":")
	if !ok {
		return "", ""
	}
	return strings.TrimSpace(k), strings.TrimSpace(v)
}

// matchKey 判断键名是否匹配任一别名（忽略大小写与空白）。
func matchKey(key string, aliases ...string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, a := range aliases {
		if key == strings.ToLower(a) {
			return true
		}
	}
	return false
}

// splitList 把「A、B；C, D」切成列表。
func splitList(s string) []string {
	f := func(r rune) bool { return r == '、' || r == ';' || r == '；' || r == ',' || r == '，' }
	var out []string
	for _, p := range strings.FieldsFunc(s, f) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
