// Copyright 2014 Manu Martinez-Almeida. All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package gin

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"maps"
	"math"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-contrib/sse"
	"github.com/gin-gonic/gin/binding"
	"github.com/gin-gonic/gin/render"
)

// 最常见数据格式的 Content-Type MIME 类型。
const (
	MIMEJSON              = binding.MIMEJSON
	MIMEHTML              = binding.MIMEHTML
	MIMEXML               = binding.MIMEXML
	MIMEXML2              = binding.MIMEXML2
	MIMEPlain             = binding.MIMEPlain
	MIMEPOSTForm          = binding.MIMEPOSTForm
	MIMEMultipartPOSTForm = binding.MIMEMultipartPOSTForm
	MIMEYAML              = binding.MIMEYAML
	MIMEYAML2             = binding.MIMEYAML2
	MIMETOML              = binding.MIMETOML
	MIMEPROTOBUF          = binding.MIMEPROTOBUF
)

// BodyBytesKey 表示默认的请求体字节数据键
const BodyBytesKey = "_gin-gonic/gin/bodybyteskey"

// ContextKey 是 Context 用于返回自身所对应的键。
const ContextKey = "_gin-gonic/gin/contextkey"

type ContextKeyType int

const ContextRequestKey ContextKeyType = 0

// abortIndex 表示中止函数中使用的典型值。
const abortIndex int8 = math.MaxInt8 >> 1

// Context 是 gin 框架中最重要的部分。它允许我们在中间件间传递变量、
// 管理流程、验证请求的 JSON 数据，以及渲染 JSON 响应等。
type Context struct {
	writermem responseWriter
	// 1. 请求与响应
	Request *http.Request
	Writer  ResponseWriter

	// 2. 路由信息
	Params   Params        // 路由参数（如 `/user/:id` 中的 `id`）
	handlers HandlersChain // 当前请求需要执行的处理器（中间件+最终处理器）链
	index    int8          // 指向当前执行到 handlers 链中的第几个处理器
	fullPath string

	engine       *Engine
	params       *Params
	skippedNodes *[]skippedNode

	// 此互斥锁用于保护 Keys 映射
	mu sync.RWMutex

	// Keys 是专用于每个请求上下文的键/值对，用于中间件/处理器间共享数据
	Keys map[any]any

	// Errors 是附加到所有使用此上下文的处理器/中间件的错误列表
	Errors errorMsgs

	// Accepted 定义用于内容协商的手动接受格式列表
	Accepted []string

	// queryCache 缓存来自 c.Request.URL.Query() 的查询结果
	queryCache url.Values

	// formCache 缓存 c.Request.PostForm，其中包含从 POST、PATCH 或 PUT 请求体参数解析的表单数据。
	formCache url.Values

	// SameSite 允许服务器定义 Cookie 属性，使浏览器无法
	// 在跨站请求中发送此 Cookie。
	sameSite http.SameSite
}

/************************************/
/********** CONTEXT CREATION ********/
/************************************/

func (c *Context) reset() {
	c.Writer = &c.writermem
	c.Params = c.Params[:0]
	c.handlers = nil
	c.index = -1

	c.fullPath = ""
	c.Keys = nil
	c.Errors = c.Errors[:0]
	c.Accepted = nil
	c.queryCache = nil
	c.formCache = nil
	c.sameSite = 0
	*c.params = (*c.params)[:0]
	*c.skippedNodes = (*c.skippedNodes)[:0]
}

// Copy 返回当前上下文的一个副本，可在请求范围之外安全使用。
// 当需要将上下文传递到 goroutine 时，必须使用此方法。
func (c *Context) Copy() *Context {
	cp := Context{
		writermem: c.writermem,
		Request:   c.Request,
		engine:    c.engine,
	}

	cp.writermem.ResponseWriter = nil
	cp.Writer = &cp.writermem
	cp.index = abortIndex
	cp.handlers = nil
	cp.fullPath = c.fullPath

	cKeys := c.Keys
	c.mu.RLock()
	cp.Keys = maps.Clone(cKeys)
	c.mu.RUnlock()

	cParams := c.Params
	cp.Params = make([]Param, len(cParams))
	copy(cp.Params, cParams)

	return &cp
}

// HandlerName 返回主处理函数的名称。例如，如果处理函数是 "handleGetUsers()"，
// 此函数将返回 "main.handleGetUsers"。
func (c *Context) HandlerName() string {
	return nameOfFunction(c.handlers.Last())
}

// HandlerNames 按照 HandlerName() 的语义，返回此上下文中所有已注册处理函数的列表（按降序排列）
func (c *Context) HandlerNames() []string {
	hn := make([]string, 0, len(c.handlers))
	for _, val := range c.handlers {
		if val == nil {
			continue
		}
		hn = append(hn, nameOfFunction(val))
	}
	return hn
}

// Handler 返回主处理函数
func (c *Context) Handler() HandlerFunc {
	return c.handlers.Last()
}

// FullPath 返回匹配路由的完整路径。对于未找到的路由，
// 返回空字符串。
//
//	router.GET("/user/:id", func(c *gin.Context) {
//	    c.FullPath() == "/user/:id" // true
//	})
func (c *Context) FullPath() string {
	return c.fullPath
}

/************************************/
/*********** FLOW CONTROL ***********/
/************************************/

// Next 应仅在中间件内部使用。
// 它会在调用处理函数中执行链中尚未执行的处理函数。
// 请参考 GitHub 中的示例。
func (c *Context) Next() {
	c.index++
	for c.index < safeInt8(len(c.handlers)) {
		if c.handlers[c.index] != nil {
			c.handlers[c.index](c)
		}
		c.index++
	}
}

// IsAborted 如果当前上下文已被中止，则返回 true。
func (c *Context) IsAborted() bool {
	return c.index >= abortIndex
}

// Abort 用于阻止后续处理函数的调用。注意，这不会停止当前处理函数的执行。
// 假设您有一个验证当前请求是否已授权的身份验证中间件。
// 如果验证失败（例如：密码不匹配），调用 Abort 可确保
// 不会执行此请求的后续处理函数。
func (c *Context) Abort() {
	c.index = abortIndex
}

// AbortWithStatus 调用 Abort() 并使用指定的状态码写入响应头。
// 例如，验证请求失败时可以使用：context.AbortWithStatus(401)。
func (c *Context) AbortWithStatus(code int) {
	c.Status(code)
	c.Writer.WriteHeaderNow()
	c.Abort()
}

// AbortWithStatusPureJSON 内部调用 Abort() 后再执行 PureJSON。
// 此方法会终止处理链，写入状态码并返回不进行转义的 JSON 响应体，
// 同时将 Content-Type 设置为 "application/json"。
func (c *Context) AbortWithStatusPureJSON(code int, jsonObj any) {
	c.Abort()
	c.PureJSON(code, jsonObj)
}

// AbortWithStatusJSON 内部调用 `Abort()` 后再执行 `JSON`。
// 此方法会终止处理链，写入状态码并返回 JSON 响应体，
// 同时将 Content-Type 设置为 "application/json"。
func (c *Context) AbortWithStatusJSON(code int, jsonObj any) {
	c.Abort()
	c.JSON(code, jsonObj)
}

// AbortWithError 内部调用 AbortWithStatus() 和 Error()。
// 此方法会终止处理链，写入状态码，并将指定的错误推送到 c.Errors 中。
// 更多细节请参见 Context.Error()。
func (c *Context) AbortWithError(code int, err error) *Error {
	c.AbortWithStatus(code)
	return c.Error(err)
}

/************************************/
/********* ERROR MANAGEMENT *********/
/************************************/

// Error 将错误关联到当前上下文。该错误会被推送到错误列表中。
// 建议在处理请求过程中每次发生错误时都调用 Error 方法。
// 可以通过中间件收集所有错误，并统一存储到数据库、
// 打印日志或添加到 HTTP 响应中。
// 如果 err 为 nil，Error 将触发 panic。
func (c *Context) Error(err error) *Error {
	if err == nil {
		panic("err is nil")
	}

	var parsedError *Error
	ok := errors.As(err, &parsedError)
	if !ok {
		parsedError = &Error{
			Err:  err,
			Type: ErrorTypePrivate,
		}
	}

	c.Errors = append(c.Errors, parsedError)
	return parsedError
}

/************************************/
/******** METADATA MANAGEMENT********/
/************************************/

// Set 用于存储专属于此上下文的键/值对。
// 如果 c.Keys 尚未初始化，该方法会进行惰性初始化。
func (c *Context) Set(key any, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Keys == nil {
		c.Keys = make(map[any]any)
	}

	c.Keys[key] = value
}

// Get 返回指定键对应的值，即：(value, true)
// 如果该值不存在，则返回 (nil, false)
func (c *Context) Get(key any) (value any, exists bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	value, exists = c.Keys[key]
	return
}

// MustGet 如果给定键存在，则返回其对应的值，否则会触发 panic
func (c *Context) MustGet(key any) any {
	if value, exists := c.Get(key); exists {
		return value
	}
	panic(fmt.Sprintf("key %v does not exist", key))
}

// 泛型函数：从Gin Context中获取指定key的指定类型值
func getTyped[T any](c *Context, key any) (res T) {
	// 1. 从Context中获取key对应的值，ok表示是否存在
	if val, ok := c.Get(key); ok && val != nil {
		// 2. 类型断言：尝试将val转换为泛型类型T，忽略转换失败的错误
		res, _ = val.(T)
	}
	// 3. 返回结果（存在且转换成功则返回对应值，否则返回T的零值）
	return
}

// GetString 返回与键关联的值（以字符串形式）
func (c *Context) GetString(key any) string {
	return getTyped[string](c, key)
}

// GetBool 返回与键关联的值（以布尔值形式）
func (c *Context) GetBool(key any) bool {
	return getTyped[bool](c, key)
}

// GetInt 返回与键关联的值（以整数形式）
func (c *Context) GetInt(key any) int {
	return getTyped[int](c, key)
}

// GetInt8 返回与键关联的值（以 8 位整数形式）。
func (c *Context) GetInt8(key any) int8 {
	return getTyped[int8](c, key)
}

// GetInt16 返回与键关联的值（以 16 位整数形式）。
func (c *Context) GetInt16(key any) int16 {
	return getTyped[int16](c, key)
}

// GetInt32 返回与键关联的值，并将其作为 32 位整数。
func (c *Context) GetInt32(key any) int32 {
	return getTyped[int32](c, key)
}

// GetInt64 返回与键关联的值，并将其作为 64 位整数。
func (c *Context) GetInt64(key any) int64 {
	return getTyped[int64](c, key)
}

// GetUint 返回与键关联的值，并将其作为无符号整数。
func (c *Context) GetUint(key any) uint {
	return getTyped[uint](c, key)
}

// GetUint8 返回与键关联的值，并将其作为 8 位无符号整数。
func (c *Context) GetUint8(key any) uint8 {
	return getTyped[uint8](c, key)
}

// GetUint16 returns the value associated with the key as an unsigned integer 16.
func (c *Context) GetUint16(key any) uint16 {
	return getTyped[uint16](c, key)
}

// GetUint32 returns the value associated with the key as an unsigned integer 32.
func (c *Context) GetUint32(key any) uint32 {
	return getTyped[uint32](c, key)
}

// GetUint64 returns the value associated with the key as an unsigned integer 64.
func (c *Context) GetUint64(key any) uint64 {
	return getTyped[uint64](c, key)
}

// GetFloat32 returns the value associated with the key as a float32.
func (c *Context) GetFloat32(key any) float32 {
	return getTyped[float32](c, key)
}

// GetFloat64 returns the value associated with the key as a float64.
func (c *Context) GetFloat64(key any) float64 {
	return getTyped[float64](c, key)
}

// GetTime returns the value associated with the key as time.
func (c *Context) GetTime(key any) time.Time {
	return getTyped[time.Time](c, key)
}

// GetDuration returns the value associated with the key as a duration.
func (c *Context) GetDuration(key any) time.Duration {
	return getTyped[time.Duration](c, key)
}

// GetIntSlice returns the value associated with the key as a slice of integers.
func (c *Context) GetIntSlice(key any) []int {
	return getTyped[[]int](c, key)
}

// GetInt8Slice returns the value associated with the key as a slice of int8 integers.
func (c *Context) GetInt8Slice(key any) []int8 {
	return getTyped[[]int8](c, key)
}

// GetInt16Slice returns the value associated with the key as a slice of int16 integers.
func (c *Context) GetInt16Slice(key any) []int16 {
	return getTyped[[]int16](c, key)
}

// GetInt32Slice returns the value associated with the key as a slice of int32 integers.
func (c *Context) GetInt32Slice(key any) []int32 {
	return getTyped[[]int32](c, key)
}

// GetInt64Slice returns the value associated with the key as a slice of int64 integers.
func (c *Context) GetInt64Slice(key any) []int64 {
	return getTyped[[]int64](c, key)
}

// GetUintSlice returns the value associated with the key as a slice of unsigned integers.
func (c *Context) GetUintSlice(key any) []uint {
	return getTyped[[]uint](c, key)
}

// GetUint8Slice returns the value associated with the key as a slice of uint8 integers.
func (c *Context) GetUint8Slice(key any) []uint8 {
	return getTyped[[]uint8](c, key)
}

// GetUint16Slice returns the value associated with the key as a slice of uint16 integers.
func (c *Context) GetUint16Slice(key any) []uint16 {
	return getTyped[[]uint16](c, key)
}

// GetUint32Slice returns the value associated with the key as a slice of uint32 integers.
func (c *Context) GetUint32Slice(key any) []uint32 {
	return getTyped[[]uint32](c, key)
}

// GetUint64Slice returns the value associated with the key as a slice of uint64 integers.
func (c *Context) GetUint64Slice(key any) []uint64 {
	return getTyped[[]uint64](c, key)
}

// GetFloat32Slice returns the value associated with the key as a slice of float32 numbers.
func (c *Context) GetFloat32Slice(key any) []float32 {
	return getTyped[[]float32](c, key)
}

// GetFloat64Slice returns the value associated with the key as a slice of float64 numbers.
func (c *Context) GetFloat64Slice(key any) []float64 {
	return getTyped[[]float64](c, key)
}

// GetStringSlice returns the value associated with the key as a slice of strings.
func (c *Context) GetStringSlice(key any) []string {
	return getTyped[[]string](c, key)
}

// GetStringMap returns the value associated with the key as a map of interfaces.
func (c *Context) GetStringMap(key any) map[string]any {
	return getTyped[map[string]any](c, key)
}

// GetStringMapString returns the value associated with the key as a map of strings.
func (c *Context) GetStringMapString(key any) map[string]string {
	return getTyped[map[string]string](c, key)
}

// GetStringMapStringSlice returns the value associated with the key as a map to a slice of strings.
func (c *Context) GetStringMapStringSlice(key any) map[string][]string {
	return getTyped[map[string][]string](c, key)
}

// Delete 从 Context 的 Key 映射中删除指定的键（如果存在）。
// 此操作可安全用于并发运行的 go-routine。
func (c *Context) Delete(key any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Keys != nil {
		delete(c.Keys, key)
	}
}

/************************************/
/************ INPUT DATA ************/
/************************************/

// Param 返回 URL 参数的值。
// 这是 c.Params.ByName(key) 的快捷方式。
//
//	router.GET("/user/:id", func(c *gin.Context) {
//	    // a GET request to /user/john
//	    id := c.Param("id") // id == "john"
//	    // a GET request to /user/john/
//	    id := c.Param("id") // id == "/john/"
//	})
func (c *Context) Param(key string) string {
	return c.Params.ByName(key)
}

// AddParam 将参数添加到上下文中，
// 并为端到端测试目的用指定值替换路径参数键
// Example Route: "/user/:id"
// AddParam("id", 1)
// Result: "/user/1"
func (c *Context) AddParam(key, value string) {
	c.Params = append(c.Params, Param{Key: key, Value: value})
}

// Query 返回指定键的 URL 查询值（如果存在）
// 否则返回空字符串 `("")`
// 这是 `c.Request.URL.Query().Get(key)` 的快捷方式。
//
//	    GET /path?id=1234&name=Manu&value=
//		   c.Query("id") == "1234"
//		   c.Query("name") == "Manu"
//		   c.Query("value") == ""
//		   c.Query("wtf") == ""
func (c *Context) Query(key string) (value string) {
	value, _ = c.GetQuery(key)
	return
}

// DefaultQuery 返回指定键的 URL 查询值（如果存在），
// 否则返回指定的默认值字符串。
// 更多信息请参考：Query() 和 GetQuery()。
//
//	GET /?name=Manu&lastname=
//	c.DefaultQuery("name", "unknown") == "Manu"
//	c.DefaultQuery("id", "none") == "none"
//	c.DefaultQuery("lastname", "none") == ""
func (c *Context) DefaultQuery(key, defaultValue string) string {
	if value, ok := c.GetQuery(key); ok {
		return value
	}
	return defaultValue
}

// GetQuery 与 Query() 类似，如果指定键的 URL 查询值存在
// 则返回 (value, true)（即使值为空字符串），
// 否则返回 ("", false)。
// 这是 c.Request.URL.Query().Get(key) 的快捷方式。
//
//	GET /?name=Manu&lastname=
//	("Manu", true) == c.GetQuery("name")
//	("", false) == c.GetQuery("id")
//	("", true) == c.GetQuery("lastname")
func (c *Context) GetQuery(key string) (string, bool) {
	if values, ok := c.GetQueryArray(key); ok {
		return values[0], ok
	}
	return "", false
}

// QueryArray 返回给定查询键对应的字符串切片。
// 切片的长度取决于具有该键的参数数量。
func (c *Context) QueryArray(key string) (values []string) {
	values, _ = c.GetQueryArray(key)
	return
}

func (c *Context) initQueryCache() {
	if c.queryCache == nil {
		if c.Request != nil && c.Request.URL != nil {
			c.queryCache = c.Request.URL.Query()
		} else {
			c.queryCache = url.Values{}
		}
	}
}

// GetQueryArray 返回给定查询键对应的字符串切片，
// 以及一个表示该键是否至少存在一个值的布尔值。
func (c *Context) GetQueryArray(key string) (values []string, ok bool) {
	c.initQueryCache()
	values, ok = c.queryCache[key]
	return
}

// QueryMap 返回给定查询键对应的映射。
func (c *Context) QueryMap(key string) (dicts map[string]string) {
	dicts, _ = c.GetQueryMap(key)
	return
}

// GetQueryMap 返回给定查询键对应的映射，
// 以及一个表示该键是否至少存在一个值的布尔值。
func (c *Context) GetQueryMap(key string) (map[string]string, bool) {
	c.initQueryCache()
	return getMapFromFormData(c.queryCache, key)
}

// PostForm 从 POST 的 urlencoded 表单或多部分表单中
// 返回指定键的值（如果存在），否则返回空字符串 ("")。
func (c *Context) PostForm(key string) (value string) {
	value, _ = c.GetPostForm(key)
	return
}

// DefaultPostForm 从 POST 的 urlencoded 表单或多部分表单中
// 返回指定键的值（如果存在），否则返回指定的默认值字符串。
// 更多信息请参考：PostForm() 和 GetPostForm()。
func (c *Context) DefaultPostForm(key, defaultValue string) string {
	if value, ok := c.GetPostForm(key); ok {
		return value
	}
	return defaultValue
}

// GetPostForm 与 PostForm(key) 类似。它从 POST 的 urlencoded
// 表单或多部分表单中返回指定键的值（如果存在）(value, true)（即使值为空字符串），
// 否则返回 ("", false)。
// 例如，在 PATCH 请求中更新用户邮箱时：
//
//	    email=mail@example.com  -->  ("mail@example.com", true) := GetPostForm("email") // set email to "mail@example.com"
//		   email=                  -->  ("", true) := GetPostForm("email") // set email to ""
//	                            -->  ("", false) := GetPostForm("email") // do nothing with email
func (c *Context) GetPostForm(key string) (string, bool) {
	if values, ok := c.GetPostFormArray(key); ok {
		return values[0], ok
	}
	return "", false
}

// PostFormArray 返回给定表单键对应的字符串切片。
// 切片的长度取决于具有该键的参数数量。
func (c *Context) PostFormArray(key string) (values []string) {
	values, _ = c.GetPostFormArray(key)
	return
}

func (c *Context) initFormCache() {
	if c.formCache == nil {
		c.formCache = make(url.Values)
		req := c.Request
		if err := req.ParseMultipartForm(c.engine.MaxMultipartMemory); err != nil {
			if !errors.Is(err, http.ErrNotMultipart) {
				debugPrint("error on parse multipart form array: %v", err)
			}
		}
		c.formCache = req.PostForm
	}
}

// GetPostFormArray 返回给定表单键对应的字符串切片，
// 以及一个表示该键是否至少存在一个值的布尔值。
func (c *Context) GetPostFormArray(key string) (values []string, ok bool) {
	c.initFormCache()
	values, ok = c.formCache[key]
	return
}

// PostFormMap 返回给定表单键对应的映射。
func (c *Context) PostFormMap(key string) (dicts map[string]string) {
	dicts, _ = c.GetPostFormMap(key)
	return
}

// GetPostFormMap 返回给定表单键对应的映射，
// 以及一个表示该键是否至少存在一个值的布尔值。
func (c *Context) GetPostFormMap(key string) (map[string]string, bool) {
	c.initFormCache()
	return getMapFromFormData(c.formCache, key)
}

// getMapFromFormData 返回满足条件的映射。
// 它会将方括号表示法的表单数据（如 "key[subkey]=value"）解析为映射。
func getMapFromFormData(m map[string][]string, key string) (map[string]string, bool) {
	d := make(map[string]string)
	found := false
	keyLen := len(key)

	for k, v := range m {
		if len(k) < keyLen+3 { // key + "[" + at least one char + "]"
			continue
		}

		if k[:keyLen] != key || k[keyLen] != '[' {
			continue
		}

		if j := strings.IndexByte(k[keyLen+1:], ']'); j > 0 {
			found = true
			d[k[keyLen+1:keyLen+1+j]] = v[0]
		}
	}

	return d, found
}

// FormFile 返回指定表单键对应的第一个文件。
func (c *Context) FormFile(name string) (*multipart.FileHeader, error) {
	if c.Request.MultipartForm == nil {
		if err := c.Request.ParseMultipartForm(c.engine.MaxMultipartMemory); err != nil {
			return nil, err
		}
	}
	f, fh, err := c.Request.FormFile(name)
	if err != nil {
		return nil, err
	}
	f.Close()
	return fh, err
}

// MultipartForm 是已解析的多部分表单数据，包括文件上传。
func (c *Context) MultipartForm() (*multipart.Form, error) {
	err := c.Request.ParseMultipartForm(c.engine.MaxMultipartMemory)
	return c.Request.MultipartForm, err
}

// SaveUploadedFile 将上传的表单文件保存到指定的目标路径。
func (c *Context) SaveUploadedFile(file *multipart.FileHeader, dst string, perm ...fs.FileMode) error {
	src, err := file.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	var mode os.FileMode = 0o750
	if len(perm) > 0 {
		mode = perm[0]
	}
	dir := filepath.Dir(dst)
	if err = os.MkdirAll(dir, mode); err != nil {
		return err
	}
	if err = os.Chmod(dir, mode); err != nil {
		return err
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, src)
	return err
}

// Bind 根据请求方法和 Content-Type 自动选择合适的绑定引擎，
// 根据 "Content-Type" 头部会使用不同的绑定方式，例如：
//
//	"application/json" --> JSON binding
//	"application/xml"  --> XML binding
//
// 如果 Content-Type == "application/json"，它会将请求体解析为 JSON（使用 JSON 或 XML 作为 JSON 输入）。
// 将 JSON 负载解码到指定的指针结构体中。
// 如果输入无效，会返回 400 错误并在响应中设置 Content-Type 头部为 "text/plain"。
func (c *Context) Bind(obj any) error {
	b := binding.Default(c.Request.Method, c.ContentType())
	return c.MustBindWith(obj, b)
}

// BindJSON 是 c.MustBindWith(obj, binding.JSON) 的快捷方式。
func (c *Context) BindJSON(obj any) error {
	return c.MustBindWith(obj, binding.JSON)
}

// BindXML 是 c.MustBindWith(obj, binding.BindXML) 的快捷方式。
func (c *Context) BindXML(obj any) error {
	return c.MustBindWith(obj, binding.XML)
}

// BindQuery 是 c.MustBindWith(obj, binding.Query) 的快捷方式。
func (c *Context) BindQuery(obj any) error {
	return c.MustBindWith(obj, binding.Query)
}

// BindYAML 是 c.MustBindWith(obj, binding.YAML) 的快捷方式。
func (c *Context) BindYAML(obj any) error {
	return c.MustBindWith(obj, binding.YAML)
}

// BindTOML 是 c.MustBindWith(obj, binding.TOML) 的快捷方式。
func (c *Context) BindTOML(obj any) error {
	return c.MustBindWith(obj, binding.TOML)
}

// BindPlain 是 c.MustBindWith(obj, binding.Plain) 的快捷方式。
func (c *Context) BindPlain(obj any) error {
	return c.MustBindWith(obj, binding.Plain)
}

// BindHeader 是 c.MustBindWith(obj, binding.Header) 的快捷方式。
func (c *Context) BindHeader(obj any) error {
	return c.MustBindWith(obj, binding.Header)
}

// BindUri 使用 binding.Uri 绑定传入的结构体指针。
// 如果发生任何错误，将中止请求并返回 HTTP 400 状态码。
func (c *Context) BindUri(obj any) error {
	if err := c.ShouldBindUri(obj); err != nil {
		c.AbortWithError(http.StatusBadRequest, err).SetType(ErrorTypeBind) //nolint: errcheck
		return err
	}
	return nil
}

// MustBindWith 使用指定的绑定引擎绑定传入的结构体指针。
// 如果发生任何错误，将中止请求并返回 HTTP 400 状态码。
// 请参见 binding 包。
func (c *Context) MustBindWith(obj any, b binding.Binding) error {
	err := c.ShouldBindWith(obj, b)
	if err != nil {
		var maxBytesErr *http.MaxBytesError

		// Note: When using sonic or go-json as JSON encoder, they do not propagate the http.MaxBytesError error
		// https://github.com/goccy/go-json/issues/485
		// https://github.com/bytedance/sonic/issues/800
		switch {
		case errors.As(err, &maxBytesErr):
			c.AbortWithError(http.StatusRequestEntityTooLarge, err).SetType(ErrorTypeBind) //nolint: errcheck
		default:
			c.AbortWithError(http.StatusBadRequest, err).SetType(ErrorTypeBind) //nolint: errcheck
		}
		return err
	}
	return nil
}

// ShouldBind 根据请求方法和 Content-Type 自动选择合适的绑定引擎，
// 根据 "Content-Type" 头部会使用不同的绑定方式，例如：
//
//	"application/json" --> JSON binding
//	"application/xml"  --> XML binding
//
// 如果 Content-Type == "application/json"，它会将请求体解析为 JSON（使用 JSON 或 XML 作为 JSON 输入）。
// 将 JSON 负载解码到指定的指针结构体中。
// 类似于 c.Bind()，但此方法在输入无效时不会将响应状态码设为 400 或中止请求。
func (c *Context) ShouldBind(obj any) error {
	b := binding.Default(c.Request.Method, c.ContentType())
	return c.ShouldBindWith(obj, b)
}

// ShouldBindJSON 是 c.ShouldBindWith(obj, binding.JSON) 的快捷方式。
//
// Example:
//
//	POST /user
//	Content-Type: application/json
//
//	Request Body:
//	{
//		"name": "Manu",
//		"age": 20
//	}
//
//	type User struct {
//		Name string `json:"name"`
//		Age  int    `json:"age"`
//	}
//
//	var user User
//	if err := c.ShouldBindJSON(&user); err != nil {
//		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
//		return
//	}
//	c.JSON(http.StatusOK, user)
func (c *Context) ShouldBindJSON(obj any) error {
	return c.ShouldBindWith(obj, binding.JSON)
}

// ShouldBindXML 是 c.ShouldBindWith(obj, binding.XML) 的快捷方式。
// 功能类似于 ShouldBindJSON，但将请求体作为 XML 数据绑定。
func (c *Context) ShouldBindXML(obj any) error {
	return c.ShouldBindWith(obj, binding.XML)
}

// ShouldBindQuery 是 c.ShouldBindWith(obj, binding.Query) 的快捷方式。
// 功能类似于 ShouldBindJSON，但绑定的是 URL 中的查询参数。
func (c *Context) ShouldBindQuery(obj any) error {
	return c.ShouldBindWith(obj, binding.Query)
}

// ShouldBindYAML 是 c.ShouldBindWith(obj, binding.YAML) 的快捷方式。
// 功能类似于 ShouldBindJSON，但将请求体作为 YAML 数据绑定。
func (c *Context) ShouldBindYAML(obj any) error {
	return c.ShouldBindWith(obj, binding.YAML)
}

// ShouldBindTOML 是 c.ShouldBindWith(obj, binding.TOML) 的快捷方式。
// 功能类似于 ShouldBindJSON，但将请求体作为 TOML 数据绑定。
func (c *Context) ShouldBindTOML(obj any) error {
	return c.ShouldBindWith(obj, binding.TOML)
}

// ShouldBindPlain 是 c.ShouldBindWith(obj, binding.Plain) 的快捷方式。
// 功能类似于 ShouldBindJSON，但绑定的是请求体中的纯文本数据。
func (c *Context) ShouldBindPlain(obj any) error {
	return c.ShouldBindWith(obj, binding.Plain)
}

// ShouldBindHeader 是 c.ShouldBindWith(obj, binding.Header) 的快捷方式。
// 功能类似于 ShouldBindJSON，但绑定的是 HTTP 头部中的值。
func (c *Context) ShouldBindHeader(obj any) error {
	return c.ShouldBindWith(obj, binding.Header)
}

// ShouldBindUri 使用指定的绑定引擎绑定传入的结构体指针。
// 功能类似于 ShouldBindJSON，但绑定的是 URI 中的参数。
func (c *Context) ShouldBindUri(obj any) error {
	m := make(map[string][]string, len(c.Params))
	for _, v := range c.Params {
		m[v.Key] = []string{v.Value}
	}
	return binding.Uri.BindUri(m, obj)
}

// ShouldBindWith 使用指定的绑定引擎绑定传入的结构体指针。
// 请参见 binding 包。
func (c *Context) ShouldBindWith(obj any, b binding.Binding) error {
	return b.Bind(c.Request, obj)
}

// ShouldBindBodyWith 与 ShouldBindWith 类似，但它会将请求体
// 存储到上下文中，并在再次调用时复用。
//
// 注意：此方法在绑定前会读取请求体。因此如果仅需调用一次，
// 建议使用 ShouldBindWith 以获得更好的性能。
func (c *Context) ShouldBindBodyWith(obj any, bb binding.BindingBody) (err error) {
	var body []byte
	if cb, ok := c.Get(BodyBytesKey); ok {
		if cbb, ok := cb.([]byte); ok {
			body = cbb
		}
	}
	if body == nil {
		body, err = io.ReadAll(c.Request.Body)
		if err != nil {
			return err
		}
		c.Set(BodyBytesKey, body)
	}
	return bb.BindBody(body, obj)
}

// ShouldBindBodyWithJSON 是 c.ShouldBindBodyWith(obj, binding.JSON) 的快捷方式。
func (c *Context) ShouldBindBodyWithJSON(obj any) error {
	return c.ShouldBindBodyWith(obj, binding.JSON)
}

// ShouldBindBodyWithXML 是 c.ShouldBindBodyWith(obj, binding.XML) 的快捷方式。
func (c *Context) ShouldBindBodyWithXML(obj any) error {
	return c.ShouldBindBodyWith(obj, binding.XML)
}

// ShouldBindBodyWithYAML 是 c.ShouldBindBodyWith(obj, binding.YAML) 的快捷方式。
func (c *Context) ShouldBindBodyWithYAML(obj any) error {
	return c.ShouldBindBodyWith(obj, binding.YAML)
}

// ShouldBindBodyWithTOML 是 c.ShouldBindBodyWith(obj, binding.TOML) 的快捷方式。
func (c *Context) ShouldBindBodyWithTOML(obj any) error {
	return c.ShouldBindBodyWith(obj, binding.TOML)
}

// ShouldBindBodyWithPlain 是 c.ShouldBindBodyWith(obj, binding.Plain) 的快捷方式。
func (c *Context) ShouldBindBodyWithPlain(obj any) error {
	return c.ShouldBindBodyWith(obj, binding.Plain)
}

// ClientIP 实现了尽力返回真实客户端 IP 的算法。
// 其底层调用 c.RemoteIP() 来检查远端 IP 是否为受信代理。
// 如果是受信代理，则会尝试解析 Engine.RemoteIPHeaders 定义的头部（默认为 [X-Forwarded-For, X-Real-IP]）。
// 如果头部语法无效 或 远端 IP 不对应于受信代理，
// 则返回远端 IP（来自 Request.RemoteAddr）。
func (c *Context) ClientIP() string {
	// 检查是否运行在受信任的平台上，如果出错则继续回溯处理
	if c.engine.TrustedPlatform != "" {
		// 开发者可自定义 Trusted Platform 头部，或使用预定义的常量
		if addr := c.requestHeader(c.engine.TrustedPlatform); addr != "" {
			return addr
		}
	}

	// Legacy "AppEngine" flag
	if c.engine.AppEngine {
		log.Println(`The AppEngine flag is going to be deprecated. Please check issues #2723 and #2739 and use 'TrustedPlatform: gin.PlatformGoogleAppEngine' instead.`)
		if addr := c.requestHeader("X-Appengine-Remote-Addr"); addr != "" {
			return addr
		}
	}

	var (
		trusted  bool
		remoteIP net.IP
	)
	// 如果 gin 监听的是 Unix 套接字，则始终信任该连接。
	localAddr, ok := c.Request.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if ok && strings.HasPrefix(localAddr.Network(), "unix") {
		trusted = true
	}

	// 回退处理
	if !trusted {
		// 同时检查 remoteIP 是否为受信代理。
		// 为执行此验证，会检查该 IP 是否包含在
		// Engine.SetTrustedProxies() 定义的至少一个 CIDR 块内
		remoteIP = net.ParseIP(c.RemoteIP())
		if remoteIP == nil {
			return ""
		}
		trusted = c.engine.isTrustedProxy(remoteIP)
	}

	if trusted && c.engine.ForwardedByClientIP && c.engine.RemoteIPHeaders != nil {
		for _, headerName := range c.engine.RemoteIPHeaders {
			headerValue := strings.Join(c.Request.Header.Values(headerName), ",")
			ip, valid := c.engine.validateHeader(headerValue)
			if valid {
				return ip
			}
		}
	}
	return remoteIP.String()
}

// RemoteIP 从 Request.RemoteAddr 解析 IP 地址，进行标准化处理并返回 IP（不含端口）。
func (c *Context) RemoteIP() string {
	ip, _, err := net.SplitHostPort(strings.TrimSpace(c.Request.RemoteAddr))
	if err != nil {
		return ""
	}
	return ip
}

// ContentType 返回请求的 Content-Type 头部。
func (c *Context) ContentType() string {
	return filterFlags(c.requestHeader("Content-Type"))
}

// IsWebsocket 如果请求头部表明客户端正在发起 websocket 握手，则返回 true。
func (c *Context) IsWebsocket() bool {
	if strings.Contains(strings.ToLower(c.requestHeader("Connection")), "upgrade") &&
		strings.EqualFold(c.requestHeader("Upgrade"), "websocket") {
		return true
	}
	return false
}

func (c *Context) requestHeader(key string) string {
	return c.Request.Header.Get(key)
}

/************************************/
/******** RESPONSE RENDERING ********/
/************************************/

// bodyAllowedForStatus 是 http.bodyAllowedForStatus 非导出函数的副本。
func bodyAllowedForStatus(status int) bool {
	switch {
	case status >= 100 && status <= 199:
		return false
	case status == http.StatusNoContent:
		return false
	case status == http.StatusNotModified:
		return false
	}
	return true
}

// Status 设置 HTTP 响应状态码。
func (c *Context) Status(code int) {
	c.Writer.WriteHeader(code)
}

// Header 是 c.Writer.Header().Set(key, value) 的智能快捷方式。
// 它会在响应中写入一个头部。
// 如果 value == ""，此方法会删除头部 c.Writer.Header().Del(key)
func (c *Context) Header(key, value string) {
	if value == "" {
		c.Writer.Header().Del(key)
		return
	}
	c.Writer.Header().Set(key, value)
}

// GetHeader 从请求头部返回值。
func (c *Context) GetHeader(key string) string {
	return c.requestHeader(key)
}

// GetRawData 返回流数据。
// 读取 HTTP 请求体的原始字节数据，且是「一次性读取」—— 读取后会清空请求体流，无法再次读取（除非重新封装）。
func (c *Context) GetRawData() ([]byte, error) {
	if c.Request.Body == nil {
		return nil, errors.New("cannot read nil body")
	}
	return io.ReadAll(c.Request.Body)
}

// SetSameSite 设置 Cookie 的 SameSite 属性
func (c *Context) SetSameSite(samesite http.SameSite) {
	c.sameSite = samesite
}

// SetCookie 向 ResponseWriter 的头部添加 Set-Cookie 头。
// 提供的 Cookie 必须具有有效的 Name 属性。无效的 Cookie 可能会被静默丢弃。
func (c *Context) SetCookie(name, value string, maxAge int, path, domain string, secure, httpOnly bool) {
	if path == "" {
		path = "/"
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     name,
		Value:    url.QueryEscape(value),
		MaxAge:   maxAge,
		Path:     path,
		Domain:   domain,
		SameSite: c.sameSite,
		Secure:   secure,
		HttpOnly: httpOnly,
	})
}

// SetCookieData 向 ResponseWriter 的头部添加 Set-Cookie 头。
// 它接受指向 http.Cookie 结构体的指针，以便更灵活地设置 Cookie 属性。
// 提供的 Cookie 必须具有有效的 Name 属性。无效的 Cookie 可能会被静默丢弃。
func (c *Context) SetCookieData(cookie *http.Cookie) {
	if cookie.Path == "" {
		cookie.Path = "/"
	}
	if cookie.SameSite == http.SameSiteDefaultMode {
		cookie.SameSite = c.sameSite
	}
	http.SetCookie(c.Writer, cookie)
}

// Cookie 返回请求中提供的指定名称的 Cookie，
// 如果未找到则返回 ErrNoCookie。返回的命名 Cookie 是未转义的。
// 如果多个 Cookie 匹配给定名称，仅返回其中一个 Cookie。
func (c *Context) Cookie(name string) (string, error) {
	cookie, err := c.Request.Cookie(name)
	if err != nil {
		return "", err
	}
	val, _ := url.QueryUnescape(cookie.Value)
	return val, nil
}

// Render 写入响应头部并调用 render.Render 来渲染数据。
func (c *Context) Render(code int, r render.Render) {
	c.Status(code)

	if !bodyAllowedForStatus(code) {
		r.WriteContentType(c.Writer)
		c.Writer.WriteHeaderNow()
		return
	}

	if err := r.Render(c.Writer); err != nil {
		// Pushing error to c.Errors
		_ = c.Error(err)
		c.Abort()
	}
}

// HTML 渲染由文件名指定的 HTTP 模板。
// 同时更新 HTTP 状态码并将 Content-Type 设置为 "text/html"。
// See http://golang.org/doc/articles/wiki/
func (c *Context) HTML(code int, name string, obj any) {
	instance := c.engine.HTMLRender.Instance(name, obj)
	c.Render(code, instance)
}

// IndentedJSON 将给定结构体序列化为带格式的 JSON（缩进+换行）并写入响应体。
// 同时将 Content-Type 设置为 "application/json"。
// 警告：建议仅在开发环境中使用，因为输出带格式的 JSON
// 会消耗更多 CPU 和带宽资源。建议使用 Context.JSON() 替代。
func (c *Context) IndentedJSON(code int, obj any) {
	c.Render(code, render.IndentedJSON{Data: obj})
}

// SecureJSON 将给定结构体序列化为 Secure JSON 并写入响应体。
// 如果给定结构体是数组值，默认在响应体前添加 "while(1),"。
// 同时将 Content-Type 设置为 "application/json"。
func (c *Context) SecureJSON(code int, obj any) {
	c.Render(code, render.SecureJSON{Prefix: c.engine.secureJSONPrefix, Data: obj})
}

// JSONP 将给定结构体序列化为 JSON 并写入响应体。
// 它在响应体前添加填充数据，以从与客户端不同域的服务器请求数据。
// 同时将 Content-Type 设置为 "application/javascript"。
func (c *Context) JSONP(code int, obj any) {
	callback := c.DefaultQuery("callback", "")
	if callback == "" {
		c.Render(code, render.JSON{Data: obj})
		return
	}
	c.Render(code, render.JsonpJSON{Callback: callback, Data: obj})
}

// JSON 将给定结构体序列化为 JSON 并写入响应体。
// 同时将 Content-Type 设置为 "application/json"。
func (c *Context) JSON(code int, obj any) {
	c.Render(code, render.JSON{Data: obj})
}

// AsciiJSON 将给定结构体序列化为 JSON 并写入响应体，同时将 Unicode 字符转换为 ASCII 字符串。
// 同时将 Content-Type 设置为 "application/json"。
func (c *Context) AsciiJSON(code int, obj any) {
	c.Render(code, render.AsciiJSON{Data: obj})
}

// PureJSON 将给定结构体序列化为 JSON 并写入响应体。
// 与 JSON 不同，PureJSON 不会将特殊 HTML 字符替换为其 Unicode 实体。
func (c *Context) PureJSON(code int, obj any) {
	c.Render(code, render.PureJSON{Data: obj})
}

// XML 将给定结构体序列化为 XML 并写入响应体。
// 同时将 Content-Type 设置为 "application/xml"。
func (c *Context) XML(code int, obj any) {
	c.Render(code, render.XML{Data: obj})
}

// YAML 将给定结构体序列化为 YAML 并写入响应体。
func (c *Context) YAML(code int, obj any) {
	c.Render(code, render.YAML{Data: obj})
}

// TOML 将给定结构体序列化为 TOML 并写入响应体。
func (c *Context) TOML(code int, obj any) {
	c.Render(code, render.TOML{Data: obj})
}

// ProtoBuf 将给定结构体序列化为 ProtoBuf 并写入响应体。
func (c *Context) ProtoBuf(code int, obj any) {
	c.Render(code, render.ProtoBuf{Data: obj})
}

// String 将给定字符串写入响应体。
func (c *Context) String(code int, format string, values ...any) {
	c.Render(code, render.String{Format: format, Data: values})
}

// Redirect 返回指向指定位置的 HTTP 重定向。
func (c *Context) Redirect(code int, location string) {
	c.Render(-1, render.Redirect{
		Code:     code,
		Location: location,
		Request:  c.Request,
	})
}

// Data 将数据写入响应体流并更新 HTTP 状态码。
func (c *Context) Data(code int, contentType string, data []byte) {
	c.Render(code, render.Data{
		ContentType: contentType,
		Data:        data,
	})
}

// DataFromReader 将指定的读取器内容写入响应体流并更新 HTTP 状态码。
func (c *Context) DataFromReader(code int, contentLength int64, contentType string, reader io.Reader, extraHeaders map[string]string) {
	c.Render(code, render.Reader{
		Headers:       extraHeaders,
		ContentType:   contentType,
		ContentLength: contentLength,
		Reader:        reader,
	})
}

// File 以高效的方式将指定文件写入响应体流。
func (c *Context) File(filepath string) {
	http.ServeFile(c.Writer, c.Request, filepath)
}

// FileFromFS 以高效的方式从 http.FileSystem 中将指定文件写入响应体流。
func (c *Context) FileFromFS(filepath string, fs http.FileSystem) {
	defer func(old string) {
		c.Request.URL.Path = old
	}(c.Request.URL.Path)

	c.Request.URL.Path = filepath

	http.FileServer(fs).ServeHTTP(c.Writer, c.Request)
}

var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

func escapeQuotes(s string) string {
	return quoteEscaper.Replace(s)
}

// FileAttachment 以高效的方式将指定文件写入响应体流
// 在客户端，文件通常会以给定的文件名被下载
func (c *Context) FileAttachment(filepath, filename string) {
	if isASCII(filename) {
		c.Writer.Header().Set("Content-Disposition", `attachment; filename="`+escapeQuotes(filename)+`"`)
	} else {
		c.Writer.Header().Set("Content-Disposition", `attachment; filename*=UTF-8''`+url.QueryEscape(filename))
	}
	http.ServeFile(c.Writer, c.Request, filepath)
}

// SSEvent 将 Server-Sent Event 写入响应体流。
func (c *Context) SSEvent(name string, message any) {
	c.Render(-1, sse.Event{
		Event: name,
		Data:  message,
	})
}

// Stream 发送流式响应，并返回一个布尔值
// 表示“客户端是否在流传输过程中断开连接”
func (c *Context) Stream(step func(w io.Writer) bool) bool {
	w := c.Writer
	clientGone := w.CloseNotify()
	for {
		select {
		case <-clientGone:
			return true
		default:
			keepOpen := step(w)
			w.Flush()
			if !keepOpen {
				return false
			}
		}
	}
}

/************************************/
/******** CONTENT NEGOTIATION *******/
/************************************/

// Negotiate 包含所有协商数据
type Negotiate struct {
	Offered      []string
	HTMLName     string
	HTMLData     any
	JSONData     any
	XMLData      any
	YAMLData     any
	Data         any
	TOMLData     any
	PROTOBUFData any
}

// Negotiate 根据可接受的 Accept 格式调用不同的 Render 方法。
func (c *Context) Negotiate(code int, config Negotiate) {
	switch c.NegotiateFormat(config.Offered...) {
	case binding.MIMEJSON:
		data := chooseData(config.JSONData, config.Data)
		c.JSON(code, data)

	case binding.MIMEHTML:
		data := chooseData(config.HTMLData, config.Data)
		c.HTML(code, config.HTMLName, data)

	case binding.MIMEXML:
		data := chooseData(config.XMLData, config.Data)
		c.XML(code, data)

	case binding.MIMEYAML, binding.MIMEYAML2:
		data := chooseData(config.YAMLData, config.Data)
		c.YAML(code, data)

	case binding.MIMETOML:
		data := chooseData(config.TOMLData, config.Data)
		c.TOML(code, data)

	case binding.MIMEPROTOBUF:
		data := chooseData(config.PROTOBUFData, config.Data)
		c.ProtoBuf(code, data)

	default:
		c.AbortWithError(http.StatusNotAcceptable, errors.New("the accepted formats are not offered by the server")) //nolint: errcheck
	}
}

// NegotiateFormat 返回一个可接受的 Accept 格式。
func (c *Context) NegotiateFormat(offered ...string) string {
	assert1(len(offered) > 0, "you must provide at least one offer")

	if c.Accepted == nil {
		c.Accepted = parseAccept(c.requestHeader("Accept"))
	}
	if len(c.Accepted) == 0 {
		return offered[0]
	}
	for _, accepted := range c.Accepted {
		for _, offer := range offered {
			// According to RFC 2616 and RFC 2396, non-ASCII characters are not allowed in headers,
			// therefore we can just iterate over the string without casting it into []rune
			i := 0
			for ; i < len(accepted) && i < len(offer); i++ {
				if accepted[i] == '*' || offer[i] == '*' {
					return offer
				}
				if accepted[i] != offer[i] {
					break
				}
			}
			if i == len(accepted) {
				return offer
			}
		}
	}
	return ""
}

// SetAccepted 设置 Accept 头部数据。
func (c *Context) SetAccepted(formats ...string) {
	c.Accepted = formats
}

/************************************/
/***** GOLANG.ORG/X/NET/CONTEXT *****/
/************************************/

// hasRequestContext 返回 c.Request 是否包含 Context 并进行回退处理。
func (c *Context) hasRequestContext() bool {
	hasFallback := c.engine != nil && c.engine.ContextWithFallback
	hasRequestContext := c.Request != nil && c.Request.Context() != nil
	return hasFallback && hasRequestContext
}

// Deadline 返回无截止时间的信息（ok==false），当 c.Request 没有 Context 时。
func (c *Context) Deadline() (deadline time.Time, ok bool) {
	if !c.hasRequestContext() {
		return
	}
	return c.Request.Context().Deadline()
}

// Done 返回 nil（将永远等待的通道），当 c.Request 没有 Context 时。
func (c *Context) Done() <-chan struct{} {
	if !c.hasRequestContext() {
		return nil
	}
	return c.Request.Context().Done()
}

// Err 返回 nil，当 c.Request 没有 Context 时。
func (c *Context) Err() error {
	if !c.hasRequestContext() {
		return nil
	}
	return c.Request.Context().Err()
}

// Value 返回与此上下文关联的键对应的值，若无关联值则返回 nil。
// 使用相同键连续调用 Value 会返回相同结果。
func (c *Context) Value(key any) any {
	if key == ContextRequestKey {
		return c.Request
	}
	if key == ContextKey {
		return c
	}
	if keyAsString, ok := key.(string); ok {
		if val, exists := c.Get(keyAsString); exists {
			return val
		}
	}
	if !c.hasRequestContext() {
		return nil
	}
	return c.Request.Context().Value(key)
}
