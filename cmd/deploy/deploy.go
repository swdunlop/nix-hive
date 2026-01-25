package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/pflag"
	"swdunlop.dev/nix-hive/pkg/hive"
)

func init() {
	pflag.StringVarP(&manifestPath, `manifest`, `m`, manifestPath, `path to the JSON manifest file describing instances we can deploy (default $HIVE_MANIFEST)`)
	pflag.StringArrayVarP(&excludePatterns, `exclude`, `x`, excludePatterns, `patterns identifying instances we should exclude from deployment (default exclude nothing)`)
	pflag.CountVarP(&verbosity, `verbose`, `v`, `increase verbosity (can be repeated: -v for info, -vv for debug)`)
	pflag.BoolVarP(&quiet, `quiet`, `q`, false, `suppress all output except errors`)
	pflag.BoolVarP(&debugOutput, `debug-output`, `d`, false, `show command output on stderr`)
	pflag.BoolVar(&noActivate, `no-activate`, false, `copy systems without activating (for pre-staging)`)
	pflag.StringVar(&activationOperation, `activation-operation`, `switch`, `specify how to activate, see nixos-rebuild(8) for supported operations`)
}

// manifestPath is the path to manifest.json -- if omitted, we use HIVE_MANIFEST from the OS environment.
var manifestPath string

// excludePatterns is a list of patterns identifying instances we should not deploy.
var excludePatterns []string

// targetPatterns is a list of patterns identifying instances we should deploy.  The order matters, we want to deploy systems in the order that they are
// specified on the command line, falling down to lexical order.
var targetPatterns []string

// targetInstances is the list of instance names we will deploy.
var targetInstances []string

// manifest contains the manifest loaded from manifestPath
var manifest hive.Manifest

// verbosity controls log output level (0=warn, 1=info, 2+=debug)
var verbosity int

// quiet suppresses all output except errors
var quiet bool

// debugOutput shows command output on stderr
var debugOutput bool

// noActivate skips activation after staging
var noActivate bool

// activationOperation specifies how to perform the switch -- this is usually "switch", "test" or "boot", but it can
// be "check" or "dry-activate" to check what will happen without activation.
var activationOperation = `switch`

func main() {
	pflag.Usage = printUsage
	pflag.Parse()
	configureLogging()
	ctx := context.Background()
	targetPatterns = pflag.Args()
	err := deploy(ctx)
	if err != nil {
		slog.ErrorContext(ctx, `deployment failed`, `error`, err)
		_ = os.Stderr.Sync()
		os.Exit(1)
	}
}

// configureLogging sets up the default slog logger based on verbosity flags.
func configureLogging() {
	var level slog.Level
	switch {
	case quiet:
		level = slog.LevelError
	case verbosity >= 2:
		level = slog.LevelDebug
	case verbosity == 1:
		level = slog.LevelInfo
	default:
		level = slog.LevelWarn
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))
}

// printUsage prints usage information for this command.
func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: %s [OPTIONS] [PATTERN...]

Deploy NixOS systems to hive instances.

Patterns use glob-style matching (*, ?) against instance names.
If no patterns are specified, all instances will be deployed.

Options:
`, filepath.Base(os.Args[0]))
	pflag.PrintDefaults()
	fmt.Fprint(os.Stderr, `
Environment:
  HIVE_MANIFEST    Path to the JSON manifest file (used if -m is not specified)

Examples:
  deploy                     Deploy all instances
  deploy web-*               Deploy instances matching "web-*"
  deploy -x staging-* prod-* Deploy prod instances, excluding staging
  deploy -v web-1            Deploy with info logging
  deploy -vv web-1           Deploy with debug logging
  deploy -d web-1            Deploy showing nix/ssh output
  deploy --no-activate       Pre-stage systems without activating
`)
}

// deploy actually performs the deployment as a series of steps using the global state set up by main.
func deploy(ctx context.Context) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	steps := []func(context.Context) error{
		loadManifest,
		selectInstances,
		excludeInstances,
		stageSystems,
	}
	if !noActivate {
		steps = append(steps, activateSystems)
	}

	for _, step := range steps {
		err := step(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

// loadManifest loads a JSON encoded manifest from manifestPath.
func loadManifest(ctx context.Context) error {
	path := manifestPath
	if path == "" {
		path = os.Getenv("HIVE_MANIFEST")
	}
	if path == "" {
		return errors.New("no manifest path specified; use -m or set HIVE_MANIFEST")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading manifest: %w", err)
	}

	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parsing manifest: %w", err)
	}

	slog.DebugContext(ctx, "loaded manifest", "path", path, "instances", len(manifest))
	return nil
}

// selectInstances builds the targetInstances slice from instances in the manifest that matched targetPatterns.
func selectInstances(ctx context.Context) error {
	// If no patterns specified, select all instances in lexical order.
	if len(targetPatterns) == 0 {
		for name := range manifest {
			targetInstances = append(targetInstances, name)
		}
		slices.Sort(targetInstances)
		slog.DebugContext(ctx, "selected all instances", "count", len(targetInstances))
		return nil
	}

	// Match instances against patterns, preserving pattern order.
	seen := make(map[string]bool)
	for _, pattern := range targetPatterns {
		for name := range manifest {
			if seen[name] {
				continue
			}
			matched, err := filepath.Match(pattern, name)
			if err != nil {
				return fmt.Errorf("invalid pattern %q: %w", pattern, err)
			}
			if matched {
				targetInstances = append(targetInstances, name)
				seen[name] = true
			}
		}
	}

	if len(targetInstances) == 0 {
		return fmt.Errorf("no instances matched patterns: %v", targetPatterns)
	}

	slog.DebugContext(ctx, "selected instances", "patterns", targetPatterns, "count", len(targetInstances))
	return nil
}

// excludeInstances refines the targetInstances slice by removing targets that match excludePatterns.
func excludeInstances(ctx context.Context) error {
	if len(excludePatterns) == 0 {
		return nil
	}

	var filtered []string
	for _, name := range targetInstances {
		excluded := false
		for _, pattern := range excludePatterns {
			matched, err := filepath.Match(pattern, name)
			if err != nil {
				return fmt.Errorf("invalid exclude pattern %q: %w", pattern, err)
			}
			if matched {
				excluded = true
				slog.DebugContext(ctx, "excluding instance", "name", name, "pattern", pattern)
				break
			}
		}
		if !excluded {
			filtered = append(filtered, name)
		}
	}

	if len(filtered) == 0 {
		return errors.New("all instances were excluded")
	}

	targetInstances = filtered
	slog.DebugContext(ctx, "after exclusions", "count", len(targetInstances))
	return nil
}

// stageSystems uses the `nix` command to transfer systems to instances in advance of activation.
func stageSystems(ctx context.Context) error {
	for _, name := range targetInstances {
		instance := manifest[name]
		slog.InfoContext(ctx, "staging system", "instance", name)

		sshOpts := buildSSHOptions(instance)
		dest := "ssh://" + name

		cmd := exec.CommandContext(ctx, "nix", "copy", "--to", dest, instance.SystemPath)
		cmd.Env = append(os.Environ(), "NIX_SSHOPTS="+sshOpts)
		if debugOutput {
			cmd.Stdout = os.Stderr
			cmd.Stderr = os.Stderr
		}

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("staging %s: %w", name, err)
		}
	}
	return nil
}

// buildSSHOptions constructs an SSH options string from an instance's sshConfig.
func buildSSHOptions(instance hive.Instance) string {
	var opts []string
	for key, value := range instance.SSHConfig {
		opts = append(opts, "-o", fmt.Sprintf("%s=%s", key, value))
	}
	return strings.Join(opts, " ")
}

// activateSystems uses SSH to activate the staged systems on each instance.
func activateSystems(ctx context.Context) error {
	for _, name := range targetInstances {
		instance := manifest[name]
		slog.InfoContext(ctx, "activating system", "instance", name)

		var activateCmd string
		if activationOperation == "check" || activationOperation == "dry-activate" {
			// For check and dry-activate, do not change the system profile
			activateCmd = fmt.Sprintf(
				"sudo %s/bin/switch-to-configuration %s",
				instance.SystemPath, activationOperation,
			)
		} else {
			activateCmd = fmt.Sprintf(
				"sudo nix-env -p /nix/var/nix/profiles/system --set %s && sudo %s/bin/switch-to-configuration %s",
				instance.SystemPath, instance.SystemPath, activationOperation,
			)
		}

		args := buildSSHArgs(instance)
		args = append(args, name, activateCmd)

		cmd := exec.CommandContext(ctx, "ssh", args...)
		if debugOutput {
			cmd.Stdout = os.Stderr
			cmd.Stderr = os.Stderr
		}

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("activating %s: %w", name, err)
		}

		slog.InfoContext(ctx, "activated", "instance", name)
	}
	return nil
}

// buildSSHArgs constructs SSH command-line arguments from an instance's sshConfig.
func buildSSHArgs(instance hive.Instance) []string {
	var args []string
	for key, value := range instance.SSHConfig {
		args = append(args, "-o", fmt.Sprintf("%s=%s", key, value))
	}
	return args
}
