// Copyright 2013 Julien Schmidt. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found
// at https://github.com/julienschmidt/httprouter/blob/master/LICENSE

package gin

import (
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin/internal/bytesconv"
)

// Param 是单个 URL 参数，由键和值组成。
type Param struct {
	Key   string
	Value string
}

// Params 是 Param 切片，由路由器返回
// 该切片是有序的，第一个 URL 参数也是第一个切片值
// 因此通过索引读取值是安全的
type Params []Param

// Get 返回第一个键与给定名称匹配的 Param 的值和一个布尔值 true
// 如果未找到匹配的 Param，则返回空字符串和布尔值 false
func (ps Params) Get(name string) (string, bool) {
	for _, entry := range ps {
		if entry.Key == name {
			return entry.Value, true
		}
	}
	return "", false
}

// ByName 返回第一个键与给定名称匹配的 Param 的值。
// 如果未找到匹配的 Param，则返回空字符串。
func (ps Params) ByName(name string) (va string) {
	va, _ = ps.Get(name)
	return
}

// 为了实现按 HTTP 方法隔离路由树而设计的轻量级结构体，核心作用是将「HTTP 方法（如 GET/POST）」
// 和「该方法对应的路由前缀树根节点（*node）」绑定在一起，是engine.trees（路由树集合）的基本组成单元。
// methodTree是连接 “HTTP 方法” 和 “前缀树” 的关键桥梁
// methodTree 绑定HTTP方法与对应的路由前缀树根节点
type methodTree struct {
	//HTTP 方法标识
	// 严格区分大小写（Gin 内部统一用大写，如"GET"而非"get"）；每个methodTree对应唯一的 HTTP 方法
	method string // HTTP请求方法（GET/POST/PUT/DELETE等，全大写）

	// 前缀树根节点
	// 是你之前学的node结构体指针，该根节点是当前 HTTP 方法所有路由的 “入口”；比如 GET 方法的root节点下，挂载了所有GET /xxx的路由节点
	root *node // 该方法对应的路由前缀树的根节点（所有该方法的路由都挂在这个根节点下）
}

type methodTrees []methodTree

func (trees methodTrees) get(method string) *node {
	for _, tree := range trees {
		if tree.method == method {
			return tree.root
		}
	}
	return nil
}

func longestCommonPrefix(a, b string) int {
	i := 0
	max_ := min(len(a), len(b))
	for i < max_ && a[i] == b[i] {
		i++
	}
	return i
}

// addChild 将添加子节点，并保持通配符子节点位于末尾
func (n *node) addChild(child *node) {
	if n.wildChild && len(n.children) > 0 {
		wildcardChild := n.children[len(n.children)-1]
		n.children = append(n.children[:len(n.children)-1], child, wildcardChild)
	} else {
		n.children = append(n.children, child)
	}
}

func countParams(path string) uint16 {
	colons := strings.Count(path, ":")
	stars := strings.Count(path, "*")
	return safeUint16(colons + stars)
}

func countSections(path string) uint16 {
	return safeUint16(strings.Count(path, "/"))
}

type nodeType uint8

const (
	static nodeType = iota
	root
	param
	catchAll
)

// 这是 Gin 实现高性能路由匹配的核心数据结构，本质是前缀树（Radix Tree / 基数树） 的节点
// 所有路由规则最终都会被构建成一棵前缀树，node就是这棵树的最小单元，用于快速匹配 HTTP 请求的路径
// HTTP 路由的本质是 “请求方法 + 路径” 到 “处理器函数” 的映射（比如GET /user/:id → userHandler）。如果用普通的 map 存储（key = 路径，value = 处理器），无法高效处理
// 带参数的路由（如/user/:id、/user/*action）
// 路由前缀匹配（如/admin/*）
// 大量路由时的匹配性能问题
// 而前缀树能把相同前缀的路由合并成一个节点，比如/user/1、/user/2、/user/profile会共享/user这个父节点，匹配时只需按路径分段（/user→/1）遍历树，时间复杂度接近 O (1)，这也是 Gin 路由性能远超普通框架的核心原因
type node struct {
	// 当前节点匹配的路径片段（不是完整路径）
	// 比如路由/user/:id，根节点path=""，子节点path="user"，孙节点path=":id"
	path string

	// 子节点的首字符拼接成的字符串
	// 比如子节点路径是"1"、"2"、"profile"，则indices="12p"；匹配时先查首字符是否在indices中，快速定位子节点，避免遍历所有子节点
	indices string

	// 是否是 “通配符子节点”
	// 若节点是:id/*action类型，其父节点的wildChild=true（标记该节点下有通配符路由）
	wildChild bool

	// 节点类型（枚举值），决定匹配规则
	// static（静态节点，如/user）、param（参数节点，如:id）、wildcard（通配符节点，如*action）
	nType nodeType

	// 节点被访问的次数
	// 访问次数越多，priority越高，匹配时会优先检查高优先级节点（比如/user/1访问 100 次，/user/2访问 10 次，匹配/user/时先查1节点）
	priority uint32

	// 子节点列表
	// 存储当前节点的下一级节点；Gin 规定：数组末尾最多 1 个参数节点（如:id），避免参数节点干扰静态节点匹配
	children []*node

	// 路由匹配到该节点时执行的处理器链
	// 比如/user/:id节点的handlers包含 “认证中间件 + userHandler”
	handlers HandlersChain

	// 当前节点对应的完整路由路径（便于调试 / 日志）
	// 比如:id节点的fullPath="/user/:id"，而非仅:id
	fullPath string
}

// 增加给定子节点的优先级，并在必要时重新排序子节点列表
// 优化路由匹配性能而设计的核心辅助函数，作用是动态提升路由节点（node）的优先级，并调整子节点的排序，
// 让访问频率更高的路由节点优先被匹配，进一步降低路由匹配的平均耗时。
func (n *node) incrementChildPrio(pos int) int {
	// 1. 找到目标子节点，优先级+1
	cs := n.children
	cs[pos].priority++
	prio := cs[pos].priority

	// 调整位置 (move to front)
	// 2. 向前遍历子节点列表，将当前节点与优先级更低的节点交换位置
	//    最终实现：优先级高的节点排在children数组前面
	newPos := pos
	for ; newPos > 0 && cs[newPos-1].priority < prio; newPos-- {
		// 交换节点位置
		cs[newPos-1], cs[newPos] = cs[newPos], cs[newPos-1]
	}

	// 构建新的索引字符字符串
	if newPos != pos {
		n.indices = n.indices[:newPos] + // 未改变的前缀，可能为空
			n.indices[pos:pos+1] + // 我们移动的索引index字符
			n.indices[newPos:pos] + n.indices[pos+1:] // 移除 'pos' 位置的字符后的剩余部分
	}

	return newPos
}

// addRoute 将具有指定处理函数（handle）的节点添加到路径中。
// 非并发安全！
func (n *node) addRoute(path string, handlers HandlersChain) {
	fullPath := path
	n.priority++

	// Empty tree
	if len(n.path) == 0 && len(n.children) == 0 {
		n.insertChild(path, fullPath, handlers)
		n.nType = root
		return
	}

	parentFullPathIndex := 0

walk:
	for {
		// 查找最长公共前缀。
		// 这也意味着公共前缀不包含 ':' 或 '*'
		// 因为现有键中不能包含这些字符。
		i := longestCommonPrefix(path, n.path)

		// Split edge
		if i < len(n.path) {
			child := node{
				path:      n.path[i:],
				wildChild: n.wildChild,
				nType:     static,
				indices:   n.indices,
				children:  n.children,
				handlers:  n.handlers,
				priority:  n.priority - 1,
				fullPath:  n.fullPath,
			}

			n.children = []*node{&child}
			// 使用 []byte 以正确处理 Unicode 字符转换，参见 #65
			n.indices = bytesconv.BytesToString([]byte{n.path[i]})
			n.path = path[:i]
			n.handlers = nil
			n.wildChild = false
			n.fullPath = fullPath[:parentFullPathIndex+i]
		}

		// 使新节点成为当前节点的子节点
		if i < len(path) {
			path = path[i:]
			c := path[0]

			// '/' after param
			if n.nType == param && c == '/' && len(n.children) == 1 {
				parentFullPathIndex += len(n.path)
				n = n.children[0]
				n.priority++
				continue walk
			}

			// 检查是否存在具有下一个路径字节的子节点
			for i, max_ := 0, len(n.indices); i < max_; i++ {
				if c == n.indices[i] {
					parentFullPathIndex += len(n.path)
					i = n.incrementChildPrio(i)
					n = n.children[i]
					continue walk
				}
			}

			// 否则插入它
			if c != ':' && c != '*' && n.nType != catchAll {
				// 使用 []byte 以正确处理 Unicode 字符转换，参见 #65
				n.indices += bytesconv.BytesToString([]byte{c})
				child := &node{
					fullPath: fullPath,
				}
				n.addChild(child)
				n.incrementChildPrio(len(n.indices) - 1)
				n = child
			} else if n.wildChild {
				// 插入通配符节点时，需检查是否与现有通配符冲突
				n = n.children[len(n.children)-1]
				n.priority++

				// 检查通配符是否匹配
				if len(path) >= len(n.path) && n.path == path[:len(n.path)] &&
					// 无法为 catchAll 节点添加子节点
					n.nType != catchAll &&
					// 检查是否存在更长的通配符，例如 :name 和 :names
					(len(n.path) >= len(path) || path[len(n.path)] == '/') {
					continue walk
				}

				// 通配符冲突
				pathSeg := path
				if n.nType != catchAll {
					pathSeg, _, _ = strings.Cut(pathSeg, "/")
				}
				prefix := fullPath[:strings.Index(fullPath, pathSeg)] + n.path
				panic("'" + pathSeg +
					"' in new path '" + fullPath +
					"' conflicts with existing wildcard '" + n.path +
					"' in existing prefix '" + prefix +
					"'")
			}

			n.insertChild(path, fullPath, handlers)
			return
		}

		// 否则将处理函数添加到当前节点
		if n.handlers != nil {
			panic("handlers are already registered for path '" + fullPath + "'")
		}
		n.handlers = handlers
		n.fullPath = fullPath
		return
	}
}

// 搜索通配符段并检查名称中的无效字符。
// 如果未找到通配符，返回 -1 作为索引。
func findWildcard(path string) (wildcard string, i int, valid bool) {
	// Find start
	escapeColon := false
	for start, c := range []byte(path) {
		if escapeColon {
			escapeColon = false
			if c == ':' {
				continue
			}
			panic("invalid escape string in path '" + path + "'")
		}
		if c == '\\' {
			escapeColon = true
			continue
		}
		// 通配符以 ':'（参数）或 '*'（全匹配）开头
		if c != ':' && c != '*' {
			continue
		}

		// 查找结束位置并检查无效字符
		valid = true
		for end, c := range []byte(path[start+1:]) {
			switch c {
			case '/':
				return path[start : start+1+end], start, valid
			case ':', '*':
				valid = false
			}
		}
		return path[start:], start, valid
	}
	return "", -1, false
}

func (n *node) insertChild(path string, fullPath string, handlers HandlersChain) {
	for {
		// 查找直到第一个通配符的前缀
		wildcard, i, valid := findWildcard(path)
		if i < 0 { // 未找到通配符
			break
		}

		// 通配符名称只能包含一个 ':' 或 '*' 字符
		if !valid {
			panic("only one wildcard per path segment is allowed, has: '" +
				wildcard + "' in path '" + fullPath + "'")
		}

		// 检查通配符是否有名称
		if len(wildcard) < 2 {
			panic("wildcards must be named with a non-empty name in path '" + fullPath + "'")
		}

		if wildcard[0] == ':' { // param
			if i > 0 {
				// 在当前通配符前插入前缀
				n.path = path[:i]
				path = path[i:]
			}

			child := &node{
				nType:    param,
				path:     wildcard,
				fullPath: fullPath,
			}
			n.addChild(child)
			n.wildChild = true
			n = child
			n.priority++

			// 如果路径不是以通配符结尾，则
			// 会有另一个以 '/' 开始的子路径
			if len(wildcard) < len(path) {
				path = path[len(wildcard):]

				child := &node{
					priority: 1,
					fullPath: fullPath,
				}
				n.addChild(child)
				n = child
				continue
			}

			// 否则我们已完成。将处理函数插入新的叶节点
			n.handlers = handlers
			return
		}

		// catchAll
		if i+len(wildcard) != len(path) {
			panic("catch-all routes are only allowed at the end of the path in path '" + fullPath + "'")
		}

		if len(n.path) > 0 && n.path[len(n.path)-1] == '/' {
			pathSeg := ""
			if len(n.children) != 0 {
				pathSeg, _, _ = strings.Cut(n.children[0].path, "/")
			}
			panic("catch-all wildcard '" + path +
				"' in new path '" + fullPath +
				"' conflicts with existing path segment '" + pathSeg +
				"' in existing prefix '" + n.path + pathSeg +
				"'")
		}

		// 当前 '/' 的固定宽度为 1
		i--
		if i < 0 || path[i] != '/' {
			panic("no / before catch-all in path '" + fullPath + "'")
		}

		n.path = path[:i]

		// 第一个节点：空路径的全匹配节点
		child := &node{
			wildChild: true,
			nType:     catchAll,
			fullPath:  fullPath,
		}

		n.addChild(child)
		n.indices = "/"
		n = child
		n.priority++

		// 第二个节点：保存变量的节点
		child = &node{
			path:     path[i:],
			nType:    catchAll,
			handlers: handlers,
			priority: 1,
			fullPath: fullPath,
		}
		n.children = []*node{child}

		return
	}

	// 如果未找到通配符，则直接插入路径和处理函数
	n.path = path
	n.handlers = handlers
	n.fullPath = fullPath
}

// nodeValue 保存 (*Node).getValue 方法的返回值
type nodeValue struct {
	handlers HandlersChain
	params   *Params
	tsr      bool
	fullPath string
}

type skippedNode struct {
	path        string
	node        *node
	paramsCount int16
}

// 返回与给定路径（键）注册的处理函数。通配符的值会被保存到映射中。
// 如果未找到处理函数，但存在与给定路径
// 多一个（或少一个）尾部斜杠的处理函数，则会建议进行尾部斜杠重定向。
func (n *node) getValue(path string, params *Params, skippedNodes *[]skippedNode, unescape bool) (value nodeValue) {
	var globalParamsCount int16

walk: // 遍历树的外层循环
	for {
		prefix := n.path
		if len(path) > len(prefix) {
			if path[:len(prefix)] == prefix {
				path = path[len(prefix):]

				// 首先通过匹配索引尝试所有非通配符子节点
				idxc := path[0]
				for i, c := range []byte(n.indices) {
					if c == idxc {
						//  strings.HasPrefix(n.children[len(n.children)-1].path, ":") == n.wildChild
						if n.wildChild {
							index := len(*skippedNodes)
							*skippedNodes = (*skippedNodes)[:index+1]
							(*skippedNodes)[index] = skippedNode{
								path: prefix + path,
								node: &node{
									path:      n.path,
									wildChild: n.wildChild,
									nType:     n.nType,
									priority:  n.priority,
									children:  n.children,
									handlers:  n.handlers,
									fullPath:  n.fullPath,
								},
								paramsCount: globalParamsCount,
							}
						}

						n = n.children[i]
						continue walk
					}
				}

				if !n.wildChild {
					// 如果循环结束时的路径不等于 '/' 且当前节点没有子节点
					// 当前节点需要回退到最后有效的跳转节点
					if path != "/" {
						for length := len(*skippedNodes); length > 0; length-- {
							skippedNode := (*skippedNodes)[length-1]
							*skippedNodes = (*skippedNodes)[:length-1]
							if strings.HasSuffix(skippedNode.path, path) {
								path = skippedNode.path
								n = skippedNode.node
								if value.params != nil {
									*value.params = (*value.params)[:skippedNode.paramsCount]
								}
								globalParamsCount = skippedNode.paramsCount
								continue walk
							}
						}
					}

					// 未找到匹配项。
					// 如果存在该路径对应的叶节点，
					// 可以建议重定向到不带尾部斜杠的相同 URL。
					value.tsr = path == "/" && n.handlers != nil
					return value
				}

				// 处理通配符子节点，它始终位于数组末尾
				n = n.children[len(n.children)-1]
				globalParamsCount++

				switch n.nType {
				case param:
					// 修复截断的参数
					// tree_test.go  line: 204

					// 查找参数结束位置（'/' 或路径结束）
					end := 0
					for end < len(path) && path[end] != '/' {
						end++
					}

					// 保存参数值
					if params != nil {
						// 如有必要，预分配容量
						if cap(*params) < int(globalParamsCount) {
							newParams := make(Params, len(*params), globalParamsCount)
							copy(newParams, *params)
							*params = newParams
						}

						if value.params == nil {
							value.params = params
						}
						// 在预分配的容量内扩展切片
						i := len(*value.params)
						*value.params = (*value.params)[:i+1]
						val := path[:end]
						if unescape {
							if v, err := url.QueryUnescape(val); err == nil {
								val = v
							}
						}
						(*value.params)[i] = Param{
							Key:   n.path[1:],
							Value: val,
						}
					}

					// 我们需要深入处理！
					if end < len(path) {
						if len(n.children) > 0 {
							path = path[end:]
							n = n.children[0]
							continue walk
						}

						// ... but we can't
						value.tsr = len(path) == end+1
						return value
					}

					if value.handlers = n.handlers; value.handlers != nil {
						value.fullPath = n.fullPath
						return value
					}
					if len(n.children) == 1 {
						// 未找到处理函数。检查该路径
						// 是否存在带尾部斜杠的处理函数，以建议尾部斜杠重定向
						n = n.children[0]
						value.tsr = (n.path == "/" && n.handlers != nil) || (n.path == "" && n.indices == "/")
					}
					return value

				case catchAll:
					// 保存参数值
					if params != nil {
						// 如有必要，预分配容量
						if cap(*params) < int(globalParamsCount) {
							newParams := make(Params, len(*params), globalParamsCount)
							copy(newParams, *params)
							*params = newParams
						}

						if value.params == nil {
							value.params = params
						}
						// 在预分配的容量内扩展切片
						i := len(*value.params)
						*value.params = (*value.params)[:i+1]
						val := path
						if unescape {
							if v, err := url.QueryUnescape(path); err == nil {
								val = v
							}
						}
						(*value.params)[i] = Param{
							Key:   n.path[2:],
							Value: val,
						}
					}

					value.handlers = n.handlers
					value.fullPath = n.fullPath
					return value

				default:
					panic("invalid node type")
				}
			}
		}

		if path == prefix {
			// 如果当前路径不等于 '/' 且节点没有注册处理函数，且最近匹配的节点有子节点
			// 当前节点需要回退到最后有效的跳转节点
			if n.handlers == nil && path != "/" {
				for length := len(*skippedNodes); length > 0; length-- {
					skippedNode := (*skippedNodes)[length-1]
					*skippedNodes = (*skippedNodes)[:length-1]
					if strings.HasSuffix(skippedNode.path, path) {
						path = skippedNode.path
						n = skippedNode.node
						if value.params != nil {
							*value.params = (*value.params)[:skippedNode.paramsCount]
						}
						globalParamsCount = skippedNode.paramsCount
						continue walk
					}
				}
				//	n = latestNode.children[len(latestNode.children)-1]
			}
			// 我们应该已到达包含处理函数的节点。
			// 检查此节点是否已注册处理函数。
			if value.handlers = n.handlers; value.handlers != nil {
				value.fullPath = n.fullPath
				return value
			}

			// 如果此路由没有处理函数，但此路由
			// 有一个通配符子节点，则该路径
			// 必定存在一个带额外尾部斜杠的处理函数
			if path == "/" && n.wildChild && n.nType != root {
				value.tsr = true
				return value
			}

			if path == "/" && n.nType == static {
				value.tsr = true
				return value
			}

			// 未找到处理函数。检查该路径
			// 是否存在带尾部斜杠的处理函数，以建议尾部斜杠重定向
			for i, c := range []byte(n.indices) {
				if c == '/' {
					n = n.children[i]
					value.tsr = (len(n.path) == 1 && n.handlers != nil) ||
						(n.nType == catchAll && n.children[0].handlers != nil)
					return value
				}
			}

			return value
		}

		// 未找到任何内容。如果存在该路径对应的叶节点，
		// 可以建议重定向到带额外尾部斜杠的相同 URL
		value.tsr = path == "/" ||
			(len(prefix) == len(path)+1 && prefix[len(path)] == '/' &&
				path == prefix[:len(prefix)-1] && n.handlers != nil)

		// 回退到最后有效的跳转节点
		if !value.tsr && path != "/" {
			for length := len(*skippedNodes); length > 0; length-- {
				skippedNode := (*skippedNodes)[length-1]
				*skippedNodes = (*skippedNodes)[:length-1]
				if strings.HasSuffix(skippedNode.path, path) {
					path = skippedNode.path
					n = skippedNode.node
					if value.params != nil {
						*value.params = (*value.params)[:skippedNode.paramsCount]
					}
					globalParamsCount = skippedNode.paramsCount
					continue walk
				}
			}
		}

		return value
	}
}

// 对给定路径执行不区分大小写的查找，并尝试找到处理程序。
// 它还可选地修复尾部斜杠。
// 它返回大小写修正后的路径和一个表示查找是否成功的布尔值。
func (n *node) findCaseInsensitivePath(path string, fixTrailingSlash bool) ([]byte, bool) {
	const stackBufSize = 128

	// 在常见情况下使用栈上的静态大小缓冲区。
	// 如果路径过长，则改用堆上分配的缓冲区。
	buf := make([]byte, 0, stackBufSize)
	if length := len(path) + 1; length > stackBufSize {
		buf = make([]byte, 0, length)
	}

	ciPath := n.findCaseInsensitivePathRec(
		path,
		buf,       // 为新路径预分配足够的内存
		[4]byte{}, // 空 rune 缓冲区
		fixTrailingSlash,
	)

	return ciPath, ciPath != nil
}

// 将数组中的字节向左移动 n 个字节
func shiftNRuneBytes(rb [4]byte, n int) [4]byte {
	switch n {
	case 0:
		return rb
	case 1:
		return [4]byte{rb[1], rb[2], rb[3], 0}
	case 2:
		return [4]byte{rb[2], rb[3]}
	case 3:
		return [4]byte{rb[3]}
	default:
		return [4]byte{}
	}
}

// n.findCaseInsensitivePath 使用的递归不区分大小写查找函数
func (n *node) findCaseInsensitivePathRec(path string, ciPath []byte, rb [4]byte, fixTrailingSlash bool) []byte {
	npLen := len(n.path)

walk: // 遍历树的外层循环
	for len(path) >= npLen && (npLen == 0 || strings.EqualFold(path[1:npLen], n.path[1:])) {
		// Add common prefix to result
		oldPath := path
		path = path[npLen:]
		ciPath = append(ciPath, n.path...)

		if len(path) == 0 {
			// 我们应该已到达包含处理函数的节点。
			// 检查此节点是否已注册处理函数。
			if n.handlers != nil {
				return ciPath
			}

			// 未找到处理函数。
			// 尝试通过添加尾部斜杠来修复路径
			if fixTrailingSlash {
				for i, c := range []byte(n.indices) {
					if c == '/' {
						n = n.children[i]
						if (len(n.path) == 1 && n.handlers != nil) ||
							(n.nType == catchAll && n.children[0].handlers != nil) {
							return append(ciPath, '/')
						}
						return nil
					}
				}
			}
			return nil
		}

		// 如果此节点没有通配符（参数或全匹配）子节点，
		// 我们可以直接查找下一个子节点并继续向下遍历树
		if !n.wildChild {
			// 跳过已处理的 rune 字节
			rb = shiftNRuneBytes(rb, npLen)

			if rb[0] != 0 {
				// 旧的 rune 尚未处理完
				idxc := rb[0]
				for i, c := range []byte(n.indices) {
					if c == idxc {
						// 继续处理子节点
						n = n.children[i]
						npLen = len(n.path)
						continue walk
					}
				}
			} else {
				// 处理新的 rune
				var rv rune

				// 查找 rune 的起始位置。
				// Rune 最长可达 4 字节，
				// -4 必定是另一个 rune。
				var off int
				for max_ := min(npLen, 3); off < max_; off++ {
					if i := npLen - off; utf8.RuneStart(oldPath[i]) {
						// read rune from cached path
						rv, _ = utf8.DecodeRuneInString(oldPath[i:])
						break
					}
				}

				// 计算当前 rune 的小写字节
				lo := unicode.ToLower(rv)
				utf8.EncodeRune(rb[:], lo)

				// 跳过已处理的字节
				rb = shiftNRuneBytes(rb, off)

				idxc := rb[0]
				for i, c := range []byte(n.indices) {
					// 小写匹配成功
					if c == idxc {
						// 必须使用递归方法，因为大写字节和小写字节
						// 都可能作为索引存在
						if out := n.children[i].findCaseInsensitivePathRec(
							path, ciPath, rb, fixTrailingSlash,
						); out != nil {
							return out
						}
						break
					}
				}

				// 如果未找到匹配项，对于大写的 rune
				// 也是如此（如果它不同）
				if up := unicode.ToUpper(rv); up != lo {
					utf8.EncodeRune(rb[:], up)
					rb = shiftNRuneBytes(rb, off)

					idxc := rb[0]
					for i, c := range []byte(n.indices) {
						// 大写匹配成功
						if c == idxc {
							// 继续处理子节点
							n = n.children[i]
							npLen = len(n.path)
							continue walk
						}
					}
				}
			}

			// 未找到任何内容。如果存在该路径对应的叶节点，
			// 可以建议重定向到不带尾部斜杠的相同 URL
			if fixTrailingSlash && path == "/" && n.handlers != nil {
				return ciPath
			}
			return nil
		}

		n = n.children[0]
		switch n.nType {
		case param:
			// 查找参数结束位置（'/' 或路径结束）
			end := 0
			for end < len(path) && path[end] != '/' {
				end++
			}

			// 将参数值添加到不区分大小写的路径中
			ciPath = append(ciPath, path[:end]...)

			// We need to go deeper!
			if end < len(path) {
				if len(n.children) > 0 {
					// 继续处理子节点
					n = n.children[0]
					npLen = len(n.path)
					path = path[end:]
					continue
				}

				// ... but we can't
				if fixTrailingSlash && len(path) == end+1 {
					return ciPath
				}
				return nil
			}

			if n.handlers != nil {
				return ciPath
			}

			if fixTrailingSlash && len(n.children) == 1 {
				// 未找到处理函数。检查该路径
				// 是否存在带尾部斜杠的处理函数
				n = n.children[0]
				if n.path == "/" && n.handlers != nil {
					return append(ciPath, '/')
				}
			}

			return nil

		case catchAll:
			return append(ciPath, path...)

		default:
			panic("invalid node type")
		}
	}

	// 未找到任何内容。
	// 尝试通过添加/移除尾部斜杠来修复路径
	if fixTrailingSlash {
		if path == "/" {
			return ciPath
		}
		if len(path)+1 == npLen && n.path[len(path)] == '/' &&
			strings.EqualFold(path[1:], n.path[1:len(path)]) && n.handlers != nil {
			return append(ciPath, n.path...)
		}
	}
	return nil
}
