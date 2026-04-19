package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/arjayads/wikivault/internal/azuredevops"
	"github.com/arjayads/wikivault/internal/config"
	"github.com/arjayads/wikivault/internal/credentials"
	"github.com/arjayads/wikivault/internal/sync"
)

const wikiYamlTemplate = `organization: "your-azure-devops-organization"
project: "Your Project Name"
wiki: "Your Project.wiki"
`

// baseURLFn is the production resolver; tests swap it out.
var baseURLFn = azuredevops.DefaultBaseURL

func newSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Download the Azure DevOps wiki into the current directory",
		RunE: func(cmd *cobra.Command, _ []string) error {
			pat, err := credentials.Get()
			if errors.Is(err, credentials.ErrNotFound) {
				return fmt.Errorf("no PAT stored — run 'wiki login' first")
			}
			if err != nil {
				return err
			}

			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			cfg, err := config.Load(cwd)
			if errors.Is(err, config.ErrNotFound) {
				if writeErr := os.WriteFile(config.Path(cwd), []byte(wikiYamlTemplate), 0o644); writeErr != nil {
					return writeErr
				}
				return fmt.Errorf("no wiki.yaml found — wrote a template to %s, edit it and re-run 'wiki sync'", config.Path(cwd))
			}
			if err != nil {
				return err
			}
			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("wiki.yaml: %w", err)
			}

			wikiName := strings.TrimSpace(cfg.Wiki)
			if wikiName == "" || wikiName == "." || wikiName == ".." || strings.ContainsAny(wikiName, `/\`) {
				return fmt.Errorf("wiki.yaml: %q is not a valid folder name for 'wiki'", cfg.Wiki)
			}
			outDir := filepath.Join(cwd, "articles", wikiName)

			client := azuredevops.NewClient(baseURLFn(cfg.Organization), pat)

			out := cmd.OutOrStdout()
			progress := func(done, total int, path string) {
				const maxPath = 60
				trimmed := path
				if len(trimmed) > maxPath {
					trimmed = "…" + trimmed[len(trimmed)-maxPath+1:]
				}
				fmt.Fprintf(out, "\r\033[2K[%d/%d] %s", done, total, trimmed)
				if done == total {
					fmt.Fprintln(out)
				}
			}

			res, err := sync.Run(cmd.Context(), sync.Options{
				Fetcher:   client,
				Project:   cfg.Project,
				Wiki:      cfg.Wiki,
				OutputDir: outDir,
				Progress:  progress,
			})
			if errors.Is(err, azuredevops.ErrUnauthorized) {
				return fmt.Errorf("Azure DevOps rejected the PAT — run 'wiki login' with a fresh token")
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "synced %d pages to %s (pruned %d stale).\n",
				res.Written, outDir, res.Deleted)
			return nil
		},
	}
}
