package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/craigmjohnston/roundup/plugins/slack"
	"github.com/craigmjohnston/roundup/task"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		if errors.Is(err, task.ErrSignedOut) {
			fmt.Fprintln(os.Stderr, "slk: no valid session, run: slk auth")
		} else {
			fmt.Fprintln(os.Stderr, "slk:", err)
		}
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var workspaceName string
	root := &cobra.Command{
		Use:           "slk",
		Short:         "Read-only Slack Web API calls, as your signed-in browser session",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&workspaceName, "workspace", os.Getenv("SLK_WORKSPACE"), "workspace subdomain, as in <workspace>.slack.com (default from SLK_WORKSPACE)")

	resolve := func() (slack.Workspace, error) { return slack.ParseWorkspace(workspaceName) }

	root.AddCommand(authCmd(resolve), whoamiCmd(resolve), apiCmd(resolve), listenCmd(resolve), tasksCmd(resolve))
	return root
}

func authCmd(resolve func() (slack.Workspace, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "auth",
		Short: "Sign in to Slack in Chrome and store the session in the keychain",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ws, err := resolve()
			if err != nil {
				return err
			}
			if err := slack.Authenticate(cmd.Context(), ws); err != nil {
				return err
			}
			return printWhoAmI(cmd.Context(), ws)
		},
	}
}

func whoamiCmd(resolve func() (slack.Workspace, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show who the stored session belongs to",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ws, err := resolve()
			if err != nil {
				return err
			}
			return printWhoAmI(cmd.Context(), ws)
		},
	}
}

func apiCmd(resolve func() (slack.Workspace, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "api <method> [key=value ...]",
		Short: "Call a read-only Web API method and print the JSON response",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := resolve()
			if err != nil {
				return err
			}
			params, err := parseParams(args[1:])
			if err != nil {
				return err
			}
			body, err := slack.NewClient(ws).Call(cmd.Context(), args[0], params)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(body))
			return err
		},
	}
}

func listenCmd(resolve func() (slack.Workspace, error)) *cobra.Command {
	var d time.Duration
	cmd := &cobra.Command{
		Use:   "listen",
		Short: "Print the live events Slack pushes to this session",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ws, err := resolve()
			if err != nil {
				return err
			}
			return slack.Listen(cmd.Context(), ws, d, cmd.OutOrStdout())
		},
	}
	cmd.Flags().DurationVar(&d, "for", time.Minute, "how long to listen")
	return cmd
}

func printWhoAmI(ctx context.Context, ws slack.Workspace) error {
	id, err := slack.NewClient(ws).WhoAmI(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("%s in %s (%s)\n", id.User, id.Team, id.URL)
	return nil
}

func parseParams(pairs []string) (url.Values, error) {
	params := url.Values{}
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("parameter %q is not key=value", p)
		}
		params.Set(k, v)
	}
	return params, nil
}
