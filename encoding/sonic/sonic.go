//go:build go1.27

// Package sonic 提供基于 Sonic 的 JSON 编解码器，并为 Proto3 消息实现 ProtoJSON 语义
package sonic

import (
	"errors"
	"reflect"

	jsonsonic "github.com/bytedance/sonic"
	"github.com/go-kratos/kratos/v3/encoding"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// Name 是向 Kratos 注册的编解码器名称
const Name = "json"

// Resolver 解析 google.protobuf.Any 中 type_url 对应的消息类型
type Resolver interface {
	protoregistry.MessageTypeResolver
}

// Options 配置 Proto3 JSON 的编码与解码行为
type Options struct {
	// Resolver 解析 Any 内嵌消息类型；为空时使用 protoregistry.GlobalTypes
	Resolver Resolver
	// DiscardUnknown 控制解码时是否忽略未知字段和未知枚举名称
	DiscardUnknown bool
	// EmitUnpopulated 控制编码时是否输出未设置字段；支持 presence 的未设置字段输出 null
	EmitUnpopulated bool
	// EmitDefaultValues 控制编码时是否输出默认值标量、空列表和空 Map，但不破坏 presence
	EmitDefaultValues bool
	// UseProtoNames 控制编码字段名是否使用 Proto 原始名称，而不是 JSON 名称
	UseProtoNames bool
	// UseEnumNumbers 控制枚举是否使用数值编码；默认使用枚举名称
	UseEnumNumbers bool
	// RecursionLimit 限制 JSON 容器和 Proto 消息的最大递归深度；非正数使用默认值 100
	RecursionLimit int
}

// codec 实现 Kratos JSON 编解码接口，并保存不可变的运行配置
type codec struct {
	options  Options
	json     jsonsonic.API
	metadata *descriptorMetadataCache
}

// init 注册默认的 Sonic JSON 编解码器
func init() {
	Register()
}

// Register 将默认 Sonic JSON 编解码器注册到 Kratos 全局注册表
func Register() {
	encoding.RegisterCodec(NewCodec())
}

// NewCodec 创建基于 Sonic 的 JSON 编解码器
// 传入多个 Options 时仅使用最后一个，避免产生难以判断的配置合并顺序
func NewCodec(options ...Options) encoding.Codec {
	configuration := Options{
		Resolver:       protoregistry.GlobalTypes,
		RecursionLimit: 100,
	}
	if len(options) > 0 {
		configuration = options[len(options)-1]
		if configuration.Resolver == nil {
			configuration.Resolver = protoregistry.GlobalTypes
		}
		if configuration.RecursionLimit <= 0 {
			configuration.RecursionLimit = 100
		}
	}
	return codec{options: configuration, json: jsonsonic.ConfigStd, metadata: new(descriptorMetadataCache)}
}

// Marshal 将输入值编码为 JSON
// Proto3 消息使用基于 Descriptor 的 ProtoJSON 兼容路径，其他值直接交给 Sonic
func (c codec) Marshal(value any) ([]byte, error) {
	if message, ok := value.(proto.Message); ok {
		descriptor := message.ProtoReflect().Descriptor()
		if descriptor != nil && descriptor.Syntax() == protoreflect.Proto3 {
			if isNil(message) {
				return []byte("{}"), nil
			}
			return marshalProto3(message.ProtoReflect(), c.options, c.json, c.metadata)
		}
	}
	return c.json.Marshal(value)
}

// Unmarshal 将 JSON 解码到目标值
// Proto3 消息会在解码前清空；双指针目标只在完整解码成功后写回新实例
func (c codec) Unmarshal(data []byte, target any) error {
	if len(data) == 0 {
		return nil
	}
	message, commit, ok, err := proto3Target(target)
	if err != nil {
		return err
	}
	if ok {
		node, err := parseJSON(data, c.options.RecursionLimit)
		if err != nil {
			return err
		}
		proto.Reset(message)
		if err := unmarshalProto3(node, message.ProtoReflect(), c.options, c.json, c.metadata); err != nil {
			return err
		}
		if commit != nil {
			commit()
		}
		return nil
	}
	return c.json.Unmarshal(data, target)
}

// Name 返回 Kratos 使用的静态内容子类型名称
func (codec) Name() string {
	return Name
}

// proto3Target 判断目标是否应走 Proto3 反射解码路径
// 对 nil 双指针先创建临时消息，并通过 commit 延迟到解码成功后再写回调用方
func proto3Target(target any) (proto.Message, func(), bool, error) {
	if message, ok := target.(proto.Message); ok {
		if isNil(message) {
			return nil, nil, false, errors.New("sonic: cannot unmarshal into a nil Proto3 message")
		}
		if isProto3Message(message) {
			return message, nil, true, nil
		}
		return nil, nil, false, nil
	}

	value := reflect.ValueOf(target)
	if value.Kind() != reflect.Pointer || value.IsNil() || value.Elem().Kind() != reflect.Pointer {
		return nil, nil, false, nil
	}
	if !value.Elem().IsNil() {
		message, ok := value.Elem().Interface().(proto.Message)
		if !ok || !isProto3Message(message) {
			return nil, nil, false, nil
		}
		return message, nil, true, nil
	}

	elementType := value.Elem().Type().Elem()
	allocated := reflect.New(elementType)
	message, ok := allocated.Interface().(proto.Message)
	if !ok || !isProto3Message(message) {
		return nil, nil, false, nil
	}
	return message, func() { value.Elem().Set(allocated) }, true, nil
}

// isProto3Message 判断消息描述符是否声明为 Proto3 语法
func isProto3Message(message proto.Message) bool {
	if message == nil {
		return false
	}
	descriptor := message.ProtoReflect().Descriptor()
	return descriptor != nil && descriptor.Syntax() == protoreflect.Proto3
}

// isNil 判断接口中保存的值是否为 nil 指针
func isNil(value any) bool {
	reflected := reflect.ValueOf(value)
	return reflected.Kind() == reflect.Pointer && reflected.IsNil()
}
