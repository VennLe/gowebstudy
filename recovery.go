// Copyright 2014 Manu Martinez-Almeida. All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package gin

import (
	"bufio"
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin/internal/bytesconv"
)

const (
	dunno     = "???"
	stackSkip = 3
)

// RecoveryFunc 定义了可传递给 CustomRecovery 的函数类型。
type RecoveryFunc func(c *Context, err any)

// Recovery 返回一个中间件，它会从任何 panic 中恢复，并在发生 panic 时返回 500 状态码。
func Recovery() HandlerFunc {
	return RecoveryWithWriter(DefaultErrorWriter)
}

// CustomRecovery 返回一个中间件，它会从任何 panic 中恢复，并调用提供的处理函数来处理该 panic。
func CustomRecovery(handle RecoveryFunc) HandlerFunc {
	return RecoveryWithWriter(DefaultErrorWriter, handle)
}

// RecoveryWithWriter 为指定的 writer 返回一个中间件，它会从任何 panic 中恢复，并在发生 panic 时写入 500 状态码。
func RecoveryWithWriter(out io.Writer, recovery ...RecoveryFunc) HandlerFunc {
	if len(recovery) > 0 {
		return CustomRecoveryWithWriter(out, recovery[0])
	}
	return CustomRecoveryWithWriter(out, defaultHandleRecovery)
}

// CustomRecoveryWithWriter 为指定的 writer 返回一个中间件，它会从任何 panic 中恢复，并调用提供的处理函数来处理该 panic。
func CustomRecoveryWithWriter(out io.Writer, handle RecoveryFunc) HandlerFunc {
	var logger *log.Logger
	if out != nil {
		logger = log.New(out, "\n\n\x1b[31m", log.LstdFlags)
	}
	return func(c *Context) {
		defer func() {
			if rec := recover(); rec != nil {
				// 检查连接是否已断开，因为这种情况通常不应导致 panic 并打印堆栈跟踪。
				var isBrokenPipe bool
				err, ok := rec.(error)
				if ok {
					isBrokenPipe = errors.Is(err, syscall.EPIPE) ||
						errors.Is(err, syscall.ECONNRESET) ||
						errors.Is(err, http.ErrAbortHandler)
				}
				if logger != nil {
					if isBrokenPipe {
						logger.Printf("%s\n%s%s", rec, secureRequestDump(c.Request), reset)
					} else if IsDebugging() {
						logger.Printf("[Recovery] %s panic recovered:\n%s\n%s\n%s%s",
							timeFormat(time.Now()), secureRequestDump(c.Request), rec, stack(stackSkip), reset)
					} else {
						logger.Printf("[Recovery] %s panic recovered:\n%s\n%s%s",
							timeFormat(time.Now()), rec, stack(stackSkip), reset)
					}
				}
				if isBrokenPipe {
					// 如果连接已断开，我们将无法向它写入状态码。
					c.Error(err) //nolint: errcheck
					c.Abort()
				} else {
					handle(c, rec)
				}
			}
		}()
		c.Next()
	}
}

// secureRequestDump 返回一个脱敏后的 HTTP 请求转储，其中 Authorization 头（如果存在）会被替换为掩码值（"Authorization: *"），以防止泄露敏感凭据。
// 目前仅对 Authorization 头进行脱敏处理。所有其他头部和请求数据均保持不变。
func secureRequestDump(r *http.Request) string {
	httpRequest, _ := httputil.DumpRequest(r, false)
	lines := strings.Split(bytesconv.BytesToString(httpRequest), "\r\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "Authorization:") {
			lines[i] = "Authorization: *"
		}
	}
	return strings.Join(lines, "\r\n")
}

func defaultHandleRecovery(c *Context, _ any) {
	c.AbortWithStatus(http.StatusInternalServerError)
}

// stack 返回格式良好的堆栈帧，并跳过指定的 skip 帧数。
func stack(skip int) []byte {
	buf := new(bytes.Buffer) // the returned data
	// As we loop, we open files and read them. These variables record the currently
	// loaded file.
	var (
		nLine    string
		lastFile string
		err      error
	)
	for i := skip; ; i++ { // Skip the expected number of frames
		pc, file, line, ok := runtime.Caller(i)
		if !ok {
			break
		}
		// Print this much at least.  If we can't find the source, it won't show.
		fmt.Fprintf(buf, "%s:%d (0x%x)\n", file, line, pc)
		if file != lastFile {
			nLine, err = readNthLine(file, line-1)
			if err != nil {
				continue
			}
			lastFile = file
		}
		fmt.Fprintf(buf, "\t%s: %s\n", function(pc), cmp.Or(nLine, dunno))
	}
	return buf.Bytes()
}

// readNthLine 从文件中读取第 n 行。
// 如果找到该行，则返回其去除首尾空白后的内容；
// 如果该行不存在，则返回空字符串。
// 如果打开文件时发生错误，则返回该错误。
func readNthLine(file string, n int) (string, error) {
	if n < 0 {
		return "", nil
	}

	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for i := 0; i < n; i++ {
		if !scanner.Scan() {
			return "", nil
		}
	}

	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text()), nil
	}

	return "", nil
}

// 此函数在可能的情况下，返回包含该程序计数器（PC）的函数名称。
func function(pc uintptr) string {
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return dunno
	}
	name := fn.Name()
	// 此名称包含了包的路径名，但由于文件名已包含，因此这是多余的。而且，路径名中包含中点符号。
	// 也就是说，我们看到的是
	//	runtime/debug.*T·ptrmethod
	// 而我们想要的是
	//	*T.ptrmethod
	// 另外，包路径中也可能包含点号（例如 code.google.com/...），
	// 因此需要先去除路径前缀
	if lastSlash := strings.LastIndexByte(name, '/'); lastSlash >= 0 {
		name = name[lastSlash+1:]
	}
	if period := strings.IndexByte(name, '.'); period >= 0 {
		name = name[period+1:]
	}
	name = strings.ReplaceAll(name, "·", ".")
	return name
}

// timeFormat 返回一个供日志记录器使用的自定义时间格式字符串。
func timeFormat(t time.Time) string {
	return t.Format("2006/01/02 - 15:04:05")
}
