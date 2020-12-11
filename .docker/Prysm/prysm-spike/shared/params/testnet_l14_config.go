package params

import "strings"

// UseL14NetworkConfig uses the L14 specific
// network config.
func UseL14NetworkConfig() {
	cfg := BeaconNetworkConfig().Copy()
	cfg.ContractDeploymentBlock = 6872995
	cfg.ChainID = 22
	cfg.NetworkID = 22
	cfg.DepositContractAddress = strings.ToLower("0xEEBbf8e25dB001f4EC9b889978DC79B302DF9Efd")
	cfg.BootstrapNodes = []string{
		//"enr:-Ku4QOA5OGWObY8ep_x35NlGBEj7IuQULTjkgxC_0G1AszqGEA0Wn2RNlyLFx9zGTNB1gdFBA6ZDYxCgIza1uJUUOj4Dh2F0dG5ldHOIAAAAAAAAAACEZXRoMpDVTPWXAAAgCf__________gmlkgnY0gmlwhDQPSjiJc2VjcDI1NmsxoQM6yTQB6XGWYJbI7NZFBjp4Yb9AYKQPBhVrfUclQUobb4N1ZHCCIyg",
		//"enr:-Ku4QOksdA2tabOGrfOOr6NynThMoio6Ggka2oDPqUuFeWCqcRM2alNb8778O_5bK95p3EFt0cngTUXm2H7o1jkSJ_8Dh2F0dG5ldHOIAAAAAAAAAACEZXRoMpDVTPWXAAAgCf__________gmlkgnY0gmlwhDaa13aJc2VjcDI1NmsxoQKdNQJvnohpf0VO0ZYCAJxGjT0uwJoAHbAiBMujGjK0SoN1ZHCCIyg",
	}
	OverrideBeaconNetworkConfig(cfg)
}

// UsePyrmontConfig sets the main beacon chain
// config for Pyrmont.
func UseL14Config() {
	beaconConfig = L14Config()
}

// L14 config defines the config for the
// L14 testnet.
func L14Config() *BeaconChainConfig {
	cfg := MainnetConfig().Copy()
	// TODO: you've got to pump this numbers up
	cfg.MinGenesisActiveValidatorCount = 2
	// Dec 1, 2020, 12pm UTC.
	cfg.MinGenesisTime = 1606824000
	cfg.GenesisDelay = 0
	cfg.NetworkName = "l14"
	cfg.GenesisDelay = 0
	cfg.SecondsPerETH1Block = 5
	return cfg
}
