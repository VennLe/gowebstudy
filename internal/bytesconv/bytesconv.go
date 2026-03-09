// Copyright 2023 Gin Core Team. All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package bytesconv

import (
	"unsafe"
)

// StringToBytes 将字符串转换为字节切片，无需进行内存分配。
// For more details, see https://github.com/golang/go/issues/53003#issuecomment-1140276077.
func StringToBytes(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

// BytesToString 将字节切片转换为字符串，无需进行内存分配。
// For more details, see https://github.com/golang/go/issues/53003#issuecomment-1140276077.
func BytesToString(b []byte) string {
	return unsafe.String(unsafe.SliceData(b), len(b))
}
