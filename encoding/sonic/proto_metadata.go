//go:build go1.27

package sonic

import (
	"sync"
	"sync/atomic"

	jsonsonic "github.com/bytedance/sonic"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const maxCachedDescriptors = 1024

// descriptorMetadataCache 按 Descriptor 身份缓存编解码共享的只读元数据
// 缓存不保存消息实例或字段值，因此可由同一 codec 的并发调用共享
//
// Descriptor 的运行时实现由 protobuf 提供且可比较，可直接作为 sync.Map 键；
// 这同时避免仅按 FullName 缓存时不同动态 schema 互相污染。
type descriptorMetadataCache struct {
	messages     sync.Map
	enums        sync.Map
	messageCount atomic.Uint32
	enumCount    atomic.Uint32
}

// messageMetadata 保存字段编码计划、解码名称索引和真实 oneof 数量
type messageMetadata struct {
	fields           []fieldMetadata
	byName           map[string]int
	oneofCount       int
	encodedNamesOnce sync.Once
	encodedNamesErr  error
}

// fieldMetadata 保存字段 Descriptor、稳定属性及预编码后的两种 JSON 成员名
type fieldMetadata struct {
	field       protoreflect.FieldDescriptor
	kind        protoreflect.Kind
	jsonName    []byte
	protoName   []byte
	oneofIndex  int
	hasOneof    bool
	isList      bool
	isMap       bool
	hasPresence bool
	mapKey      protoreflect.FieldDescriptor
	mapValue    protoreflect.FieldDescriptor
}

// enumEncodingMetadata 保存枚举数值到预编码 JSON 名称的只读映射
type enumEncodingMetadata struct {
	values map[protoreflect.EnumNumber][]byte
}

// message 获取消息元数据，并在首次访问时构建后缓存
func (c *descriptorMetadataCache) message(descriptor protoreflect.MessageDescriptor) *messageMetadata {
	if cached, ok := c.messages.Load(descriptor); ok {
		return cached.(*messageMetadata)
	}

	fields := descriptor.Fields()
	oneofIndexes := make(map[protoreflect.OneofDescriptor]int, descriptor.Oneofs().Len())
	for index := 0; index < descriptor.Oneofs().Len(); index++ {
		oneof := descriptor.Oneofs().Get(index)
		if !oneof.IsSynthetic() {
			oneofIndexes[oneof] = len(oneofIndexes)
		}
	}
	metadata := &messageMetadata{
		fields:     make([]fieldMetadata, fields.Len()),
		byName:     make(map[string]int, fields.Len()*2),
		oneofCount: len(oneofIndexes),
	}
	for index := 0; index < fields.Len(); index++ {
		field := fields.Get(index)
		oneofIndex := -1
		if oneof := field.ContainingOneof(); oneof != nil && !oneof.IsSynthetic() {
			oneofIndex = oneofIndexes[oneof]
		}
		entry := fieldMetadata{
			field:       field,
			kind:        field.Kind(),
			oneofIndex:  oneofIndex,
			hasOneof:    field.ContainingOneof() != nil,
			isList:      field.IsList(),
			isMap:       field.IsMap(),
			hasPresence: field.HasPresence(),
		}
		if entry.isMap {
			entry.mapKey = field.MapKey()
			entry.mapValue = field.MapValue()
		}
		metadata.fields[index] = entry
		metadata.byName[field.JSONName()] = index
		metadata.byName[field.TextName()] = index
	}
	if !reserveCacheSlot(&c.messageCount) {
		return metadata
	}
	actual, loaded := c.messages.LoadOrStore(descriptor, metadata)
	if loaded {
		c.messageCount.Add(^uint32(0))
	}
	return actual.(*messageMetadata)
}

// encodeNames 在编码路径首次使用时生成带引号的字段名称
func (m *messageMetadata) encodeNames(jsonAPI jsonsonic.API) error {
	m.encodedNamesOnce.Do(func() {
		for index := range m.fields {
			field := m.fields[index].field
			m.fields[index].jsonName, m.encodedNamesErr = jsonAPI.Marshal(field.JSONName())
			if m.encodedNamesErr != nil {
				return
			}
			m.fields[index].protoName, m.encodedNamesErr = jsonAPI.Marshal(field.TextName())
			if m.encodedNamesErr != nil {
				return
			}
		}
	})
	return m.encodedNamesErr
}

// enum 获取枚举名称元数据，并在首次访问时构建后缓存
func (c *descriptorMetadataCache) enum(descriptor protoreflect.EnumDescriptor, jsonAPI jsonsonic.API) (*enumEncodingMetadata, error) {
	if cached, ok := c.enums.Load(descriptor); ok {
		return cached.(*enumEncodingMetadata), nil
	}

	values := descriptor.Values()
	metadata := &enumEncodingMetadata{values: make(map[protoreflect.EnumNumber][]byte, values.Len())}
	for index := 0; index < values.Len(); index++ {
		value := values.Get(index)
		encoded, err := jsonAPI.Marshal(string(value.Name()))
		if err != nil {
			return nil, err
		}
		if _, exists := metadata.values[value.Number()]; !exists {
			metadata.values[value.Number()] = encoded
		}
	}
	if !reserveCacheSlot(&c.enumCount) {
		return metadata, nil
	}
	actual, loaded := c.enums.LoadOrStore(descriptor, metadata)
	if loaded {
		c.enumCount.Add(^uint32(0))
	}
	return actual.(*enumEncodingMetadata), nil
}

// reserveCacheSlot 在固定上限内原子预留一个缓存槽位
func reserveCacheSlot(count *atomic.Uint32) bool {
	for {
		current := count.Load()
		if current >= maxCachedDescriptors {
			return false
		}
		if count.CompareAndSwap(current, current+1) {
			return true
		}
	}
}
