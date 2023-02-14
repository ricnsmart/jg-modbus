package modbus

import (
	"fmt"
)

/*
使用可变帧长。数据传输顺序为低位在前，高位在后；低字节在前，高字节在后。

1字节	6字节	1字节	1字节	1字节	1字节  4字节		N字节	1字节	1字节
68H		开关地址	68H		控制		命令码	长度L  标志码		数据信息	校验CS	16H

终端地址：6个字节，选址范围为000000000001H～FFFFFFFFFFFFH，其中FFFFFFFFFFFFH为广播地址，000000000000H为无效地址。
控制字：1个字节，定义数据传送方向及数据帧种类。
长度L：用户数据的字节总长度。
校验和CS：1个字节，是控制字、终端地址、命令码、用户数据的字节的八位位组算术和，不考虑溢出位，即：CS＝（控制字+终端地址+命令码+用户数据）MOD 256。

注册包、心跳包
88 xx xx xx xx xx xx(网关地址) 88 00 16
*/

// Frame Address: 开关终端地址地址	Function：命令代码	Data：数据
type Frame struct {
	Address  []byte
	Function uint8
	Data     []byte
}

// NewFrame converts a packet to a JG frame.
func NewFrame(packet []byte) (*Frame, error) {

	pLen := len(packet)

	csExpect := packet[pLen-2]
	csCalc := crcModbus(packet[0 : pLen-2])

	if csExpect != csCalc {
		return nil, fmt.Errorf("jg: frame error: CheckSum (expected 0x%x, got 0x%x)", csExpect, csCalc)
	}

	frame := &Frame{
		Address:  packet[1:7],
		Function: packet[8],
		Data:     crcGetData(packet[10 : pLen-2]),
	}

	return frame, nil
}

func NewFrameByData(address, data []byte, f uint8) *Frame {

	return &Frame{
		Address:  address,
		Function: f,
		Data:     crcSetData(data),
	}
}

// Bytes returns the byte stream based on the Frame fields
func (frame *Frame) Bytes() []byte {
	b := make([]byte, 0)

	// 添加定界符
	b = append(b, 0x68)

	b = append(b, frame.Address...)

	b = append(b, 0x68)

	b = append(b, frame.Function)

	b = append(b, byte(len(frame.Data)))

	if frame.Data != nil {
		b = append(b, frame.Data...)
	}

	// Calculate the CheckSum.

	cs := crcModbus(b)

	b = append(b, cs)
	b = append(b, 0x16)
	return b
}

// GetFunction returns the function code.
func (frame *Frame) GetFunction() uint8 {
	return frame.Function
}

// GetData returns the Frame Data byte field.
func (frame *Frame) GetData() []byte {
	return frame.Data
}

// SetData sets the Frame Data byte field and updates the frame length
// accordingly.
func (frame *Frame) SetData(data []byte) {
	frame.Data = data
}
