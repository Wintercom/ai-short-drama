package services

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cuiwenyang/ai-short-drama/internal/fsx"
)

// LocalT2I 是零成本的文生图实现：用 SVG 排版关键帧画面，
// 经 macOS 自带 qlmanage 渲染为 PNG（支持中文），再用 ffmpeg 规整到精确分辨率。
//
// 为什么不用 ffmpeg drawtext：Homebrew 的 ffmpeg bottle 未编译 libfreetype，
// 无 drawtext 滤镜，无法直接在画面渲染中文。SVG+qlmanage 是 macOS 上
// 零额外安装、且中文排版更灵活的方案。后续可无缝替换为真实 T2I 大模型。
//
// 一致性设计：seed 决定背景配色方案，同一角色（固定 seed）跨镜头配色统一，
// 是对"画风一致"的占位级模拟；接入真实模型后 seed 直接作为采样种子。
type LocalT2I struct {
	FFmpeg string
	Font   string // CJK 字体名（SVG font-family）
	Width  int
	Height int
}

// NewLocalT2I 构造本地文生图。
func NewLocalT2I(ffmpeg string, w, h int) *LocalT2I {
	return &LocalT2I{FFmpeg: ffmpeg, Font: "PingFang SC", Width: w, Height: h}
}

// 预设配色方案：由 seed 选定，保证同角色跨镜头视觉调性一致。
var palettes = [][2]string{
	{"#1a2238", "#394867"}, // 冷蓝
	{"#2d132c", "#801336"}, // 暗红
	{"#0b3d2e", "#1e6f5c"}, // 墨绿
	{"#3a2618", "#8d6e4f"}, // 暖棕
	{"#1b1b2f", "#4a306d"}, // 紫夜
}

// Generate 生成镜头关键帧。
// refImage 在真实模型中作为角色参考图注入；本地实现下用于在画面标注角色，
// 体现"角色一致性"的数据流向。
func (t *LocalT2I) Generate(ctx context.Context, prompt, refImage string, seed int, outPath string) error {
	if err := fsx.EnsureDir(filepath.Dir(outPath)); err != nil {
		return err
	}

	pal := palettes[((seed%len(palettes))+len(palettes))%len(palettes)]
	svg := t.buildSVG(prompt, refImage, pal)

	// 1) 写临时 SVG
	tmpSVG := outPath + ".svg"
	if err := fsx.WriteFile(tmpSVG, svg); err != nil {
		return err
	}
	defer os.Remove(tmpSVG)

	// 2) qlmanage 渲染 SVG → PNG（按最长边缩放，得到方形画布）
	rawPNG := outPath + ".raw.png"
	defer os.Remove(rawPNG)
	if err := t.qlRender(ctx, tmpSVG, rawPNG); err != nil {
		return err
	}

	// 3) ffmpeg 规整到精确 W×H（qlmanage 按最长边缩放，必须 scale+pad 修正宽高比）
	vf := fmt.Sprintf(
		"scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2",
		t.Width, t.Height, t.Width, t.Height,
	)
	cmd := exec.CommandContext(ctx, t.FFmpeg, "-y", "-hide_banner", "-loglevel", "error",
		"-i", rawPNG, "-vf", vf, outPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("关键帧规整失败: %w\n%s", err, tail(string(out), 400))
	}
	return nil
}

// qlRender 调用 qlmanage 把 SVG 转 PNG。
func (t *LocalT2I) qlRender(ctx context.Context, svgPath, pngPath string) error {
	outDir := filepath.Dir(pngPath)
	cmd := exec.CommandContext(ctx, "qlmanage", "-t", "-s", strconv.Itoa(maxInt(t.Width, t.Height)),
		"-o", outDir, svgPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("qlmanage 渲染失败: %w\n%s", err, tail(string(out), 300))
	}
	// qlmanage 固定输出 "<svg文件名>.png"，重命名到目标路径。
	generated := filepath.Join(outDir, filepath.Base(svgPath)+".png")
	if !fsx.Exists(generated) {
		return fmt.Errorf("qlmanage 未生成预期文件: %s", generated)
	}
	return os.Rename(generated, pngPath)
}

// buildSVG 用提示词拼装一帧画面（渐变背景 + 标题 + 描述 + 角色标注）。
func (t *LocalT2I) buildSVG(prompt, refImage string, pal [2]string) string {
	role := ""
	if refImage != "" {
		role = "角色：" + strings.TrimSuffix(filepath.Base(refImage), filepath.Ext(refImage))
	}
	lines := wrapRunes(prompt, 18) // 中文按字数折行
	var body strings.Builder
	y := t.Height/2 - 20
	for i, ln := range lines {
		if i >= 4 {
			break
		}
		body.WriteString(fmt.Sprintf(
			`<text x="80" y="%d" font-family="%s" font-size="40" fill="#e8e8ef">%s</text>`,
			y, t.Font, xmlEscape(ln)))
		y += 60
	}

	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
<defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1">
<stop offset="0" stop-color="%s"/><stop offset="1" stop-color="%s"/></linearGradient></defs>
<rect width="%d" height="%d" fill="url(#g)"/>
<rect x="40" y="40" width="%d" height="%d" fill="none" stroke="#ffffff22" stroke-width="2"/>
<text x="80" y="130" font-family="%s" font-size="30" fill="#9ad">%s</text>
%s
</svg>`,
		t.Width, t.Height, t.Width, t.Height,
		pal[0], pal[1],
		t.Width, t.Height,
		t.Width-80, t.Height-80,
		t.Font, xmlEscape(role),
		body.String(),
	)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// wrapRunes 按 runes 数折行（适配中文等宽排版）。
func wrapRunes(s string, n int) []string {
	r := []rune(strings.TrimSpace(s))
	var out []string
	for len(r) > n {
		out = append(out, string(r[:n]))
		r = r[n:]
	}
	if len(r) > 0 {
		out = append(out, string(r))
	}
	return out
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}
