// Copyright 2014 Manu Martinez-Almeida. All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package gin

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
)

const (
	noWritten     = -1
	defaultStatus = http.StatusOK
)

var errHijackAlreadyWritten = errors.New("gin: response body already written")

// ResponseWriter ...
type ResponseWriter interface {
	http.ResponseWriter
	http.Hijacker
	http.Flusher
	http.CloseNotifier

	// Status 返回当前请求的 HTTP 响应状态码。
	Status() int

	// Size 返回已写入响应 HTTP 正文的字节数。
	// 请参见 Written() 方法。
	Size() int

	// WriteString 将字符串写入响应正文。
	WriteString(string) (int, error)

	// Written 如果响应正文已被写入，则返回 true。
	Written() bool

	// WriteHeaderNow 强制立即写入 HTTP 头部（状态码 + 响应头）。
	WriteHeaderNow()

	// Pusher 获取用于服务器推送的 http.Pusher 接口。
	Pusher() http.Pusher
}

type responseWriter struct {
	http.ResponseWriter
	size   int
	status int
}

var _ ResponseWriter = (*responseWriter)(nil)

func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *responseWriter) reset(writer http.ResponseWriter) {
	w.ResponseWriter = writer
	w.size = noWritten
	w.status = defaultStatus
}

func (w *responseWriter) WriteHeader(code int) {
	if code > 0 && w.status != code {
		if w.Written() {
			debugPrint("[WARNING] Headers were already written. Wanted to override status code %d with %d", w.status, code)
			return
		}
		w.status = code
	}
}

func (w *responseWriter) WriteHeaderNow() {
	if !w.Written() {
		w.size = 0
		w.ResponseWriter.WriteHeader(w.status)
	}
}

func (w *responseWriter) Write(data []byte) (n int, err error) {
	w.WriteHeaderNow()
	n, err = w.ResponseWriter.Write(data)
	w.size += n
	return
}

func (w *responseWriter) WriteString(s string) (n int, err error) {
	w.WriteHeaderNow()
	n, err = io.WriteString(w.ResponseWriter, s)
	w.size += n
	return
}

func (w *responseWriter) Status() int {
	return w.status
}

func (w *responseWriter) Size() int {
	return w.size
}

func (w *responseWriter) Written() bool {
	return w.size != noWritten
}

// Hijack implements the http.Hijacker interface.
func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	// Allow hijacking before any data is written (size == -1) or after headers are written (size == 0),
	// but not after body data is written (size > 0). For compatibility with websocket libraries (e.g., github.com/coder/websocket)
	if w.size > 0 {
		return nil, nil, errHijackAlreadyWritten
	}
	if w.size < 0 {
		w.size = 0
	}
	return w.ResponseWriter.(http.Hijacker).Hijack()
}

// CloseNotify implements the http.CloseNotifier interface.
func (w *responseWriter) CloseNotify() <-chan bool {
	return w.ResponseWriter.(http.CloseNotifier).CloseNotify()
}

// Flush implements the http.Flusher interface.
func (w *responseWriter) Flush() {
	w.WriteHeaderNow()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *responseWriter) Pusher() (pusher http.Pusher) {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher
	}
	return nil
}
