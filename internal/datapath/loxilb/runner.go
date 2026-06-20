package loxilb

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// Runner executes loxicmd. Tests use a fake runner so parser and reconcile
// logic are fully testable on macOS.
type Runner interface {
	Run(ctx context.Context, args ...string) ([]byte, error)
}

type ExecRunner struct {
	Bin string
}

func NewExecRunner() ExecRunner {
	return ExecRunner{Bin: "loxicmd"}
}

func (r ExecRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	bin := r.Bin
	if bin == "" {
		bin = "loxicmd"
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("%s %v: %w: %s", bin, args, err, stderr.String())
		}
		return nil, fmt.Errorf("%s %v: %w", bin, args, err)
	}
	return out, nil
}
