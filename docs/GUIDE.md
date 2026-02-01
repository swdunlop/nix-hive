# Setting Up a Home Network with nix-hive

This guide walks through using nix-hive to manage a small home network consisting of three NixOS machines:

- **gateway** - A firewall/router handling NAT and basic packet filtering
- **apps** - An application server running web services
- **storage** - A file server providing NFS shares

By the end, you'll have a single flake that builds and deploys all three systems.

## Prerequisites

- Familiarity with NixOS configuration (you've edited `configuration.nix` before)
- Three machines with NixOS installed (or VMs for testing)
- SSH access to each machine
- Nix with flakes enabled

## Planning the Network

Before writing any Nix, let's plan our network:

```
Internet
    │
    ▼
┌─────────┐
│ gateway │ 192.168.1.1 (internal)
└────┬────┘
     │
─────┴─────────────────────── 192.168.1.0/24
     │              │
┌────┴────┐   ┌─────┴─────┐
│  apps   │   │  storage  │
│ .1.10   │   │   .1.20   │
└─────────┘   └───────────┘
```

Each machine needs:
- A NixOS configuration defining its services
- An SSH address so nix-hive can reach it

## Creating the Flake Structure

Create a new directory for your hive:

```sh
mkdir home-network && cd home-network
```

Create the following file structure:

```
home-network/
├── flake.nix           # Main flake definition
├── modules/
│   └── common.nix      # Shared configuration
├── hosts/
│   ├── gateway.nix     # Firewall configuration
│   ├── apps.nix        # App server configuration
│   └── storage.nix     # File server configuration
```

## Step 1: The Common Module

Start with configuration shared across all machines. Create `modules/common.nix`:

```nix
{ config, inputs, ... }: {
  # Use the instance name as the hostname
  networking.hostName = config.hive.instance.name;

  # Every machine gets the same admin user
  users.users.admin = {
    isNormalUser = true;
    extraGroups = [ "wheel" ];
    openssh.authorizedKeys.keys = [
      "ssh-ed25519 AAAA... your-key-here"
    ];
  };

  # Allow passwordless sudo for deployments
  security.sudo.wheelNeedsPassword = false;

  # Enable SSH for remote management
  services.openssh = {
    enable = true;
    settings.PermitRootLogin = "prohibit-password";
  };

  # Basic nix settings
  nix.settings.experimental-features = [ "nix-command" "flakes" ];

  system.stateVersion = "25.11";
}
```

Notice `config.hive.instance.name` - this is automatically set by nix-hive to the instance's name (`gateway`, `apps`, or `storage`). You don't need to hardcode hostnames.

## Step 2: The Gateway Configuration

Create `hosts/gateway.nix`:

```nix
{ config, lib, ... }: {
  # Two network interfaces: WAN and LAN
  networking = {
    interfaces.eth0.useDHCP = true;  # WAN - gets address from ISP
    interfaces.eth1.ipv4.addresses = [{
      address = "192.168.1.1";
      prefixLength = 24;
    }];

    nat = {
      enable = true;
      externalInterface = "eth0";
      internalInterfaces = [ "eth1" ];
    };

    firewall = {
      enable = true;
      trustedInterfaces = [ "eth1" ];
      allowedTCPPorts = [ 22 ];
    };
  };

  # DHCP server for the LAN
  services.dnsmasq = {
    enable = true;
    settings = {
      interface = "eth1";
      dhcp-range = [ "192.168.1.100,192.168.1.200,24h" ];
      dhcp-host = [
        "aa:bb:cc:dd:ee:10,192.168.1.10"   # apps
        "aa:bb:cc:dd:ee:20,192.168.1.20"   # storage
      ];
    };
  };

  boot.loader.grub.devices = [ "/dev/sda" ];
  fileSystems."/" = { device = "/dev/sda1"; fsType = "ext4"; };
}
```

## Step 3: The App Server Configuration

Create `hosts/apps.nix`:

```nix
{ config, ... }: {
  networking = {
    interfaces.eth0.ipv4.addresses = [{
      address = "192.168.1.10";
      prefixLength = 24;
    }];
    defaultGateway = "192.168.1.1";
    nameservers = [ "192.168.1.1" ];
  };

  # Run a web server
  services.caddy = {
    enable = true;
    virtualHosts."apps.home".extraConfig = ''
      root * /var/www
      file_server
    '';
  };

  # Open the web port
  networking.firewall.allowedTCPPorts = [ 80 443 ];

  boot.loader.grub.devices = [ "/dev/sda" ];
  fileSystems."/" = { device = "/dev/sda1"; fsType = "ext4"; };
}
```

## Step 4: The Storage Server Configuration

Create `hosts/storage.nix`:

```nix
{ config, ... }: {
  networking = {
    interfaces.eth0.ipv4.addresses = [{
      address = "192.168.1.20";
      prefixLength = 24;
    }];
    defaultGateway = "192.168.1.1";
    nameservers = [ "192.168.1.1" ];
  };

  # NFS exports
  services.nfs.server = {
    enable = true;
    exports = ''
      /srv/share 192.168.1.0/24(rw,sync,no_subtree_check)
    '';
  };

  # Create the share directory
  systemd.tmpfiles.rules = [
    "d /srv/share 0755 nobody nogroup -"
  ];

  # Open NFS ports
  networking.firewall.allowedTCPPorts = [ 2049 ];

  boot.loader.grub.devices = [ "/dev/sda" ];
  fileSystems."/" = { device = "/dev/sda1"; fsType = "ext4"; };
}
```

## Step 5: Putting It Together with flake.nix

Now create the main `flake.nix` that ties everything together:

```nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.11";
    nix-hive.url = "github:swdunlop/nix-hive";
  };

  outputs = { self, nixpkgs, nix-hive, ... }: let

    # Define the configurations for each role
    # Each configuration specifies the system architecture and modules to include
    configurations = {
      gateway = {
        system = "x86_64-linux";
        modules = [
          ./modules/common.nix
          ./hosts/gateway.nix
        ];
      };

      apps = {
        system = "x86_64-linux";
        modules = [
          ./modules/common.nix
          ./hosts/apps.nix
        ];
      };

      storage = {
        system = "x86_64-linux";
        modules = [
          ./modules/common.nix
          ./hosts/storage.nix
        ];
      };
    };

    # Create the hive with our configurations
    hive = nix-hive.lib.makeHive {
      # Pass nixpkgs so configurations can access it
      inputs = { inherit nixpkgs; };

      # Default SSH settings for all instances
      defaults = {
        sshConfig = {
          User = "admin";
        };
      };

      # Define each instance
      instances = {
        gateway = {
          configuration = configurations.gateway;
          sshConfig.HostName = "192.168.1.1";
        };

        apps = {
          configuration = configurations.apps;
          sshConfig.HostName = "192.168.1.10";
        };

        storage = {
          configuration = configurations.storage;
          sshConfig.HostName = "192.168.1.20";
        };
      };
    };

  in {
    # Expose the hive for inspection
    lib.hive = hive;

    # Deployment packages (deploy, ssh, scp, sftp)
    packages = hive.packages;

    # Dev shell with all tools
    devShells = hive.devShells;
  };
}
```

## Step 6: Building and Inspecting

Before deploying, let's verify everything builds correctly.

Check the manifest:

```sh
nix eval --json .#lib.hive.manifest | jq
```

You should see output like:

```json
{
  "gateway": {
    "systemPath": "/nix/store/...-nixos-system-gateway-...",
    "sshConfig": { "User": "admin", "HostName": "192.168.1.1" }
  },
  "apps": {
    "systemPath": "/nix/store/...-nixos-system-apps-...",
    "sshConfig": { "User": "admin", "HostName": "192.168.1.10" }
  },
  "storage": {
    "systemPath": "/nix/store/...-nixos-system-storage-...",
    "sshConfig": { "User": "admin", "HostName": "192.168.1.20" }
  }
}
```

Notice that each system has a different hostname - that's `config.hive.instance.name` at work.

View the generated SSH config:

```sh
nix eval .#lib.hive.sshConfigText
```

## Step 7: Deploying

Enter the development shell:

```sh
nix develop
```

This gives you access to the `deploy`, `ssh`, `scp`, and `sftp` commands, all pre-configured for your hive.

Deploy a single instance:

```sh
deploy gateway
```

Deploy all instances:

```sh
deploy
```

Deploy without activating (useful for testing):

```sh
deploy --no-activation apps
```

SSH into an instance:

```sh
ssh apps
```

Copy files:

```sh
scp ./config.txt storage:/srv/share/
```

## Using hive.instance.name for Variation

Sometimes you want the same configuration to behave differently based on which instance it's running on. The `hive.instance.name` option makes this possible.

For example, to create multiple identical app servers:

```nix
instances = {
  apps-1 = {
    configuration = configurations.apps;
    sshConfig.HostName = "192.168.1.10";
  };
  apps-2 = {
    configuration = configurations.apps;
    sshConfig.HostName = "192.168.1.11";
  };
};
```

Each instance gets its own hostname automatically. You can also use it for conditional configuration:

```nix
# In a module
{ config, lib, ... }: {
  services.someService = {
    # Primary instance handles writes, others are read-only
    readOnly = config.hive.instance.name != "apps-1";
  };
}
```

## Next Steps

Now that you have a working hive:

- **Add more instances** - Same configuration, different addresses
- **Create role-specific modules** - Group related configuration
- **Use secrets management** - Consider agenix or sops-nix for sensitive data
- **Set up remote builds** - Build on a powerful machine, deploy from anywhere (see [manifest mode](./REFERENCE.md#manifest-mode) in the reference)

## Troubleshooting

### "Instance 'x' not found in manifest"

The instance name you're deploying doesn't match any name in your `instances` attribute set. Check spelling.

### Configuration evaluation fails

Run the NixOS evaluation directly to see detailed errors:

```sh
nix eval .#lib.hive.instances.gateway.systemPath
```

### SSH connection refused

1. Verify the instance is reachable at the `sshConfig.HostName` address
2. Check that SSH is enabled in your configuration
3. Ensure your SSH key is in the admin user's authorized keys

### Deploy fails with "permission denied"

The admin user needs passwordless sudo. Add to your common module:

```nix
security.sudo.wheelNeedsPassword = false;
```

### Changes don't take effect after deploy

The deploy script runs `switch-to-configuration switch`, which activates most changes immediately. Some changes (kernel, boot loader) require a reboot:

```sh
ssh gateway sudo reboot
```

### Deploy fails with "cannot add path '...' because it lacks a signature by a trusted key"

This occurs when copying store paths to target machines that don't trust your builder. The solution is to sign your builds and configure targets to trust your signing key.

#### Setting up a signing key on your builder

First, check if your builder already has a signing key configured:

```sh
# On NixOS, check for secret-key-files in your configuration
grep -r secret-key-files /etc/nixos/

# Or check the nix config directly
nix config show secret-key-files
```

If no signing key is configured, generate one:

```sh
# Generate a key pair (run on your builder as root)
sudo nix key generate-secret --key-name "$(hostname)-1" | sudo tee /etc/nix/secret-key > /dev/null
sudo chmod 600 /etc/nix/secret-key

# Derive the public key
sudo nix key convert-secret-to-public < /etc/nix/secret-key | sudo tee /etc/nix/public-key
```

The key name (e.g., `mybuilder-1`) appears in the public key and helps identify which builder signed a path. Including a version number allows key rotation later.

Next, configure your builder to sign paths it builds. On NixOS, add to your configuration:

```nix
nix.settings.secret-key-files = [ "/etc/nix/secret-key" ];
```

Then rebuild: `sudo nixos-rebuild switch`

For non-NixOS systems, add to `/etc/nix/nix.conf`:

```
secret-key-files = /etc/nix/secret-key
```

Then restart the nix daemon: `sudo systemctl restart nix-daemon`

#### Signing paths that were built before configuring the key

If you built your systems before setting up the signing key (or substituted them from a cache), the paths won't have your signature. Use the `sign` command to retroactively sign them:

```sh
# Sign all instances in the manifest
nix run .#sign -- --key-file /etc/nix/secret-key

# Sign specific instances
nix run .#sign -- --key-file /etc/nix/secret-key apps storage
```

Then retry deployment.

#### Configuring targets to trust your builder

Add your builder's public key to your hive's common module so all instances trust it:

```nix
nix.settings.trusted-public-keys = [
  "cache.nixos.org-1:6NCHdD59X431o0gWypbMrAURkbJ16ZPMQFGspcDShjY="
  "mybuilder-1:base64encodedpublickey=="  # contents of /etc/nix/public-key
];
```

#### Bootstrapping trust on a new instance

When converting an existing NixOS machine to hive management, you face a chicken-and-egg problem: the instance doesn't trust your builder yet, so you can't deploy the configuration that would establish trust.

Use the `--bootstrap` flag to bypass signature checking for the initial deployment:

```sh
deploy --bootstrap apps
```

This uses `nix-store --export | nix-store --import` instead of `nix copy`, which transfers store paths without requiring signature verification on the target. The deployment proceeds normally, installing your hive configuration—including the `trusted-public-keys` setting. Once complete, subsequent deployments use the normal `deploy` workflow since the target now trusts your builder's signatures.

**Note:** Bootstrap mode transfers the entire closure from your local store. Unlike `nix copy`, it cannot fetch paths from remote substituters (like cache.nixos.org), so the deployment host must have all required paths built locally. This may require more disk space, bandwidth, and time for the initial deployment.

**Alternative: local rebuild**

If bootstrap mode isn't suitable (e.g., network constraints or very large closures), you can perform the initial deployment directly on the target machine:

```sh
# Copy your flake to the target (or clone from git)
scp -r ./home-network admin@192.168.1.10:~/

# SSH in and rebuild
ssh admin@192.168.1.10
cd ~/home-network
sudo nixos-rebuild switch --flake .#apps
```

This builds locally or fetches from cache.nixos.org, avoiding the need to copy paths from your builder.
