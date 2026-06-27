# CLAUDE.md

本文件为 Claude Code（claude.ai/code）在此代码库中工作时提供指导。

## 项目概述

**ai-short-drama** 是一个基于多智能体协作的 AI 短剧自动创作系统，实现从剧本生成到视频渲染的全流程自动化。

核心设计：以 **ProjectState 黑板模式**串联五大智能体，用**角色注册表**保证角色一致性，用**叙事记忆 + 分层递进**保证长篇连贯——针对性地解决 AI 视频创作的三大行业痛点（流程割裂、角色一致性差、长篇叙事断裂）。

详细架构见 [docs/architecture.md](docs/architecture.md)。

## 常用命令

```bash
# 构建
go build -o bin/drama ./cmd/drama

# 入口一：文本剧本 → 视频（基础闭环，离线零成本，不经 LLM）
./bin/drama -script examples/screenplay.txt
cat examples/screenplay.txt | ./bin/drama -script -   # 也支持 stdin

# 入口二：创意 → AI 生成剧本 → 视频
./bin/drama -idea "一个程序员重拾儿时画家梦想的故事" -genre 治愈

# 断点续跑（跳过已完成节点，不重复烧算力）
./bin/drama -resume <project_id>

# 测试
go test ./...                              # 全部
go test ./internal/agents/ -run TestJSONBlock -v   # 单个

# 格式化与静态检查
gofmt -w . && go vet ./...
```

成片输出在 `workspace/{project_id}/final/output.mp4`，过程产物（角色图、关键帧、片段、配音）同目录留存。

**文本剧本格式**（`examples/screenplay.txt` 为完整样例，离线确定性解析，见 `agents/script_parser.go`）：

```
# 标题：重拾画笔        # 元信息：标题/题材/主题/梗概/节拍
## 角色
- 林夏 | 性格设定 | 外貌描述
## 分镜
### 镜头一
场景：办公室-夜-内    角色：林夏    景别：全景    运镜：推
画面：画面描述        台词：对白
```

## 环境依赖

- **Go 1.24+**
- **ffmpeg / ffprobe**：视频渲染核心（`brew install ffmpeg`）
- **macOS `say`**（可选）：系统语音配音，缺失时自动降级为静音轨
- **LLM**（可选）：默认用内置离线 Stub（零成本）；配置 `LLM_API_KEY` 可切 DeepSeek/Ollama 等 OpenAI 兼容端点。配置项见 `.env.example`

> 注意：Homebrew 的 ffmpeg 未编译 `drawtext` 滤镜，本地关键帧渲染改用 **SVG → qlmanage → PNG** 路径（见 `services/t2i_local.go`）。

## 代码架构

五层架构，数据通过 ProjectState 黑板单向流动，详见 [docs/architecture.md](docs/architecture.md)：

| 目录 | 层 | 职责 |
|------|-----|------|
| `cmd/drama/` | 接入层 | CLI 入口，组装并启动流水线 |
| `internal/orchestrator/` | ① 总控调度层 | DAG 流水线、并发 Runner、断点续跑、状态黑板 |
| `internal/agents/` | ② 智能体层 | 剧本引擎 / 资产管理 / 视觉分镜 / 音频合成 / 后期合成 |
| `internal/services/` | ③ 能力服务层 | LLM、T2I、I2V、TTS、Editor（接口 + 可插拔实现） |
| `internal/memory/` | ④ 记忆层 | 角色注册表（一致性）、叙事记忆（连贯性） |
| `internal/models/` | 数据契约 | ProjectState 及 Project/Character/Shot 等强类型结构 |

**关键约定**：

- 所有智能体实现 `orchestrator.Agent` 接口（`Name()` + `Run(ctx, *ProjectState)`），**只通过 ProjectState 黑板交换数据**，彼此不直接调用——这是消灭"流程割裂"的根本手段。
- 智能体层依赖 `services` 的**接口**而非具体实现；换供应商（如把本地 ffmpeg 换成真实视频大模型）只改 `services/factory.go`，不动编排与智能体。
- 镜头级并发在智能体**内部**用 goroutine + 信号量实现（见 `storyboard.go` / `audio_synth.go`）；节点级按 DAG 拓扑顺序驱动以保证可续跑。
- 产物缓存：已存在且非空的产物直接跳过（`fsx.Exists`），视频生成贵且慢，避免重复生成。

## 架构设计意图

系统采用**多智能体协作 + 黑板模式**，五大智能体按流水线协作：

1. **剧本引擎**（`script_engine.go`）— 双入口：①文本剧本离线解析（`script_parser.go`）；②创意分层递进生成（大纲→角色→分镜）。二者产出同构，汇入同一黑板
2. **资产/角色管理**（`asset_manager.go`）— 锁定角色一致性三要素（参考图 + seed + 音色）
3. **视觉分镜**（`storyboard.go`）— 并发生成关键帧（T2I）与运镜片段（I2V）
4. **音频合成**（`audio_synth.go`）— 按角色锁定音色并发配音（TTS）
5. **后期合成**（`compositor.go`）— 音画对齐并拼接成最终视频

## 语言要求

| 场景 | 语言 | 示例 |
|-----|------|------|
| 回复、注释、commit、PR | 中文 | `// 检查域名是否存在` |
| 代码（函数名、变量名） | 英文 | `checkDomainExists` |
| 专有名词、缩写 | 英文 | MongoDB、HTTP、API |
| error message、log | 英文 | `errors.New("domain already exists")` |

## 编程语言

优先使用Go语言

## 编码规范

- **包名**：小写无下划线 (`fusioncdn`)
- **文件名**：蛇形命名 (`router_domain.go`)
- **导入分组**：标准库 → 公共包 → 私有包
- **函数**：保持 50 行以下

## 开发原则

### 1. 先设计后编码
- 清晰描述实现方案后再编码
- 需求不明确时**先澄清**，不基于猜测编码

### 2. 任务分解
- 涉及 >3 个文件时，必须分解为子任务
- 按顺序逐个完成，避免大范围同时修改

### 3. 代码自审
- 检查逻辑、边界条件、错误处理
- 编写测试覆盖正常流程、边界、错误场景

### 4. Bug 修复（TDD）
1. 先写能重现 Bug 的测试
2. 确认测试失败
3. 修复代码
4. 确认测试通过
5. 确保不破坏其他测试

### 5. 持续学习规则

每次用户纠正 Claude 的错误后，需要：
- 在本章节下方的「经验教训」中添加新规则
- 规则应具体、可执行，防止类似问题再次发生

 ### 6. 自动更新文档
- 每次新开发的服务，代码，文档等需要及时总结更新CLAUDE.md

### 7. 文档精简高效
- 保持CLAUDE.md的行数在合理范围内，如果涉及更长篇幅的文档，需要作为子md文档，外链到CLAUDE.md中


