//go:build windows

package windows

import (
	"context"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
)

type agentService struct {
	run func(ctx context.Context) error
}

func (s *agentService) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	status <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- s.run(ctx)
	}()

	status <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		select {
		case err := <-done:
			if err != nil && err != context.Canceled {
				return true, 1
			}
			return false, 0
		case req := <-requests:
			switch req.Cmd {
			case svc.Interrogate:
				status <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending, Accepts: accepted}
				cancel()
				<-done
				return false, 0
			default:
			}
		}
	}
}

func RunningAsService() (bool, error) {
	return svc.IsWindowsService()
}

func RunAgentService(name string, run func(ctx context.Context) error) error {
	return svc.Run(name, &agentService{run: run})
}

func RunAgentServiceDebug(name string, run func(ctx context.Context) error) error {
	return debug.Run(name, &agentService{run: run})
}
