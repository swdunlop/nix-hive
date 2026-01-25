{ nixpkgs, deployHive }:

{
  makeHive = import ./makeHive.nix { inherit nixpkgs deployHive; };
}
