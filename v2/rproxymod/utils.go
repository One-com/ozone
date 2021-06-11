package rproxymod

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"os"
)

var serverHostName string

func init() {
	serverHostName, _ = os.Hostname()
}

// ServerHostName returns the os.Hostname as set by init()
func ServerHostName() string {
	return serverHostName
}

// CreateResponse is a shorthand for making simple responses
func CreateResponse(status int, body string) *http.Response {
	b := bytes.NewBufferString(body)

	response := &http.Response{
		Status:        http.StatusText(status),
		StatusCode:    status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Close:         true,
		ContentLength: int64(len(body)),
		Body:          ioutil.NopCloser(b),
	}
	response.Header = make(http.Header)
	return response
}
