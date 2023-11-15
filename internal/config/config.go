package config

import (
	"gopkg.in/yaml.v3"
	_ "gopkg.in/yaml.v3"
	"os"
)

type Member struct {
	Name string `yaml:"name"`
	Id   string `yaml:"id"`
}

type Room struct {
	Number      string   `yaml:"number"`
	Members     []Member `yaml:"members"`
	SwapPending bool     `yaml:"swap-pending"`
}

type Config struct {
	Rooms         []Room   `yaml:"rooms"`
	Timings       []string `yaml:"timings"`
	Frequency     int      `yaml:"frequency"`
	Current       string   `yaml:"current"`
	CleanDay      string   `yaml:"clean-day"`
	CleanHour     string   `yaml:"clean-hour"`
	Retries       int8     `yaml:"retries"`
	RetryInterval int8     `yaml:"retry-interval"`
	Dad           int      `yaml:"dad"` // todo: overflow in 32bit systems?
	Admins        []int    `yaml:"admins"`
}

func Load(path string) (*Config, error) {

	content, err := os.ReadFile(path)
	if err != nil {
		return &Config{}, err
	}

	var cfg Config
	err = yaml.Unmarshal(content, &cfg)
	if err != nil {
		return &Config{}, err
	}

	return &cfg, nil
}

func (c *Config) Save(path string) error {

	serialized, err := yaml.Marshal(c)
	if err != nil {
		return err
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}

	_, err = file.Write(serialized)
	if err != nil {
		return err
	}
	return nil
}
