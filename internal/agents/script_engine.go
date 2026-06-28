package agents

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/cuiwenyang/ai-short-drama/internal/config"
	"github.com/cuiwenyang/ai-short-drama/internal/fsx"
	"github.com/cuiwenyang/ai-short-drama/internal/logx"
	"github.com/cuiwenyang/ai-short-drama/internal/models"
	"github.com/cuiwenyang/ai-short-drama/internal/services"
)

// ScriptEngine 是创意剧本引擎（流水线第一个智能体）。
//
// 支持两种入口，二者产出同构、汇入同一黑板，下游流程完全一致：
//
//   - 剧本模式（Project.Source=="script"）：把用户提供的"文本剧本"离线解析为分镜。
//     纯规则解析、不依赖 LLM，保证"文本→视频"主链零成本、可离线、不中断。
//   - 创意模式（默认）：用 LLM "分层递进"生成剧本，逐级约束、层层细化：
//     大纲(Outline) → 角色(Characters) → 分镜脚本(Shots)。
//
// 无论哪种入口，每一层都写回 ProjectState 黑板。
type ScriptEngine struct {
	cfg *config.Config
	llm services.LLM
}

// NewScriptEngine 构造剧本引擎。
func NewScriptEngine(cfg *config.Config, llm services.LLM) *ScriptEngine {
	return &ScriptEngine{cfg: cfg, llm: llm}
}

// Name 节点名。
func (a *ScriptEngine) Name() string { return "script_engine" }

// Verify 报告剧本产物是否就绪：已解析出镜头即视为完成（状态型产物，无磁盘文件）。
func (a *ScriptEngine) Verify(st *models.ProjectState) bool {
	return len(st.Shots) > 0
}

// Run 按入口分发：剧本模式走离线解析，创意模式走 AI 分层生成。
func (a *ScriptEngine) Run(ctx context.Context, st *models.ProjectState) error {
	if st.Project.Source == "script" {
		return a.runFromScript(ctx, st)
	}
	return a.runFromIdea(ctx, st)
}

// runFromScript 解析用户提供的文本剧本（"文本→视频"入口）。
func (a *ScriptEngine) runFromScript(ctx context.Context, st *models.ProjectState) error {
	logx.Stage("📝", "剧本引擎：解析文本剧本")

	// 已解析过则跳过（断点续跑）。
	if len(st.Shots) > 0 {
		logx.Step("剧本已解析，跳过")
		return nil
	}

	ps, err := parseScreenplay(st.Project.Script)
	if err != nil {
		return fmt.Errorf("解析文本剧本失败: %w", err)
	}

	st.Outline = ps.outline
	st.Characters = ps.characters
	resolveCharImages(st.Characters, st.Project.ScriptBaseDir) // 把相对画像路径解析为绝对路径
	scenes, shots := assembleScenes(ps.shots, ps.characters)
	st.Scenes = scenes
	st.Shots = shots

	logx.Done("剧本解析完成：《%s》", st.Outline.Title)
	logx.Step("角色 %d 位 / 场景 %d 场 / 镜头 %d 镜", len(st.Characters), len(scenes), len(shots))
	for _, c := range st.Characters {
		logx.Step("%s（%s）", c.Name, c.ID)
	}
	return nil
}

// resolveCharImages 把用户在剧本里写的相对画像路径解析为绝对路径（相对剧本目录）。
// 绝对路径原样保留；路径指向的文件不存在时打印警告并清空，回退到 AI 生成锚点（不中断）。
func resolveCharImages(chars []models.Character, baseDir string) {
	for i := range chars {
		ref := chars[i].RefImage
		if ref == "" {
			continue
		}
		if !filepath.IsAbs(ref) && baseDir != "" {
			ref = filepath.Join(baseDir, ref)
		}
		if !fsx.Exists(ref) {
			logx.Warn("角色[%s]指定画像不存在：%s（将回退 AI 生成锚点）", chars[i].Name, ref)
			chars[i].RefImage = ""
			continue
		}
		chars[i].RefImage = ref
		logx.Step("角色[%s]使用指定画像：%s", chars[i].Name, filepath.Base(ref))
	}
}

// runFromIdea 执行三层递进生成（创意 → 剧本）。
func (a *ScriptEngine) runFromIdea(ctx context.Context, st *models.ProjectState) error {
	logx.Stage("📝", "剧本引擎：分层递进生成剧本")

	// —— 第 1 层：大纲 ——
	if st.Outline.Title == "" {
		outline, err := a.genOutline(ctx, st.Project)
		if err != nil {
			return fmt.Errorf("生成大纲失败: %w", err)
		}
		st.Outline = outline
		logx.Done("大纲已生成：《%s》—— %s", outline.Title, outline.Logline)
	} else {
		logx.Step("大纲已存在，跳过")
	}

	// —— 第 2 层：角色（注入大纲约束）——
	if len(st.Characters) == 0 {
		chars, err := a.genCharacters(ctx, st.Project, st.Outline)
		if err != nil {
			return fmt.Errorf("生成角色失败: %w", err)
		}
		st.Characters = chars
		logx.Done("角色已生成：%d 位", len(chars))
		for _, c := range chars {
			logx.Step("%s（%s）：%s", c.Name, c.ID, c.Persona)
		}
	} else {
		logx.Step("角色已存在，跳过")
	}

	// —— 第 3 层：分镜脚本（注入大纲+角色约束）——
	if len(st.Shots) == 0 {
		scenes, shots, err := a.genShots(ctx, st.Project, st.Outline, st.Characters)
		if err != nil {
			return fmt.Errorf("生成分镜失败: %w", err)
		}
		st.Scenes = scenes
		st.Shots = shots
		logx.Done("分镜已生成：%d 场 / %d 镜", len(scenes), len(shots))
	} else {
		logx.Step("分镜已存在，跳过")
	}

	return nil
}

// genOutline 生成顶层大纲。
func (a *ScriptEngine) genOutline(ctx context.Context, p models.Project) (models.Outline, error) {
	system := "你是资深短剧编剧。只输出 JSON，不要任何解释文字。"
	user := fmt.Sprintf(`[[TASK:outline]]
请为以下短剧创意构思大纲。
IDEA: %s
GENRE: %s
输出 JSON 对象，字段：title(剧名), logline(一句话故事), theme(主题), synopsis(梗概), beats(关键节拍字符串数组,4条起承转合)。`,
		p.Idea, p.Genre)

	raw, err := a.llm.Complete(ctx, system, user)
	if err != nil {
		return models.Outline{}, err
	}
	var o models.Outline
	if err := unmarshalLoose(raw, &o); err != nil {
		return models.Outline{}, fmt.Errorf("解析大纲 JSON 失败: %w\n原文: %s", err, raw)
	}
	if o.Title == "" {
		o.Title = "无题短剧"
	}
	return o, nil
}

// genCharacters 基于大纲生成角色表。
func (a *ScriptEngine) genCharacters(ctx context.Context, p models.Project, o models.Outline) ([]models.Character, error) {
	system := "你是资深角色设定师。只输出 JSON 数组，不要任何解释文字。"
	user := fmt.Sprintf(`[[TASK:characters]]
基于以下故事设定 2-3 个主要角色。
IDEA: %s
GENRE: %s
剧名: %s
梗概: %s
输出 JSON 数组，每个元素字段：id(英文小写下划线,如 char_lead), name(中文名), persona(性格背景), appearance(外貌描述,用于生成形象参考图)。`,
		p.Idea, p.Genre, o.Title, o.Synopsis)

	raw, err := a.llm.Complete(ctx, system, user)
	if err != nil {
		return nil, err
	}
	var chars []models.Character
	if err := unmarshalLoose(raw, &chars); err != nil {
		return nil, fmt.Errorf("解析角色 JSON 失败: %w\n原文: %s", err, raw)
	}
	if len(chars) == 0 {
		return nil, fmt.Errorf("未生成任何角色")
	}
	// 兜底：确保每个角色都有 ID。
	for i := range chars {
		if chars[i].ID == "" {
			chars[i].ID = fmt.Sprintf("char_%d", i+1)
		}
	}
	return chars, nil
}

// shotDTO 是分镜的中间解析结构（LLM 返回扁平结构，再拆解为 Scene+Shot）。
type shotDTO struct {
	SceneHeading   string `json:"scene_heading"`
	Location       string `json:"location"`
	TimeOfDay      string `json:"time_of_day"`
	CharID         string `json:"char_id"`
	ShotType       string `json:"shot_type"`
	Camera         string `json:"camera"`
	KeyframePrompt string `json:"keyframe_prompt"`
	Dialogue       string `json:"dialogue"`
}

// genShots 基于大纲+角色生成分镜脚本，并拆解为场景与镜头。
func (a *ScriptEngine) genShots(ctx context.Context, p models.Project, o models.Outline, chars []models.Character) ([]models.Scene, []models.Shot, error) {
	// 注入角色 ID 清单，约束 LLM 只能引用已有角色，保证人物一致。
	var charList string
	for _, c := range chars {
		charList += fmt.Sprintf("%s=%s; ", c.ID, c.Name)
	}

	system := "你是资深分镜师。只输出 JSON 数组，不要任何解释文字。"
	user := fmt.Sprintf(`[[TASK:shots]]
基于以下故事与角色，编写 4-6 个镜头的分镜脚本，覆盖完整起承转合。
IDEA: %s
GENRE: %s
剧名: %s
节拍: %v
可用角色(只能引用这些 char_id): %s
输出 JSON 数组，每个镜头字段：scene_heading(场景标题), location(地点), time_of_day(时间), char_id(出镜主角,必须是上面列出的), shot_type(景别:远景/全景/中景/近景/特写), camera(运镜:推/拉/摇/移/固定), keyframe_prompt(画面描述), dialogue(该镜头对白)。`,
		p.Idea, p.Genre, o.Title, o.Beats, charList)

	raw, err := a.llm.Complete(ctx, system, user)
	if err != nil {
		return nil, nil, err
	}
	var dtos []shotDTO
	if err := unmarshalLoose(raw, &dtos); err != nil {
		return nil, nil, fmt.Errorf("解析分镜 JSON 失败: %w\n原文: %s", err, raw)
	}
	if len(dtos) == 0 {
		return nil, nil, fmt.Errorf("未生成任何镜头")
	}

	scenes, shots := assembleScenes(dtos, chars)
	return scenes, shots, nil
}

// assembleScenes 把扁平的分镜 DTO 拆解为去重后的场景列表与有序镜头列表。
// 相同 scene_heading 的连续镜头归入同一场景，并校正非法的 char_id（回退到首个角色）。
func assembleScenes(dtos []shotDTO, chars []models.Character) ([]models.Scene, []models.Shot) {
	valid := map[string]bool{}
	for _, c := range chars {
		valid[c.ID] = true
	}
	fallback := ""
	if len(chars) > 0 {
		fallback = chars[0].ID
	}

	var scenes []models.Scene
	var shots []models.Shot
	sceneIndex := map[string]string{} // heading -> scene id

	for i, d := range dtos {
		// 场景去重
		sid, ok := sceneIndex[d.SceneHeading]
		if !ok {
			sid = fmt.Sprintf("scene_%d", len(scenes)+1)
			sceneIndex[d.SceneHeading] = sid
			scenes = append(scenes, models.Scene{
				ID:        sid,
				Index:     len(scenes) + 1,
				Heading:   d.SceneHeading,
				Location:  d.Location,
				TimeOfDay: d.TimeOfDay,
				Summary:   d.KeyframePrompt,
			})
		}

		// 校正非法角色引用，保证人物一致
		cid := d.CharID
		if !valid[cid] {
			cid = fallback
		}

		shots = append(shots, models.Shot{
			ID:             fmt.Sprintf("shot_%02d", i+1),
			SceneID:        sid,
			Index:          i + 1,
			ShotType:       orStr(d.ShotType, "中景"),
			Camera:         orStr(d.Camera, "固定"),
			CharID:         cid,
			KeyframePrompt: d.KeyframePrompt,
			Dialogue:       d.Dialogue,
			Duration:       0, // 由音频时长回填，先置 0
		})
	}
	return scenes, shots
}

func orStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
