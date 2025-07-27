{
  description = "A powerful terminal-based AI assistant for developers";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    gomod2nix = {
      url = "github:nix-community/gomod2nix";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.flake-utils.follows = "flake-utils";
    };
  };

  outputs = { self, nixpkgs, flake-utils, gomod2nix, ... }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
          overlays = [ gomod2nix.overlays.default ];
        };
        go-version = pkgs.go_22;
      in
      {
        packages.default = pkgs.buildGoApplication {
          pname = "opencode";
          version = "0.1.0";
          src = ./.;
          modules = ./gomod2nix.toml;
          nativeBuildInputs = [ go-version ];
        };

        devShells.default = pkgs.mkShell {
          buildInputs = [
            go-version
            pkgs.gopls
            pkgs.go-tools
            pkgs.gotools
            pkgs.go-outline
            pkgs.delve
          ];
        };

        nixosModules.default = {
          config = {
            environment.systemPackages = [ self.packages.${system}.default ];
          };
        };

        nixosConfigurations.container = nixpkgs.lib.nixosSystem {
          inherit system;
          modules = [
            self.nixosModules.default
            ({ pkgs, ... }: {
              users.users.opencode = {
                isNormalUser = true;
                extraGroups = [ "wheel" ];
              };
              networking.hostName = "opencode";
              system.stateVersion = "23.11";
            })
          ];
        };
      });
}
