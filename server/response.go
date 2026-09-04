package server

import (
	"encoding/json"
	"errors"
	nethttp "net/http"

	"github.com/go-kratos/kratos/v3/encoding"
	kratoserrors "github.com/go-kratos/kratos/v3/errors"
)

// baseResponse 统一 HTTP JSON 返回结构
type baseResponse struct {
	Code    int             `json:"code"`
	Data    json.RawMessage `json:"data"`
	Msg     string          `json:"msg"`
	Success bool            `json:"success"`
	Reason  string          `json:"reason,omitempty"`
}

// responseEncoder 统一成功响应编码器
func responseEncoder(w nethttp.ResponseWriter, r *nethttp.Request, v interface{}) error {
	codec := encoding.GetCodec("json")
	dataBytes, err := codec.Marshal(v)
	if err != nil {
		return err
	}
	reply := &baseResponse{
		Code:    200,
		Data:    dataBytes,
		Msg:     "操作成功",
		Success: true,
	}
	data, err := json.Marshal(reply)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(data)
	return err
}

// errorEncoder 统一错误响应编码器
func errorEncoder(w nethttp.ResponseWriter, r *nethttp.Request, err error) {
	message := extractMessageFromError(err)
	reply := &baseResponse{
		Code:    500,
		Data:    json.RawMessage("null"),
		Msg:     message,
		Success: false,
		Reason:  extractReasonFromError(err),
	}
	data, _ := json.Marshal(reply)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(nethttp.StatusOK)
	w.Write(data)
}

// extractReasonFromError 从 Kratos 错误中提取稳定的错误原因。
func extractReasonFromError(err error) string {
	var kratosErr *kratoserrors.Error
	if errors.As(err, &kratosErr) {
		return kratosErr.Reason
	}
	return "INTERNAL_ERROR"
}

// extractMessageFromError 从错误中提取消息
func extractMessageFromError(err error) string {
	// 尝试从 JSON 序列化后的错误中获取 message 字段
	marshal, err2 := json.Marshal(err)
	if err2 != nil {
		return err.Error()
	}
	var em struct {
		Message string `json:"message"`
	}
	if e := json.Unmarshal(marshal, &em); e != nil || em.Message == "" {
		return err.Error()
	}
	return em.Message
}
