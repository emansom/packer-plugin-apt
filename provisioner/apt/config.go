//go:generate mapstructure-to-hcl2 -type Config
//go:generate packer-sdc struct-markdown
package apt

import (
	"github.com/hashicorp/packer-plugin-sdk/common"
	"github.com/hashicorp/packer-plugin-sdk/template/config"
	"github.com/hashicorp/packer-plugin-sdk/template/interpolate"
)

type Config struct {
	common.PackerConfig `mapstructure:",squash"`
	Packages            []string `mapstructure:"packages"`
	Sources             []string `mapstructure:"sources"`
	Keys                []string `mapstructure:"keys"`
	CacheDir            string   `mapstructure:"cache_dir"`
	ctx                 interpolate.Context
}

func (c *Config) Prepare(raws ...interface{}) error {
	err := config.Decode(c, &config.DecodeOpts{
		Interpolate: true,
	}, raws...)
	if err != nil {
		return err
	}

	if c.CacheDir == "" {
		c.CacheDir = "/var/cache/apt/archives"
	}

	return nil
}
