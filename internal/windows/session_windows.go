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
	wtsapi32                  = syswindows.NewLazySystemDLL("wtsapi32.dll")
	procAdjustTokenPrivileges = advapi32.NewProc("AdjustTokenPrivileges")
	procLockWorkStation       = user32.NewProc("LockWorkStation")
	procSendMessageW          = user32.NewProc("SendMessageW")
	procSetSuspendState       = powrprof.NewProc("SetSuspendState")
	procWTSQuerySessionInfo   = wtsapi32.NewProc("WTSQuerySessionInformationW")
)

const (
	wtsSessionInfoEx      = 25
	wtsSessionStateLock   = 0
	wtsSessionStateUnlock = 1
)

type wtsInfoEx struct {
	Level uint32
	Data  wtsInfoExLevel
}

type wtsInfoExLevel struct {
	Level1 wtsInfoExLevel1
}

type wtsInfoExLevel1 struct {
	SessionID               uint32
	SessionState            uint32
	SessionFlags            int32
	WinStationName          [33]uint16
	UserName                [21]uint16
	DomainName              [18]uint16
	LogonTime               int64
	ConnectTime             int64
	DisconnectTime          int64
	LastInputTime           int64
	CurrentTime             int64
	IncomingBytes           uint32
	OutgoingBytes           uint32
	IncomingFrames          uint32
	OutgoingFrames          uint32
	IncomingCompressedBytes uint32
	OutgoingCompressedBytes uint32
}

func LockWorkStation() error {
	r1, _, err := procLockWorkStation.Call()
	if r1 == 0 {
		return syscallError("LockWorkStation", err)
	}
	return nil
}

func WorkstationLocked() (bool, error) {
	sessionID, err := activeSessionID()
	if err != nil {
		return false, err
	}

	var buffer uintptr
	var bytesReturned uint32
	r1, _, queryErr := procWTSQuerySessionInfo.Call(
		0,
		uintptr(sessionID),
		wtsSessionInfoEx,
		uintptr(unsafe.Pointer(&buffer)),
		uintptr(unsafe.Pointer(&bytesReturned)),
	)
	if r1 == 0 {
		return false, syscallError("WTSQuerySessionInformationW", queryErr)
	}
	defer syswindows.WTSFreeMemory(buffer)

	if buffer == 0 || uintptr(bytesReturned) < unsafe.Sizeof(wtsInfoEx{}) {
		return false, fmt.Errorf("WTSQuerySessionInformationW returned %d bytes", bytesReturned)
	}
	info := (*wtsInfoEx)(unsafe.Pointer(buffer))
	if info.Level != 1 {
		return false, fmt.Errorf("unsupported WTS session info level %d", info.Level)
	}
	switch info.Data.Level1.SessionFlags {
	case wtsSessionStateLock:
		return true, nil
	case wtsSessionStateUnlock:
		return false, nil
	default:
		return false, fmt.Errorf("unexpected WTS session flags %d", info.Data.Level1.SessionFlags)
	}
}

func activeSessionID() (uint32, error) {
	var sessions *syswindows.WTS_SESSION_INFO
	var count uint32
	if err := syswindows.WTSEnumerateSessions(0, 0, 1, &sessions, &count); err != nil {
		return 0, fmt.Errorf("enumerate WTS sessions: %w", err)
	}
	defer syswindows.WTSFreeMemory(uintptr(unsafe.Pointer(sessions)))

	for _, session := range unsafe.Slice(sessions, int(count)) {
		if session.State == syswindows.WTSActive {
			return session.SessionID, nil
		}
	}
	return 0, fmt.Errorf("no active WTS session")
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
