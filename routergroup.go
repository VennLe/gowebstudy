// Copyright 2014 Manu Martinez-Almeida. All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package gin

import (
	"net/http"
	"path"
	"regexp"
	"strings"
)

var (
	// regEnLetter 匹配 HTTP 方法名的英文字母
	regEnLetter = regexp.MustCompile("^[A-Z]+$")

	// anyMethods for RouterGroup Any method
	anyMethods = []string{
		http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodHead, http.MethodOptions, http.MethodDelete, http.MethodConnect,
		http.MethodTrace,
	}
)

// IRouter 定义了所有路由器处理接口，包括单一路由和路由组。
type IRouter interface {
	IRoutes
	Group(string, ...HandlerFunc) *RouterGroup
}

// IRoutes 定义了所有路由器处理接口。
type IRoutes interface {
	Use(...HandlerFunc) IRoutes

	Handle(string, string, ...HandlerFunc) IRoutes
	Any(string, ...HandlerFunc) IRoutes
	GET(string, ...HandlerFunc) IRoutes
	POST(string, ...HandlerFunc) IRoutes
	DELETE(string, ...HandlerFunc) IRoutes
	PATCH(string, ...HandlerFunc) IRoutes
	PUT(string, ...HandlerFunc) IRoutes
	OPTIONS(string, ...HandlerFunc) IRoutes
	HEAD(string, ...HandlerFunc) IRoutes
	Match([]string, string, ...HandlerFunc) IRoutes

	StaticFile(string, string) IRoutes
	StaticFileFS(string, string, http.FileSystem) IRoutes
	Static(string, string) IRoutes
	StaticFS(string, http.FileSystem) IRoutes
}

// RouterGroup 在内部用于配置路由器，一个 RouterGroup 关联着
// 一个前缀和一组处理函数（中间件）。
type RouterGroup struct {
	Handlers HandlersChain
	basePath string
	engine   *Engine
	root     bool
}

var _ IRouter = (*RouterGroup)(nil)

// Use 向路由组添加中间件，示例代码请参见 GitHub。
func (group *RouterGroup) Use(middleware ...HandlerFunc) IRoutes {
	group.Handlers = append(group.Handlers, middleware...)
	return group.returnObj()
}

// Group 创建一个新的路由组。您应将具有公共中间件或相同路径前缀的所有路由加入此组。
// 例如，所有需要使用公共授权中间件的路由可以归为一组。
func (group *RouterGroup) Group(relativePath string, handlers ...HandlerFunc) *RouterGroup {
	return &RouterGroup{
		Handlers: group.combineHandlers(handlers),
		basePath: group.calculateAbsolutePath(relativePath),
		engine:   group.engine,
	}
}

// BasePath 返回路由组的基础路径。
// 例如, 如果 v := router.Group("/rest/n/v1/api"), v.BasePath() 就是 "/rest/n/v1/api".
func (group *RouterGroup) BasePath() string {
	return group.basePath
}

func (group *RouterGroup) handle(httpMethod, relativePath string, handlers HandlersChain) IRoutes {
	absolutePath := group.calculateAbsolutePath(relativePath)
	handlers = group.combineHandlers(handlers)
	group.engine.addRoute(httpMethod, absolutePath, handlers)
	return group.returnObj()
}

// Handle 为给定的路径和方法注册新的请求处理函数及中间件。
// 最后一个处理函数应为实际的处理程序，其他处理函数应为可在不同路由间共享的中间件。
// 示例代码请参见 GitHub。
//
// 对于 GET、POST、PUT、PATCH 和 DELETE 请求，可使用相应的快捷函数。
//
// 此函数用于批量加载，并允许使用不常用、
// 非标准化或自定义的方法（例如用于与代理的内部通信）。
func (group *RouterGroup) Handle(httpMethod, relativePath string, handlers ...HandlerFunc) IRoutes {
	if matched := regEnLetter.MatchString(httpMethod); !matched {
		panic("http method " + httpMethod + " is not valid")
	}
	return group.handle(httpMethod, relativePath, handlers)
}

// POST 是 router.Handle("POST", path, handlers) 的快捷方式。
func (group *RouterGroup) POST(relativePath string, handlers ...HandlerFunc) IRoutes {
	return group.handle(http.MethodPost, relativePath, handlers)
}

// GET 是 router.Handle("GET", path, handlers) 的快捷方式。
func (group *RouterGroup) GET(relativePath string, handlers ...HandlerFunc) IRoutes {
	return group.handle(http.MethodGet, relativePath, handlers)
}

// DELETE 是 router.Handle("DELETE", path, handlers) 的快捷方式。
func (group *RouterGroup) DELETE(relativePath string, handlers ...HandlerFunc) IRoutes {
	return group.handle(http.MethodDelete, relativePath, handlers)
}

// PATCH 是 router.Handle("PATCH", path, handlers) 的快捷方式。
func (group *RouterGroup) PATCH(relativePath string, handlers ...HandlerFunc) IRoutes {
	return group.handle(http.MethodPatch, relativePath, handlers)
}

// PUT 是 router.Handle("PUT", path, handlers) 的快捷方式。
func (group *RouterGroup) PUT(relativePath string, handlers ...HandlerFunc) IRoutes {
	return group.handle(http.MethodPut, relativePath, handlers)
}

// OPTIONS 是 router.Handle("OPTIONS", path, handlers) 的快捷方式。
func (group *RouterGroup) OPTIONS(relativePath string, handlers ...HandlerFunc) IRoutes {
	return group.handle(http.MethodOptions, relativePath, handlers)
}

// HEAD 是 router.Handle("HEAD", path, handlers) 的快捷方式。
func (group *RouterGroup) HEAD(relativePath string, handlers ...HandlerFunc) IRoutes {
	return group.handle(http.MethodHead, relativePath, handlers)
}

// Any 注册一个匹配所有 HTTP 方法的路由。
// GET, POST, PUT, PATCH, HEAD, OPTIONS, DELETE, CONNECT, TRACE.
func (group *RouterGroup) Any(relativePath string, handlers ...HandlerFunc) IRoutes {
	for _, method := range anyMethods {
		group.handle(method, relativePath, handlers)
	}

	return group.returnObj()
}

// Match 注册一个匹配您声明的指定方法的路由。
func (group *RouterGroup) Match(methods []string, relativePath string, handlers ...HandlerFunc) IRoutes {
	for _, method := range methods {
		group.handle(method, relativePath, handlers)
	}

	return group.returnObj()
}

// StaticFile 注册单个路由以提供本地文件系统中的单个文件。
// router.StaticFile("favicon.ico", "./resources/favicon.ico")
func (group *RouterGroup) StaticFile(relativePath, filepath string) IRoutes {
	return group.staticFileHandler(relativePath, func(c *Context) {
		c.File(filepath)
	})
}

// StaticFileFS 的功能与 StaticFile 相同，但可以使用自定义的 http.FileSystem。
// router.StaticFileFS("favicon.ico", "./resources/favicon.ico", Dir{".", false})
// Gin 默认使用：gin.Dir()
func (group *RouterGroup) StaticFileFS(relativePath, filepath string, fs http.FileSystem) IRoutes {
	return group.staticFileHandler(relativePath, func(c *Context) {
		c.FileFromFS(filepath, fs)
	})
}

func (group *RouterGroup) staticFileHandler(relativePath string, handler HandlerFunc) IRoutes {
	if strings.Contains(relativePath, ":") || strings.Contains(relativePath, "*") {
		panic("URL parameters can not be used when serving a static file")
	}
	group.GET(relativePath, handler)
	group.HEAD(relativePath, handler)
	return group.returnObj()
}

// Static 从给定的文件系统根目录提供文件服务。
// 内部使用 http.FileServer，因此会调用 http.NotFound 而不是
// 路由器的 NotFound 处理程序。
// 要使用操作系统的文件系统实现，请使用：
//
//	router.Static("/static", "/var/www")
func (group *RouterGroup) Static(relativePath, root string) IRoutes {
	return group.StaticFS(relativePath, Dir(root, false))
}

// StaticFS 的功能与 Static() 相同，但可以使用自定义的 http.FileSystem。
// Gin 默认使用：gin.Dir()
func (group *RouterGroup) StaticFS(relativePath string, fs http.FileSystem) IRoutes {
	if strings.Contains(relativePath, ":") || strings.Contains(relativePath, "*") {
		panic("URL parameters can not be used when serving a static folder")
	}
	handler := group.createStaticHandler(relativePath, fs)
	urlPattern := path.Join(relativePath, "/*filepath")

	// Register GET and HEAD handlers
	group.GET(urlPattern, handler)
	group.HEAD(urlPattern, handler)
	return group.returnObj()
}

func (group *RouterGroup) createStaticHandler(relativePath string, fs http.FileSystem) HandlerFunc {
	absolutePath := group.calculateAbsolutePath(relativePath)
	fileServer := http.StripPrefix(absolutePath, http.FileServer(fs))

	return func(c *Context) {
		if _, noListing := fs.(*OnlyFilesFS); noListing {
			c.Writer.WriteHeader(http.StatusNotFound)
		}

		file := c.Param("filepath")
		// 检查文件是否存在和/或我们是否有访问权限
		f, err := fs.Open(file)
		if err != nil {
			c.Writer.WriteHeader(http.StatusNotFound)
			c.handlers = group.engine.noRoute
			// 重置索引 index
			c.index = -1
			return
		}
		f.Close()

		fileServer.ServeHTTP(c.Writer, c.Request)
	}
}

func (group *RouterGroup) combineHandlers(handlers HandlersChain) HandlersChain {
	finalSize := len(group.Handlers) + len(handlers)
	assert1(finalSize < int(abortIndex), "too many handlers")
	mergedHandlers := make(HandlersChain, finalSize)
	copy(mergedHandlers, group.Handlers)
	copy(mergedHandlers[len(group.Handlers):], handlers)
	return mergedHandlers
}

func (group *RouterGroup) calculateAbsolutePath(relativePath string) string {
	return joinPaths(group.basePath, relativePath)
}

func (group *RouterGroup) returnObj() IRoutes {
	if group.root {
		return group.engine
	}
	return group
}
