package config

import (
	"fmt"
	"math"
)

type SignerAppConfig struct {
	MaxStakingTransactionHeight int `mapstructure:"max-staking-transaction-height"`
}

type ParsedSignerAppConfig struct {
	MaxStakingTransactionHeight uint32
}

func (c *SignerAppConfig) Parse() (*ParsedSignerAppConfig, error) {
	if c.MaxStakingTransactionHeight < 0 {
		return nil, fmt.Errorf("max staking transaction height is too small. Min value is 0")
	}

	if c.MaxStakingTransactionHeight > math.MaxUint32 {
		return nil, fmt.Errorf("max staking transaction height is too large. Max value is %d", math.MaxUint32)
	}

	return &ParsedSignerAppConfig{
		MaxStakingTransactionHeight: uint32(c.MaxStakingTransactionHeight),
	}, nil
}

func DefaultSignerAppConfig() *SignerAppConfig {
	return &SignerAppConfig{
		MaxStakingTransactionHeight: math.MaxUint32,
	}
}
