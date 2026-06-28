package agents

import "testing"

// TestParseScreenplay 验证文本剧本解析的核心能力：元信息、角色、分镜、键值与全角标点。
func TestParseScreenplay(t *testing.T) {
	script := `# 标题：重拾画笔
# 题材：治愈
# 主题：勇气
# 节拍：被打破、挣扎、改变

## 角色
- 林夏 | 程序员，怀揣画家梦 | 短发风衣
- 陈默 | 画室老师 | 戴眼镜

## 分镜
### 镜头一
场景：办公室-夜-内
角色：林夏
景别：全景
运镜：推
画面：深夜的办公室
台词：这真的是我想要的吗？

### 镜头二
角色：陈默
景别：中景
台词：听听自己心里的声音。`

	ps, err := parseScreenplay(script)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}

	// 元信息（含全角冒号与顿号列表）
	if ps.outline.Title != "重拾画笔" {
		t.Errorf("标题解析错误：%q", ps.outline.Title)
	}
	if ps.outline.Theme != "勇气" {
		t.Errorf("主题解析错误：%q", ps.outline.Theme)
	}
	if len(ps.outline.Beats) != 3 {
		t.Errorf("节拍列表解析错误：%v", ps.outline.Beats)
	}

	// 角色
	if len(ps.characters) != 2 {
		t.Fatalf("角色数错误：期望 2，实得 %d", len(ps.characters))
	}
	if ps.characters[0].Name != "林夏" || ps.characters[0].Persona == "" || ps.characters[0].Appearance == "" {
		t.Errorf("角色字段解析不全：%+v", ps.characters[0])
	}

	// 分镜
	if len(ps.shots) != 2 {
		t.Fatalf("镜头数错误：期望 2，实得 %d", len(ps.shots))
	}
	s0 := ps.shots[0]
	if s0.SceneHeading != "办公室-夜-内" || s0.ShotType != "全景" || s0.Camera != "推" {
		t.Errorf("镜头字段解析错误：%+v", s0)
	}
	if s0.Dialogue != "这真的是我想要的吗？" {
		t.Errorf("台词解析错误：%q", s0.Dialogue)
	}

	// 角色引用应被译为角色 ID（林夏 → char_1）
	if s0.CharID != "char_1" {
		t.Errorf("角色引用未译为 ID：%q", s0.CharID)
	}
	if ps.shots[1].CharID != "char_2" {
		t.Errorf("第二镜角色引用错误：%q", ps.shots[1].CharID)
	}
}

// TestParseScreenplayAutoCharacter 验证：分镜引用了未在角色表声明的角色时自动补建，流程不中断。
func TestParseScreenplayAutoCharacter(t *testing.T) {
	script := `# 标题：测试
## 分镜
### 镜头
角色：路人甲
台词：你好。`

	ps, err := parseScreenplay(script)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(ps.characters) != 1 || ps.characters[0].Name != "路人甲" {
		t.Errorf("未自动补建角色：%+v", ps.characters)
	}
	if ps.shots[0].CharID != ps.characters[0].ID {
		t.Errorf("自动补建角色未正确关联：%q vs %q", ps.shots[0].CharID, ps.characters[0].ID)
	}
}

// TestParseScreenplayEmpty 验证：无镜头的剧本应报错，避免静默产出空视频。
func TestParseScreenplayEmpty(t *testing.T) {
	if _, err := parseScreenplay("# 标题：空剧本\n## 角色\n- 张三"); err == nil {
		t.Error("无镜头剧本应返回错误，却未报错")
	}
}

// TestParseScreenplayGender 验证角色行第 4 段性别解析（中/英/男女）。
func TestParseScreenplayGender(t *testing.T) {
	script := `## 角色
- 老王 | 沉稳 | 大叔 | 男
- 小美 | 活泼 | 少女 | female
- 路人 | 神秘 | 黑衣

## 分镜
### 镜头
角色：老王
台词：交给我。`

	ps, err := parseScreenplay(script)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if ps.characters[0].Gender != "male" {
		t.Errorf("「男」未解析为 male：%q", ps.characters[0].Gender)
	}
	if ps.characters[1].Gender != "female" {
		t.Errorf("「female」未解析为 female：%q", ps.characters[1].Gender)
	}
	if ps.characters[2].Gender != "" {
		t.Errorf("未标性别应为空（留待猜测）：%q", ps.characters[2].Gender)
	}
}
func TestParseScreenplayEnglishHeaders(t *testing.T) {
	script := `# Title: Test

## Characters
- Alice | brave | short hair

## Shots
### Shot 1
location: cafe
camera: 推
role: Alice
line: Hello world.`

	ps, err := parseScreenplay(script)
	if err != nil {
		t.Fatalf("英文标题解析失败: %v", err)
	}
	if len(ps.characters) != 1 || ps.characters[0].Name != "Alice" {
		t.Errorf("英文「## Characters」未识别：%+v", ps.characters)
	}
	if len(ps.shots) != 1 {
		t.Fatalf("英文「## Shots」未识别，镜头数=%d", len(ps.shots))
	}
	if ps.shots[0].Location != "cafe" || ps.shots[0].Dialogue != "Hello world." {
		t.Errorf("英文字段解析错误：%+v", ps.shots[0])
	}
	// 角色引用（role: Alice）应译为已声明角色的 ID
	if ps.shots[0].CharID != ps.characters[0].ID {
		t.Errorf("英文角色引用未关联：%q vs %q", ps.shots[0].CharID, ps.characters[0].ID)
	}
}
