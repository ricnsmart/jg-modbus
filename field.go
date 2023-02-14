package modbus

import "fmt"

// Field 京硅协议中没有寄存器地址的消息体
type Field interface {
	// Name 字段名称
	Name() string
	// Start 字段起始位置
	Start() int
	// Len 字段长度，单位：字节
	Len() int
}

type Decoder interface {
	Decode(data []byte, values map[string]any)
}

type DecodableField interface {
	Field
	Decoder
}

// DecodableFields
// len不一定等于fields中字段的长度
// 因此需要len字段来确定字段组整体长度
type DecodableFields struct {
	len int

	fields []DecodableField
}

func NewDecodableFields(len int, fields []DecodableField) *DecodableFields {
	return &DecodableFields{
		len:    len,
		fields: fields,
	}
}

func (p *DecodableFields) Len() int {
	return p.len
}

func (p *DecodableFields) Decode(data []byte, values map[string]any) {
	for _, fd := range p.fields {
		start := fd.Start()
		end := start + fd.Len()
		fd.Decode(data[start:end], values)
	}
}

type Encoder interface {
	Encode(params map[string]interface{}, dst []byte) error
}

type EncodableField interface {
	Encoder
	Field
}

func Len[T Field](fs []T) int {
	var l int
	for _, f := range fs {
		l = l + f.Len()
	}
	return l
}

func Encode[T EncodableField](params map[string]interface{}, fs []T) ([]byte, error) {
	result := make([]byte, Len(fs))
	for _, field := range fs {
		start := field.Start() - 0
		end := start + field.Len()
		if err := field.Encode(params, result[start:end]); err != nil {
			return nil, fmt.Errorf("decode error: %v; field: %v, start: %v,end: %v", err, field.Name(), start, end)
		}
	}
	return result, nil
}

func NewFrameWithFields[T EncodableField](function uint8, params map[string]interface{}, fs []T) (*Frame, error) {
	data, err := Encode(params, fs)
	if err != nil {
		return nil, err
	}
	f := &Frame{Function: function}
	f.SetData(data)
	return f, nil
}
