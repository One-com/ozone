package main

import (
	"flag"
	"net/http"
	"os"

	"github.com/One-com/gone/jconf"
	"github.com/One-com/ozone/v2"
)

var (
	configFile string
)

type handlerConfig struct {
	Response string
}

func createHandler(name string, js jconf.SubConfig, handlerByName func(string) (http.Handler, error)) (h http.Handler, cleanup func() error, err error) {
	h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var cfg *handlerConfig
		err = js.ParseInto(&cfg)
		if err != nil {
			return
		}
		w.Write([]byte(cfg.Response + ": " + name + "\n"))
	})
	return
}

var staticHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Hello from the code\n"))
})

func init() {
	flag.StringVar(&configFile, "c", "handlers.json", "Configuration file")
	flag.Parse()
}

func main() {

	ozone.RegisterStaticHTTPHandler("statichandler", staticHandler)
	ozone.RegisterHTTPHandlerType("configurable", createHandler)

	err := ozone.Main(configFile)
	if err != nil {
		os.Exit(1)
	}
}
