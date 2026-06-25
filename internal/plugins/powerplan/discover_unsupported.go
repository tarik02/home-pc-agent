//go:build !windows

package powerplan

import (
	"context"
	"fmt"
)

func discoverPowerPlans(ctx context.Context) ([]Mode, error) {
	return nil, fmt.Errorf("power plan discovery is only supported on Windows")
}
