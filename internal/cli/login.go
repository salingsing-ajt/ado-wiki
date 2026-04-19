package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/arjayads/wikivault/internal/credentials"
)

// readPasswordFn is swapped by tests; production calls term.ReadPassword on
// the stdin file descriptor.
var readPasswordFn = func() (string, error) {
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// isTerminalFn is swapped by tests to simulate tty vs pipe.
var isTerminalFn = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

func newLoginCmd() *cobra.Command {
	var pat string
	c := &cobra.Command{
		Use:   "login",
		Short: "Store your Azure DevOps PAT in the OS keyring",
		Long: "Store your Azure DevOps PAT in the OS keyring. Provide it via " +
			"--pat, pipe it on stdin, or omit both to be prompted (preferred — " +
			"keeps the token out of shell history).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if pat == "" {
				if isTerminalFn() {
					fmt.Fprint(cmd.ErrOrStderr(), "Azure DevOps PAT: ")
					got, err := readPasswordFn()
					fmt.Fprintln(cmd.ErrOrStderr())
					if err != nil {
						return err
					}
					pat = strings.TrimSpace(got)
				} else {
					data, err := io.ReadAll(cmd.InOrStdin())
					if err != nil {
						return err
					}
					pat = strings.TrimSpace(string(data))
				}
			}
			if pat == "" {
				return fmt.Errorf("PAT is empty — pass --pat <PAT>, pipe it via stdin, or enter it at the prompt")
			}
			if err := credentials.Save(pat); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "login: PAT stored in OS keyring.")
			return nil
		},
	}
	c.Flags().StringVar(&pat, "pat", "", "Azure DevOps PAT (prefer stdin/prompt to avoid shell history)")
	return c
}
