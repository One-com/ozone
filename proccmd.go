package ozone

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/One-com/gone/daemon"
	"github.com/One-com/gone/log"
	"github.com/One-com/gone/sd"
)

var procControl = &procCommand{}

// ---------------------------------------------------------------
// A simple control socket command controlling the daemon process

type procCommand struct{}

func (p *procCommand) ShortUsage() (syntax, comment string) {
	syntax = "[reload|respawn|kill|stop <timeout seconds>]"
	comment = "control the daemon process"
	return
}

func (p *procCommand) Usage(cmd string, w io.Writer) {
	fmt.Fprintln(w, cmd, "control the process")
}

func (p *procCommand) Invoke(ctx context.Context, w io.Writer, cmd string, args []string) (async func(), persistent string, err error) {
	cmd = args[0]
	switch cmd {
	case "reload":
		onSignalReload()
	case "kill":
		onSignalExit()
	case "stop":
		var timeout time.Duration
		if len(args) > 1 && args[1] != "" {
			var to int
			to, err = strconv.Atoi(args[1])
			if err != nil {
				return
			}
			timeout = time.Second * time.Duration(to)
		}
		log.Printf("Graceful Exit - timeout: %s", timeout.String())
		sd.Notify(0, "STOPPING=1")
		daemon.ExitGracefulWithTimeout(timeout)
	case "respawn":
		onSignalRespawn()
	default:
		fmt.Fprintln(w, "Unknown action")
	}
	return
}
