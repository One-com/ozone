package cache

import (
	"fmt"

	"github.com/One-com/gone/jconf"

	"github.com/One-com/ozone/rproxymod"
)

type CacheConfig struct {
	Type   string
	Config *jconf.OptionalSubConfig `json:",omitempty"`
}

func NewCache(js jconf.SubConfig) (cache rproxymod.Cache, err error) {
	var cfg *CacheConfig

	err = js.ParseInto(&cfg)
	if err != nil {
		return
	}

	if cfg == nil {
		return NewNullCache(), nil
	}

	switch cfg.Type {
	case "YBC":
		cache, err = NewYbcCache(cfg.Config)
	default:
		err = fmt.Errorf("Unknown Cache type %s", cfg.Type)
	}

	return
}
