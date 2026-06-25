package externalcmd

import (
	"fmt"
	"os/exec"
)

func Require(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("required command %q not found on PATH", name)
	}
	return nil
}

func RequireAll(names ...string) error {
	for _, name := range names {
		if err := Require(name); err != nil {
			return err
		}
	}
	return nil
}
