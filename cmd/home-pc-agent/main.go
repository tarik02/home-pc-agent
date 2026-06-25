package main

import (
	"fmt"
	"os"

	"github.com/tarik02/home-pc-agent/internal/app"
)

func main() {
	if err := app.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
