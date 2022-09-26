# <hive/build.nix> builds a named system from the systems section of <deployment> and returns the resulting path.
# This is invoked by Nix-Hive using the paths from <hive/config.nix> like:
#  nix build -I nixpkgs=... -I deployment=... --argstr "name" ... nix/system.nix
{ name }:
let
  deployment = import <deployment>;
  systems = deployment.systems or (throw "systems not specified in deployment");
  system = systems.${name} or (throw "system ${name} could not be found");
  nixosEval = import <nixpkgs/nixos/lib/eval-config.nix>;
  configuration =
    if !builtins.hasAttr "configuration" system then
      throw "missing system configuration"
    else if !builtins.isFunction system.configuration then
      throw "system configuration should be a function"
    else
      system.configuration;
  result = nixosEval ({
    modules = [ configuration ];
    system = system.system or builtins.currentSystem;
  } // (if system ? evalConfigArgs then system.evalConfigArgs else { }));
in
result.config.system.build.toplevel
