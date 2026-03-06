// Copyright 2014 Manu Martinez-Almeida. All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package render

import "net/http"

// Data 二进制数据渲染器
type Data struct {
	ContentType string //响应的Content-Type（如image/png、application/octet-stream）
	Data        []byte // 要返回的二进制数据
}

// Render (Data) writes data with custom ContentType.
// 把Data结构体中存储的二进制数据，按照指定的Content-Type写入 HTTP 响应，完成最终的响应输出。
// Render 是render.Data实现的Render接口方法，核心作用是：
// 1. 设置响应的Content-Type头（告诉客户端如何解析数据）
// 2. 将二进制数据写入HTTP响应体
// 参数w：Go标准库的http.ResponseWriter，用于向客户端写响应
// 返回值err：写入过程中出现的错误（如连接断开）
func (r Data) Render(w http.ResponseWriter) (err error) {
	r.WriteContentType(w)
	_, err = w.Write(r.Data)
	return
}

// WriteContentType (Data) writes custom ContentType.
func (r Data) WriteContentType(w http.ResponseWriter) {
	writeContentType(w, []string{r.ContentType})
}
