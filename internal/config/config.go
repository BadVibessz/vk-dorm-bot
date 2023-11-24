package config

import (
	"errors"
	"gopkg.in/yaml.v3"
	_ "gopkg.in/yaml.v3"
	"os"

	stringutils "vk-bot/pkg/utils/string"
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
	ChatID        int      `yaml:"chat-id"`
	Rooms         []Room   `yaml:"rooms"`
	DutyTimings   []string `yaml:"duty-timings"`
	CleanTimings  []string `yaml:"clean-timings"`
	Frequency     int      `yaml:"frequency"`
	Current       string   `yaml:"current"`
	CleanDay      string   `yaml:"clean-day"`
	CleanHour     string   `yaml:"clean-hour"`
	Retries       int8     `yaml:"retries"`
	RetryInterval int8     `yaml:"retry-interval"`
	Dad           int      `yaml:"dad"` // todo: overflow in 32bit systems?
	Admins        []int    `yaml:"admins"`
}

// Validate todo: use validator from tinkoff-golang-course hw 7
func (c *Config) Validate() error {

	if len(c.Rooms) == 0 {
		return errors.New("validate config: no rooms provided")
	}

	if len(c.DutyTimings) == 0 {
		return errors.New("validate config: no duty timings provided")
	}

	if len(c.CleanTimings) == 0 {
		return errors.New("validate config: no clean timings provided")
	}

	if c.CleanHour == "" {
		return errors.New("validate config: no clean hour provided")
	}

	// validate provided timings
	for _, v := range append(c.CleanTimings, append(c.DutyTimings, c.CleanHour)...) {
		if err := stringutils.ValidateTime(v); err != nil {
			return errors.New("validate config: " + err.Error())
		}
	}

	if c.Current == "" {
		return errors.New("validate config: no current provided")
	}

	if c.CleanDay == "" {
		return errors.New("validate config: no current provided")
	}

	if c.Frequency == 0 {
		return errors.New("validate config: invalid frequency provided")
	}

	for _, v := range append(c.Admins, c.Dad, c.ChatID) {
		if v < 0 {
			return errors.New("validate config: invalid ids provided")
		}
	}

	return nil
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
