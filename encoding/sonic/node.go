//go:build go1.27

package sonic

import (
	"encoding/json"
	"errors"
	"unicode/utf8"

	jsonsonic "github.com/bytedance/sonic"
	"github.com/bytedance/sonic/ast"
)

// nodeKind 标识解析后 JSON 节点的语义类型
type nodeKind uint8

const (
	nodeNull nodeKind = iota
	nodeBool
	nodeString
	nodeNumber
	nodeObject
	nodeArray
)

// objectMember 保存 JSON 对象成员，并保留输入顺序和重复名称
type objectMember struct {
	// name 是未经字段归一化的 JSON 对象成员名称
	name string
	// value 是成员对应的 JSON 值
	value *jsonNode
}

// jsonNode 表示保留数字原文和对象成员顺序的轻量 JSON 语法节点
type jsonNode struct {
	// kind 指示当前节点应读取哪个值字段
	kind nodeKind
	// boolean 保存布尔节点的值
	boolean bool
	// text 保存字符串值或 JSON 数字原文
	text string
	// object 保存对象成员，允许后续检测重复字段
	object []objectMember
	// array 保存数组元素并维持输入顺序
	array []*jsonNode
}

// nodeFrame 记录正在构建的对象或数组以及对象成员的待写入键
type nodeFrame struct {
	// node 是当前容器节点
	node *jsonNode
	// pendingKey 是对象已读取但尚未关联值的成员名
	pendingKey string
	// hasKey 区分空字符串键和不存在待写入键两种状态
	hasKey bool
}

// nodeVisitor 将 Sonic 的顺序回调组装为可供 Proto 语义层消费的 JSON 节点树
type nodeVisitor struct {
	// root 是本次输入唯一的根 JSON 值
	root *jsonNode
	// stack 保存尚未结束的容器，并用于限制嵌套深度
	stack []nodeFrame
	// recursionLimit 是允许的最大 JSON 容器嵌套层数
	recursionLimit int
}

// parseJSON 校验并解析单个 JSON 值，同时保留重复字段、成员顺序和数字原文
func parseJSON(data []byte, recursionLimit int) (*jsonNode, error) {
	if !utf8.Valid(data) {
		return nil, errors.New("sonic: invalid UTF-8 in JSON")
	}
	if !jsonsonic.Valid(data) {
		return nil, errors.New("sonic: invalid JSON")
	}
	visitor := &nodeVisitor{recursionLimit: recursionLimit}
	if err := ast.Preorder(string(data), visitor, &ast.VisitorOptions{OnlyNumber: true}); err != nil {
		return nil, err
	}
	if visitor.root == nil || len(visitor.stack) != 0 {
		return nil, errors.New("sonic: incomplete JSON value")
	}
	return visitor.root, nil
}

// OnNull 接收 JSON null 节点
func (v *nodeVisitor) OnNull() error {
	return v.add(&jsonNode{kind: nodeNull})
}

// OnBool 接收 JSON 布尔节点
func (v *nodeVisitor) OnBool(value bool) error {
	return v.add(&jsonNode{kind: nodeBool, boolean: value})
}

// OnString 校验并接收 JSON 字符串节点
func (v *nodeVisitor) OnString(value string) error {
	if !utf8.ValidString(value) {
		return errors.New("sonic: invalid UTF-8 string")
	}
	return v.add(&jsonNode{kind: nodeString, text: value})
}

// OnInt64 接收可表示为 int64 的 JSON 数字，并保留数字原文
func (v *nodeVisitor) OnInt64(_ int64, number json.Number) error {
	return v.add(&jsonNode{kind: nodeNumber, text: number.String()})
}

// OnFloat64 接收浮点 JSON 数字，并保留数字原文以便按 Proto 字段类型重新解析
func (v *nodeVisitor) OnFloat64(_ float64, number json.Number) error {
	return v.add(&jsonNode{kind: nodeNumber, text: number.String()})
}

// OnObjectBegin 开始构建 JSON 对象，并在分配前检查递归深度
func (v *nodeVisitor) OnObjectBegin(capacity int) error {
	if len(v.stack)+1 > v.recursionLimit {
		return errors.New("sonic: exceeded max recursion depth")
	}
	node := &jsonNode{kind: nodeObject, object: make([]objectMember, 0, capacity)}
	if err := v.add(node); err != nil {
		return err
	}
	v.stack = append(v.stack, nodeFrame{node: node})
	return nil
}

// OnObjectKey 记录下一个对象值对应的成员名称
func (v *nodeVisitor) OnObjectKey(key string) error {
	if !utf8.ValidString(key) {
		return errors.New("sonic: invalid UTF-8 object name")
	}
	if len(v.stack) == 0 || v.stack[len(v.stack)-1].node.kind != nodeObject {
		return errors.New("sonic: object key outside object")
	}
	frame := &v.stack[len(v.stack)-1]
	if frame.hasKey {
		return errors.New("sonic: object key without value")
	}
	frame.pendingKey = key
	frame.hasKey = true
	return nil
}

// OnObjectEnd 结束当前 JSON 对象
func (v *nodeVisitor) OnObjectEnd() error {
	return v.pop(nodeObject)
}

// OnArrayBegin 开始构建 JSON 数组，并在分配前检查递归深度
func (v *nodeVisitor) OnArrayBegin(capacity int) error {
	if len(v.stack)+1 > v.recursionLimit {
		return errors.New("sonic: exceeded max recursion depth")
	}
	node := &jsonNode{kind: nodeArray, array: make([]*jsonNode, 0, capacity)}
	if err := v.add(node); err != nil {
		return err
	}
	v.stack = append(v.stack, nodeFrame{node: node})
	return nil
}

// OnArrayEnd 结束当前 JSON 数组
func (v *nodeVisitor) OnArrayEnd() error {
	return v.pop(nodeArray)
}

// add 将节点设置为根值，或追加到当前对象、数组容器
func (v *nodeVisitor) add(node *jsonNode) error {
	if len(v.stack) == 0 {
		if v.root != nil {
			return errors.New("sonic: multiple root values")
		}
		v.root = node
		return nil
	}
	frame := &v.stack[len(v.stack)-1]
	switch frame.node.kind {
	case nodeArray:
		frame.node.array = append(frame.node.array, node)
	case nodeObject:
		if !frame.hasKey {
			return errors.New("sonic: object value without key")
		}
		frame.node.object = append(frame.node.object, objectMember{name: frame.pendingKey, value: node})
		frame.pendingKey = ""
		frame.hasKey = false
	default:
		return errors.New("sonic: value has invalid parent")
	}
	return nil
}

// pop 校验并弹出指定类型的当前容器
func (v *nodeVisitor) pop(kind nodeKind) error {
	if len(v.stack) == 0 {
		return errors.New("sonic: unexpected container end")
	}
	frame := v.stack[len(v.stack)-1]
	if frame.node.kind != kind || frame.hasKey {
		return errors.New("sonic: mismatched container end")
	}
	v.stack = v.stack[:len(v.stack)-1]
	return nil
}
