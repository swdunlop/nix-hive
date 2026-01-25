{
  inputs = {
    nix-hive.url = "path:.."; # in the real world, this would be "github:swdunlop/nix-hive"
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.11";
  };

  outputs = { self, nix-hive, nixpkgs, ... }: let
    # Configuration inputs (system + modules) - used by makeHive
    configurations.www = {
      system = "x86_64-linux";
      modules = [
        {
          # Minimal bootable configuration for the example
          boot.loader.grub.devices = [ "/dev/sda" ];
          fileSystems."/" = { device = "/dev/sda1"; fsType = "ext4"; };
          system.stateVersion = "25.11";

          services.caddy.enable = true;
        }
      ];
    };

    hive = nix-hive.lib.makeHive {
      inputs = { inherit nixpkgs; };
      instances = with configurations; {
        www-1 = { configuration = www; sshConfig.HostName = "10.1.1.1"; };
        www-2 = { configuration = www; sshConfig.HostName = "10.1.1.2"; };
        www-3 = { configuration = www; sshConfig.HostName = "10.1.1.3"; };
      };
    };
  in {
    # Exposes the hive for inspection by nix eval .#lib.hive
    lib.hive = hive;

    # nix-hive provides packages for deploying and managing the hive
    packages = hive.packages;

    # it also provides a dev shell with all of the packages
    devShells = hive.devShells;
  };
}
