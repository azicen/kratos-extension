package log

import (
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
)

// modulePath 缓存当前二进制的 main module path，在 init 时获取一次。
var modulePath string

func init() {
	if info, ok := debug.ReadBuildInfo(); ok {
		modulePath = info.Main.Path
	}
}

// callerFromPC 从程序计数器提取调用者文件路径和行号
// 输出从 module 根目录开始的相对路径
// 当无法确定 module 路径时，fallback 到完整路径
func callerFromPC(pc uintptr) string {
	fs := runtime.CallersFrames([]uintptr{pc})
	f, _ := fs.Next()

	file := f.File
	line := f.Line

	if modulePath != "" {
		pkgPath := extractPkgPath(f.Function)
		if strings.HasPrefix(pkgPath, modulePath+"/") {
			// 包在 module 内的相对路径
			relPkg := pkgPath[len(modulePath)+1:]
			// file 的目录应以 relPkg 结尾
			dir := file
			if i := strings.LastIndexByte(file, '/'); i >= 0 {
				dir = file[:i]
			}
			if strings.HasSuffix(dir, relPkg) {
				trimLen := len(dir) - len(relPkg)
				return file[trimLen:] + ":" + strconv.Itoa(line)
			}
		}
	}

	// fallback: 完整路径
	return file + ":" + strconv.Itoa(line)
}

// extractPkgPath 从完整的函数名中提取 package path
func extractPkgPath(funcName string) string {
	lastSlash := strings.LastIndexByte(funcName, '/')
	if lastSlash < 0 {
		lastSlash = 0
	}
	if dotIdx := strings.IndexByte(funcName[lastSlash:], '.'); dotIdx >= 0 {
		return funcName[:lastSlash+dotIdx]
	}
	return funcName
}
