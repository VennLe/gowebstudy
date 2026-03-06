// Copyright 2014 Manu Martinez-Almeida. All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package gin

import (
	"flag"
	"io"
	"os"
	"sync/atomic"

	"github.com/gin-gonic/gin/binding"
)

// EnvGinMode 表示 gin 模式的环境变量名。
const EnvGinMode = "GIN_MODE"

const (
	// DebugMode indicates gin mode is debug.
	DebugMode = "debug"
	// ReleaseMode indicates gin mode is release.
	ReleaseMode = "release"
	// TestMode indicates gin mode is test.
	TestMode = "test"
)

const (
	debugCode = iota
	releaseCode
	testCode
)

// DefaultWriter 是 Gin 默认使用的 io.Writer，用于输出调试信息以及中间件的输出，比如 Logger() 或 Recovery()。
// 注意，Logger 和 Recovery 都提供了自定义输出流 io.Writer 的方式。
// 要在 Windows 中支持彩色输出，请使用：
//
//	import "github.com/mattn/go-colorable"
//	gin.DefaultWriter = colorable.NewColorableStdout()
var DefaultWriter io.Writer = os.Stdout

// DefaultErrorWriter 是 Gin 默认用于输出调试错误的 io.Writer。
var DefaultErrorWriter io.Writer = os.Stderr

var (
	ginMode  int32 = debugCode
	modeName atomic.Value
)

func init() {
	mode := os.Getenv(EnvGinMode)
	SetMode(mode)
}

// SetMode 根据输入的字符串设置 Gin 的运行模式。
func SetMode(value string) {
	if value == "" {
		if flag.Lookup("test.v") != nil {
			value = TestMode
		} else {
			value = DebugMode
		}
	}

	switch value {
	case DebugMode:
		atomic.StoreInt32(&ginMode, debugCode)
	case ReleaseMode:
		atomic.StoreInt32(&ginMode, releaseCode)
	case TestMode:
		atomic.StoreInt32(&ginMode, testCode)
	default:
		panic("gin mode unknown: " + value + " (available mode: debug release test)")
	}
	modeName.Store(value)
}

// DisableBindValidation 会关闭默认的验证器
func DisableBindValidation() {
	binding.Validator = nil
}

// EnableJsonDecoderUseNumber 将 binding.EnableDecoderUseNumber 设为 true，以在 JSON 解码器实例上调用 UseNumber 方法
func EnableJsonDecoderUseNumber() {
	binding.EnableDecoderUseNumber = true
}

// EnableJsonDecoderDisallowUnknownFields 将 binding.EnableDecoderDisallowUnknownFields 设为 true，以在 JSON 解码器实例上调用 DisallowUnknownFields 方法
func EnableJsonDecoderDisallowUnknownFields() {
	binding.EnableDecoderDisallowUnknownFields = true
}

// Mode 返回当前 Gin 的运行模式。
func Mode() string {
	return modeName.Load().(string)
}
