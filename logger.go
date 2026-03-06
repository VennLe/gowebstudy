// Copyright 2014 Manu Martinez-Almeida. All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package gin

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/mattn/go-isatty"
)

type consoleColorModeValue int

const (
	autoColor consoleColorModeValue = iota
	disableColor
	forceColor
)

const (
	green   = "\033[97;42m"
	white   = "\033[90;47m"
	yellow  = "\033[90;43m"
	red     = "\033[97;41m"
	blue    = "\033[97;44m"
	magenta = "\033[97;45m"
	cyan    = "\033[97;46m"
	reset   = "\033[0m"
)

var consoleColorMode = autoColor

// LoggerConfig 定义了 Logger 中间件的配置。
type LoggerConfig struct {
	// 可选。默认值为 gin.defaultLogFormatter
	Formatter LogFormatter

	// Output 是日志写入的目标输出流。
	// 可选。默认值为 gin.DefaultWriter。
	Output io.Writer

	// SkipPaths 是日志不记录的 URL 路径数组。
	// 可选。
	SkipPaths []string

	// Skip 是一个 Skipper，用于指示哪些日志不应被记录。
	// 可选。
	Skip Skipper
}

// Skipper 是一个基于提供的 Context 跳过日志记录的函数
type Skipper func(c *Context) bool

// LogFormatter 定义了传递给 LoggerWithFormatter 的格式化函数的签名
type LogFormatter func(params LogFormatterParams) string

// LogFormatterParams 是日志记录时传递给任何格式化器的结构体
type LogFormatterParams struct {
	Request *http.Request

	// TimeStamp 显示服务器返回响应后的时间。
	TimeStamp time.Time
	// StatusCode 是 HTTP 响应状态码。
	StatusCode int
	// Latency 是服务器处理特定请求所花费的时间。
	Latency time.Duration
	// ClientIP 等同于 Context 的 ClientIP 方法。
	ClientIP string
	// Method 是请求使用的 HTTP 方法。
	Method string
	// Path 是客户端请求的路径。
	Path string
	// ErrorMessage 在处理请求过程中发生错误时设置。
	ErrorMessage string
	// isTerm 指示 gin 的输出描述符是否指向终端。
	isTerm bool
	// BodySize 是响应体的大小
	BodySize int
	// Keys 是在请求的上下文中设置的键。
	Keys map[any]any
}

// StatusCodeColor 是用于在终端中适当记录 HTTP 状态码的 ANSI 颜色。
func (p *LogFormatterParams) StatusCodeColor() string {
	code := p.StatusCode

	switch {
	case code >= http.StatusContinue && code < http.StatusOK:
		return white
	case code >= http.StatusOK && code < http.StatusMultipleChoices:
		return green
	case code >= http.StatusMultipleChoices && code < http.StatusBadRequest:
		return white
	case code >= http.StatusBadRequest && code < http.StatusInternalServerError:
		return yellow
	default:
		return red
	}
}

// LatencyColor 是延迟的 ANSI 颜色
func (p *LogFormatterParams) LatencyColor() string {
	latency := p.Latency
	switch {
	case latency < time.Millisecond*100:
		return white
	case latency < time.Millisecond*200:
		return green
	case latency < time.Millisecond*300:
		return cyan
	case latency < time.Millisecond*500:
		return blue
	case latency < time.Second:
		return yellow
	case latency < time.Second*2:
		return magenta
	default:
		return red
	}
}

// MethodColor 是用于在终端中适当记录 HTTP 方法的 ANSI 颜色。
func (p *LogFormatterParams) MethodColor() string {
	method := p.Method

	switch method {
	case http.MethodGet:
		return blue
	case http.MethodPost:
		return cyan
	case http.MethodPut:
		return yellow
	case http.MethodDelete:
		return red
	case http.MethodPatch:
		return green
	case http.MethodHead:
		return magenta
	case http.MethodOptions:
		return white
	default:
		return reset
	}
}

// ResetColor 重置所有转义属性。
func (p *LogFormatterParams) ResetColor() string {
	return reset
}

// IsOutputColor 指示是否可以将颜色输出到日志。
func (p *LogFormatterParams) IsOutputColor() bool {
	return consoleColorMode == forceColor || (consoleColorMode == autoColor && p.isTerm)
}

// defaultLogFormatter 是 Logger 中间件使用的默认日志格式化函数。
var defaultLogFormatter = func(param LogFormatterParams) string {
	var statusColor, methodColor, resetColor, latencyColor string
	if param.IsOutputColor() {
		statusColor = param.StatusCodeColor()
		methodColor = param.MethodColor()
		resetColor = param.ResetColor()
		latencyColor = param.LatencyColor()
	}

	switch {
	case param.Latency > time.Minute:
		param.Latency = param.Latency.Truncate(time.Second * 10)
	case param.Latency > time.Second:
		param.Latency = param.Latency.Truncate(time.Millisecond * 10)
	case param.Latency > time.Millisecond:
		param.Latency = param.Latency.Truncate(time.Microsecond * 10)
	}

	return fmt.Sprintf("[GIN] %v |%s %3d %s|%s %8v %s| %15s |%s %-7s %s %#v\n%s",
		param.TimeStamp.Format("2006/01/02 - 15:04:05"),
		statusColor, param.StatusCode, resetColor,
		latencyColor, param.Latency, resetColor,
		param.ClientIP,
		methodColor, param.Method, resetColor,
		param.Path,
		param.ErrorMessage,
	)
}

// DisableConsoleColor 禁用控制台中的颜色输出。
func DisableConsoleColor() {
	consoleColorMode = disableColor
}

// ForceConsoleColor 强制控制台中的颜色输出。
func ForceConsoleColor() {
	consoleColorMode = forceColor
}

// ErrorLogger 返回适用于任何错误类型的 HandlerFunc。
func ErrorLogger() HandlerFunc {
	return ErrorLoggerT(ErrorTypeAny)
}

// ErrorLoggerT 返回适用于给定错误类型的 HandlerFunc。
func ErrorLoggerT(typ ErrorType) HandlerFunc {
	return func(c *Context) {
		c.Next()
		errors := c.Errors.ByType(typ)
		if len(errors) > 0 {
			c.JSON(-1, errors)
		}
	}
}

// Logger 实例化一个 Logger 中间件，该中间件会将日志写入 gin.DefaultWriter。
// 默认情况下，gin.DefaultWriter = os.Stdout。
func Logger() HandlerFunc {
	return LoggerWithConfig(LoggerConfig{})
}

// LoggerWithFormatter 通过指定的日志格式函数实例化 Logger 中间件。
func LoggerWithFormatter(f LogFormatter) HandlerFunc {
	return LoggerWithConfig(LoggerConfig{
		Formatter: f,
	})
}

// LoggerWithWriter 通过指定的写入器缓冲区实例化 Logger 中间件。
// 示例：os.Stdout、以写入模式打开的文件、socket 等。
func LoggerWithWriter(out io.Writer, notlogged ...string) HandlerFunc {
	return LoggerWithConfig(LoggerConfig{
		Output:    out,
		SkipPaths: notlogged,
	})
}

// LoggerWithConfig 通过配置实例化 Logger 中间件。
func LoggerWithConfig(conf LoggerConfig) HandlerFunc {
	formatter := conf.Formatter
	if formatter == nil {
		formatter = defaultLogFormatter
	}

	out := conf.Output
	if out == nil {
		out = DefaultWriter
	}

	notlogged := conf.SkipPaths

	isTerm := true

	if w, ok := out.(*os.File); !ok || os.Getenv("TERM") == "dumb" ||
		(!isatty.IsTerminal(w.Fd()) && !isatty.IsCygwinTerminal(w.Fd())) {
		isTerm = false
	}

	var skip map[string]struct{}

	if length := len(notlogged); length > 0 {
		skip = make(map[string]struct{}, length)

		for _, path := range notlogged {
			skip[path] = struct{}{}
		}
	}

	return func(c *Context) {
		// Start timer
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		// Process request
		c.Next()

		// 仅在不跳过时记录日志
		if _, ok := skip[path]; ok || (conf.Skip != nil && conf.Skip(c)) {
			return
		}

		param := LogFormatterParams{
			Request: c.Request,
			isTerm:  isTerm,
			Keys:    c.Keys,
		}

		// Stop timer
		param.TimeStamp = time.Now()
		param.Latency = param.TimeStamp.Sub(start)

		param.ClientIP = c.ClientIP()
		param.Method = c.Request.Method
		param.StatusCode = c.Writer.Status()
		param.ErrorMessage = c.Errors.ByType(ErrorTypePrivate).String()

		param.BodySize = c.Writer.Size()

		if raw != "" {
			path = path + "?" + raw
		}

		param.Path = path

		fmt.Fprint(out, formatter(param))
	}
}
