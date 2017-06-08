// Package tlsconf provides standard JSON configs for server and client TLS listeners, extending
// github.com/One-com/gone/jconf
// It relies on the presence of and openssl executable to parse OpenSSL ciphers strings
// - if you use Cipher Format "openssl"
package tlsconf

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/ioutil"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/One-com/gone/jconf"
)

// To parse the output of "openssl ciphers -V" to get IANA codes for an OpenSSL cipher spec.
var openSSLCipherlineRe *regexp.Regexp

// CipherConfig specifies a group of TLS ciphers and the format of the cipher specification
// Available formats are:
// "hex": A string with space separated 16-bit hexadecimal numbers
// "openssl": An OpenSSL cipherstring (requires an openssl binary present)
type CipherConfig struct {
	Format  string
	Ciphers *jconf.MandatorySubConfig
}

// CertConfig holds file path names for x509 certificates and key PEM files.
type CertConfig struct {
	KeyPEMFile string
	CrtPEMFile string
}

// TLSClientConfig holds TLS configuration relevant for TLS clients
type TLSClientConfig struct {
	RootCAs            map[string]string
	InsecureSkipVerify bool
	Certificates       map[string]CertConfig
	CipherSuites       *CipherConfig
	MinVersion         string
	MaxVersion         string
	ServerName         string
}

// TLSServerConfig holds TLS configuration relevant for TLS servers
type TLSServerConfig struct {
	Certificates             map[string]CertConfig
	CipherSuites             *CipherConfig
	ClientCAs                map[string]string
	ClientAuthType           string
	ClientSessionCacheSize   int
	PreferServerCipherSuites bool
	MinVersion               string
	MaxVersion               string
}

func init() {

	// Regular expression for parsing output of openssl ciphers command
	// like
	// 0x00,0x3C - AES128-SHA256           TLSv1.2 Kx=RSA      Au=RSA  Enc=AES(128)  Mac=SHA256
	// multiline mode to parse entire output
	openSSLCipherlineRe =
		regexp.MustCompile("(?m)^\\s+0x(?P<high>[[:xdigit:]]{2}),0x(?P<low>[[:xdigit:]]{2}) - [-\\w]+\\s+(TLS|SSLv3)") // don't care about rest of line

}

// Translate strings to Go TLS version constants
func versionStrings2versions(strmin, strmax string) (vmin, vmax uint16, err error) {
	vmin, err = versionString2version(strmin)
	if err != nil {
		return
	}
	vmax, err = versionString2version(strmax)
	return
}

func versionString2version(str string) (v uint16, err error) {

	switch str {
	case "":
		v = 0
	case "SSLv30":
		v = tls.VersionSSL30
	case "TLSv10":
		v = tls.VersionTLS10
	case "TLSv11":
		v = tls.VersionTLS11
	case "TLSv12":
		v = tls.VersionTLS12
	default:
		err = fmt.Errorf("Unknown TLS version. Not in ( SSLv30, TLSv1[012] ): %s", str)
	}
	return
}

// GetTLSClientConfig creates a tls.Config object from configuration
func GetTLSClientConfig(cfg *TLSClientConfig) (tlsConf *tls.Config, err error) {
	if cfg == nil {
		return
	}

	var min, max uint16
	min, max, err = versionStrings2versions(cfg.MinVersion, cfg.MaxVersion)
	if err != nil {
		return
	}

	// must have ciphers
	var ciphers []uint16
	if cfg.CipherSuites != nil {
		ciphers, err = getCiphers(cfg.CipherSuites)
		if err != nil {
			return
		}
	}

	rootCaPool, err := getCaPool(cfg.RootCAs)
	if err != nil {
		return nil, err
	}

	var certificates []tls.Certificate

	// Load certificates if available
	certCount := len(cfg.Certificates)
	if certCount > 0 {
		certificates = make([]tls.Certificate, certCount)
		var i int
		for _, cert := range cfg.Certificates {
			certificates[i], err = tls.LoadX509KeyPair(cert.CrtPEMFile, cert.KeyPEMFile)
			if err != nil {
				return nil, errors.New("Unable to load certificate/key file " + cert.CrtPEMFile + " / " + cert.CrtPEMFile + " " + err.Error())
			}
			i++
		}
	}

	tlsConf = &tls.Config{
		MinVersion:         min,
		MaxVersion:         max,
		InsecureSkipVerify: cfg.InsecureSkipVerify,
		CipherSuites:       ciphers,
		Certificates:       certificates,
		RootCAs:            rootCaPool,
		ServerName:         cfg.ServerName,
	}
	return
}

// GetTLSServerConfig creates a tls.Config object from configuration
func GetTLSServerConfig(cfg *TLSServerConfig) (tlsConf *tls.Config, err error) {
	if cfg == nil {
		return
	}

	var min, max uint16
	min, max, err = versionStrings2versions(cfg.MinVersion, cfg.MaxVersion)
	if err != nil {
		return
	}

	// must have ciphers
	var ciphers []uint16
	ciphers, err = getCiphers(cfg.CipherSuites)
	if err != nil {
		return
	}

	var certificates []tls.Certificate

	// Load certificates if available, else rely on External SNI cert cache only...
	certCount := len(cfg.Certificates)
	if certCount > 0 {
		certificates = make([]tls.Certificate, certCount)
		var i int
		for _, cert := range cfg.Certificates {
			certificates[i], err = tls.LoadX509KeyPair(cert.CrtPEMFile, cert.KeyPEMFile)
			if err != nil {
				return nil, errors.New("Unable to load certificate/key file " + cert.CrtPEMFile + " / " + cert.CrtPEMFile + " " + err.Error())
			}
			i++
		}
	}

	clientCaPool, err := getCaPool(cfg.ClientCAs)
	if err != nil {
		return
	}

	var clientAuthType tls.ClientAuthType
	switch cfg.ClientAuthType {
	case "":
		fallthrough
	case "NoClientCert":
		clientAuthType = tls.NoClientCert
	case "RequestClientCert":
		clientAuthType = tls.RequestClientCert
	case "RequireAnyClientCert":
		clientAuthType = tls.RequireAnyClientCert
	case "VerifyClientCertIfGiven":
		clientAuthType = tls.VerifyClientCertIfGiven
	case "RequireAndVerifyClientCert":
		clientAuthType = tls.RequireAndVerifyClientCert
	default:
		err = fmt.Errorf("Invalid ClientAuthType: %s (See Go tls docs)", cfg.ClientAuthType)
		return
	}

	var sessionCache tls.ClientSessionCache
	if cfg.ClientSessionCacheSize > 0 {
		sessionCache = tls.NewLRUClientSessionCache(cfg.ClientSessionCacheSize)
	}
	tlsConf = &tls.Config{
		MinVersion:               min,
		MaxVersion:               max,
		CipherSuites:             ciphers,
		Certificates:             certificates,
		ClientCAs:                clientCaPool,
		ClientAuth:               clientAuthType,
		PreferServerCipherSuites: cfg.PreferServerCipherSuites,
		ClientSessionCache:       sessionCache,
	}

	tlsConf.BuildNameToCertificate()

	return
}

func getCaPool(caFiles map[string]string) (*x509.CertPool, error) {
	caPool := x509.NewCertPool()
	for _, caFile := range caFiles {
		pem, err := ioutil.ReadFile(caFile)
		if err != nil {
			return nil, errors.New("Unable to read CA file " + caFile + " : " + err.Error())
		}
		if caPool.AppendCertsFromPEM(pem) == false {
			return nil, errors.New("Unable to load certs' PEM file for CA " + caFile)
		}
	}
	return caPool, nil
}

func getCiphers(cfg *CipherConfig) (ciphers []uint16, err error) {

	ciphers = make([]uint16, 0, 20) // 20 is what some relatively sane OpenSSL cipher spec returns on Debian Jessie

	switch cfg.Format {
	case "openssl":
		var spec string

		err = cfg.Ciphers.ParseInto(&spec)
		if err != nil {
			return
		}

		//executes openssl command line program to return the valid ciphers
		// TODO: ... full path for openssl binary?
		output, e := exec.Command("openssl", "ciphers", "-V", spec).Output()
		if e != nil {
			err = e
			return
		}
		matches := openSSLCipherlineRe.FindAllSubmatch(output, -1) // unlimited matches
		if matches == nil {
			err = fmt.Errorf("CipherSpec %s resulted in no ciphers", spec)
			return
		}
		for _, match := range matches {
			high := match[1]
			low := match[2]
			cipher, e := strconv.ParseUint(string(high)+string(low), 16, 16)
			if e != nil {
				err = errors.New("Unreachable: OpenSSL TLS cipher config error") // we have already regexp parsed this as a HEX number
				return
			}
			//				cipher := stringToHex16(string(high), string(low))
			ciphers = append(ciphers, uint16(cipher))
		}
	case "hex":
		// an array of hex values (no leading "0x")
		var specstr string
		var spec []string

		err = cfg.Ciphers.ParseInto(&specstr)
		if err != nil {
			return
		}

		spec = strings.Split(specstr, " ")

		for _, str := range spec {
			c, e := strconv.ParseUint(str, 16, 16)
			if e != nil {
				err = e
				return
			}
			ciphers = append(ciphers, uint16(c))
		}

	default:
		err = fmt.Errorf("Invalid TLS Cipher spec: %s", cfg.Format)
	}

	return
}
