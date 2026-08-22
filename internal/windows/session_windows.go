//go:build windows

package windows

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	syswindows "golang.org/x/sys/windows"
)

var (
	advapi32                  = syswindows.NewLazySystemDLL("advapi32.dll")
	user32                    = syswindows.NewLazySystemDLL("user32.dll")
	powrprof                  = syswindows.NewLazySystemDLL("powrprof.dll")
	procAdjustTokenPrivileges = advapi32.NewProc("AdjustTokenPrivileges")
	procLockWorkStation       = user32.NewProc("LockWorkStation")
	procSendMessageW          = user32.NewProc("SendMessageW")
	procSetSuspendState       = powrprof.NewProc("SetSuspendState")
)

func LockWorkStation() error {
	r1, _, err := procLockWorkStation.Call()
	if r1 == 0 {
		return syscallError("LockWorkStation", err)
	}
	return nil
}

func Sleep() (err error) {
	var token syswindows.Token
	if err := syswindows.OpenProcessToken(
		syswindows.CurrentProcess(),
		syswindows.TOKEN_ADJUST_PRIVILEGES|syswindows.TOKEN_QUERY,
		&token,
	); err != nil {
		return fmt.Errorf("open process token: %w", err)
	}
	defer func() {
		err = errors.Join(err, token.Close())
	}()

	privilegeName, err := syswindows.UTF16PtrFromString("SeShutdownPrivilege")
	if err != nil {
		return fmt.Errorf("encode shutdown privilege name: %w", err)
	}
	var privilegeLUID syswindows.LUID
	if err := syswindows.LookupPrivilegeValue(nil, privilegeName, &privilegeLUID); err != nil {
		return fmt.Errorf("look up shutdown privilege: %w", err)
	}

	privileges := syswindows.Tokenprivileges{
		PrivilegeCount: 1,
		Privileges: [1]syswindows.LUIDAndAttributes{
			{
				Luid:       privilegeLUID,
				Attributes: syswindows.SE_PRIVILEGE_ENABLED,
			},
		},
	}
	var previousPrivileges syswindows.Tokenprivileges
	var previousPrivilegesSize uint32
	r1, _, adjustErr := procAdjustTokenPrivileges.Call(
		uintptr(token),
		0,
		uintptr(unsafe.Pointer(&privileges)),
		unsafe.Sizeof(previousPrivileges),
		uintptr(unsafe.Pointer(&previousPrivileges)),
		uintptr(unsafe.Pointer(&previousPrivilegesSize)),
	)
	if r1 == 0 {
		return syscallError("AdjustTokenPrivileges", adjustErr)
	}
	if adjustErr == syswindows.ERROR_NOT_ALL_ASSIGNED {
		return fmt.Errorf("enable shutdown privilege: %w", adjustErr)
	}
	defer func() {
		restoreErr := syswindows.AdjustTokenPrivileges(token, false, &previousPrivileges, 0, nil, nil)
		if restoreErr != nil {
			restoreErr = fmt.Errorf("restore process privileges: %w", restoreErr)
		}
		err = errors.Join(err, restoreErr)
	}()

	suspendResult, _, suspendErr := procSetSuspendState.Call(0, 0, 0)
	if suspendResult == 0 {
		return syscallError("SetSuspendState", suspendErr)
	}
	return nil
}

func DisplayOff() error {
	const (
		hwndBroadcast  = uintptr(0xffff)
		wmSysCommand   = uintptr(0x0112)
		scMonitorPower = uintptr(0xf170)
		monitorOff     = uintptr(2)
	)
	procSendMessageW.Call(hwndBroadcast, wmSysCommand, scMonitorPower, monitorOff)
	return nil
}

func syscallError(name string, err error) error {
	if err == nil || err == syscall.Errno(0) {
		return fmt.Errorf("%s failed", name)
	}
	return fmt.Errorf("%s failed: %w", name, err)
}
