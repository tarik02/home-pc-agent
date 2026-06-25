//go:build windows

package powerplan

import (
	"context"
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	powerDataAccessorScheme = 16
	errorNoMoreItems        = syscall.Errno(259)
)

var (
	powrprofDLL               = windows.NewLazySystemDLL("powrprof.dll")
	procPowerEnumerate        = powrprofDLL.NewProc("PowerEnumerate")
	procPowerReadFriendlyName = powrprofDLL.NewProc("PowerReadFriendlyName")
)

func discoverPowerPlans(ctx context.Context) ([]Mode, error) {
	var modes []Mode
	for index := uint32(0); ; index++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		var guid windows.GUID
		size := uint32(unsafe.Sizeof(guid))
		ret, _, _ := procPowerEnumerate.Call(
			0,
			0,
			0,
			uintptr(powerDataAccessorScheme),
			uintptr(index),
			uintptr(unsafe.Pointer(&guid)),
			uintptr(unsafe.Pointer(&size)),
		)

		switch errno := syscall.Errno(ret); errno {
		case 0:
			name, err := powerPlanFriendlyName(guid)
			if err != nil {
				return nil, err
			}
			modes = append(modes, Mode{
				Name: name,
				GUID: strings.Trim(guid.String(), "{}"),
			})
		case errorNoMoreItems:
			if len(modes) == 0 {
				return nil, fmt.Errorf("Windows returned no power schemes")
			}
			return modes, nil
		default:
			return nil, fmt.Errorf("PowerEnumerate index %d: %w", index, errno)
		}
	}
}

func powerPlanFriendlyName(guid windows.GUID) (string, error) {
	var size uint32
	ret, _, _ := procPowerReadFriendlyName.Call(
		0,
		uintptr(unsafe.Pointer(&guid)),
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&size)),
	)
	if errno := syscall.Errno(ret); errno != 0 && errno != windows.ERROR_MORE_DATA {
		return "", fmt.Errorf("PowerReadFriendlyName size for %s: %w", guid.String(), errno)
	}
	if size == 0 {
		return strings.Trim(guid.String(), "{}"), nil
	}

	buffer := make([]uint16, (size+1)/2)
	ret, _, _ = procPowerReadFriendlyName.Call(
		0,
		uintptr(unsafe.Pointer(&guid)),
		0,
		0,
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if errno := syscall.Errno(ret); errno != 0 {
		return "", fmt.Errorf("PowerReadFriendlyName for %s: %w", guid.String(), errno)
	}

	name := strings.TrimSpace(windows.UTF16ToString(buffer))
	if name == "" {
		return strings.Trim(guid.String(), "{}"), nil
	}
	return name, nil
}
