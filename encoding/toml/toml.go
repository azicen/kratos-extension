package toml

import (
	"github.com/pelletier/go-toml/v2"
	"github.com/go-kratos/kratos/v2/encoding"
)

const Name = "toml"

func init() {
	encoding.RegisterCodec(codec{})
}

type codec struct{}

func (c codec) Marshal(v interface{}) ([]byte, error) {
	return toml.Marshal(v)
}

func (c codec) Unmarshal(data []byte, v interface{}) error {
	return toml.Unmarshal(data, v)
}

func (c codec) Name() string {
	return Name
}
