// Copyright 2014 Manu Martinez-Almeida. All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package render

import (
	"fmt"
	"net/http"
)

// Redirect 包含 HTTP 请求引用，并重定向状态码和位置。
type Redirect struct {
	Code     int
	Request  *http.Request
	Location string
}

// Render (Redirect) 将 HTTP 请求重定向到新位置，并写入重定向响应。
func (r Redirect) Render(w http.ResponseWriter) error {
	if (r.Code < http.StatusMultipleChoices || r.Code > http.StatusPermanentRedirect) && r.Code != http.StatusCreated {
		panic(fmt.Sprintf("Cannot redirect with status code %d", r.Code))
	}
	http.Redirect(w, r.Request, r.Location, r.Code)
	return nil
}

// WriteContentType (Redirect) 不写入任何内容类型。
func (r Redirect) WriteContentType(http.ResponseWriter) {}
