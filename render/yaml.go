// Copyright 2014 Manu Martinez-Almeida. All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package render

import (
	"net/http"

	"github.com/goccy/go-yaml"
)

// YAML 包含给定的接口对象。
type YAML struct {
	Data any
}

var yamlContentType = []string{"application/yaml; charset=utf-8"}

// Render (YAML) 对给定的接口对象进行序列化，并使用自定义内容类型写入数据。
func (r YAML) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)

	bytes, err := yaml.Marshal(r.Data) // WriteContentType (YAML) 为响应写入 YAML 内容类型。
	if err != nil {
		return err
	}

	_, err = w.Write(bytes)
	return err
}

// WriteContentType (YAML) 为响应写入 YAML 内容类型。
func (r YAML) WriteContentType(w http.ResponseWriter) {
	writeContentType(w, yamlContentType)
}
