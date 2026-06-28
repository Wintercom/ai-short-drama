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
// 升级版画面层次：渐变背景 + 场景氛围线条 + 人物剪影（按性别/景别区分）+ 台词字幕条。
// 这让画面从"信息卡"升级为可辨识的"可视化故事板（storyboard）"风格。
//
// prompt 编码约定（由 storyboard 智能体拼入，LocalT2I 解析使用）：
//
//	"画面描述||gender:male||dialogue:台词||scene:室内-夜||shottype:全景"
//
// 无以上标注时降级为纯文字画面（向后兼容）。
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

// shotMeta 是从 prompt 中解析出的结构化元信息。
type shotMeta struct {
	description string // 主画面描述
	gender      string // male/female/""
	appearance  string // 角色外貌（短发/风衣/年龄等），角色一致性的关键锚点
	dialogue    string // 台词
	scene       string // 场景（室内/室外/夜/日等）
	shotType    string // 景别：全景/近景/特写等
}

// parsePrompt 从 prompt 中提取结构化元信息。
// prompt 格式："描述||gender:male||appearance:外貌||dialogue:台词||scene:室内-夜||shottype:全景"
// 不含分隔符时，整段作为 description，其余字段为空。
func parsePrompt(prompt string) shotMeta {
	parts := strings.Split(prompt, "||")
	m := shotMeta{description: strings.TrimSpace(parts[0])}
	for _, p := range parts[1:] {
		p = strings.TrimSpace(p)
		if v, ok := strings.CutPrefix(p, "gender:"); ok {
			m.gender = strings.TrimSpace(v)
		} else if v, ok := strings.CutPrefix(p, "appearance:"); ok {
			m.appearance = strings.TrimSpace(v)
		} else if v, ok := strings.CutPrefix(p, "dialogue:"); ok {
			m.dialogue = strings.TrimSpace(v)
		} else if v, ok := strings.CutPrefix(p, "scene:"); ok {
			m.scene = strings.TrimSpace(v)
		} else if v, ok := strings.CutPrefix(p, "shottype:"); ok {
			m.shotType = strings.TrimSpace(v)
		}
	}
	return m
}

// Generate 生成镜头关键帧。
func (t *LocalT2I) Generate(ctx context.Context, prompt, refImage string, seed int, outPath string) error {
	if err := fsx.EnsureDir(filepath.Dir(outPath)); err != nil {
		return err
	}

	pal := palettes[((seed%len(palettes))+len(palettes))%len(palettes)]
	meta := parsePrompt(prompt)
	svg := t.buildSVG(meta, refImage, pal)

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

// buildSVG 生成包含人物剪影、场景背景、台词字幕的分镜帧。
func (t *LocalT2I) buildSVG(meta shotMeta, refImage string, pal [2]string) string {
	W, H := t.Width, t.Height

	// 场景背景层（根据场景关键词选择不同背景线条元素）
	bgLayer := t.buildSceneBG(meta.scene, W, H, pal)

	// 人物剪影层（按性别和景别选取）
	figureLayer := t.buildFigure(meta.gender, meta.shotType, W, H)

	// 画面描述文字（左上角小字）
	descLines := wrapRunes(meta.description, 20)
	var descSVG strings.Builder
	dy := 56
	for i, ln := range descLines {
		if i >= 3 {
			break
		}
		descSVG.WriteString(fmt.Sprintf(
			`<text x="30" y="%d" font-family="%s" font-size="22" fill="#ffffffaa">%s</text>`,
			dy, t.Font, xmlEscape(ln)))
		dy += 30
	}

	// 台词字幕条（底部，有台词才显示）
	subtitleLayer := t.buildSubtitle(meta.dialogue, W, H)

	// 角色名标注（右下角小字，来自 refImage 文件名）
	charLabel := ""
	if refImage != "" {
		name := strings.TrimSuffix(filepath.Base(refImage), filepath.Ext(refImage))
		// 去掉 "char_N_" 前缀
		if idx := strings.Index(name, "_"); idx >= 0 {
			if idx2 := strings.Index(name[idx+1:], "_"); idx2 >= 0 {
				name = name[idx+1+idx2+1:]
			}
		}
		charLabel = fmt.Sprintf(
			`<text x="%d" y="%d" font-family="%s" font-size="20" fill="#ffffffcc" text-anchor="end">%s</text>`,
			W-20, H-16, t.Font, xmlEscape(name))
	}

	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
<defs>
  <linearGradient id="bg" x1="0" y1="0" x2="0.6" y2="1">
    <stop offset="0" stop-color="%s"/><stop offset="1" stop-color="%s"/>
  </linearGradient>
</defs>
<!-- 渐变背景 -->
<rect width="%d" height="%d" fill="url(#bg)"/>
<!-- 场景背景线条 -->
%s
<!-- 人物剪影 -->
%s
<!-- 画面描述 -->
%s
<!-- 台词字幕 -->
%s
<!-- 角色标注 -->
%s
</svg>`,
		W, H, W, H,
		pal[0], pal[1],
		W, H,
		bgLayer, figureLayer, descSVG.String(), subtitleLayer, charLabel,
	)
}

// buildSceneBG 根据场景关键词生成背景装饰线条（室内/室外/日/夜）。
func (t *LocalT2I) buildSceneBG(scene string, W, H int, pal [2]string) string {
	var sb strings.Builder
	isNight := strings.Contains(scene, "夜") || strings.Contains(scene, "night")
	isIndoor := strings.Contains(scene, "内") || strings.Contains(scene, "室内") || strings.Contains(scene, "indoor")
	isOutdoor := strings.Contains(scene, "外") || strings.Contains(scene, "室外") || strings.Contains(scene, "outdoor")

	if isNight {
		// 夜晚：散落星点
		stars := [][2]int{{80, 60}, {200, 90}, {400, 40}, {600, 80}, {900, 50}, {1100, 100},
			{150, 140}, {500, 120}, {800, 70}, {1050, 45}, {350, 100}, {700, 130}}
		for _, s := range stars {
			if s[0] < W && s[1] < H/2 {
				sb.WriteString(fmt.Sprintf(`<circle cx="%d" cy="%d" r="2" fill="#ffffff88"/>`, s[0], s[1]))
			}
		}
		// 月亮
		sb.WriteString(fmt.Sprintf(`<circle cx="%d" cy="70" r="30" fill="#e8d56044"/>`, W-100))
		sb.WriteString(fmt.Sprintf(`<circle cx="%d" cy="58" r="27" fill="%s"/>`, W-88, pal[0]))
	} else {
		// 白天：简单地平线 + 云朵
		groundY := H * 2 / 3
		sb.WriteString(fmt.Sprintf(
			`<line x1="0" y1="%d" x2="%d" y2="%d" stroke="#ffffff22" stroke-width="1"/>`,
			groundY, W, groundY))
		// 云朵（椭圆组合）
		cloudY := H / 5
		for _, cx := range []int{W / 5, W / 2, W * 4 / 5} {
			sb.WriteString(fmt.Sprintf(
				`<ellipse cx="%d" cy="%d" rx="60" ry="20" fill="#ffffff18"/>`, cx, cloudY))
			sb.WriteString(fmt.Sprintf(
				`<ellipse cx="%d" cy="%d" rx="40" ry="16" fill="#ffffff14"/>`, cx+30, cloudY+6))
		}
	}

	if isIndoor {
		// 室内：窗框线条
		wx := W * 3 / 4
		sb.WriteString(fmt.Sprintf(
			`<rect x="%d" y="60" width="180" height="240" fill="none" stroke="#ffffff22" stroke-width="2" rx="4"/>`,
			wx))
		// 窗格中线
		sb.WriteString(fmt.Sprintf(
			`<line x1="%d" y1="60" x2="%d" y2="300" stroke="#ffffff18" stroke-width="1"/>`,
			wx+90, wx+90))
		sb.WriteString(fmt.Sprintf(
			`<line x1="%d" y1="180" x2="%d" y2="180" stroke="#ffffff18" stroke-width="1"/>`,
			wx, wx+180))
		// 桌子（室内常见道具）
		tableY := H * 7 / 8
		sb.WriteString(fmt.Sprintf(
			`<rect x="%d" y="%d" width="%d" height="8" fill="#ffffff22" rx="2"/>`,
			W/6, tableY, W*2/3))
	} else if isOutdoor {
		// 室外：远山轮廓
		mH := H / 2
		sb.WriteString(fmt.Sprintf(
			`<polygon points="0,%d %d,%d %d,%d %d,%d" fill="#ffffff0c"/>`,
			H, W/4, mH, W/2, mH+40, W, H))
		sb.WriteString(fmt.Sprintf(
			`<polygon points="%d,%d %d,%d %d,%d" fill="#ffffff08"/>`,
			W/3, H, W*2/3, mH-30, W, H))
	}

	return sb.String()
}

// buildFigure 根据性别和景别返回 SVG 矢量人物剪影。
//
// 景别决定人物大小和位置：
//   - 全景/远景：全身小剪影，居中偏下
//   - 中景：半身（腰部以上），居中
//   - 近景/特写：头肩特写，居中偏大
func (t *LocalT2I) buildFigure(gender, shotType string, W, H int) string {
	isMale := gender == "male"
	cx := W / 2 // 水平居中

	switch shotType {
	case "特写":
		return buildCloseup(cx, H, isMale, t.Font)
	case "近景":
		return buildMediumClose(cx, H, isMale, t.Font)
	case "中景":
		return buildMediumShot(cx, H, isMale, t.Font)
	default: // 全景、远景
		return buildFullShot(cx, H, isMale, t.Font)
	}
}

// buildFullShot 全景：完整站立人物剪影（小）。
func buildFullShot(cx, H int, isMale bool, font string) string {
	// 人物中心 X，脚底 Y
	footY := H * 85 / 100
	headR := 22
	bodyH := 100
	headY := footY - bodyH - headR*2

	return buildHumanFigure(cx, headY, headR, bodyH, footY, isMale, font)
}

// buildMediumShot 中景：腰部以上剪影（中）。
func buildMediumShot(cx, H int, isMale bool, font string) string {
	footY := H + 20 // 腰部在画面外，底部裁剪
	headR := 38
	bodyH := 130
	headY := H/2 - headR - bodyH/3

	return buildHumanFigure(cx, headY, headR, bodyH, footY, isMale, font)
}

// buildMediumClose 近景：肩部以上（大）。
func buildMediumClose(cx, H int, isMale bool, font string) string {
	headR := 55
	headY := H/2 - headR

	var sb strings.Builder
	// 头
	sb.WriteString(fmt.Sprintf(`<circle cx="%d" cy="%d" r="%d" fill="#00000088"/>`, cx, headY, headR))
	// 颈+肩
	neckW := headR / 3
	sb.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" fill="#00000088" rx="4"/>`,
		cx-neckW, headY+headR-4, neckW*2, 30))
	sb.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" fill="#00000088" rx="8"/>`,
		cx-headR*2, headY+headR+20, headR*4, 50))
	// 发型标识
	if isMale {
		sb.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="12" fill="#00000099" rx="3"/>`,
			cx-headR, headY-headR, headR*2))
	} else {
		// 女性：长发流线
		sb.WriteString(fmt.Sprintf(`<path d="M%d %d Q%d %d %d %d" stroke="#00000099" stroke-width="8" fill="none"/>`,
			cx-headR, headY, cx-headR-15, headY+headR+30, cx-headR-5, headY+headR+80))
		sb.WriteString(fmt.Sprintf(`<path d="M%d %d Q%d %d %d %d" stroke="#00000099" stroke-width="8" fill="none"/>`,
			cx+headR, headY, cx+headR+15, headY+headR+30, cx+headR+5, headY+headR+80))
	}
	return sb.String()
}

// buildCloseup 特写：面部大特写（最大）。
func buildCloseup(cx, H int, isMale bool, font string) string {
	headR := 90
	headY := H/2 - 10

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`<circle cx="%d" cy="%d" r="%d" fill="#00000088"/>`, cx, headY, headR))
	// 眼睛（两个小椭圆）
	eyeY := headY - headR/5
	eyeOff := headR / 3
	sb.WriteString(fmt.Sprintf(`<ellipse cx="%d" cy="%d" rx="12" ry="7" fill="#ffffff44"/>`, cx-eyeOff, eyeY))
	sb.WriteString(fmt.Sprintf(`<ellipse cx="%d" cy="%d" rx="12" ry="7" fill="#ffffff44"/>`, cx+eyeOff, eyeY))
	// 嘴
	mouthY := headY + headR/3
	sb.WriteString(fmt.Sprintf(`<path d="M%d %d Q%d %d %d %d" stroke="#ffffff55" stroke-width="3" fill="none"/>`,
		cx-14, mouthY, cx, mouthY+8, cx+14, mouthY))
	// 发型
	if isMale {
		sb.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="18" fill="#00000099" rx="4"/>`,
			cx-headR, headY-headR, headR*2))
	} else {
		for _, side := range []int{-1, 1} {
			sx := cx + side*(headR)
			sb.WriteString(fmt.Sprintf(`<path d="M%d %d Q%d %d %d %d" stroke="#00000099" stroke-width="12" fill="none"/>`,
				sx, headY-headR/2, sx+side*20, headY+headR/2, sx+side*10, headY+headR+40))
		}
	}
	return sb.String()
}

// buildHumanFigure 绘制通用站立人物剪影（全景/中景共用基础形状）。
func buildHumanFigure(cx, headY, headR, bodyH, footY int, isMale bool, font string) string {
	var sb strings.Builder
	// 头
	sb.WriteString(fmt.Sprintf(`<circle cx="%d" cy="%d" r="%d" fill="#00000088"/>`, cx, headY+headR, headR))
	// 颈
	neckW := headR / 3
	neckY := headY + headR*2
	sb.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" fill="#00000088"/>`,
		cx-neckW, neckY, neckW*2, headR))
	// 躯干
	torsoW := headR * 2
	torsoY := neckY + headR
	sb.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" fill="#00000088" rx="6"/>`,
		cx-torsoW, torsoY, torsoW*2, bodyH))
	// 腿（只在全景显示）
	if footY-torsoY-bodyH > 20 {
		legY := torsoY + bodyH
		legW := headR * 2 / 3
		sb.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" fill="#00000088" rx="4"/>`,
			cx-legW*2+legW/2, legY, legW, footY-legY))
		sb.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" fill="#00000088" rx="4"/>`,
			cx+legW/2, legY, legW, footY-legY))
	}
	// 发型区分性别
	if !isMale {
		// 女性长发
		hairY := headY + headR/2
		for _, side := range []int{-1, 1} {
			sx := cx + side*(headR)
			sb.WriteString(fmt.Sprintf(`<path d="M%d %d Q%d %d %d %d" stroke="#00000088" stroke-width="6" fill="none"/>`,
				sx, hairY, sx+side*12, hairY+headR+20, sx+side*6, hairY+headR*2+20))
		}
	}
	return sb.String()
}

// buildSubtitle 在画面底部生成台词字幕条。
func (t *LocalT2I) buildSubtitle(dialogue string, W, H int) string {
	if dialogue == "" {
		return ""
	}
	// 最多显示两行，每行 22 字
	lines := wrapRunes(dialogue, 22)
	if len(lines) > 2 {
		lines = lines[:2]
	}
	barH := 36 + len(lines)*44
	barY := H - barH - 16

	var sb strings.Builder
	// 半透明黑底字幕条
	sb.WriteString(fmt.Sprintf(
		`<rect x="0" y="%d" width="%d" height="%d" fill="#000000bb" rx="4"/>`,
		barY, W, barH))
	// 台词文字（白色，居中）
	for i, ln := range lines {
		ty := barY + 38 + i*44
		sb.WriteString(fmt.Sprintf(
			`<text x="%d" y="%d" font-family="%s" font-size="32" fill="#ffffff" text-anchor="middle">%s</text>`,
			W/2, ty, t.Font, xmlEscape(ln)))
	}
	return sb.String()
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
