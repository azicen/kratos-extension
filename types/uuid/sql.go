package uuid

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

var (
	_ driver.Valuer = (*UUID)(nil)
	_ sql.Scanner   = (*UUID)(nil)
)

// Value 实现 driver.Valuer 接口，将 UUID 以标准 36 字符字符串形式写入数据库
// 未设置（空值）时写入 NULL
func (x *UUID) Value() (driver.Value, error) {
	if x == nil || len(x.Data) == 0 {
		return nil, nil
	}
	if len(x.Data) != len(uuid.UUID{}) {
		return nil, errors.New("uuid: value must be exactly 16 bytes")
	}
	return uuid.UUID(x.Data).String(), nil
}

// Scan 实现 sql.Scanner 接口，从数据库读取 UUID
// 支持 string、[]byte（16 字节二进制形式或 36 字节字符串形式）、[16]byte 以及 nil
func (x *UUID) Scan(src any) error {
	if x == nil {
		return errors.New("uuid: Scan called on nil UUID")
	}

	switch v := src.(type) {
	case nil:
		x.Data = nil
	case string:
		return x.scanString(v)
	case []byte:
		switch len(v) {
		case 0:
			x.Data = nil
			return nil
		case len(uuid.UUID{}):
			x.Data = append(x.Data[:0], v...)
			return nil
		default:
			return x.scanString(string(v))
		}
	case uuid.UUID:
		x.Data = append(x.Data[:0], v[:]...)
	case [16]byte:
		x.Data = append(x.Data[:0], v[:]...)
	default:
		return fmt.Errorf("uuid: cannot scan type %T", src)
	}
	return nil
}

func (x *UUID) scanString(s string) error {
	if s == "" {
		x.Data = nil
		return nil
	}
	uid, err := uuid.Parse(s)
	if err != nil {
		return fmt.Errorf("uuid: cannot scan %q as UUID: %w", s, err)
	}
	x.Data = append(x.Data[:0], uid[:]...)
	return nil
}
