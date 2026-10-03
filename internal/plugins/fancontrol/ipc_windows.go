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
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

const (
	fanControlPipeName = `\\.\pipe\FanControl`

	grpcStatusOK = 0
)

type fanControlConfigs struct {
	Configs       []string
	CurrentConfig string
	ConfigFolder  string
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

	packet, err := encodeUnaryRequest(method, payload)
	if err != nil {
		return nil, err
	}
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
	request, err := proto.Marshal(&LoadConfigRequest{ConfigName: configName})
	if err != nil {
		return fmt.Errorf("encode FanControl load-config request: %w", err)
	}
	payload, err := r.conn.CallUnary(ctx, "/FanControlRPC/LoadConfig", request)
	if err != nil {
		return err
	}
	status, user, err := parseCommandReply(payload)
	if err != nil {
		return err
	}
	if status != CommandStatus_COMMAND_STATUS_OK {
		return fmt.Errorf("FanControl returned %s for user %q", commandStatusName(status), user)
	}
	return nil
}

func encodeUnaryRequest(method string, payload []byte) ([]byte, error) {
	requestInit, err := proto.Marshal(&TransportMessage{
		RequestInit: &RequestInit{Method: method, CallType: 1},
	})
	if err != nil {
		return nil, fmt.Errorf("encode FanControl request init: %w", err)
	}
	requestControl := protowire.AppendTag(nil, 2, protowire.BytesType)
	requestControl = protowire.AppendBytes(requestControl, nil)
	payloadInfo, err := proto.Marshal(&TransportMessage{
		PayloadInfo: &PayloadInfo{Size: int32(len(payload)), InSamePacket: true},
	})
	if err != nil {
		return nil, fmt.Errorf("encode FanControl payload info: %w", err)
	}

	packet := protowire.AppendBytes(nil, requestInit)
	packet = protowire.AppendBytes(packet, requestControl)
	packet = protowire.AppendBytes(packet, payloadInfo)
	packet = append(packet, payload...)
	return packet, nil
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
			message, read := protowire.ConsumeBytes(packet[offset:])
			if read < 0 {
				return nil, fmt.Errorf("parse FanControl IPC transport message: %w", protowire.ParseError(read))
			}
			offset += read

			var transport TransportMessage
			if err := proto.Unmarshal(message, &transport); err != nil {
				return nil, fmt.Errorf("decode FanControl IPC transport message: %w", err)
			}
			if transport.PayloadInfo != nil {
				info := transport.PayloadInfo
				if info.Size < 0 {
					return nil, fmt.Errorf("invalid FanControl IPC payload size %d", info.Size)
				}
				if info.InSamePacket {
					size := int(info.Size)
					if len(packet)-offset < size {
						return nil, io.ErrUnexpectedEOF
					}
					payload = append(payload, packet[offset:offset+size]...)
					offset += size
				} else {
					extra, err := readPipeBytes(handle, int(info.Size))
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

func parseListAvailableConfigsReply(data []byte) (fanControlConfigs, error) {
	var reply ListAvailableConfigsReply
	if err := proto.Unmarshal(data, &reply); err != nil {
		return fanControlConfigs{}, err
	}
	return fanControlConfigs{
		Configs:       reply.Configs,
		CurrentConfig: reply.CurrentConfig,
		ConfigFolder:  reply.ConfigFolder,
	}, nil
}

func parseCommandReply(data []byte) (CommandStatus, string, error) {
	var reply CommandReply
	if err := proto.Unmarshal(data, &reply); err != nil {
		return CommandStatus_COMMAND_STATUS_UNSPECIFIED, "", err
	}
	return reply.Status, reply.User, nil
}

func commandStatusName(status CommandStatus) string {
	switch status {
	case CommandStatus_COMMAND_STATUS_OK:
		return "Ok"
	case CommandStatus_COMMAND_STATUS_EXIT:
		return "Exit"
	case CommandStatus_COMMAND_STATUS_REJECTED:
		return "Rejected"
	case CommandStatus_COMMAND_STATUS_UNSUPPORTED:
		return "Unsupported"
	default:
		return fmt.Sprintf("CommandStatus(%d)", status)
	}
}
