package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(buildCmd)
}

var buildCmd = &cobra.Command{
	Use:   `build`,
	Short: `Builds systems for deployment`,
	Long:  `Build will build NixOS systems locally for deployment.`,
	RunE:  runBuild,
}

func runBuild(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	systems, err := inv.matchSystems(args...)
	if err != nil {
		return err
	}
	var dont struct {
		realise bool
	}
	for _, step := range strings.Split(no, ",") {
		switch step {
		case `realise`:
			dont.realise = true
		}
	}
	if dont.realise {
		err = inv.instantiate(ctx, systems...)
	} else {
		err = inv.build(ctx, systems...)
	}
	if err != nil {
		return err
	}
	results := make(map[string]string, len(systems))
	for _, system := range systems {
		results[system] = inv.Systems[system].Result
	}
	return json.NewEncoder(os.Stdout).Encode(inv)
}

// build builds systems for a specific target, such as "system" for a NixOS system or "vhd" for a disk image.
func (inv *Inventory) build(ctx context.Context, systems ...string) error {
	for _, system := range systems {
		err := inv.Systems[system].build(ctx, system)
		if err != nil {
			return fmt.Errorf(`%w while building %q`, err, system)
		}
	}
	return nil
}

// equivalent to build that does not realise generated derivations
func (inv *Inventory) instantiate(ctx context.Context, systems ...string) error {
	for _, system := range systems {
		err := inv.Systems[system].instantiate(ctx, system)
		if err != nil {
			return fmt.Errorf(`%w while building %q`, err, system)
		}
	}
	return nil
}

// instantiate creates a .drv in the local store, but for compatibility with build returns the $out path
func (cfg *System) instantiate(ctx context.Context, system string) error {
	if cfg.Result != `` {
		return nil // already built.
	}
	args := make([]string, 0, 64)
	inform(ctx, `instantiating %q`, system)

	args = append(args, `--include`, `deployment=`+deploymentPath)
	paths := inv.systemPaths(system)
	for _, path := range paths {
		args = append(args, `--include`, path)
	}
	args = append(args, `--argstr`, `name`, system)
	args = append(args, `--expr`, `(import <hive/build.nix>)`)
	derivationFilename, err := eval(ctx, `nix-instantiate`, args...)
	if err != nil {
		return err
	}
	cfg.ResultDrv = strings.TrimRight(string(derivationFilename), "\n")
	derivationJson, err := eval(ctx, `nix`, `show-derivation`, cfg.ResultDrv)
	if err != nil {
		return err
	}
	resultDerivation := make(map[string]struct {
		Outputs struct {
			Out struct {
				Path string `json:"path"`
			} `json:"out"`
		} `json:"outputs"`
	})
	if err := json.Unmarshal(derivationJson, &resultDerivation); err != nil {
		return err
	}
	if len(resultDerivation) != 1 {
		return fmt.Errorf("Result of parsing %v does not have exactly one derivation", cfg.ResultDrv)
	}
	for _, drv := range resultDerivation {
		cfg.Result = drv.Outputs.Out.Path
		return err
	}
	panic("unreachable")
}

func (cfg *System) build(ctx context.Context, system string) error {
	if cfg.Result != `` {
		return nil // already built.
	}
	inform(ctx, `building %q`, system)
	link := filepath.Join(tmp, `system-`+system)
	args := make([]string, 0, 64)
	args = append(args, inv.Nix.Build.Flags...)
	args = append(args, `--out-link`, link, `--include`, `deployment=`+deploymentPath)
	paths := inv.systemPaths(system)
	for _, path := range paths {
		args = append(args, `--include`, path)
	}
	args = append(args, `--argstr`, `name`, system)
	args = append(args, `--expr`, `(import <hive/build.nix>)`)
	_, err := eval(ctx, `nix-build`, args...)
	if err != nil {
		return err
	}
	result, err := os.Readlink(link)
	if err != nil {
		return err
	}
	cfg.Result = result
	derivationFilename, err := eval(ctx, `nix-store`, `--query`, `--deriver`, result)
	if err == nil {
		cfg.ResultDrv = strings.TrimRight(string(derivationFilename), "\n")
	}
	return err
}
