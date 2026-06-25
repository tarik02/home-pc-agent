//go:build windows

package windows

import (
	"fmt"
	"syscall"

	syswindows "golang.org/x/sys/windows"
)

var (
	user32              = syswindows.NewLazySystemDLL("user32.dll")
	powrprof            = syswindows.NewLazySystemDLL("powrprof.dll")
	procLockWorkStation = user32.NewProc("LockWorkStation")
	procSendMessageW    = user32.NewProc("SendMessageW")
	procSetSuspendState = powrprof.NewProc("SetSuspendState")
)

func LockWorkStation() error {
	r1, _, err := procLockWorkStation.Call()
	if r1 == 0 {
		return syscallError("LockWorkStation", err)
	}
	return nil
}

func Sleep() error {
	r1, _, err := procSetSuspendState.Call(0, 0, 0)
	if r1 == 0 {
		return syscallError("SetSuspendState", err)
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
