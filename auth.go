// Copyright 2014 Manu Martinez-Almeida. All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package gin

import (
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin/internal/bytesconv"
)

// AuthUserKey 是基本认证中用于用户凭证的 Cookie 名称。
const AuthUserKey = "user"

// AuthProxyUserKey 是代理基本认证中用于代理用户凭证的 Cookie 名称。
const AuthProxyUserKey = "proxy_user"

// Accounts 定义了授权登录用户/密码列表的键值对。
type Accounts map[string]string

type authPair struct {
	value string
	user  string
}

type authPairs []authPair

// searchCredential 是 Gin 处理 HTTP Basic Auth 认证时的核心方法，
// 作用是：校验客户端传入的认证值（authValue）是否和服务端预配置的凭证（pair.value）一致。
func (a authPairs) searchCredential(authValue string) (string, bool) {
	if authValue == "" {
		return "", false
	}
	for _, pair := range a {
		// 核心校验：对比服务端凭证和客户端传入的凭证是否一致
		// Gin 自己封装了 bytesconv.StringToBytes 而不是直接用 []byte(s)，是为了避免字符串转字节切片时的内存额外分配（Gin 追求极致性能，自定义的转换函数做了优化）。
		// subtle.ConstantTimeCompare 是 Go 标准库 crypto/subtle 包提供的「常量时间比较函数」，作用是以固定时间对比两个字节切片是否相等，返回 1 表示相等，0 表示不相等。
		//（1）为什么不用普通的 == 对比？
		// 如果用普通的 a == b 对比字符串 / 字节切片，会存在「计时攻击（Timing Attack）」的安全风险：
		// 普通对比是「逐字节比较，发现不相等就立刻返回」，比如对比 123456 和 123789，会在第 4 个字节（4 vs 7）时停止对比。
		// 攻击者可以通过多次请求，统计服务器返回的耗时差异，逐步猜出正确的凭证（比如先试第一位是 1，耗时略长→正确；再试第二位是 2，耗时略长→正确，直到完全猜出）。
		//（2）常量时间对比的优势
		// subtle.ConstantTimeCompare 不管两个字节切片是否相等，都会遍历完所有字节再返回结果，耗时是固定的：
		// 比如对比 123456 和 123789，会把 6 个字节全部对比完，再返回 0；
		// 对比 123456 和 123456，也会遍历 6 个字节，再返回 1；
		//攻击者无法通过耗时差异猜出凭证，从根本上杜绝了计时攻击。
		// 所以这行代码的逻辑是：如果服务端的凭证和客户端传入的凭证完全一致，就返回用户名和 true（认证通过）。
		// 总结这行代码的两个核心设计思路：
		// 安全优先：用「常量时间对比」替代普通对比，防止计时攻击（HTTP Basic Auth 本身是明文传输，Gin 从校验环节尽可能降低安全风险）；
		// 性能优化：用自定义的 bytesconv.StringToBytes 做类型转换，减少内存分配，符合 Gin 极致高性能的设计理念。
		// 总结
		// 这行代码的核心作用是安全地校验 HTTP Basic Auth 凭证是否匹配；
		// subtle.ConstantTimeCompare 是关键：通过「常量时间对比」避免计时攻击，返回 1 表示凭证一致；
		// bytesconv.StringToBytes 是 Gin 做的性能优化，把字符串转字节切片供对比函数使用。
		//简单来说，这行代码就是「用安全且高性能的方式，确认客户端输入的密码和服务端存的密码是不是同一个」。
		if subtle.ConstantTimeCompare(bytesconv.StringToBytes(pair.value), bytesconv.StringToBytes(authValue)) == 1 {
			return pair.user, true
		}
	}
	return "", false
}

// BasicAuthForRealm 返回一个基础的 HTTP 授权中间件。其参数为一个 map[string]string，其中键是用户名，值是密码，以及一个 Realm 名称。
// 如果 Realm 为空，默认将使用 "Authorization Required"。
// (see http://tools.ietf.org/html/rfc2617#section-1.2)
func BasicAuthForRealm(accounts Accounts, realm string) HandlerFunc {
	if realm == "" {
		realm = "Authorization Required"
	}
	// strconv.Quote(realm)对传入的 realm字符串进行安全地引用（转义），确保它在 HTTP 头中是格式正确且安全的
	realm = "Basic realm=" + strconv.Quote(realm)
	pairs := processAccounts(accounts)
	return func(c *Context) {
		// 在允许的凭据切片中搜索用户。
		user, found := pairs.searchCredential(c.requestHeader("Authorization"))
		if !found {
			// 凭据不匹配，我们将返回 401 状态码并终止后续处理程序的执行链。
			c.Header("WWW-Authenticate", realm)
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// 用户凭据验证通过，将用户的 ID 设置到当前上下文中键为 AuthUserKey 的位置。
		// 之后可以通过 c.MustGet(gin.AuthUserKey) 来读取此用户 ID。
		c.Set(AuthUserKey, user)
	}
}

// BasicAuth 返回一个基础的 HTTP 授权中间件。其参数为一个 map[string]string，其中键是用户名，值是密码。
func BasicAuth(accounts Accounts) HandlerFunc {
	return BasicAuthForRealm(accounts, "")
}

func processAccounts(accounts Accounts) authPairs {
	length := len(accounts)
	assert1(length > 0, "Empty list of authorized credentials")
	pairs := make(authPairs, 0, length)
	for user, password := range accounts {
		assert1(user != "", "User can not be empty")
		value := authorizationHeader(user, password)
		pairs = append(pairs, authPair{
			value: value,
			user:  user,
		})
	}
	return pairs
}

func authorizationHeader(user, password string) string {
	base := user + ":" + password
	return "Basic " + base64.StdEncoding.EncodeToString(bytesconv.StringToBytes(base))
}

// BasicAuthForProxy 返回一个基础的 HTTP 代理授权中间件。
// 如果 Realm 为空，默认将使用 "Proxy Authorization Required"。
func BasicAuthForProxy(accounts Accounts, realm string) HandlerFunc {
	if realm == "" {
		realm = "Proxy Authorization Required"
	}
	realm = "Basic realm=" + strconv.Quote(realm)
	pairs := processAccounts(accounts)
	return func(c *Context) {
		proxyUser, found := pairs.searchCredential(c.requestHeader("Proxy-Authorization"))
		if !found {
			// 凭据不匹配，我们将返回 407 状态码并终止后续处理程序的执行链。
			c.Header("Proxy-Authenticate", realm)
			c.AbortWithStatus(http.StatusProxyAuthRequired)
			return
		}
		// 代理用户凭据验证通过，将代理用户的 ID 设置到当前上下文中键为 AuthProxyUserKey 的位置。
		// 之后可以通过 c.MustGet(gin.AuthProxyUserKey) 来读取此代理用户 ID。
		c.Set(AuthProxyUserKey, proxyUser)
	}
}
