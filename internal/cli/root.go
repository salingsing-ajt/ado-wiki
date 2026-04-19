package cli

import (
	"context"

	"github.com/spf13/cobra"
)

func newRoot(version string) *cobra.Command {
	root := &cobra.Command{
		Use:           "wiki",
		Short:         "Sync an Azure DevOps wiki to your local filesystem",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newLoginCmd(), newLogoutCmd(), newSyncCmd())
	return root
}

func Execute(ctx context.Context, version string) error {
	return newRoot(version).ExecuteContext(ctx)
}
