package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/nhatnam1507/social-mcp/internal/browser"
	"github.com/nhatnam1507/social-mcp/internal/oauth"
	"github.com/nhatnam1507/social-mcp/internal/service"
)

// loginHosts lists the hosts each platform's login URL may point to.
var loginHosts = map[string][]string{
	"youtube":   {"accounts.google.com"},
	"tiktok":    {"www.tiktok.com"},
	"facebook":  {"www.facebook.com"},
	"instagram": {"www.facebook.com"},
}

// authPoll and authTimeout control how account add waits; shortened in tests.
var (
	authPoll    = time.Second
	authTimeout = 10 * time.Minute
)

func accountCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "account", Short: "Connect and manage platform accounts"}
	cmd.AddCommand(accountAddCmd(), accountListCmd(), accountRemoveCmd(), accountReauthCmd())
	return cmd
}

func accountAddCmd() *cobra.Command {
	var noBrowser bool
	cmd := &cobra.Command{
		Use:       "add <youtube|tiktok|facebook|instagram>",
		Short:     "Connect accounts by signing in through the browser",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"youtube", "tiktok", "facebook", "instagram"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return login(cmd, strings.ToLower(args[0]), "", noBrowser)
		},
	}
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "only print the login URL")
	return cmd
}

func accountReauthCmd() *cobra.Command {
	var noBrowser bool
	cmd := &cobra.Command{
		Use:   "reauth <account-id>",
		Short: "Sign in again for an account whose authorization expired",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			h, err := home()
			if err != nil {
				return err
			}
			accs, err := dialService(h).ListAccounts("")
			if err != nil {
				return err
			}
			for _, a := range accs {
				if a.ID == args[0] {
					return login(cmd, a.Platform, a.ID, noBrowser)
				}
			}
			return fmt.Errorf("no account %s (see `social-mcp account list`)", args[0])
		},
	}
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "only print the login URL")
	return cmd
}

func login(cmd *cobra.Command, platform, reauthID string, noBrowser bool) error {
	h, err := home()
	if err != nil {
		return err
	}
	svc := dialService(h)
	start, err := svc.BeginAuth(platform, reauthID)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	opened := !noBrowser && browser.Open(start.AuthURL, loginHosts[platform])
	if opened {
		fmt.Fprintln(out, "Opened your browser to sign in. If it did not open, visit:")
	} else {
		fmt.Fprintln(out, "Open this URL in your browser to sign in:")
	}
	fmt.Fprintf(out, "\n  %s\n\nWaiting for the login to finish (Ctrl+C to abort)...\n", start.AuthURL)

	deadline := time.Now().Add(authTimeout)
	for time.Now().Before(deadline) {
		time.Sleep(authPoll)
		v, err := svc.AuthSession(start.SessionID)
		if err != nil {
			return err
		}
		switch v.State {
		case oauth.StatusSucceeded:
			fmt.Fprintf(out, "Connected %d account(s):\n", len(v.Accounts))
			printAccounts(out, v.Accounts)
			return nil
		case oauth.StatusFailed:
			return fmt.Errorf("login failed: %s", v.Error)
		case oauth.StatusExpired:
			return errors.New("login expired; run the command again")
		}
	}
	return errors.New("timed out waiting for the login to finish")
}

func accountListCmd() *cobra.Command {
	var platform string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List connected accounts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			h, err := home()
			if err != nil {
				return err
			}
			accs, err := dialService(h).ListAccounts(platform)
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(cmd.OutOrStdout(), accs)
			}
			if len(accs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no accounts connected; run `social-mcp account add <platform>`")
				return nil
			}
			printAccounts(cmd.OutOrStdout(), accs)
			return nil
		},
	}
	cmd.Flags().StringVar(&platform, "platform", "", "only this platform")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func printAccounts(w io.Writer, accs []service.AccountView) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tPLATFORM\tNAME\tHEALTH\tRESTRICTIONS")
	for _, a := range accs {
		r := strings.Join(a.Capabilities.Restrictions, ",")
		if r == "" {
			r = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", a.ID, a.Platform, a.DisplayName, a.Health, r)
	}
	tw.Flush()
	for _, a := range accs {
		if a.HealthDetail != "" {
			fmt.Fprintf(w, "  %s: %s\n", a.ID, a.HealthDetail)
		}
	}
}

func accountRemoveCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "remove <account-id>",
		Short: "Remove a connected account and its stored tokens",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			h, err := home()
			if err != nil {
				return err
			}
			if !yes {
				fmt.Fprintf(cmd.OutOrStdout(), "Remove account %s and delete its stored tokens? [y/N] ", args[0])
				answer, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
					fmt.Fprintln(cmd.OutOrStdout(), "aborted")
					return nil
				}
			}
			if err := dialService(h).RemoveAccount(args[0]); err != nil {
				if errors.Is(err, service.ErrNotFound) {
					return fmt.Errorf("no account %s", args[0])
				}
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed %s. To revoke the app's access on the platform too, remove it in the platform's connected-apps settings.\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	return cmd
}
