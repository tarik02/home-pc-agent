//go:build windows

package fancontrol

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

const (
	fanControlPipeName = `\\.\pipe\FanControl`

	grpcStatusOK = 0

	commandStatusOK          = 1
	commandStatusExit        = 2
	commandStatusRejected    = 3
	commandStatusUnsupported = 4
)

type fanControlConfigs struct {
	Configs       []string
	CurrentConfig string
	ConfigFolder  string
}

type transportPayloadInfo struct {
	Size         int
	InSamePacket bool
}

type transportTrailers struct {
	StatusCode   int
	StatusDetail string
}

type transportMessage struct {
	PayloadInfo *transportPayloadInfo
	Trailers    *transportTrailers
}

type fanControlIPCConn struct {
	handle    windows.Handle
	closeOnce sync.Once
}

type fanControlRPC struct {
	conn *fanControlIPCConn
}

func newFanControlRPC(conn *fanControlIPCConn) fanControlRPC {
	return fanControlRPC{conn: conn}
}

func openFanControlIPC(ctx context.Context) (*fanControlIPCConn, error) {
	name, err := windows.UTF16PtrFromString(fanControlPipeName)
	if err != nil {
		return nil, err
	}

	retry := 100 * time.Millisecond
	for {
		handle, err := windows.CreateFile(
			name,
			windows.GENERIC_READ|windows.GENERIC_WRITE,
			0,
			nil,
			windows.OPEN_EXISTING,
			windows.FILE_ATTRIBUTE_NORMAL,
			0,
		)
		if err == nil {
			mode := uint32(windows.PIPE_READMODE_MESSAGE)
			if err := windows.SetNamedPipeHandleState(handle, &mode, nil, nil); err != nil {
				_ = windows.CloseHandle(handle)
				return nil, fmt.Errorf("set FanControl IPC pipe read mode: %w", err)
			}
			return &fanControlIPCConn{handle: handle}, nil
		}

		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
			return nil, fmt.Errorf("%w: %s", errFanControlIPCUnavailable, fanControlPipeName)
		}
		if !errors.Is(err, windows.ERROR_PIPE_BUSY) {
			return nil, fmt.Errorf("open FanControl IPC pipe %s: %w", fanControlPipeName, err)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(retry):
			if retry < time.Second {
				retry *= 2
			}
		}
	}
}

func (c *fanControlIPCConn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		err = windows.CloseHandle(c.handle)
	})
	return err
}

func (c *fanControlIPCConn) CallUnary(ctx context.Context, method string, payload []byte) ([]byte, error) {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = c.Close()
		case <-done:
		}
	}()
	defer close(done)

	packet := encodeUnaryRequest(method, payload)
	var written uint32
	if err := windows.WriteFile(c.handle, packet, &written, nil); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("write FanControl IPC request: %w", err)
	}
	if int(written) != len(packet) {
		return nil, io.ErrShortWrite
	}

	response, err := readUnaryResponse(ctx, c.handle)
	if err != nil {
		return nil, err
	}
	return response, nil
}

func (r fanControlRPC) ListConfigs(ctx context.Context) (fanControlConfigs, error) {
	payload, err := r.conn.CallUnary(ctx, "/FanControlRPC/ListAvailableConfigs", nil)
	if err != nil {
		return fanControlConfigs{}, err
	}
	return parseListAvailableConfigsReply(payload)
}

func (r fanControlRPC) LoadConfig(ctx context.Context, configName string) error {
	request := appendStringField(nil, 1, configName)
	payload, err := r.conn.CallUnary(ctx, "/FanControlRPC/LoadConfig", request)
	if err != nil {
		return err
	}
	status, user, err := parseCommandReply(payload)
	if err != nil {
		return err
	}
	if status != commandStatusOK {
		return fmt.Errorf("FanControl returned %s for user %q", commandStatusName(status), user)
	}
	return nil
}

func encodeUnaryRequest(method string, payload []byte) []byte {
	requestInit := appendStringField(nil, 1, method)
	requestInit = appendVarintField(requestInit, 3, 1)

	packet := appendDelimited(nil, appendBytesField(nil, 1, requestInit))
	packet = appendDelimited(packet, appendBytesField(nil, 2, nil))

	payloadInfo := appendVarintField(nil, 1, uint64(len(payload)))
	payloadInfo = appendBoolField(payloadInfo, 2, true)
	packet = appendDelimited(packet, appendBytesField(nil, 3, payloadInfo))
	packet = append(packet, payload...)
	return packet
}

func readUnaryResponse(ctx context.Context, handle windows.Handle) ([]byte, error) {
	var payload []byte
	for {
		packet, err := readPipeMessage(handle)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("read FanControl IPC response: %w", err)
		}

		offset := 0
		for offset < len(packet) {
			message, read, err := consumeDelimited(packet[offset:])
			if err != nil {
				return nil, fmt.Errorf("parse FanControl IPC transport message: %w", err)
			}
			offset += read

			transport, err := parseTransportMessage(message)
			if err != nil {
				return nil, err
			}
			if transport.PayloadInfo != nil {
				info := transport.PayloadInfo
				if info.Size < 0 {
					return nil, fmt.Errorf("invalid FanControl IPC payload size %d", info.Size)
				}
				if info.InSamePacket {
					if len(packet)-offset < info.Size {
						return nil, io.ErrUnexpectedEOF
					}
					payload = append(payload, packet[offset:offset+info.Size]...)
					offset += info.Size
				} else {
					extra, err := readPipeBytes(handle, info.Size)
					if err != nil {
						return nil, err
					}
					payload = append(payload, extra...)
				}
			}
			if transport.Trailers != nil {
				if transport.Trailers.StatusCode != grpcStatusOK {
					return nil, fmt.Errorf("FanControl IPC transport returned status %d: %s",
						transport.Trailers.StatusCode,
						transport.Trailers.StatusDetail,
					)
				}
				return payload, nil
			}
		}
	}
}

func readPipeMessage(handle windows.Handle) ([]byte, error) {
	buffer := make([]byte, 16*1024)
	var message []byte
	for {
		var read uint32
		err := windows.ReadFile(handle, buffer, &read, nil)
		if read > 0 {
			message = append(message, buffer[:read]...)
		}
		if err == nil {
			return message, nil
		}
		if errors.Is(err, windows.ERROR_MORE_DATA) {
			continue
		}
		return nil, err
	}
}

func readPipeBytes(handle windows.Handle, size int) ([]byte, error) {
	payload := make([]byte, size)
	readTotal := 0
	for readTotal < size {
		var read uint32
		err := windows.ReadFile(handle, payload[readTotal:], &read, nil)
		readTotal += int(read)
		if err != nil && !errors.Is(err, windows.ERROR_MORE_DATA) {
			return nil, fmt.Errorf("read FanControl IPC payload: %w", err)
		}
	}
	return payload, nil
}

func parseTransportMessage(data []byte) (transportMessage, error) {
	var message transportMessage
	for len(data) > 0 {
		field, wireType, n, err := consumeKey(data)
		if err != nil {
			return transportMessage{}, err
		}
		data = data[n:]

		switch field {
		case 2:
			skipped, err := skipField(wireType, data)
			if err != nil {
				return transportMessage{}, err
			}
			data = data[skipped:]
		case 3:
			if wireType != wireBytes {
				return transportMessage{}, fmt.Errorf("payload_info has wire type %d", wireType)
			}
			value, read, err := consumeBytes(data)
			if err != nil {
				return transportMessage{}, err
			}
			info, err := parsePayloadInfo(value)
			if err != nil {
				return transportMessage{}, err
			}
			message.PayloadInfo = &info
			data = data[read:]
		case 5:
			if wireType != wireBytes {
				return transportMessage{}, fmt.Errorf("trailers has wire type %d", wireType)
			}
			value, read, err := consumeBytes(data)
			if err != nil {
				return transportMessage{}, err
			}
			trailers, err := parseTrailers(value)
			if err != nil {
				return transportMessage{}, err
			}
			message.Trailers = &trailers
			data = data[read:]
		default:
			skipped, err := skipField(wireType, data)
			if err != nil {
				return transportMessage{}, err
			}
			data = data[skipped:]
		}
	}
	return message, nil
}

func parsePayloadInfo(data []byte) (transportPayloadInfo, error) {
	var info transportPayloadInfo
	for len(data) > 0 {
		field, wireType, n, err := consumeKey(data)
		if err != nil {
			return transportPayloadInfo{}, err
		}
		data = data[n:]
		switch field {
		case 1:
			if wireType != wireVarint {
				return transportPayloadInfo{}, fmt.Errorf("payload size has wire type %d", wireType)
			}
			size, read, err := consumeVarint(data)
			if err != nil {
				return transportPayloadInfo{}, err
			}
			info.Size = int(size)
			data = data[read:]
		case 2:
			if wireType != wireVarint {
				return transportPayloadInfo{}, fmt.Errorf("payload in_same_packet has wire type %d", wireType)
			}
			value, read, err := consumeVarint(data)
			if err != nil {
				return transportPayloadInfo{}, err
			}
			info.InSamePacket = value != 0
			data = data[read:]
		default:
			skipped, err := skipField(wireType, data)
			if err != nil {
				return transportPayloadInfo{}, err
			}
			data = data[skipped:]
		}
	}
	return info, nil
}

func parseTrailers(data []byte) (transportTrailers, error) {
	var trailers transportTrailers
	for len(data) > 0 {
		field, wireType, n, err := consumeKey(data)
		if err != nil {
			return transportTrailers{}, err
		}
		data = data[n:]
		switch field {
		case 2:
			if wireType != wireVarint {
				return transportTrailers{}, fmt.Errorf("status_code has wire type %d", wireType)
			}
			status, read, err := consumeVarint(data)
			if err != nil {
				return transportTrailers{}, err
			}
			trailers.StatusCode = int(status)
			data = data[read:]
		case 3:
			if wireType != wireBytes {
				return transportTrailers{}, fmt.Errorf("status_detail has wire type %d", wireType)
			}
			value, read, err := consumeBytes(data)
			if err != nil {
				return transportTrailers{}, err
			}
			trailers.StatusDetail = string(value)
			data = data[read:]
		default:
			skipped, err := skipField(wireType, data)
			if err != nil {
				return transportTrailers{}, err
			}
			data = data[skipped:]
		}
	}
	return trailers, nil
}

func parseListAvailableConfigsReply(data []byte) (fanControlConfigs, error) {
	var configs fanControlConfigs
	for len(data) > 0 {
		field, wireType, n, err := consumeKey(data)
		if err != nil {
			return fanControlConfigs{}, err
		}
		data = data[n:]
		switch field {
		case 1:
			if wireType != wireBytes {
				return fanControlConfigs{}, fmt.Errorf("configs has wire type %d", wireType)
			}
			value, read, err := consumeBytes(data)
			if err != nil {
				return fanControlConfigs{}, err
			}
			configs.Configs = append(configs.Configs, string(value))
			data = data[read:]
		case 2:
			if wireType != wireBytes {
				return fanControlConfigs{}, fmt.Errorf("current_config has wire type %d", wireType)
			}
			value, read, err := consumeBytes(data)
			if err != nil {
				return fanControlConfigs{}, err
			}
			configs.CurrentConfig = string(value)
			data = data[read:]
		case 3:
			if wireType != wireBytes {
				return fanControlConfigs{}, fmt.Errorf("config_folder has wire type %d", wireType)
			}
			value, read, err := consumeBytes(data)
			if err != nil {
				return fanControlConfigs{}, err
			}
			configs.ConfigFolder = string(value)
			data = data[read:]
		default:
			skipped, err := skipField(wireType, data)
			if err != nil {
				return fanControlConfigs{}, err
			}
			data = data[skipped:]
		}
	}
	return configs, nil
}

func parseCommandReply(data []byte) (int, string, error) {
	var status int
	var user string
	for len(data) > 0 {
		field, wireType, n, err := consumeKey(data)
		if err != nil {
			return 0, "", err
		}
		data = data[n:]
		switch field {
		case 1:
			if wireType != wireVarint {
				return 0, "", fmt.Errorf("command status has wire type %d", wireType)
			}
			value, read, err := consumeVarint(data)
			if err != nil {
				return 0, "", err
			}
			status = int(value)
			data = data[read:]
		case 2:
			if wireType != wireBytes {
				return 0, "", fmt.Errorf("command user has wire type %d", wireType)
			}
			value, read, err := consumeBytes(data)
			if err != nil {
				return 0, "", err
			}
			user = string(value)
			data = data[read:]
		default:
			skipped, err := skipField(wireType, data)
			if err != nil {
				return 0, "", err
			}
			data = data[skipped:]
		}
	}
	return status, user, nil
}

func commandStatusName(status int) string {
	switch status {
	case commandStatusOK:
		return "Ok"
	case commandStatusExit:
		return "Exit"
	case commandStatusRejected:
		return "Rejected"
	case commandStatusUnsupported:
		return "Unsupported"
	default:
		return fmt.Sprintf("CommandStatus(%d)", status)
	}
}
