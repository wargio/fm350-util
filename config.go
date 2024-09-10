package main

import (
	"log"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Baud      int    `yaml:"baud"`
	Timeout   int    `yaml:"timeout"`
	ContextId int    `yaml:"context_id"`
	Serial    string `yaml:"serial"`
	Netdev    string `yaml:"netdev"`
	SimPin    string `yaml:"simpin"`
	Apn       string `yaml:"apn"`
	Route     bool   `yaml:"route"`
	Ntp       string `yaml:"ntp"`
}

func NewConfig() *Config {
	return &Config{
		Baud:      115200,
		Timeout:   300,
		ContextId: 5,
		Serial:    "",
		Netdev:    findNetDev(),
		SimPin:    "",
		Apn:       "",
		Route:     false,
		Ntp:       "",
	}
}

func (c *Config) Update(cfgfile string) {
	bytes, err := os.ReadFile(cfgfile)
	if err != nil {
		// if the file was not found, let's fail silently.
		// maybe has been configured via cmd line.
		return
	}

	err = yaml.Unmarshal(bytes, c)
	if err != nil {
		log.Fatal(err)
	}
	if debug {
		log.Printf("config: %+v\n", c)
	}
}
