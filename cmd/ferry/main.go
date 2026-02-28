// cmd/ferry/main.go
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/andrewstuart/ferry/internal/app"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "Show version")
	flag.BoolVar(showVersion, "v", false, "Show version")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `ferry — secure file transfer, terminal style

Usage:
  ferry                     Launch connection picker
  ferry <host>              Connect to SSH host
  ferry <user@host>         Connect with explicit user
  ferry <user@host:port>    Connect with explicit user and port
  ferry s3://<bucket>       Connect to S3 bucket
  ferry s3://<bucket>/path  Connect to S3 bucket at prefix

Flags:
`)
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Printf("ferry %s\n", version)
		os.Exit(0)
	}

	var host string
	if flag.NArg() > 0 {
		host = flag.Arg(0)
	}

	opts := app.Options{}
	if strings.HasPrefix(host, "s3://") {
		opts.S3URI = host
	} else {
		opts.Host = host
	}

	p := tea.NewProgram(app.NewWithOptions(opts), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "ferry: %v\n", err)
		os.Exit(1)
	}
}
