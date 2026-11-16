// Command roundup-github is a GitHub source, as a program that speaks the protocol in PROTOCOL.md.
// It runs the source named by its argument: reviews or authored.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/craigmjohnston/roundup/plugins/github"
	"github.com/craigmjohnston/roundup/protocol"
	"github.com/craigmjohnston/roundup/task"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string, in io.Reader, out io.Writer) error {
	flags := flag.NewFlagSet("roundup-github", flag.ContinueOnError)
	var botLogins []string
	flags.Func("bot-login", "an account that acts for automation but appears as an ordinary user; repeat for several", func(login string) error {
		botLogins = append(botLogins, login)
		return nil
	})
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: roundup-github [-bot-login login]... reviews|authored")
	}
	var src task.Source
	switch name := flags.Arg(0); name {
	case "reviews":
		reviews := github.NewReviews()
		reviews.BotLogins = botLogins
		src = reviews
	case "authored":
		authored := github.NewAuthored()
		authored.BotLogins = botLogins
		src = authored
	default:
		return fmt.Errorf("unknown source %q; want reviews or authored", name)
	}
	return protocol.ServeSource(ctx, in, out, src)
}
