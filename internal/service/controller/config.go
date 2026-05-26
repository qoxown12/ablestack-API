package controller

import (
	"os"

	"github.com/goccy/go-json"
)

type Config struct {
	Neighbor []TypeNeighbor `json:"neighbor"`
}

const (
	defaultConfigFile       = "/etc/ablestack/config.json"
	developmentConfigFile   = "configs/config.json"
	defaultConfigFileEnvKey = "CUBE_CONFIG_PATH"
)

func configPath() string {
	if v := os.Getenv(defaultConfigFileEnvKey); v != "" {
		return v
	}
	if _, err := os.Stat(defaultConfigFile); err == nil {
		return defaultConfigFile
	}
	if _, err := os.Stat(developmentConfigFile); err == nil {
		return developmentConfigFile
	}
	return defaultConfigFile
}

func SaveConfig() {
	config := Config{
		Neighbor: controller.Neighbor.Neighbors,
	}

	fc, err := os.OpenFile(configPath(), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return
	}
	defer fc.Close()

	strconfig, err := json.Marshal(config)

	_, err = fc.Write(strconfig)
	if err != nil {
		return
	}

}
