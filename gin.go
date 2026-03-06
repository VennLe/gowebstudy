// Copyright 2014 Manu Martinez-Almeida. All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package gin

import (
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/gin-gonic/gin/internal/bytesconv"
	filesystem "github.com/gin-gonic/gin/internal/fs"
	"github.com/gin-gonic/gin/render"
	"github.com/quic-go/quic-go/http3"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

const (
	defaultMultipartMemory = 32 << 20 // 32 MB
	escapedColon           = "\\:"
	colon                  = ":"
	backslash              = "\\"
)

var (
	default404Body = []byte("404 page not found")
	default405Body = []byte("405 method not allowed")
)

var defaultPlatform string

var defaultTrustedCIDRs = []*net.IPNet{
	{ // 0.0.0.0/0 (IPv4)
		IP:   net.IP{0x0, 0x0, 0x0, 0x0},
		Mask: net.IPMask{0x0, 0x0, 0x0, 0x0},
	},
	{ // ::/0 (IPv6)
		IP:   net.IP{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
		Mask: net.IPMask{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
	},
}

// HandlerFunc 定义gin中间件所使用的处理器作为返回值
type HandlerFunc func(*Context)

// OptionFunc 定义用于修改默认配置的函数
type OptionFunc func(*Engine)

// HandlersChain 定义了一个处理器函数切片
type HandlersChain []HandlerFunc

// Last 返回链中的最后一个处理器，即最后一个处理器是主处理器
func (c HandlersChain) Last() HandlerFunc {
	if length := len(c); length > 0 {
		return c[length-1]
	}
	return nil
}

// RouteInfo 表示一个请求路由的规格，其中包含方法、路径及其对应的处理器
type RouteInfo struct {
	Method      string
	Path        string
	Handler     string
	HandlerFunc HandlerFunc
}

// RoutesInfo 定义了一个路由信息切片
type RoutesInfo []RouteInfo

// 可信平台
const (
	// PlatformGoogleAppEngine when running on Google App Engine. Trust X-Appengine-Remote-Addr
	// for determining the client's IP
	PlatformGoogleAppEngine = "X-Appengine-Remote-Addr"
	// PlatformCloudflare when using Cloudflare's CDN. Trust CF-Connecting-IP for determining
	// the client's IP
	PlatformCloudflare = "CF-Connecting-IP"
	// PlatformFlyIO when running on Fly.io. Trust Fly-Client-IP for determining the client's IP
	PlatformFlyIO = "Fly-Client-IP"
)

// Engine 是框架的实例，它包含了多路复用器、中间件和配置设置。
// 可通过 New() 或 Default() 函数创建一个 Engine 实例
type Engine struct {
	RouterGroup

	// routeTreesUpdated 确保路由树（用于路由 HTTP 请求）的初始化或更新仅执行一次，即便在并发情况下多次调用亦然。
	routeTreesUpdated sync.Once

	// RedirectTrailingSlash 启用自动重定向功能：当当前路由无法匹配，但存在带（或不带）尾部斜杠的路径处理器时，
	// 客户端将被重定向到正确的路径。例如，若请求路径为 /foo/ 但仅存在 /foo 的路由，
	// 客户端将被重定向到 /foo，其中 GET 请求返回 HTTP 状态码 301，其他请求方法返回 307。
	RedirectTrailingSlash bool

	// RedirectFixedPath 若启用此选项，当请求路径未注册对应的处理器时，路由器会尝试修正该路径。
	// 首先，多余的路径元素（如 ../ 或 //）会被移除。
	// 随后，路由器会对清理后的路径执行不区分大小写的查找。
	// 若找到该路径对应的处理器，路由器将重定向到修正后的路径，GET 请求使用状态码 301，其他请求方法使用 307。
	// 例如，/FOO 和 /..//Foo 可能被重定向到 /foo。
	// 此功能与 RedirectTrailingSlash 选项相互独立。
	RedirectFixedPath bool

	// HandleMethodNotAllowed 如果启用，路由器会检查当前路由是否允许其他方法，
	// 如果当前请求无法被路由。
	// 在这种情况下，请求将响应 'Method Not Allowed'
	// 和 HTTP 状态码 405。
	// 如果没有其他方法被允许，请求将委托给 NotFound
	// 处理程序。
	HandleMethodNotAllowed bool

	// ForwardedByClientIP 如果启用，将从请求的头部中解析客户端 IP，
	// 这些头部与 `(*gin.Engine).RemoteIPHeaders` 中存储的头部匹配。如果未获取到 IP，
	// 将回退到从 `(*gin.Context).Request.RemoteAddr` 获取的 IP。
	ForwardedByClientIP bool

	// AppEngine 已弃用。
	// 弃用：请使用值 gin.PlatformGoogleAppEngine 的 TrustedPlatform
	// #726 #755 如果启用，它将信任以 'X-AppEngine...' 开头的
	// 部分头部，以便更好地与该 PaaS 集成。
	AppEngine bool

	// UseRawPath 如果启用，将使用 url.RawPath 来查找参数。
	// RawPath 仅作为提示，应使用 EscapedPath()。（参考：https://pkg.go.dev/net/url@master#URL）
	// 仅在明确知晓用途时使用 RawPath。
	UseRawPath bool

	// UseEscapedPath 如果启用，将使用 url.EscapedPath() 来查找参数
	// 它会覆盖 UseRawPath 的设置
	UseEscapedPath bool

	// UnescapePathValues 如果为 true，路径值将被解码。
	// 如果 UseRawPath 和 UseEscapedPath 为 false（默认值），则 UnescapePathValues 实际上为 true，
	// 因为将使用 url.Path，它已经是解码后的值。
	UnescapePathValues bool

	// RemoveExtraSlash 即使 URL 中包含额外的斜杠，也可以从中解析参数。
	// 参见 PR #1817 和 issue #1644
	RemoveExtraSlash bool

	// RemoteIPHeaders 当 (*gin.Engine).ForwardedByClientIP 为 true 且
	// (*gin.Context).Request.RemoteAddr 与 (*gin.Engine).SetTrustedProxies() 定义的
	// 网络源列表中的至少一个匹配时，用于获取客户端 IP 的头部列表。
	RemoteIPHeaders []string

	// TrustedPlatform 如果设置为 gin.Platform* 的常量值，将信任由该平台设置的头部，
	// 例如用于确定客户端 IP。
	TrustedPlatform string

	// MaxMultipartMemory 是传递给 http.Request 的 ParseMultipartForm
	// 方法调用的 'maxMemory' 参数值。
	MaxMultipartMemory int64

	// UseH2C 启用 h2c 支持。
	// 核心作用：当 UseH2C = true 时，Gin 会开启对 H2C 协议的支持，允许服务端在不配置 TLS/HTTPS的情况下，处理客户端的 HTTP/2 明文请求。
	UseH2C bool

	// ContextWithFallback 当 Context.Request.Context() 非空时，
	// 启用回退的 Context.Deadline(), Context.Done(), Context.Err() 和 Context.Value() 功能。
	ContextWithFallback bool

	// 模板分隔符
	// 自定义 HTML 模板的左右分隔符（默认是 {{ 和 }}），比如想改成<%和%>时配置这个字段。
	//示例：r.Delims = render.Delims{Left: "<%", Right: "%>"}
	delims render.Delims

	secureJSONPrefix string

	// HTML 渲染器
	// 负责加载、解析 HTML 模板并渲染，Gin 内置了HTMLDebug（开发环境，热更新模板）和HTMLProduction（生产环境，缓存模板）两种实现，通过这个字段指定。
	HTMLRender render.HTMLRender

	// 模板函数映射
	// 给 HTML 模板注册自定义函数（比如格式化时间、字符串处理），示例：r.FuncMap = template.FuncMap{"now": time.Now}，模板中可直接用{{now}}调用。
	FuncMap template.FuncMap

	// 全局 404 处理器链
	// 内部字段，是noRoute + 全局中间件的合并结果（Gin 内部自动拼接，无需手动配置）。
	allNoRoute HandlersChain

	// 全局 405 处理器链
	// 内部字段，是 noMethod + 全局中间件的合并结果（Gin 内部自动拼接）
	allNoMethod HandlersChain

	// 404 处理器链
	// 当请求的路由不存在时，执行这个处理器链（自定义 404 页面 / 响应）。
	// 示例：r.NoRoute(func(c *gin.Context) { c.String(404, "页面不存在") })
	noRoute HandlersChain

	// 405 处理器链
	// 当路由存在但请求方法不允许（比如 GET 路由接收到 POST 请求），执行这个处理器链（自定义 405 响应）。
	// 示例：r.NoMethod(func(c *gin.Context) { c.String(405, "方法不允许") })
	noMethod HandlersChain

	// Context 对象池
	// Gin 的核心性能优化点：sync.Pool用于复用gin.Context对象，避免每次请求都创建新的 Context（减少 GC 压力）。
	// 每次请求过来，从 pool 中取一个 Context，请求结束后放回 pool。
	pool sync.Pool

	// 方法路由树
	// Gin 的核心路由结构，是一个数组（每个元素对应一个 HTTP 方法：GET/POST 等），每个元素是一棵前缀树（Radix Tree），用于快速匹配路由路径。
	// 比如trees[0]对应 GET 方法的路由树，trees[1]对应 POST 方法的路由树。
	trees methodTrees

	// 最大路径参数数量
	// 记录路由中最多包含的路径参数数量（比如/user/:id/:name有 2 个参数），用于预分配内存，提升路由匹配效率（Gin 内部自动计算，无需手动配置）。
	maxParams uint16

	// 最大路径分段数量
	// 记录路由路径最多被/分割的段数（比如/a/b/c有 3 段），同样用于预分配内存，优化路由匹配性能（内部自动计算）。
	maxSections uint16

	// 可信代理列表
	// 当 Gin 服务部署在反向代理（Nginx/Traefik）后面时，配置可信的代理 IP / 网段，Gin 会从X-Forwarded-For等请求头中正确解析客户端真实 IP。
	// 示例：r.SetTrustedProxies([]string{"192.168.1.0/24"})
	trustedProxies []string
	// 可信代理 CIDR
	// trustedProxies的解析结果（内部字段），Gin 会把trustedProxies中的字符串转换成net.IPNet类型，方便快速判断代理是否可信。
	trustedCIDRs []*net.IPNet
}

var _ IRouter = (*Engine)(nil)

// New 返回一个全新的、未附加任何中间件的 Engine 实例。
// 默认配置如下：
// - RedirectTrailingSlash:  true
// - RedirectFixedPath:      false
// - HandleMethodNotAllowed: false
// - ForwardedByClientIP:    true
// - UseRawPath:             false
// - UseEscapedPath: 		 false
// - UnescapePathValues:     true
func New(opts ...OptionFunc) *Engine {
	debugPrintWARNINGNew()
	engine := &Engine{
		RouterGroup: RouterGroup{
			Handlers: nil,
			basePath: "/",
			root:     true,
		},
		FuncMap:                template.FuncMap{},
		RedirectTrailingSlash:  true,
		RedirectFixedPath:      false,
		HandleMethodNotAllowed: false,
		ForwardedByClientIP:    true,
		RemoteIPHeaders:        []string{"X-Forwarded-For", "X-Real-IP"},
		TrustedPlatform:        defaultPlatform,
		UseRawPath:             false,
		UseEscapedPath:         false,
		RemoveExtraSlash:       false,
		UnescapePathValues:     true,
		MaxMultipartMemory:     defaultMultipartMemory,
		trees:                  make(methodTrees, 0, 9),
		delims:                 render.Delims{Left: "{{", Right: "}}"},
		secureJSONPrefix:       "while(1);",
		trustedProxies:         []string{"0.0.0.0/0", "::/0"},
		trustedCIDRs:           defaultTrustedCIDRs,
	}
	engine.engine = engine
	engine.pool.New = func() any {
		return engine.allocateContext(engine.maxParams)
	}
	return engine.With(opts...)
}

// Default 返回一个已附加了 Logger 和 Recovery 中间件的 Engine 实例
func Default(opts ...OptionFunc) *Engine {
	debugPrintWARNINGDefault()
	engine := New()
	engine.Use(Logger(), Recovery())
	return engine.With(opts...)
}

func (engine *Engine) Handler() http.Handler {
	if !engine.UseH2C {
		return engine
	}

	h2s := &http2.Server{}
	return h2c.NewHandler(engine, h2s)
}

// 这是 Gin 为了优化路由匹配性能而设计的 Context 初始化方法，核心目的是为每个 HTTP 请求提前分配一个带有预分配内存的gin.Context对象，
// 避免运行时频繁扩容切片，减少 GC（垃圾回收）压力。
// Gin 的Context对象承载了请求的所有上下文信息（路径参数、请求头、响应 Writer 等），其中：
// params：存储路由匹配的路径参数（如/user/:id中的id=123），类型是Params（本质是[]Param切片）；
// skippedNodes：存储路由匹配过程中跳过的节点（内部优化用），类型是[]skippedNode切片。
// 如果每次请求都用make(Params, 0)创建空切片（默认容量 0），当路径参数较多时，切片会频繁扩容（每次扩容都会分配新内存、拷贝数据），这会显著增加 GC 负担，降低高并发下的性能。
// allocateContext的核心就是根据路由的最大参数数 / 最大分段数，提前给切片分配足够的初始容量，避免运行时扩容。
func (engine *Engine) allocateContext(maxParams uint16) *Context {
	v := make(Params, 0, maxParams)
	skippedNodes := make([]skippedNode, 0, engine.maxSections)
	return &Context{engine: engine, params: &v, skippedNodes: &skippedNodes}
}

// Delims 设置模板的左右定界符，并返回一个 Engine 实例
func (engine *Engine) Delims(left, right string) *Engine {
	engine.delims = render.Delims{Left: left, Right: right}
	return engine
}

// SecureJsonPrefix 设置 Context.SecureJSON 中使用的 secureJSONPrefix 前缀
func (engine *Engine) SecureJsonPrefix(prefix string) *Engine {
	engine.secureJSONPrefix = prefix
	return engine
}

// LoadHTMLGlob 通过 glob 模式加载 HTML 文件
// 并将结果与 HTML 渲染器关联。
func (engine *Engine) LoadHTMLGlob(pattern string) {
	left := engine.delims.Left
	right := engine.delims.Right
	templ := template.Must(template.New("").Delims(left, right).Funcs(engine.FuncMap).ParseGlob(pattern))

	if IsDebugging() {
		debugPrintLoadTemplate(templ)
		engine.HTMLRender = render.HTMLDebug{Glob: pattern, FuncMap: engine.FuncMap, Delims: engine.delims}
		return
	}

	engine.SetHTMLTemplate(templ)
}

// LoadHTMLFiles 加载一系列HTML文件，并将结果与HTML渲染器相关联
func (engine *Engine) LoadHTMLFiles(files ...string) {
	if IsDebugging() {
		engine.HTMLRender = render.HTMLDebug{Files: files, FuncMap: engine.FuncMap, Delims: engine.delims}
		return
	}

	templ := template.Must(template.New("").Delims(engine.delims.Left, engine.delims.Right).Funcs(engine.FuncMap).ParseFiles(files...))
	engine.SetHTMLTemplate(templ)
}

// LoadHTMLFS 加载一个 http.FileSystem 和一组匹配模式，并将结果与HTML渲染器相关联。
func (engine *Engine) LoadHTMLFS(fs http.FileSystem, patterns ...string) {
	if IsDebugging() {
		engine.HTMLRender = render.HTMLDebug{FileSystem: fs, Patterns: patterns, FuncMap: engine.FuncMap, Delims: engine.delims}
		return
	}

	templ := template.Must(template.New("").Delims(engine.delims.Left, engine.delims.Right).Funcs(engine.FuncMap).ParseFS(
		filesystem.FileSystem{FileSystem: fs}, patterns...))
	engine.SetHTMLTemplate(templ)
}

// SetHTMLTemplate 将模板与HTML渲染器关联。
func (engine *Engine) SetHTMLTemplate(templ *template.Template) {
	if len(engine.trees) > 0 {
		debugPrintWARNINGSetHTMLTemplate()
	}

	engine.HTMLRender = render.HTMLProduction{Template: templ.Funcs(engine.FuncMap)}
}

// SetFuncMap 设置用于 template.FuncMap 的 FuncMap。
func (engine *Engine) SetFuncMap(funcMap template.FuncMap) {
	engine.FuncMap = funcMap
}

// NoRoute 添加 NoRoute 的路由处理器。默认返回404状态码
func (engine *Engine) NoRoute(handlers ...HandlerFunc) {
	engine.noRoute = handlers
	engine.rebuild404Handlers()
}

// NoMethod 设置当 Engine.HandleMethodNotAllowed = true 时被调用的处理器
func (engine *Engine) NoMethod(handlers ...HandlerFunc) {
	engine.noMethod = handlers
	engine.rebuild405Handlers()
}

// Use 将全局中间件附加到路由器。即通过 Use() 附加的中间件将会被
// 包含在每一个请求的处理链中，即使是 404、405、静态文件等...
// 例如，记录日志或错误处理的中间件适合放在这里。
func (engine *Engine) Use(middleware ...HandlerFunc) IRoutes {
	engine.RouterGroup.Use(middleware...)
	engine.rebuild404Handlers()
	engine.rebuild405Handlers()
	return engine
}

// With 返回一个配置了 OptionFunc 中设置的 Engine。
func (engine *Engine) With(opts ...OptionFunc) *Engine {
	for _, opt := range opts {
		opt(engine)
	}

	return engine
}

func (engine *Engine) rebuild404Handlers() {
	engine.allNoRoute = engine.combineHandlers(engine.noRoute)
}

func (engine *Engine) rebuild405Handlers() {
	engine.allNoMethod = engine.combineHandlers(engine.noMethod)
}

// 这是 Gin 路由注册的底层核心实现，负责将handle方法传递过来的「HTTP 方法、绝对路径、处理器链」最终写入到对应的前缀树（trees）中，
// 同时更新路由的全局统计信息（最大参数数、最大分段数），是连接上层路由注册和底层路由树的关键桥梁。
func (engine *Engine) addRoute(method, path string, handlers HandlersChain) {
	// 1. 合法性断言校验（不满足则panic）
	assert1(path[0] == '/', "path must begin with '/'")              // 路径必须以/开头
	assert1(method != "", "HTTP method can not be empty")            // HTTP方法不能为空
	assert1(len(handlers) > 0, "there must be at least one handler") // 至少有一个处理器

	// 2. 调试模式下打印路由信息（如GET /admin/profile [AuthMiddleware, ProfileHandler]）
	debugPrintRoute(method, path, handlers)

	// 3. 找到当前HTTP方法对应的路由树根节点（比如GET方法的root）
	root := engine.trees.get(method)
	if root == nil {
		// 3.1 若不存在该方法的路由树，新建根节点并加入trees数组
		root = new(node)
		root.fullPath = "/"
		engine.trees = append(engine.trees, methodTree{method: method, root: root})
	}
	// 4. 调用根节点的addRoute方法，将路径+处理器链插入到前缀树中
	root.addRoute(path, handlers)

	// 5. 更新全局最大路径参数数（用于Context预分配）
	if paramsCount := countParams(path); paramsCount > engine.maxParams {
		engine.maxParams = paramsCount
	}

	// 6. 更新全局最大路径分段数（用于Context预分配）
	if sectionsCount := countSections(path); sectionsCount > engine.maxSections {
		engine.maxSections = sectionsCount
	}
}

// Routes 返回已注册路由的切片，包含一些有用信息，例如：
// HTTP 方法、路径和处理函数名称。
func (engine *Engine) Routes() (routes RoutesInfo) {
	for _, tree := range engine.trees {
		routes = iterate("", tree.method, routes, tree.root)
	}
	return routes
}

func iterate(path, method string, routes RoutesInfo, root *node) RoutesInfo {
	path += root.path
	if len(root.handlers) > 0 {
		handlerFunc := root.handlers.Last()
		routes = append(routes, RouteInfo{
			Method:      method,
			Path:        path,
			Handler:     nameOfFunction(handlerFunc),
			HandlerFunc: handlerFunc,
		})
	}
	for _, child := range root.children {
		routes = iterate(path, method, routes, child)
	}
	return routes
}

func (engine *Engine) prepareTrustedCIDRs() ([]*net.IPNet, error) {
	if engine.trustedProxies == nil {
		return nil, nil
	}

	cidr := make([]*net.IPNet, 0, len(engine.trustedProxies))
	for _, trustedProxy := range engine.trustedProxies {
		if !strings.Contains(trustedProxy, "/") {
			ip := parseIP(trustedProxy)
			if ip == nil {
				return cidr, &net.ParseError{Type: "IP address", Text: trustedProxy}
			}

			switch len(ip) {
			case net.IPv4len:
				trustedProxy += "/32"
			case net.IPv6len:
				trustedProxy += "/128"
			}
		}
		_, cidrNet, err := net.ParseCIDR(trustedProxy)
		if err != nil {
			return cidr, err
		}
		cidr = append(cidr, cidrNet)
	}
	return cidr, nil
}

// SetTrustedProxies 设置一组信任的网络源地址（IPv4地址、
// IPv4 CIDR、IPv6地址或IPv6 CIDR）。当 (*gin.Engine).ForwardedByClientIP 为 true 时，
// 将从这些受信任的代理中读取包含客户端真实IP的请求头。
// TrustedProxies 功能默认启用，且默认信任所有代理。
// 若要禁用此功能，请使用 Engine.SetTrustedProxies(nil)，
// 此后 Context.ClientIP() 将直接返回远端地址。
func (engine *Engine) SetTrustedProxies(trustedProxies []string) error {
	engine.trustedProxies = trustedProxies
	return engine.parseTrustedProxies()
}

// isUnsafeTrustedProxies 检查 Engine.trustedCIDRs 是否包含所有IP地址，若包含则不安全（返回true）
func (engine *Engine) isUnsafeTrustedProxies() bool {
	return engine.isTrustedProxy(net.ParseIP("0.0.0.0")) || engine.isTrustedProxy(net.ParseIP("::"))
}

// parseTrustedProxies 将 Engine.trustedProxies 解析为 Engine.trustedCIDRs
func (engine *Engine) parseTrustedProxies() error {
	trustedCIDRs, err := engine.prepareTrustedCIDRs()
	engine.trustedCIDRs = trustedCIDRs
	return err
}

// isTrustedProxy 将根据 Engine.trustedCIDRs 检查IP地址是否包含在受信列表中
func (engine *Engine) isTrustedProxy(ip net.IP) bool {
	if engine.trustedCIDRs == nil {
		return false
	}
	for _, cidr := range engine.trustedCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// validateHeader 将解析 X-Forwarded-For 请求头并返回受信的客户端IP地址
func (engine *Engine) validateHeader(header string) (clientIP string, valid bool) {
	if header == "" {
		return "", false
	}
	items := strings.Split(header, ",")
	for i := len(items) - 1; i >= 0; i-- {
		ipStr := strings.TrimSpace(items[i])
		ip := net.ParseIP(ipStr)
		if ip == nil {
			break
		}

		// X-Forwarded-For 是由代理添加的
		// 反向检查IP地址，并在发现不可信代理时停止
		if (i == 0) || (!engine.isTrustedProxy(ip)) {
			return ipStr, true
		}
	}
	return "", false
}

// updateRouteTree 递归地更新路由树
func updateRouteTree(n *node) {
	n.path = strings.ReplaceAll(n.path, escapedColon, colon)
	n.fullPath = strings.ReplaceAll(n.fullPath, escapedColon, colon)
	n.indices = strings.ReplaceAll(n.indices, backslash, colon)
	if n.children == nil {
		return
	}
	for _, child := range n.children {
		updateRouteTree(child)
	}
}

// updateRouteTrees 递归地更新路由树
func (engine *Engine) updateRouteTrees() {
	for _, tree := range engine.trees {
		updateRouteTree(tree.root)
	}
}

// parseIP 解析字符串表示的IP地址，返回一个具有最小字节表示形式的 net.IP，若输入无效则返回 nil。
func parseIP(ip string) net.IP {
	parsedIP := net.ParseIP(ip)

	if ipv4 := parsedIP.To4(); ipv4 != nil {
		// 以4字节表示形式返回IP地址
		return ipv4
	}

	// 以16字节表示形式返回IP地址，若无效则返回 nil
	return parsedIP
}

// Run 将路由器绑定到 http.Server 并开始监听和处理HTTP请求。
// 这是 http.ListenAndServe(addr, router) 的快捷方式
// 注意：除非发生错误，否则此方法将无限期阻塞调用协程。
func (engine *Engine) Run(addr ...string) (err error) {
	defer func() { debugPrintError(err) }()

	if engine.isUnsafeTrustedProxies() {
		debugPrint("[WARNING] You trusted all proxies, this is NOT safe. We recommend you to set a value.\n" +
			"Please check https://github.com/gin-gonic/gin/blob/master/docs/doc.md#dont-trust-all-proxies for details.")
	}
	engine.updateRouteTrees()
	address := resolveAddress(addr)
	debugPrint("Listening and serving HTTP on %s\n", address)
	server := &http.Server{ // #nosec G112
		Addr:    address,
		Handler: engine.Handler(),
	}
	err = server.ListenAndServe()
	return
}

// RunTLS 将路由器绑定到 http.Server 并开始监听和处理HTTPS（安全）请求。
// 这是 http.ListenAndServeTLS(addr, certFile, keyFile, router) 的快捷方式
// 注意：除非发生错误，否则此方法将无限期阻塞调用协程。
func (engine *Engine) RunTLS(addr, certFile, keyFile string) (err error) {
	debugPrint("Listening and serving HTTPS on %s\n", addr)
	defer func() { debugPrintError(err) }()

	if engine.isUnsafeTrustedProxies() {
		debugPrint("[WARNING] You trusted all proxies, this is NOT safe. We recommend you to set a value.\n" +
			"Please check https://github.com/gin-gonic/gin/blob/master/docs/doc.md#dont-trust-all-proxies for details.")
	}

	server := &http.Server{ // #nosec G112
		Addr:    addr,
		Handler: engine.Handler(),
	}
	err = server.ListenAndServeTLS(certFile, keyFile)
	return
}

// RunUnix 将路由器绑定到 http.Server 并通过指定的 Unix 套接字（即一个文件）
// 开始监听和处理 HTTP 请求。
// 注意：除非发生错误，否则此方法将无限期阻塞调用协程。
func (engine *Engine) RunUnix(file string) (err error) {
	debugPrint("Listening and serving HTTP on unix:/%s", file)
	defer func() { debugPrintError(err) }()

	if engine.isUnsafeTrustedProxies() {
		debugPrint("[WARNING] You trusted all proxies, this is NOT safe. We recommend you to set a value.\n" +
			"Please check https://github.com/gin-gonic/gin/blob/master/docs/doc.md#dont-trust-all-proxies for details.")
	}

	listener, err := net.Listen("unix", file)
	if err != nil {
		return
	}
	defer listener.Close()
	defer os.Remove(file)

	server := &http.Server{ // #nosec G112
		Handler: engine.Handler(),
	}
	err = server.Serve(listener)
	return
}

// RunFd 将路由器绑定到 http.Server 并通过指定的文件描述符
// 开始监听和处理 HTTP 请求。
// 注意：除非发生错误，否则此方法将无限期阻塞调用协程。
func (engine *Engine) RunFd(fd int) (err error) {
	debugPrint("Listening and serving HTTP on fd@%d", fd)
	defer func() { debugPrintError(err) }()

	if engine.isUnsafeTrustedProxies() {
		debugPrint("[WARNING] You trusted all proxies, this is NOT safe. We recommend you to set a value.\n" +
			"Please check https://github.com/gin-gonic/gin/blob/master/docs/doc.md#dont-trust-all-proxies for details.")
	}

	f := os.NewFile(uintptr(fd), fmt.Sprintf("fd@%d", fd))
	defer f.Close()
	listener, err := net.FileListener(f)
	if err != nil {
		return
	}
	defer listener.Close()
	err = engine.RunListener(listener)
	return
}

// RunQUIC 将路由器绑定到 http.Server 并开始监听和处理 QUIC 请求。
// 这是 http3.ListenAndServeQUIC(addr, certFile, keyFile, router) 的快捷方式
// 注意：除非发生错误，否则此方法将无限期阻塞调用协程。
func (engine *Engine) RunQUIC(addr, certFile, keyFile string) (err error) {
	debugPrint("Listening and serving QUIC on %s\n", addr)
	defer func() { debugPrintError(err) }()

	if engine.isUnsafeTrustedProxies() {
		debugPrint("[WARNING] You trusted all proxies, this is NOT safe. We recommend you to set a value.\n" +
			"Please check https://github.com/gin-gonic/gin/blob/master/docs/doc.md#dont-trust-all-proxies for details.")
	}

	err = http3.ListenAndServeQUIC(addr, certFile, keyFile, engine.Handler())
	return
}

// RunListener 将路由器绑定到 http.Server 并通过指定的 net.Listener
// 开始监听和处理 HTTP 请求
func (engine *Engine) RunListener(listener net.Listener) (err error) {
	debugPrint("Listening and serving HTTP on listener what's bind with address@%s", listener.Addr())
	defer func() { debugPrintError(err) }()

	if engine.isUnsafeTrustedProxies() {
		debugPrint("[WARNING] You trusted all proxies, this is NOT safe. We recommend you to set a value.\n" +
			"Please check https://github.com/gin-gonic/gin/blob/master/docs/doc.md#dont-trust-all-proxies for details.")
	}

	server := &http.Server{ // #nosec G112
		Handler: engine.Handler(),
	}
	err = server.Serve(listener)
	return
}

// ServeHTTP 遵循 http.Handler 接口。
func (engine *Engine) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	engine.routeTreesUpdated.Do(func() {
		engine.updateRouteTrees()
	})

	c := engine.pool.Get().(*Context)
	c.writermem.reset(w)
	c.Request = req
	c.reset()

	engine.handleHTTPRequest(c)

	engine.pool.Put(c)
}

// HandleContext 重新进入一个已被重写的上下文。
// 可通过将 c.Request.URL.Path 设置为新目标路径来实现。
// 注意：不当使用可能导致循环处理，请谨慎使用。
func (engine *Engine) HandleContext(c *Context) {
	oldIndexValue := c.index
	oldHandlers := c.handlers
	c.reset()
	engine.handleHTTPRequest(c)

	c.index = oldIndexValue
	c.handlers = oldHandlers
}

func (engine *Engine) handleHTTPRequest(c *Context) {
	httpMethod := c.Request.Method
	rPath := c.Request.URL.Path
	unescape := false

	if engine.UseEscapedPath {
		rPath = c.Request.URL.EscapedPath()
		unescape = engine.UnescapePathValues
	} else if engine.UseRawPath && len(c.Request.URL.RawPath) > 0 {
		rPath = c.Request.URL.RawPath
		unescape = engine.UnescapePathValues
	}

	if engine.RemoveExtraSlash {
		rPath = cleanPath(rPath)
	}

	// 查找指定 HTTP 方法对应的路由树根节点
	t := engine.trees
	for i, tl := 0, len(t); i < tl; i++ {
		if t[i].method != httpMethod {
			continue
		}
		root := t[i].root
		// 在路由树中查找匹配的路由
		value := root.getValue(rPath, c.params, c.skippedNodes, unescape)
		if value.params != nil {
			c.Params = *value.params
		}
		if value.handlers != nil {
			c.handlers = value.handlers
			c.fullPath = value.fullPath
			c.Next()
			c.writermem.WriteHeaderNow()
			return
		}
		if httpMethod != http.MethodConnect && rPath != "/" {
			if value.tsr && engine.RedirectTrailingSlash {
				redirectTrailingSlash(c)
				return
			}
			if engine.RedirectFixedPath && redirectFixedPath(c, root, engine.RedirectFixedPath) {
				return
			}
		}
		break
	}

	if engine.HandleMethodNotAllowed && len(t) > 0 {
		// 根据 RFC 7231 第 6.5.5 节，必须在响应中生成包含目标资源当前支持方法列表的 Allow 头部字段。
		allowed := make([]string, 0, len(t)-1)
		for _, tree := range engine.trees {
			if tree.method == httpMethod {
				continue
			}
			if value := tree.root.getValue(rPath, nil, c.skippedNodes, unescape); value.handlers != nil {
				allowed = append(allowed, tree.method)
			}
		}
		if len(allowed) > 0 {
			c.handlers = engine.allNoMethod
			c.writermem.Header().Set("Allow", strings.Join(allowed, ", "))
			serveError(c, http.StatusMethodNotAllowed, default405Body)
			return
		}
	}

	c.handlers = engine.allNoRoute
	serveError(c, http.StatusNotFound, default404Body)
}

var mimePlain = []string{MIMEPlain}

func serveError(c *Context, code int, defaultMessage []byte) {
	c.writermem.status = code
	c.Next()
	if c.writermem.Written() {
		return
	}
	if c.writermem.Status() == code {
		c.writermem.Header()["Content-Type"] = mimePlain
		_, err := c.Writer.Write(defaultMessage)
		if err != nil {
			debugPrint("cannot write message to writer during serve error: %v", err)
		}
		return
	}
	c.writermem.WriteHeaderNow()
}

func redirectTrailingSlash(c *Context) {
	req := c.Request
	p := req.URL.Path
	if prefix := path.Clean(c.Request.Header.Get("X-Forwarded-Prefix")); prefix != "." {
		prefix = sanitizePathChars(prefix)
		prefix = removeRepeatedChar(prefix, '/')

		p = prefix + "/" + req.URL.Path
	}
	req.URL.Path = p + "/"
	if length := len(p); length > 1 && p[length-1] == '/' {
		req.URL.Path = p[:length-1]
	}
	redirectRequest(c)
}

// sanitizePathChars 从路径字符串中移除不安全的字符，
// 仅保留 ASCII 字母、ASCII 数字、正斜杠和连字符。
func sanitizePathChars(s string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '/' || r == '-' {
			return r
		}
		return -1
	}, s)
}

func redirectFixedPath(c *Context, root *node, trailingSlash bool) bool {
	req := c.Request
	rPath := req.URL.Path

	if fixedPath, ok := root.findCaseInsensitivePath(cleanPath(rPath), trailingSlash); ok {
		req.URL.Path = bytesconv.BytesToString(fixedPath)
		redirectRequest(c)
		return true
	}
	return false
}

func redirectRequest(c *Context) {
	req := c.Request
	rPath := req.URL.Path
	rURL := req.URL.String()

	code := http.StatusMovedPermanently // 永久重定向，使用 GET 方法的请求
	if req.Method != http.MethodGet {
		code = http.StatusTemporaryRedirect
	}
	debugPrint("redirecting request %d: %s --> %s", code, rPath, rURL)
	http.Redirect(c.Writer, req, rURL, code)
	c.writermem.WriteHeaderNow()
}
