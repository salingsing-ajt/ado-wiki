package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/arjayads/wikivault/internal/credentials"
)

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the stored Azure DevOps PAT from the OS keyring",
		RunE: func(cmd *cobra.Command, _ []string) error {
			err := credentials.Delete()
			if errors.Is(err, credentials.ErrNotFound) {
				fmt.Fprintln(cmd.OutOrStdout(), "logout: no PAT was stored.")
				return nil
			}
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "logout: PAT removed from OS keyring.")
			return nil
		},
	}
}
