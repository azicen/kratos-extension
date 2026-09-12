//go:build go1.27

// Package sonic 使用 ByteDance Sonic 实现 Kratos JSON 编解码器。
//
// 普通 Go 值直接交给 Sonic。实现 proto.Message 且描述符语法为 Proto3 的值，
// 使用基于 Descriptor 的 Proto3 JSON 编解码路径；其他 Protobuf 语法按普通 Go 值处理。
//
// Proto3 JSON 表示遵循 ProtoJSON 规则，但 uuid.UUID 是明确的扩展：JSON 使用规范 UUID
// 字符串，Protobuf 二进制仍保持 16 字节。生产实现不会调用 protojson。
//
// 编解码器使用名称 "json" 注册。不要在同一应用中同时以副作用方式导入本包和本模块的
// encoding/json 包，因为 Kratos 会静默覆盖同名编解码器。
package sonic
