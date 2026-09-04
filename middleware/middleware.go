package middleware

import (
	"sort"

	"github.com/go-kratos/kratos/v3/middleware"
)

// 优先级常量
const (
	PriorityTracing  = 0
	PriorityRecovery = 10
	PriorityAuth     = 50
	PriorityValidate = 90
)

// HTTPMiddleware HTTP 中间件封装
type HTTPMiddleware struct {
	Handler  middleware.Middleware
	Priority int
}

// NewHTTPMiddleware 创建 HTTP 中间件封装
func NewHTTPMiddleware(h middleware.Middleware, priority int) HTTPMiddleware {
	return HTTPMiddleware{Handler: h, Priority: priority}
}

// GRPCMiddleware gRPC 中间件封装
type GRPCMiddleware struct {
	Handler  middleware.Middleware
	Priority int
}

// NewGRPCMiddleware 创建 gRPC 中间件封装
func NewGRPCMiddleware(h middleware.Middleware, priority int) GRPCMiddleware {
	return GRPCMiddleware{Handler: h, Priority: priority}
}

// SortHTTPMiddlewares 按优先级升序排序 HTTP 中间件
func SortHTTPMiddlewares(mws []HTTPMiddleware) {
	sort.SliceStable(mws, func(i, j int) bool {
		return mws[i].Priority < mws[j].Priority
	})
}

// SortGRPCMiddlewares 按优先级升序排序 gRPC 中间件
func SortGRPCMiddlewares(mws []GRPCMiddleware) {
	sort.SliceStable(mws, func(i, j int) bool {
		return mws[i].Priority < mws[j].Priority
	})
}
