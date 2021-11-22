package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

func execNix(ctx context.Context, args ...string) ([]byte, error) {
	return eval(ctx, `nix`, args...)
}

func eval(ctx context.Context, bin string, args ...string) ([]byte, error) {
	inform(ctx, `running %v %v`, bin, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stderr = os.Stderr
	data, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return data, nil
}
