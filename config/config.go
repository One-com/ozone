package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/pkg/errors"

	"github.com/One-com/gone/jconf"

	"github.com/One-com/ozone/tlsconf"
)

type ConfigError error

func WrapError(wrapped error) ConfigError {
	return ConfigError(errors.Wrap(wrapped, "Config error"))
}

// TLSServerConfig defines JSON for a server TLS configuration including
// a callback to augment the *tls.Config.
type TLSServerConfig struct {
	tlsconf.TLSServerConfig
	EnableExternalSNI bool // backward compatible - use "" as pluginname
	TLSPlugin         string
}

// ListenerConfig defined the JSON used to configure a listener.
type ListenerConfig struct {
	Address           string
	Port              int
	IOActivityTimeout jconf.Duration   `json:",omitempty"`
	TLS               *TLSServerConfig `json:",omitempty"`
	SocketFdName      string           `json:",omitempty"`
	SocketInheritOnly bool             `json:",omitempty"`
}

// HTTPServerConfig defines the JSON to configure a HTTP server.
type HTTPServerConfig struct {
	Listeners map[string]ListenerConfig

	// either string, or map (for a mux) or "404" for NotFound
	Handler *jconf.MandatorySubConfig

	// overrides the global Accesslog definition
	AccessLog string

	// a , separated string of return code specs: "2XX,412,5XX,404"
	Metrics string

	DisableKeepAlives bool

	ReadHeaderTimeout jconf.Duration
	IdleTimeout       jconf.Duration
	ReadTimeout       jconf.Duration
	WriteTimeout      jconf.Duration
}

// RedirectHandlerConfig is configuration for a 30X redirect handler
type RedirectHandlerConfig struct {
	Code int
	URL  string
}

// HandlerConfig specifies the type and config for a handler.
// Potentiall found in a plugin.
// All handlers can be wrapped in metrics spec specific for them.
type HandlerConfig struct {
	Type    string
	Plugin  string
	Metrics string                   `json:",omitempty"`
	Config  *jconf.OptionalSubConfig `json:",omitempty"`
}

// TLSPluginConfig defines configuration for loading and configuring
// a TLS plugin
type TLSPluginConfig struct {
	Type   string
	Plugin string
	Config *jconf.OptionalSubConfig `json:",omitempty"`
}

// MetricsConfig is the global configuration for a statsd server.
type MetricsConfig struct {
	Address     string
	Interval    jconf.Duration
	Prefix      string
	Application string
	Ident       string
}

type HTTPServersConfig map[string]HTTPServerConfig
type HandlersConfig map[string]HandlerConfig
type TLSPluginsConfig map[string]TLSPluginConfig

type LogConfig struct {
	AccessLog string
}

// Config defined JSON for the top level server config
type Config struct {
	Log          *LogConfig                `json:",omitempty"`
	HTTPServers  HTTPServersConfig        `json:"HTTP"`
	Handlers     HandlersConfig           `json:"Handlers"`
	Metrics      *MetricsConfig            `json:",omitempty"`
	SNI          *jconf.OptionalSubConfig `json:"SNI,omitempty"` // a special backwards compatible option
	TLSPlugins   TLSPluginsConfig         `json:",omitempty"`
	TLSPluginDir string                   `json:",omitempty"`
}

// Dump serialized the JSON config as configured to standard output
func (cfg *Config) Dump(dest io.Writer) {

	var out bytes.Buffer
	b, err := json.Marshal(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}

	err = json.Indent(&out, b, "", "    ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	_, err = out.Write([]byte("\n"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	out.WriteTo(dest)
}

// ParseConfigFromFile returns a pointer to a new Config object
// after parsing config file content.
func ParseConfigFromFile(filename string) (*Config, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return parseConfig(file)
}

// ParseConfigFromReadSeeker returns a pointer to a new Config object.
// The config is read from a in memory buffer.
func ParseConfigFromReadSeeker(data io.ReadSeeker) (*Config, error) {
	data.Seek(0, io.SeekStart)
	return parseConfig(data)
}

// ParseConfig Read config from the supplied io.Reader and parse it
func parseConfig(stream io.Reader) (*Config, error) {
	var config *Config
	err := jconf.ParseInto(stream, &config)
	return config, err
}
