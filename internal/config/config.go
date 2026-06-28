// Package config 负责加载运行配置。
//
// 配置来源优先级：环境变量 > .env 文件 > 内置默认值。
// 所有项都有安全默认，保证零配置即可用本地工具链跑通闭环。
package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config 汇总全部运行参数。
type Config struct {
	// LLM
	LLMProvider string // stub | openai-compatible
	LLMBaseURL  string
	LLMModel    string
	LLMAPIKey   string

	// 多媒体能力供应商
	T2IProvider string // local | pollinations
	I2VProvider string // local | wan
	TTSProvider string // say | silent | edge

	// I2V（图生视频）真实模型配置（I2V_PROVIDER=wan 时生效）
	I2VAPIKey  string // 阿里云百炼 DashScope API Key
	I2VModel   string // 通义万相模型，如 wan2.2-i2v-plus
	I2VBaseURL string // DashScope 服务地址

	// 运行参数
	ShotConcurrency int
	VideoWidth      int
	VideoHeight     int
	VideoFPS        int
	WorkspaceDir    string
	FFmpegBin       string
	FFprobeBin      string
}

// Load 读取 .env（若存在）并叠加环境变量，返回最终配置。
// .env 查找顺序：当前目录 → 逐级向上（最多 5 层），兼容从子目录（如 cmd/drama/）运行的场景。
func Load() *Config {
	loadDotEnvUp(".env", 5)

	return &Config{
		LLMProvider: getStr("LLM_PROVIDER", "stub"),
		LLMBaseURL:  getStr("LLM_BASE_URL", "https://api.deepseek.com/v1"),
		LLMModel:    getStr("LLM_MODEL", "deepseek-chat"),
		LLMAPIKey:   getStr("LLM_API_KEY", ""),

		T2IProvider: getStr("T2I_PROVIDER", "local"),
		I2VProvider: getStr("I2V_PROVIDER", "local"),
		TTSProvider: getStr("TTS_PROVIDER", "say"),

		// I2V_API_KEY 优先，回退到 DashScope 官方惯用的 DASHSCOPE_API_KEY
		I2VAPIKey:  getStr("I2V_API_KEY", getStr("DASHSCOPE_API_KEY", "")),
		I2VModel:   getStr("I2V_MODEL", "wan2.2-i2v-plus"),
		I2VBaseURL: getStr("I2V_BASE_URL", "https://dashscope.aliyuncs.com"),

		ShotConcurrency: getInt("SHOT_CONCURRENCY", 4),
		VideoWidth:      getInt("VIDEO_WIDTH", 1280),
		VideoHeight:     getInt("VIDEO_HEIGHT", 720),
		VideoFPS:        getInt("VIDEO_FPS", 25),
		WorkspaceDir:    getStr("WORKSPACE_DIR", "workspace"),
		FFmpegBin:       getStr("FFMPEG_BIN", "ffmpeg"),
		FFprobeBin:      getStr("FFPROBE_BIN", "ffprobe"),
	}
}

// loadDotEnvUp 从 cwd 起逐级向上查找 .env，找到即加载，最多向上 maxLevels 层。
func loadDotEnvUp(name string, maxLevels int) {
	dir, err := os.Getwd()
	if err != nil {
		return
	}
	for i := 0; i <= maxLevels; i++ {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			loadDotEnv(p)
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // 已到文件系统根目录
		}
		dir = parent
	}
}

// loadDotEnv 解析 KEY=VALUE 形式的 .env，仅当环境变量未设置时写入。
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return // 没有 .env 是正常情况
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if _, exists := os.LookupEnv(k); !exists {
			os.Setenv(k, v)
		}
	}
}

func getStr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}
