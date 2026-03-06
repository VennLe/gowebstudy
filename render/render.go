// Copyright 2014 Manu Martinez-Almeida. All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package render

import "net/http"

// Render 是一个接口，需要由 JSON、XML、HTML、YAML 等格式来实现
// 将数据转换成 HTTP 响应的核心逻辑，所有渲染器都实现了render.Render接口
type Render interface {
	// Render 方法按照自定义的 Content-Type 写入数据
	// Render 执行渲染，写入ResponseWriter
	Render(http.ResponseWriter) error
	// WriteContentType 写入自定义的 Content-Type
	// WriteContentType 设置响应的Content-Type头
	WriteContentType(w http.ResponseWriter)
}

var (
	_ Render     = (*JSON)(nil)
	_ Render     = (*IndentedJSON)(nil)
	_ Render     = (*SecureJSON)(nil)
	_ Render     = (*JsonpJSON)(nil)
	_ Render     = (*XML)(nil)
	_ Render     = (*String)(nil)
	_ Render     = (*Redirect)(nil)
	_ Render     = (*Data)(nil)
	_ Render     = (*HTML)(nil)
	_ HTMLRender = (*HTMLDebug)(nil)
	_ HTMLRender = (*HTMLProduction)(nil)
	_ Render     = (*YAML)(nil)
	_ Render     = (*Reader)(nil)
	_ Render     = (*AsciiJSON)(nil)
	_ Render     = (*ProtoBuf)(nil)
	_ Render     = (*TOML)(nil)
)

func writeContentType(w http.ResponseWriter, value []string) {
	header := w.Header()
	if val := header["Content-Type"]; len(val) == 0 {
		header["Content-Type"] = value
	}
}
