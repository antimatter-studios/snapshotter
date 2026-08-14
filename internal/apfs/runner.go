package apfs

import (
	"context"
	"os/exec"
)

// Runner executes an external command and returns its combined output.
//
// Everything in this package talks to the system through a Runner so the
// parsing and the safety guards can be tested without invoking tmutil, which
// would create and destroy real snapshots on the developer's machine.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (string, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out), err
}

// SystemRunner returns a Runner backed by os/exec.
func SystemRunner() Runner { return execRunner{} }
