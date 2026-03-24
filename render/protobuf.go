// Copyright 2018 Gin Core Team. All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package render

import (
	"net/http"

	"google.golang.org/protobuf/proto"
)

// ProtoBuf 包含给定的接口对象。
type ProtoBuf struct {
	Data any
}

var protobufContentType = []string{"application/x-protobuf"}

// Render (ProtoBuf) 对给定的接口对象进行序列化，并使用自定义内容类型写入数据。
func (r ProtoBuf) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)

	bytes, err := proto.Marshal(r.Data.(proto.Message))
	if err != nil {
		return err
	}

	_, err = w.Write(bytes)
	return err
}

// WriteContentType (ProtoBuf) 写入 ProtoBuf 内容类型。
func (r ProtoBuf) WriteContentType(w http.ResponseWriter) {
	writeContentType(w, protobufContentType)
}
