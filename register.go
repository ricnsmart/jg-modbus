package modbus

type Register interface {
	Name() string

	// Start 返回在字节流中的起始地址
	Start() int

	// Len 返回字节长度
	Len() int

	// Addr 返回参数信息地址
	Addr() []byte
}

type Readable interface {
	// Decode 为了适应单个字段，两个字节代表2个参数的情况和
	// 单个字段，每个比特代表不同参数的情况
	// 所以使用了result去获取decode结果
	Decode(data []byte, results map[string]any)
}

type ReadRegister interface {
	Register
	Readable
}

type ReadRegisters []ReadRegister

func (p ReadRegisters) Len() int {
	var r int
	for _, w := range p {
		r = r + w.Len()
	}
	return r
}

// Decode 不支持跳过预留字段
func (p ReadRegisters) Decode(data []byte) map[string]any {
	m := make(map[string]any)
	for _, register := range p {
		start := register.Start()
		end := start + register.Len()
		register.Decode(data[start:end], m)
	}
	return m
}

func (p ReadRegisters) NewReadRegisters(function uint8, address []byte) ([][]byte, error) {
	cmd := make([][]byte, 0)
	for _, field := range p {
		f := Frame{Function: function, Address: address}
		f.SetData(crcSetData(field.Addr()))
		cmd = append(cmd, f.Bytes())
	}
	return cmd, nil
}
