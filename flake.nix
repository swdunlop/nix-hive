{
  description = "NixOS configuration deployment and management for remote systems";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs }:
  let
    lib = nixpkgs.lib;

    # Build the deploy-hive command for a given system
    deployHiveForSystem = system: let
      pkgs = nixpkgs.legacyPackages.${system};
    in pkgs.buildGoModule {
      pname = "deploy-hive";
      version = "0.1.0";
      src = ./.;
      vendorHash = "sha256-Z8V1a3uJdG/lj6AP4Xly01MQSq/yBnB2/TuERrrj0o0=";
      subPackages = [ "cmd/deploy" ];
      postInstall = ''
        mv $out/bin/deploy $out/bin/deploy-hive
      '';
    };
  in {
    # Library functions for creating and managing hives
    lib = import ./lib { inherit nixpkgs; deployHive = deployHiveForSystem; };

    # The deploy-hive command for each system
    packages = lib.genAttrs lib.systems.flakeExposed (system: {
      deploy-hive = deployHiveForSystem system;
      default = deployHiveForSystem system;
    });
  };
}
