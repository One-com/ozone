package main

import (
	"github.com/One-com/gone/jconf"
	"net/http"

	"github.com/One-com/ozone"
)

func init() {
	ozone.RegisterHTTPHandlerType("hello", initHelloHandler)
}

// Config structure to instantiate from the provided JSON config
type Config struct {
	Who string
}

func initHelloHandler(name string, js jconf.SubConfig, handlerByName func(string) (http.Handler, error)) (h http.Handler, cleanup func() error, err error) {

	var cfg *Config
	err = js.ParseInto(&cfg)
	if err != nil {
		return
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello " + cfg.Who + "\n"))
	}), nil, nil

}
