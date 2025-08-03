{ pkgs ? import <nixpkgs> {} }:

(pkgs.callPackage ./flake.nix {}).devShells.x86_64-linux
