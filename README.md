# ai-short-drama

> 基于多智能体协作的 AI 短剧全流程自动化创作系统 —— 从一句创意到完整视频成片。

通过总控调度层串联**剧本引擎、资产/角色管理、视觉分镜、音频合成、后期合成**五大智能体，实现"文本→视频"的工业化闭环生产，针对性解决 AI 视频创作的三大痛点：**流程割裂、角色一致性差、长篇叙事断裂**。

## ✨ 特性

- **两种创作入口**：①直接输入**文本剧本**（`-script`，离线解析、零成本、不经 LLM，对齐"流程不中断"）；②一句**创意**由 AI 生成剧本（`-idea`）。二者产出同构，下游流程完全一致。
- **零配置跑通闭环**：默认用离线 LLM + ffmpeg + macOS 系统语音，**无需任何 API key、零成本**即可产出真实 mp4。
- **多智能体黑板协作**：五大智能体通过统一的 `ProjectState` 共享状态协作，流程不割裂。
- **角色一致性**：角色注册表锁定（参考图 + 种子 + 音色），同一角色跨镜头长相/画风/声音统一。
- **长篇连贯**：分层递进生成（大纲→角色→分镜）+ 叙事记忆注入，剧情不脱节。
- **工程化**：DAG 编排、镜头级并发、断点续跑、产物缓存——视频生成贵且慢，绝不重复烧算力。
- **可插拔**：所有 AI 能力封装为接口，换供应商（DeepSeek/可灵/ElevenLabs…）只改一处工厂，不动业务。

## 🚀 快速开始

### 环境依赖

```bash
# 必需
brew install go ffmpeg

# 可选：macOS 自带 say 命令用于配音；不装也会自动降级
```

### 构建与运行

```bash
# 构建
go build -o bin/drama ./cmd/drama

# 入口一：文本剧本 → 视频（基础闭环，离线零成本）
./bin/drama -script examples/screenplay.txt
cat examples/screenplay.txt | ./bin/drama -script -   # 也支持 stdin 管道

# 入口二：一句创意 → AI 生成剧本 → 视频
./bin/drama -idea "一个程序员重拾儿时画家梦想的故事" -genre 治愈

# 成片输出在 workspace/{project_id}/final/output.mp4
```

### 文本剧本格式

完整样例见 [examples/screenplay.txt](examples/screenplay.txt)，离线确定性解析，无需 LLM：

```
# 标题：重拾画笔
# 题材：治愈
# 主题：勇气与自我和解

## 角色
- 林夏 | 坚韧而敏感的程序员，怀揣画家梦 | 二十多岁，短发，风衣
- 陈默 | 沉稳的画室老师 | 三十多岁，眼镜，深色大衣

## 分镜
### 镜头一
场景：办公室-夜-内
角色：林夏
景别：全景
运镜：推
画面：深夜空荡的办公室，只剩林夏一人对着屏幕
台词：又是凌晨两点……这真的是我想要的生活吗？
```

运行示例输出：

```
=== 📝  剧本引擎：分层递进生成剧本 ===
  ✓ 大纲已生成：《重拾画笔》
  ✓ 角色已生成：2 位
  ✓ 分镜已生成：4 场 / 4 镜
=== 🎭  资产管理器：锁定角色一致性 ===
  ✓ 林夏：seed=206565 音色=Tingting
=== 🎬  视觉分镜：并发生成关键帧与运镜片段 ===
=== 🔊  音频合成：按角色锁定音色并发配音 ===
=== 🎞️  后期合成：音画对齐并拼接成片 ===
  ✓ 成片已输出：workspace/.../output.mp4（总时长 18.1s）
```

### 断点续跑

```bash
# 中断后从已完成处继续，已生成产物直接复用
./bin/drama -resume <project_id>
```

### 升级到真实模型（可选）

复制 `.env.example` 为 `.env`，填入 LLM 配置即可切换（推荐 DeepSeek，几分钱一部；或本地 Ollama 完全免费）：

```bash
LLM_PROVIDER=openai-compatible
LLM_BASE_URL=https://api.deepseek.com/v1
LLM_MODEL=deepseek-chat
LLM_API_KEY=sk-xxx
```

## 🏗️ 架构

五层架构 + 黑板模式，数据通过 `ProjectState` 单向流动：

```
创意 →[剧本引擎]→[资产管理]→ ┬[视觉分镜]┬ →[后期合成]→ 成片.mp4
                            └[音频合成]┘
```

详细设计、痛点解法、可插拔机制见 **[docs/architecture.md](docs/architecture.md)**。

## 🧪 测试

```bash
go test ./...
```

## 📂 项目结构

```
cmd/drama/            CLI 入口
internal/
├── orchestrator/     总控调度：DAG、Runner、Checkpoint、State
├── agents/           五大智能体
├── services/         能力服务：LLM/T2I/I2V/TTS/Editor（可插拔）
├── memory/           角色注册表 + 叙事记忆
├── models/           数据契约（ProjectState 等）
├── config/           配置加载
└── ...               日志、文件工具
docs/architecture.md  架构文档
```

## 📜 技术选型

纯 **Go 标准库**实现，零第三方依赖；多媒体处理基于 **ffmpeg**。详见架构文档。
