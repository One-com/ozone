package main

import (
	"flag"
	"github.com/One-com/ozone"
	"os"
)

var (
	configFile string
)

func init() {
	flag.StringVar(&configFile, "c", "minimal.json", "Configuration file")
	flag.Parse()
}

func main() {
	err := ozone.Main(configFile)
	if err != nil {
		os.Exit(1)
	}
}
