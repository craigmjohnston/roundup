// Command roundup-slack is the Slack Later source, as a program that speaks the protocol in PROTOCOL.md.
package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/craigmjohnston/roundup/plugins/slack"
	"github.com/craigmjohnston/roundup/protocol"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string, in io.Reader, out io.Writer) error {
	flags := flag.NewFlagSet("roundup-slack", flag.ContinueOnError)
	workspace := flags.String("workspace", "", "Slack subdomain, as in <workspace>.slack.com")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *workspace == "" {
		return errors.New("-workspace is required")
	}
	ws, err := slack.ParseWorkspace(*workspace)
	if err != nil {
		return err
	}
	return protocol.ServeSource(ctx, in, out, slack.NewLater(ws))
}
