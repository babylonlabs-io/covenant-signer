package config

import "math"

type SignerAppConfig struct {
	MaxStakingTransactionHeight uint32 `mapstructure:"max-staking-transaction-height"`
}

type ParsedSignerAppConfig struct {
	MaxStakingTransactionHeight uint32
}

func (c *SignerAppConfig) Parse() (*ParsedSignerAppConfig, error) {
	// TODO Add some validations
	return &ParsedSignerAppConfig{
		MaxStakingTransactionHeight: c.MaxStakingTransactionHeight,
	}, nil
}

func DefaultSignerAppConfig() *SignerAppConfig {
	return &SignerAppConfig{
		MaxStakingTransactionHeight: math.MaxUint32,
	}
}
