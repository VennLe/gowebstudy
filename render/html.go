// Copyright 2014 Manu Martinez-Almeida. All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package render

import (
	"html/template"
	"net/http"

	"github.com/gin-gonic/gin/internal/fs"
)

// Delims 表示用于 HTML 模板渲染的一组左分隔符和右分隔符。
type Delims struct {
	// 左分隔符默认是 {{.
	Left string
	// 右分隔符默认是 }}.
	Right string
}

// HTMLRender 接口由 HTMLProduction 和 HTMLDebug 实现。
type HTMLRender interface {
	// Instance 返回一个 HTML 实例。
	Instance(string, any) Render
}

// HTMLProduction 包含模板引用及其分隔符。
type HTMLProduction struct {
	Template *template.Template
	Delims   Delims
}

// HTMLDebug 包含模板分隔符、模式以及处理文件列表的函数。
type HTMLDebug struct {
	Files      []string
	Glob       string
	FileSystem http.FileSystem
	Patterns   []string
	Delims     Delims
	FuncMap    template.FuncMap
}

// HTML 包含模板引用、其名称以及给定的接口对象。
type HTML struct {
	Template *template.Template
	Name     string
	Data     any
}

var htmlContentType = []string{"text/html; charset=utf-8"}

// Instance (HTMLProduction) returns an HTML instance which it realizes Render interface.
func (r HTMLProduction) Instance(name string, data any) Render {
	return HTML{
		Template: r.Template,
		Name:     name,
		Data:     data,
	}
}

// Instance (HTMLDebug) returns an HTML instance which it realizes Render interface.
func (r HTMLDebug) Instance(name string, data any) Render {
	return HTML{
		Template: r.loadTemplate(),
		Name:     name,
		Data:     data,
	}
}

func (r HTMLDebug) loadTemplate() *template.Template {
	if r.FuncMap == nil {
		r.FuncMap = template.FuncMap{}
	}
	if len(r.Files) > 0 {
		return template.Must(template.New("").Delims(r.Delims.Left, r.Delims.Right).Funcs(r.FuncMap).ParseFiles(r.Files...))
	}
	if r.Glob != "" {
		return template.Must(template.New("").Delims(r.Delims.Left, r.Delims.Right).Funcs(r.FuncMap).ParseGlob(r.Glob))
	}
	if r.FileSystem != nil && len(r.Patterns) > 0 {
		return template.Must(template.New("").Delims(r.Delims.Left, r.Delims.Right).Funcs(r.FuncMap).ParseFS(
			fs.FileSystem{FileSystem: r.FileSystem}, r.Patterns...))
	}
	panic("the HTML debug render was created without files or glob pattern or file system with patterns")
}

// Render (HTML) executes template and writes its result with custom ContentType for response.
func (r HTML) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)

	if r.Name == "" {
		return r.Template.Execute(w, r.Data)
	}
	return r.Template.ExecuteTemplate(w, r.Name, r.Data)
}

// WriteContentType (HTML) writes HTML ContentType.
func (r HTML) WriteContentType(w http.ResponseWriter) {
	writeContentType(w, htmlContentType)
}
