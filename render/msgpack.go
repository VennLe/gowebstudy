// Copyright 2017 Manu Martinez-Almeida. All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

//go:build !nomsgpack

package render

import (
	"net/http"

	"github.com/ugorji/go/codec"
)

// 在此检查接口实现以支持 go build 标签 nomsgpack。
// See: https://github.com/gin-gonic/gin/pull/1852/
var (
	_ Render = MsgPack{}
)

// MsgPack 包含给定的接口对象。
type MsgPack struct {
	Data any
}

var msgpackContentType = []string{"application/msgpack; charset=utf-8"}

// WriteContentType (MsgPack) 写入 MsgPack 内容类型。
func (r MsgPack) WriteContentType(w http.ResponseWriter) {
	writeContentType(w, msgpackContentType)
}

// Render (MsgPack) 编码给定的接口对象，并使用自定义内容类型写入数据。
func (r MsgPack) Render(w http.ResponseWriter) error {
	return WriteMsgPack(w, r.Data)
}

// WriteMsgPack 写入 MsgPack 内容类型并编码给定的接口对象。
func WriteMsgPack(w http.ResponseWriter, obj any) error {
	writeContentType(w, msgpackContentType)
	var mh codec.MsgpackHandle
	return codec.NewEncoder(w, &mh).Encode(obj)
}
