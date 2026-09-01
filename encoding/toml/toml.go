package toml

import (
	"github.com/go-kratos/kratos/v3/encoding"
	"github.com/pelletier/go-toml/v2"
)

const Name = "toml"

func init() {
	encoding.RegisterCodec(codec{})
}

type codec struct{}

func (c codec) Marshal(v any) ([]byte, error) {
	return toml.Marshal(v)
}

func (c codec) Unmarshal(data []byte, v any) error {
	return toml.Unmarshal(data, v)
}

func (c codec) Name() string {
	return Name
}
