package server

import (
	"github.com/azicen/kratos-extension/middleware"

	"github.com/go-kratos/kratos/contrib/otel/v3/tracing"
	kmiddleware "github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/middleware/recovery"
	kvalidate "github.com/go-kratos/kratos/v3/middleware/validate"
	"github.com/go-kratos/kratos/v3/transport/grpc"
	"go.uber.org/fx"
)

// GRPCServerParams gRPC 服务器依赖参数
type GRPCServerParams struct {
	fx.In

	Middlewares []middleware.GRPCMiddleware `group:"grpc-middlewares"`
	Options     []grpc.ServerOption         `group:"grpc-server-options"`
}

// NewGRPCServer 创建 gRPC 服务器
func NewGRPCServer(params GRPCServerParams) *grpc.Server {
	mws := append([]middleware.GRPCMiddleware{
		middleware.NewGRPCMiddleware(tracing.Server(), middleware.PriorityTracing),
		middleware.NewGRPCMiddleware(recovery.Recovery(), middleware.PriorityRecovery),
		middleware.NewGRPCMiddleware(kvalidate.Validator(), middleware.PriorityValidate),
	}, params.Middlewares...)
	middleware.SortGRPCMiddlewares(mws)

	handlers := make([]kmiddleware.Middleware, 0, len(mws))
	for _, mw := range mws {
		if mw.Handler != nil {
			handlers = append(handlers, mw.Handler)
		}
	}

	var opts = []grpc.ServerOption{
		grpc.Middleware(handlers...),
	}
	opts = append(opts, params.Options...)
	srv := grpc.NewServer(opts...)
	return srv
}
