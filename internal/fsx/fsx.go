// Package fsx 封装文件系统、哈希与外部命令调用等通用工具。
package fsx

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// EnsureDir 确保目录存在。
func EnsureDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

// Exists 判断文件是否存在且非空（用于产物缓存命中判断）。
func Exists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Size() > 0
}

// Hash 计算任意字符串切片的稳定指纹（用于"同输入不重算"的缓存键）。
func Hash(parts ...string) string {
	h := sha1.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// WriteFile 写入文本文件，自动建目录。
func WriteFile(path, content string) error {
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// Run 执行外部命令（如 ffmpeg / say），失败时返回包含 stderr 的错误。
func Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("命令失败 %s: %w\n%s", name, err, truncate(string(out), 500))
	}
	return nil
}

// HasBinary 检查某可执行文件是否在 PATH 中。
func HasBinary(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
