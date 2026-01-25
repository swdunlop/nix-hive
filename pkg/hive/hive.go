package hive

// Manifest maps instance names to their deployment information.
type Manifest map[string]Instance

// Instance describes a single NixOS instance in the hive.
type Instance struct {
	// SystemPath is the Nix store path to the NixOS system configuration.
	SystemPath string `json:"systemPath"`

	// SSHConfig contains SSH connection options for this instance.
	SSHConfig map[string]string `json:"sshConfig"`
}
