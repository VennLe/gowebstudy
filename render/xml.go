// Copyright 2014 Manu Martinez-Almeida. All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package render

import (
	"encoding/xml"
	"net/http"
)

// XML 包含给定的接口对象。
type XML struct {
	Data any
}

var xmlContentType = []string{"application/xml; charset=utf-8"}

// Render (XML) 编码给定的接口对象，并使用自定义内容类型写入数据。
func (r XML) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)
	return xml.NewEncoder(w).Encode(r.Data)
}

// WriteContentType (XML) 为响应写入 XML 内容类型。
func (r XML) WriteContentType(w http.ResponseWriter) {
	writeContentType(w, xmlContentType)
}
