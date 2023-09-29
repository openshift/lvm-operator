package lvmd

import (
	"fmt"
	"os"

	"github.com/topolvm/topolvm/lvmd"
	lvmdCMD "github.com/topolvm/topolvm/pkg/lvmd/cmd"
	"sigs.k8s.io/yaml"
)

type Config = lvmdCMD.Config

type DeviceClass = lvmd.DeviceClass
type ThinPoolConfig = lvmd.ThinPoolConfig

var TypeThin = lvmd.TypeThin

const DefaultFileConfigPath = "/etc/topolvm/lvmd.yaml"

func DefaultConfigurator() FileConfig {
	return NewFileConfigurator(DefaultFileConfigPath)
}

func NewFileConfigurator(path string) FileConfig {
	return FileConfig{path: path}
}

type Configurator interface {
	Load() (*Config, error)
	Save(config *Config) error
	Delete() error
}

type FileConfig struct {
	path string
}

func (c FileConfig) Load() (*Config, error) {
	cfgBytes, err := os.ReadFile(c.path)
	if os.IsNotExist(err) {
		// If the file does not exist, return nil for both
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to load config file %s: %w", c.path, err)
	} else {
		config := &Config{}
		if err = yaml.Unmarshal(cfgBytes, config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config file %s: %w", c.path, err)
		}
		return config, nil
	}
}

func (c FileConfig) Save(config *Config) error {
	out, err := yaml.Marshal(config)
	if err == nil {
		err = os.WriteFile(c.path, out, 0600)
	}
	if err != nil {
		return fmt.Errorf("failed to save config file %s: %w", c.path, err)
	}
	return nil
}

func (c FileConfig) Delete() error {
	err := os.Remove(c.path)
	if err != nil {
		return fmt.Errorf("failed to delete config file %s: %w", c.path, err)
	}
	return err
}
