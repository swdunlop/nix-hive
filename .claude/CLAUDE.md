# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

nix-hive is a Nix Flake that provides helpers for managing deployments of multiple NixOS instances. It builds NixOS configurations, generates a manifest of system paths and SSH settings, and provides deployment tools.

## Architecture

The flake exports a single function: `lib.makeHive`. It operates in two modes:

**Build mode** (default): Takes `inputs`, `defaults`, and `instances`. Evaluates NixOS configurations via `lib.nixosSystem`, injects a `hive.instance.name` option into each, and produces a manifest mapping instance names to `{ systemPath, sshConfig }`.

**Manifest mode**: Takes a pre-built `manifest` attribute. Skips NixOS evaluation and uses the manifest directly for deployment tools. Used when builds happen on a separate machine.

Key files:
- `flake.nix` — Exports `lib` by importing `./lib`
- `lib/default.nix` — Entry point, passes nixpkgs to makeHive.nix
- `lib/makeHive.nix` — Core implementation (254 lines)

The `makeHive` return value includes:
- `manifest` / `manifestJSON` — Deployment info per instance
- `packages.<system>.*` — Deployment tools (deploy, ssh, scp, sftp, manifest, sshConfig)
- `devShells.<system>.default` — Shell with all tools
- `instances` — Processed NixOS configurations (build mode only)
- `meta` — Instance names and count

## Commands

Evaluate the example manifest:
```sh
nix eval --json ./example#lib.hive.manifest
```

Build a package:
```sh
nix build ./example#packages.x86_64-linux.deploy
```

Enter the example dev shell:
```sh
nix develop ./example
```

Available packages: `manifest`, `sshConfig`, `ssh`, `scp`, `sftp`, `deploy`
