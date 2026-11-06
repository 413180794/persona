//go:build !persona_dev_resources || !linux

package sandbox

import (
	"context"
	"errors"
	"os/exec"
)

func developmentValidationProcess(context.Context, string, string, []string) (*exec.Cmd, func(), error) {
	return nil, nil, errors.New("development validator resources unavailable in this build")
}
func runDevelopmentValidation([]string) int { return 2 }
func verifyDevelopmentValidationEntry() error {
	return errors.New("development validator resources unavailable in this build")
}
