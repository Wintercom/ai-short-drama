package agents

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cuiwenyang/ai-short-drama/internal/config"
	"github.com/cuiwenyang/ai-short-drama/internal/models"
)

// TestParseCharacterImagePath 验证角色行第 5 段被解析为 RefImage。
func TestParseCharacterImagePath(t *testing.T) {
	script := `## 角色
- 林夏 | 程序员 | 短发风衣 | 女 | ./faces/linxia.png
- 陈默 | 老师 | 眼镜大衣 | 男
- 路人 | 神秘 | 黑衣 | | imgs/passerby.jpg

## 分镜
### 镜头
角色：林夏
台词：测试`

	ps, err := parseScreenplay(script)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if ps.characters[0].RefImage != "./faces/linxia.png" {
		t.Errorf("林夏画像路径解析错误：%q", ps.characters[0].RefImage)
	}
	if ps.characters[1].RefImage != "" {
		t.Errorf("陈默未指定画像，应为空：%q", ps.characters[1].RefImage)
	}
	// 第 4 段（性别）留空、第 5 段给路径的情况
	if ps.characters[2].RefImage != "imgs/passerby.jpg" {
		t.Errorf("路人画像路径解析错误：%q", ps.characters[2].RefImage)
	}
}

// TestResolveCharImagesRelative 验证相对路径相对剧本目录解析为绝对路径。
func TestResolveCharImagesRelative(t *testing.T) {
	dir := t.TempDir()
	// 造一张存在的画像
	facesDir := filepath.Join(dir, "faces")
	if err := os.MkdirAll(facesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	img := filepath.Join(facesDir, "linxia.png")
	if err := os.WriteFile(img, []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}

	chars := []models.Character{
		{Name: "林夏", RefImage: "faces/linxia.png"},  // 相对路径，存在
		{Name: "陈默", RefImage: "faces/missing.png"}, // 相对路径，不存在
		{Name: "路人", RefImage: ""},                  // 未指定
	}
	resolveCharImages(chars, dir)

	if chars[0].RefImage != img {
		t.Errorf("林夏应解析为绝对路径 %s，实际 %q", img, chars[0].RefImage)
	}
	if chars[1].RefImage != "" {
		t.Errorf("陈默画像不存在应清空回退，实际 %q", chars[1].RefImage)
	}
	if chars[2].RefImage != "" {
		t.Errorf("路人未指定应保持空，实际 %q", chars[2].RefImage)
	}
}

// TestResolveCharImagesAbsolute 验证绝对路径原样保留（存在时）。
func TestResolveCharImagesAbsolute(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "abs.png")
	if err := os.WriteFile(img, []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}
	chars := []models.Character{{Name: "甲", RefImage: img}}
	resolveCharImages(chars, "/some/other/base")
	if chars[0].RefImage != img {
		t.Errorf("绝对路径应原样保留：%q", chars[0].RefImage)
	}
}

// TestMatchFaceByName 验证按角色名在 faces 目录匹配画像（多扩展名）。
func TestMatchFaceByName(t *testing.T) {
	facesDir := t.TempDir()
	// 林夏.png 存在，陈默.jpg 存在，路人 无
	if err := os.WriteFile(filepath.Join(facesDir, "林夏.png"), []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(facesDir, "陈默.jpg"), []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := &AssetManager{cfg: &config.Config{FacesDir: facesDir}}

	if got := a.matchFaceByName("林夏"); got == "" || filepath.Base(got) != "林夏.png" {
		t.Errorf("林夏应匹配 林夏.png，实际 %q", got)
	}
	if got := a.matchFaceByName("陈默"); got == "" || filepath.Base(got) != "陈默.jpg" {
		t.Errorf("陈默应匹配 陈默.jpg，实际 %q", got)
	}
	if got := a.matchFaceByName("路人"); got != "" {
		t.Errorf("路人无画像应返回空，实际 %q", got)
	}
	// faces 目录为空配置时不匹配
	empty := &AssetManager{cfg: &config.Config{FacesDir: ""}}
	if got := empty.matchFaceByName("林夏"); got != "" {
		t.Errorf("FacesDir 为空应返回空，实际 %q", got)
	}
}
