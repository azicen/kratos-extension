package server

import (
	"github.com/azicen/kratos-extension/middleware"

	"github.com/go-kratos/kratos/contrib/otel/v3/tracing"
	kmiddleware "github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/middleware/recovery"
	kvalidate "github.com/go-kratos/kratos/v3/middleware/validate"
	"github.com/go-kratos/kratos/v3/transport/http"
	"go.uber.org/fx"
)

// HTTPServerParams HTTP 服务器依赖参数
type HTTPServerParams struct {
	fx.In

	Middlewares []middleware.HTTPMiddleware `group:"http-middlewares"`
	Options     []http.ServerOption         `group:"http-server-options"`
}

// NewHTTPServer 创建 HTTP 服务器
func NewHTTPServer(params HTTPServerParams) *http.Server {
	mws := append([]middleware.HTTPMiddleware{
		middleware.NewHTTPMiddleware(tracing.Server(), middleware.PriorityTracing),
		middleware.NewHTTPMiddleware(recovery.Recovery(), middleware.PriorityRecovery),
		middleware.NewHTTPMiddleware(kvalidate.Validator(), middleware.PriorityValidate),
	}, params.Middlewares...)
	middleware.SortHTTPMiddlewares(mws)

	handlers := make([]kmiddleware.Middleware, 0, len(mws))
	for _, mw := range mws {
		if mw.Handler != nil {
			handlers = append(handlers, mw.Handler)
		}
	}

	var opts = []http.ServerOption{
		http.Middleware(handlers...),
		http.ResponseEncoder(responseEncoder),
		http.ErrorEncoder(errorEncoder),
	}
	opts = append(opts, params.Options...)
	srv := http.NewServer(opts...)
	return srv
}
