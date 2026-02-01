{ nixpkgs, deployHive }:

# makeHive: Creates a hive of managed NixOS instances
#
# Arguments (build mode - builds NixOS configurations):
#   inputs: Flake inputs to pass to NixOS configurations (must include nixpkgs)
#   defaults: Default settings applied to all instances
#     sshConfig: Default SSH configuration options (e.g., User, Port)
#   instances: An attribute set mapping instance names to instance definitions
#
# Arguments (manifest mode - uses pre-built manifest):
#   manifest: A pre-built manifest from another hive (e.g., from a build server)
#
# Instance definition (build mode only):
#   configuration: A NixOS configuration input ({ system, modules, ... })
#   sshConfig: SSH configuration options (HostName, User, Port, etc.)
#
# Returns:
#   An attribute set containing:
#     instances: The processed instances with their configurations (build mode only)
#     manifest/manifestJSON: JSON-serializable deployment info
#     sshConfigText: Combined SSH config for all instances
#     packages: Attrset of deployment packages (system -> package-name -> derivation)
#     devShells: Attrset of dev shells (system -> shell-name -> derivation)

{ inputs ? {}, defaults ? {}, instances ? {}, manifest ? null }:

let
  lib = nixpkgs.lib;

  # Determine if we're in manifest mode (using pre-built manifest) or build mode
  useManifest = manifest != null;

  # === Build Mode Logic ===

  # Default SSH config applied to all instances
  defaultSSHConfig = defaults.sshConfig or {};

  # Process a single instance, injecting hive metadata into the configuration
  processInstance = name: instance: let
    baseConfig = instance.configuration;
    sshConfig = defaultSSHConfig // (instance.sshConfig or {});

    # Create the hive metadata module
    hiveModule = {
      options.hive = {
        instance = {
          name = lib.mkOption {
            type = lib.types.str;
            default = name;
            description = "The name of this hive instance";
            readOnly = true;
          };
        };
      };
    };

    # Build the full NixOS configuration with hive metadata
    nixosConfiguration = lib.nixosSystem {
      inherit (baseConfig) system;
      modules = (baseConfig.modules or []) ++ [ hiveModule ];
      specialArgs = (baseConfig.specialArgs or {}) // {
        inherit inputs;
      };
    };
  in {
    inherit name sshConfig;
    configuration = baseConfig;

    # The system path for this instance
    systemPath = nixosConfiguration.config.system.build.toplevel;
  };

  # Process all instances (only evaluated in build mode)
  processedInstances = lib.mapAttrs processInstance instances;

  # Generate the manifest data for an instance (JSON-serializable)
  instanceManifest = name: instance: {
    systemPath = toString instance.systemPath;
    sshConfig = instance.sshConfig;
  };

  # The complete manifest mapping instance names to their deployment info (build mode)
  builtManifest = lib.mapAttrs instanceManifest processedInstances;

  # === Final Values (mode-dependent) ===

  # Use provided manifest or built manifest
  finalManifest = if useManifest then manifest else builtManifest;

  # JSON-encoded manifest
  manifestJSON = builtins.toJSON finalManifest;

  # Generate SSH config block for a single instance (works with manifest entries)
  instanceSSHConfig = name: entry: let
    options = entry.sshConfig;
    optionLines = lib.mapAttrsToList (key: value: "    ${key} ${toString value}") options;
  in ''
    Host ${name}
    ${lib.concatStringsSep "\n" optionLines}
  '';

  # Combined SSH config for all instances (uses finalManifest to work in both modes)
  sshConfigText = lib.concatStringsSep "\n" (
    lib.mapAttrsToList instanceSSHConfig finalManifest
  );

  # Generate packages for a given system
  packagesForSystem = system: let
    pkgs = nixpkgs.legacyPackages.${system};
    sshConfigFile = pkgs.writeText "hive-ssh-config" sshConfigText;
    manifestFile = pkgs.writeText "hive-manifest.json" manifestJSON;
  in {
    # JSON manifest file as a derivation
    manifest = manifestFile;

    # SSH config file as a derivation
    sshConfig = sshConfigFile;

    # SSH wrapper using the hive's SSH config
    ssh = pkgs.writeShellScriptBin "ssh" ''
      exec ${pkgs.openssh}/bin/ssh -F ${sshConfigFile} "$@"
    '';

    # SCP wrapper using the hive's SSH config
    scp = pkgs.writeShellScriptBin "scp" ''
      exec ${pkgs.openssh}/bin/scp -F ${sshConfigFile} "$@"
    '';

    # SFTP wrapper using the hive's SSH config
    sftp = pkgs.writeShellScriptBin "sftp" ''
      exec ${pkgs.openssh}/bin/sftp -F ${sshConfigFile} "$@"
    '';

    # Deploy command: wrapper around deploy-hive with manifest pre-configured
    deploy = pkgs.writeShellScriptBin "deploy" ''
      export HIVE_MANIFEST="${manifestFile}"
      exec ${deployHive system}/bin/deploy-hive "$@"
    '';

    # Sign command: signs all system paths in the manifest with a user-provided key
    sign = pkgs.writeShellScriptBin "sign" ''
      set -euo pipefail

      usage() {
        echo "Usage: sign --key-file <path-to-signing-key> [instance...]"
        echo ""
        echo "Signs system paths from the hive manifest with the specified signing key."
        echo "If no instances are specified, all instances in the manifest are signed."
        echo ""
        echo "Options:"
        echo "  --key-file, -k  Path to the Nix signing secret key file (required)"
        echo "  --help, -h      Show this help message"
        exit "''${1:-1}"
      }

      KEY_FILE=""
      INSTANCES=()

      while [[ $# -gt 0 ]]; do
        case $1 in
          --key-file|-k)
            KEY_FILE="$2"
            shift 2
            ;;
          --help|-h)
            usage 0
            ;;
          *)
            INSTANCES+=("$1")
            shift
            ;;
        esac
      done

      if [[ -z "$KEY_FILE" ]]; then
        echo "Error: --key-file is required"
        usage
      fi

      if [[ ! -f "$KEY_FILE" ]]; then
        echo "Error: Key file not found: $KEY_FILE"
        exit 1
      fi

      # Read manifest and extract system paths
      MANIFEST="${manifestFile}"

      if [[ ''${#INSTANCES[@]} -eq 0 ]]; then
        # Sign all instances
        PATHS=$(${pkgs.jq}/bin/jq -r '.[].systemPath' "$MANIFEST")
      else
        # Sign only specified instances
        PATHS=""
        for instance in "''${INSTANCES[@]}"; do
          path=$(${pkgs.jq}/bin/jq -r --arg name "$instance" '.[$name].systemPath // empty' "$MANIFEST")
          if [[ -z "$path" ]]; then
            echo "Warning: Instance '$instance' not found in manifest, skipping"
          else
            PATHS="$PATHS $path"
          fi
        done
      fi

      if [[ -z "$PATHS" ]]; then
        echo "No system paths to sign"
        exit 0
      fi

      echo "Signing system paths with key: $KEY_FILE"
      for path in $PATHS; do
        echo "  Signing: $path"
        ${pkgs.nix}/bin/nix store sign --recursive --key-file "$KEY_FILE" "$path"
      done
      echo "Done"
    '';
  };

  # Packages for all flake-exposed systems (mimics flake packages output structure)
  packages = lib.genAttrs lib.systems.flakeExposed packagesForSystem;

  # Generate dev shells for a given system
  devShellsForSystem = system: let
    pkgs = nixpkgs.legacyPackages.${system};
    systemPackages = packagesForSystem system;
  in {
    default = pkgs.mkShell {
      packages = [
        systemPackages.deploy
        systemPackages.sign
        systemPackages.ssh
        systemPackages.scp
        systemPackages.sftp
      ];
    };
  };

  # Dev shells for all flake-exposed systems (mimics flake devShells output structure)
  devShells = lib.genAttrs lib.systems.flakeExposed devShellsForSystem;

in {
  # The processed instances (only available in build mode)
  instances = if useManifest then {} else processedInstances;

  # JSON-serializable manifest of instances
  manifest = finalManifest;
  inherit manifestJSON;

  # SSH config text for all instances
  inherit sshConfigText;

  # Packages for deploying and managing instances
  inherit packages;

  # Dev shells with hive tools
  inherit devShells;

  # Metadata about the hive
  meta = {
    instanceNames = lib.attrNames finalManifest;
    instanceCount = lib.length (lib.attrNames finalManifest);
  };
}
