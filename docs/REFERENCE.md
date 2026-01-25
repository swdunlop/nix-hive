# nix-hive Reference

Complete API documentation for `makeHive` and the hive object it returns.

## makeHive

```nix
nix-hive.lib.makeHive {
  inputs = { ... };
  defaults = { ... };
  instances = { ... };
}
```

### Arguments

#### `inputs` (required in build mode)

An attribute set of flake inputs to pass to your NixOS configurations. Must include `nixpkgs`.

```nix
inputs = { inherit nixpkgs; };
```

These inputs become available in your NixOS modules via `specialArgs`. You can access them like this:

```nix
# In your NixOS module
{ inputs, ... }: {
  # inputs.nixpkgs is available here
}
```

#### `defaults` (optional)

Default settings applied to all instances.

| Attribute | Type | Description |
|-----------|------|-------------|
| `sshConfig` | attribute set | SSH options applied to all instances |

```nix
defaults = {
  sshConfig = {
    User = "admin";
    Port = 2222;
  };
};
```

#### `instances` (required in build mode)

An attribute set mapping instance names to instance definitions.

```nix
instances = {
  web-server = { ... };
  database = { ... };
};
```

#### `manifest` (manifest mode only)

A pre-built manifest from another hive. When provided, `makeHive` skips building NixOS configurations and uses this manifest directly. See [Manifest Mode](#manifest-mode) below.

---

## Instance Definition

Each instance in the `instances` attribute set has the following structure:

```nix
instance-name = {
  configuration = {
    system = "x86_64-linux";
    modules = [ ... ];
    specialArgs = { ... };  # optional
  };
  sshConfig = { ... };  # optional
};
```

### `configuration` (required)

The NixOS configuration for this instance.

| Attribute | Type | Description |
|-----------|------|-------------|
| `system` | string | The system architecture (e.g., `"x86_64-linux"`, `"aarch64-linux"`) |
| `modules` | list | List of NixOS modules to include |
| `specialArgs` | attribute set | Additional arguments passed to modules (optional) |

### `sshConfig` (optional)

SSH configuration options for reaching this instance. These are written directly to an ssh_config(5) file fragment.

Common options:

| Option | Description |
|--------|-------------|
| `HostName` | IP address or hostname of the instance |
| `User` | SSH username |
| `Port` | SSH port number |
| `IdentityFile` | Path to SSH private key |
| `ProxyJump` | Bastion/jump host |

```nix
sshConfig = {
  HostName = "192.168.1.10";
  User = "root";
  IdentityFile = "~/.ssh/hive_key";
};
```

The instance's `sshConfig` is merged with `defaults.sshConfig`, with instance-specific values taking precedence.

---

## The hive.instance.name Option

Every NixOS configuration in a hive receives a special option: `config.hive.instance.name`. This contains the instance's name as defined in the `instances` attribute set.

Use this to differentiate identical configurations:

```nix
{ config, ... }: {
  networking.hostName = config.hive.instance.name;

  # Different behavior per instance
  services.nginx.virtualHosts."example.com" = {
    root = "/var/www/${config.hive.instance.name}";
  };
}
```

This option is read-only and is automatically set by nix-hive.

---

## Return Value

`makeHive` returns an attribute set with the following:

### `instances`

The processed instances with their NixOS configurations. Only available in build mode (empty in manifest mode).

### `manifest`

An attribute set mapping instance names to deployment information:

```nix
{
  instance-name = {
    systemPath = "/nix/store/...-nixos-system-...";
    sshConfig = { HostName = "..."; ... };
  };
}
```

### `manifestJSON`

The manifest as a JSON string. Useful for passing to external tools or remote deployment systems.

### `sshConfigText`

A complete ssh_config(5) file fragment for all instances:

```
Host web-server
    HostName 192.168.1.10
    User admin

Host database
    HostName 192.168.1.11
    User admin
```

### `packages`

An attribute set of deployment packages, keyed by system architecture:

```nix
hive.packages.x86_64-linux.deploy
hive.packages.x86_64-linux.ssh
# etc.
```

Available packages:

| Package | Description |
|---------|-------------|
| `manifest` | JSON manifest file as a derivation |
| `sshConfig` | SSH config file as a derivation |
| `ssh` | OpenSSH `ssh` wrapper with hive SSH config |
| `scp` | OpenSSH `scp` wrapper with hive SSH config |
| `sftp` | OpenSSH `sftp` wrapper with hive SSH config |
| `deploy` | Deployment script (see below) |

### `devShells`

Development shells with hive tools pre-installed:

```nix
hive.devShells.x86_64-linux.default
```

Enter with `nix develop`.

### `meta`

Metadata about the hive:

| Attribute | Type | Description |
|-----------|------|-------------|
| `instanceNames` | list of strings | Names of all instances |
| `instanceCount` | integer | Number of instances |

---

## The deploy Command

The `deploy` package provides a script for deploying systems to instances.

### Usage

```sh
deploy [OPTIONS] [INSTANCE...]
```

### Options

| Option | Description |
|--------|-------------|
| `--no-activation` | Copy the system but don't activate it |
| `-h`, `--help` | Show help message |

### Examples

Deploy all instances:
```sh
deploy
```

Deploy specific instances:
```sh
deploy web-server database
```

Copy without activating (for staged rollouts):
```sh
deploy --no-activation web-server
```

### What deploy Does

1. Reads the manifest to find each instance's `systemPath`
2. Runs `nix copy --to ssh://instance-name` to transfer the system closure
3. If activation is enabled:
   - Sets the system profile: `nix-env -p /nix/var/nix/profiles/system --set $systemPath`
   - Runs: `$systemPath/bin/switch-to-configuration switch`

---

## Manifest Mode

For distributed build/deploy workflows, you can build systems on one machine and deploy from another.

### On the Build Machine

```nix
# Build and export the manifest
nix eval --json .#lib.hive.manifest > manifest.json
```

### On the Deploy Machine

```nix
{
  outputs = { self, hive-flake, nixpkgs, ... }: let
    manifest = builtins.fromJSON (builtins.readFile ./manifest.json);
    hive = hive-flake.lib.makeHive { inherit manifest; };
  in {
    packages = hive.packages;
  };
}
```

When `manifest` is provided, `makeHive` skips NixOS evaluation entirely and produces deployment tools that reference the pre-built system paths.

> **Warning**: The manifest format may change between nix-hive versions. Ensure your build and deploy systems use the same version.

---

## Flake Outputs

A typical hive flake exposes:

```nix
{
  outputs = { self, nixpkgs, hive-flake, ... }: let
    hive = hive-flake.lib.makeHive { ... };
  in {
    # Expose the hive for inspection
    lib.hive = hive;

    # Deployment packages
    packages = hive.packages;

    # Development shell
    devShells = hive.devShells;
  };
}
```

### Inspect the Hive

```sh
# View the manifest
nix eval --json .#lib.hive.manifest

# View instance names
nix eval --json .#lib.hive.meta.instanceNames

# View the generated SSH config
nix eval .#lib.hive.sshConfigText
```

### Build Packages

```sh
# Build the deploy script
nix build .#packages.x86_64-linux.deploy

# Build the SSH wrapper
nix build .#packages.x86_64-linux.ssh
```
