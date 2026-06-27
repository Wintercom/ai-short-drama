// Package logx 提供带阶段前缀的轻量日志（零依赖）。
package logx

import (
	"fmt"
	"log"
	"os"
)

var std = log.New(os.Stdout, "", log.Ltime)

// Stage 打印流水线阶段标题。
func Stage(emoji, name string) {
	std.Printf("\n=== %s  %s ===", emoji, name)
}

// Info 普通信息。
func Info(format string, a ...any) {
	std.Printf("    "+format, a...)
}

// Step 子步骤（带缩进与符号）。
func Step(format string, a ...any) {
	std.Printf("  • "+format, a...)
}

// Warn 警告（不中断流程，通常表示已降级处理）。
func Warn(format string, a ...any) {
	std.Printf("  ! "+format, a...)
}

// Done 成功完成提示。
func Done(format string, a ...any) {
	std.Printf("  ✓ "+format, a...)
}

// Fatal 致命错误并退出。
func Fatal(err error) {
	std.Printf("  ✗ %v", err)
	os.Exit(1)
}

// Sprintf 便捷封装。
func Sprintf(format string, a ...any) string { return fmt.Sprintf(format, a...) }
