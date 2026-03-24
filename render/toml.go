// Copyright 2022 Gin Core Team. All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package render

import (
	"net/http"

	"github.com/pelletier/go-toml/v2"
)

// TOML 包含给定的接口对象。
type TOML struct {
	Data any
}

var tomlContentType = []string{"application/toml; charset=utf-8"}

// Render (TOML) 对给定的接口对象进行序列化，并使用自定义内容类型写入数据。
func (r TOML) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)

	bytes, err := toml.Marshal(r.Data)
	if err != nil {
		return err
	}

	_, err = w.Write(bytes)
	return err
}

// WriteContentType (TOML) 为响应写入 TOML 内容类型。
func (r TOML) WriteContentType(w http.ResponseWriter) {
	writeContentType(w, tomlContentType)
}
