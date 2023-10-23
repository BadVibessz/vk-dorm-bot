package config

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
)

type Member struct {
	Name string `yaml:"name"`
	Id   string `yaml:"id"`
}

type Room struct {
	Number  string   `yaml:"number"`
	Members []Member `yaml:"members"`
}

type Config struct {
	Rooms []Room `yaml:"rooms"`
}

func Load(path string) (Config, error) {

	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer func(f *os.File) {
		err := f.Close()
		if err != nil {
			// todo:
			fmt.Println(err)
		}
	}(f)

	var cfg Config
	decoder := yaml.NewDecoder(f)
	err = decoder.Decode(&cfg)
	if err != nil {
		// todo:
		return Config{}, err
	}
	return cfg, nil
}
