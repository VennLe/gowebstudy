// Copyright 2014 Manu Martinez-Almeida. All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

//go:build !nomsgpack

package binding

import "net/http"

// Content-Type MIME of the most common data formats.
const (
	MIMEJSON              = "application/json"
	MIMEHTML              = "text/html"
	MIMEXML               = "application/xml"
	MIMEXML2              = "text/xml"
	MIMEPlain             = "text/plain"
	MIMEPOSTForm          = "application/x-www-form-urlencoded"
	MIMEMultipartPOSTForm = "multipart/form-data"
	MIMEPROTOBUF          = "application/x-protobuf"
	MIMEMSGPACK           = "application/x-msgpack"
	MIMEMSGPACK2          = "application/msgpack"
	MIMEYAML              = "application/x-yaml"
	MIMEYAML2             = "application/yaml"
	MIMETOML              = "application/toml"
)

// Binding 描述了为绑定请求中数据（如 JSON 请求体、查询参数或表单 POST 数据）所需实现的接口。
type Binding interface {
	Name() string
	Bind(*http.Request, any) error
}

// BindingBody 为 Binding 添加了 BindBody 方法。BindBody 与 Bind 类似，但它是从提供的字节中读取请求体，而非 req.Body。
type BindingBody interface {
	Binding
	BindBody([]byte, any) error
}

// BindingUri 为 Binding 添加 BindUri 方法。BindUri 类似于 Bind，但它是读取路由参数。
type BindingUri interface {
	Name() string
	BindUri(map[string][]string, any) error
}

// StructValidator 是需要实现的最小接口，以便将其用作验证器引擎，确保请求的正确性。Gin 为此提供了一个默认实现，使用
// https://github.com/go-playground/validator/tree/v10.6.1.
type StructValidator interface {
	// ValidateStruct 可以接收任何类型的值，即使配置不正确，它也绝不应发生恐慌。
	// 如果接收到的类型是切片或数组，则应对每个元素执行遍历验证。
	// 如果接收到的类型不是结构体或切片/数组，则应跳过所有验证并必须返回 nil。
	// 如果接收到的类型是结构体或指向结构体的指针，则应执行验证。
	// 如果结构体无效或验证本身失败，则应返回描述性错误。
	// 否则必须返回 nil。
	ValidateStruct(any) error

	// Engine 返回支撑该 StructValidator 实现的基础验证器引擎。
	Engine() any
}

// Validator 是实现 StructValidator 接口的默认验证器。它底层使用 https://github.com/go-playground/validator/tree/v10.6.1。
var Validator StructValidator = &defaultValidator{}

// 这些实现了 Binding 接口，可用于将请求中存在的数据绑定到结构体实例。
var (
	JSON          BindingBody = jsonBinding{}
	XML           BindingBody = xmlBinding{}
	Form          Binding     = formBinding{}
	Query         Binding     = queryBinding{}
	FormPost      Binding     = formPostBinding{}
	FormMultipart Binding     = formMultipartBinding{}
	ProtoBuf      BindingBody = protobufBinding{}
	MsgPack       BindingBody = msgpackBinding{}
	YAML          BindingBody = yamlBinding{}
	Uri           BindingUri  = uriBinding{}
	Header        Binding     = headerBinding{}
	Plain         BindingBody = plainBinding{}
	TOML          BindingBody = tomlBinding{}
)

// Default 根据 HTTP 方法和内容类型返回适当的 Binding 实例。
func Default(method, contentType string) Binding {
	if method == http.MethodGet {
		return Form
	}

	switch contentType {
	case MIMEJSON:
		return JSON
	case MIMEXML, MIMEXML2:
		return XML
	case MIMEPROTOBUF:
		return ProtoBuf
	case MIMEMSGPACK, MIMEMSGPACK2:
		return MsgPack
	case MIMEYAML, MIMEYAML2:
		return YAML
	case MIMETOML:
		return TOML
	case MIMEMultipartPOSTForm:
		return FormMultipart
	default: // case MIMEPOSTForm:
		return Form
	}
}

func validate(obj any) error {
	if Validator == nil {
		return nil
	}
	return Validator.ValidateStruct(obj)
}
