package fancontrol

import (
	"fmt"
	"io"
)

const (
	wireVarint  = 0
	wireFixed64 = 1
	wireBytes   = 2
	wireFixed32 = 5
)

func appendDelimited(dst []byte, message []byte) []byte {
	dst = appendVarint(dst, uint64(len(message)))
	return append(dst, message...)
}

func appendVarintField(dst []byte, fieldNumber int, value uint64) []byte {
	dst = appendKey(dst, fieldNumber, wireVarint)
	return appendVarint(dst, value)
}

func appendBoolField(dst []byte, fieldNumber int, value bool) []byte {
	if value {
		return appendVarintField(dst, fieldNumber, 1)
	}
	return appendVarintField(dst, fieldNumber, 0)
}

func appendStringField(dst []byte, fieldNumber int, value string) []byte {
	return appendBytesField(dst, fieldNumber, []byte(value))
}

func appendBytesField(dst []byte, fieldNumber int, value []byte) []byte {
	dst = appendKey(dst, fieldNumber, wireBytes)
	dst = appendVarint(dst, uint64(len(value)))
	return append(dst, value...)
}

func appendKey(dst []byte, fieldNumber int, wireType int) []byte {
	return appendVarint(dst, uint64(fieldNumber<<3|wireType))
}

func appendVarint(dst []byte, value uint64) []byte {
	for value >= 0x80 {
		dst = append(dst, byte(value)|0x80)
		value >>= 7
	}
	return append(dst, byte(value))
}

func consumeDelimited(data []byte) ([]byte, int, error) {
	size, n, err := consumeVarint(data)
	if err != nil {
		return nil, 0, err
	}
	if uint64(len(data)-n) < size {
		return nil, 0, io.ErrUnexpectedEOF
	}
	end := n + int(size)
	return data[n:end], end, nil
}

func consumeKey(data []byte) (int, int, int, error) {
	key, n, err := consumeVarint(data)
	if err != nil {
		return 0, 0, 0, err
	}
	field := int(key >> 3)
	wireType := int(key & 0x7)
	if field <= 0 {
		return 0, 0, 0, fmt.Errorf("invalid protobuf field number %d", field)
	}
	return field, wireType, n, nil
}

func consumeBytes(data []byte) ([]byte, int, error) {
	size, n, err := consumeVarint(data)
	if err != nil {
		return nil, 0, err
	}
	if uint64(len(data)-n) < size {
		return nil, 0, io.ErrUnexpectedEOF
	}
	end := n + int(size)
	return data[n:end], end, nil
}

func consumeVarint(data []byte) (uint64, int, error) {
	var value uint64
	for i, b := range data {
		if i == 10 {
			return 0, 0, fmt.Errorf("protobuf varint overflows uint64")
		}
		value |= uint64(b&0x7f) << (7 * i)
		if b < 0x80 {
			return value, i + 1, nil
		}
	}
	return 0, 0, io.ErrUnexpectedEOF
}

func skipField(wireType int, data []byte) (int, error) {
	switch wireType {
	case wireVarint:
		_, n, err := consumeVarint(data)
		return n, err
	case wireFixed64:
		if len(data) < 8 {
			return 0, io.ErrUnexpectedEOF
		}
		return 8, nil
	case wireBytes:
		_, n, err := consumeBytes(data)
		return n, err
	case wireFixed32:
		if len(data) < 4 {
			return 0, io.ErrUnexpectedEOF
		}
		return 4, nil
	default:
		return 0, fmt.Errorf("unsupported protobuf wire type %d", wireType)
	}
}
