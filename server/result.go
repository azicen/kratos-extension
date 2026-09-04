package server

// Result 统一业务返回结构，可用于非 HTTP 场景或内部封装
type Result[T any] struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data *T     `json:"data,omitempty"`
}

// IsSuccess 判断是否成功
func (r Result[T]) IsSuccess() bool {
	return r.Code == 200
}

// SuccessResult 创建成功结果
func SuccessResult[T any](msg string, data *T) Result[T] {
	if msg == "" {
		msg = "操作成功"
	}
	return Result[T]{
		Code: 200,
		Msg:  msg,
		Data: data,
	}
}

// FailResult 创建失败结果
func FailResult[T any](code int, msg string, data *T) Result[T] {
	if code == 0 {
		code = 400
	}
	if msg == "" {
		msg = "操作异常"
	}
	return Result[T]{
		Code: code,
		Msg:  msg,
		Data: data,
	}
}
