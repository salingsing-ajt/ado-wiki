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
			lineProgress := func(label string) func(done, total int, name string) {
				return func(done, total int, name string) {
					const maxLabel = 60
					trimmed := name
					if len(trimmed) > maxLabel {
						trimmed = "…" + trimmed[len(trimmed)-maxLabel+1:]
					}
					fmt.Fprintf(out, "\r\033[2K[%s %d/%d] %s", label, done, total, trimmed)
					if done == total {
						fmt.Fprintln(out)
					}
				}
			}

			res, err := sync.Run(cmd.Context(), sync.Options{
				Fetcher:        client,
				Project:        cfg.Project,
				Wiki:           cfg.Wiki,
				OutputDir:      outDir,
				Progress:       lineProgress("pages"),
				AttachProgress: lineProgress("attach"),
			})
			if errors.Is(err, azuredevops.ErrUnauthorized) {
				return fmt.Errorf("Azure DevOps rejected the PAT — run 'wiki login' with a fresh token")
			}
			if err != nil {
				return err
			}
			msg := fmt.Sprintf("synced %d pages, %d attachments to %s (pruned %d stale)",
				res.Written, res.Attachments, outDir, res.Deleted)
			if res.Missing > 0 {
				msg += fmt.Sprintf("; %d attachment(s) missing from repo and skipped", res.Missing)
			}
			fmt.Fprintln(cmd.OutOrStdout(), msg+".")
			return nil
		},
	}
}
