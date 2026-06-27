package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OpenAILLM 是 OpenAI 兼容协议的 LLM 客户端。
// 适配 DeepSeek、Ollama、Moonshot 等一切 /chat/completions 兼容端点——
// 仅靠改 BaseURL/Model/APIKey 即可切换供应商，无需改代码。
type OpenAILLM struct {
	BaseURL string
	Model   string
	APIKey  string
	client  *http.Client
}

// NewOpenAILLM 构造客户端。
func NewOpenAILLM(baseURL, model, apiKey string) *OpenAILLM {
	return &OpenAILLM{
		BaseURL: baseURL,
		Model:   model,
		APIKey:  apiKey,
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Complete 调用 /chat/completions 返回文本。
func (l *OpenAILLM) Complete(ctx context.Context, system, user string) (string, error) {
	reqBody := chatRequest{
		Model: l.Model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	}
	buf, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		l.BaseURL+"/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+l.APIKey)

	resp, err := l.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("LLM 请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("LLM 返回 %d: %s", resp.StatusCode, string(body))
	}

	var cr chatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return "", fmt.Errorf("解析 LLM 响应失败: %w", err)
	}
	if cr.Error != nil {
		return "", fmt.Errorf("LLM 错误: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("LLM 未返回内容")
	}
	return cr.Choices[0].Message.Content, nil
}
