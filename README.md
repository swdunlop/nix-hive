# nix-hive -- a Nix Flake for Managing Deployments of NixOS

Nix-hive lets you define a collection of NixOS systems (a "hive") and provides tools to deploy and manage them. Instances in the hive may share a common configuration while supporting specialization where necessary using NixOS modules.

## Features

- NixOS system configurations are defined as Nix flakes
- Multiple instances of a given configuration can be deployed over SSH
- Deployments can leverage remote Nix builders and substituters to offload resource usage.

## Quick Start

Add hive-flake to your flake inputs:

```nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.11";
    nix-hive.url = "github:swdunlop/nix-hive";
  };

  outputs = { self, nixpkgs, nix-hive, ... }: let
    hive = nix-hive.lib.makeHive {
      inputs = { inherit nixpkgs; };
      instances = {
        server-1 = {
          configuration = {
            system = "x86_64-linux";
            modules = [ ./configuration.nix ];
          };
          sshConfig.HostName = "192.168.1.10";
        };
      };
    };
  in {
    inherit (hive) packages devShells;
  };
}
```

Enter the dev shell and deploy:

```sh
nix run .#deploy server-1
```

## Documentation

- **[Guide](./docs/GUIDE.md)** - A walkthrough of setting up a small home network with hive-flake
- **[Reference](./docs/REFERENCE.md)** - Complete API documentation for `makeHive`
- **[Go Package](https://pkg.go.dev/swdunlop.dev/nix-hive/pkg/hive)** - Go package describing the hive manifest.

## What's in a Hive?

A hive produces a **manifest** that maps instance names to their deployment information:

```sh
nix eval --json .#lib.hive.manifest
```

```json
{
  "server-1": {
    "systemPath": "/nix/store/...-nixos-system-...",
    "sshConfig": { "HostName": "192.168.1.10" }
  }
}
```

The `systemPath` is the built NixOS system in the Nix store. The `sshConfig` contains SSH options for reaching that instance.

## Packages

nix-hive provides these packages for managing your hive:

| Package | Description |
|---------|-------------|
| `deploy` | Copy systems to instances and activate them |
| `sign` | Sign system paths with a Nix signing key |
| `ssh` | OpenSSH wrapper configured for your hive |
| `scp` | SCP wrapper configured for your hive |
| `sftp` | SFTP wrapper configured for your hive |

## License

MIT

## Contributions

Bug fixes are welcome. Features and feature requests are viewed with suspicion.  Take what you like and run with it.

