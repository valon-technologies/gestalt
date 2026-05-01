package daemon

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/valon-technologies/gestalt/server/services/providerdev"
)

type providerAttachCommandOptions struct {
	Remote      string
	RemoteToken string
}

func runProviderAttach(args []string) error {
	if len(args) == 0 {
		printProviderAttachUsage(os.Stderr)
		return flag.ErrHelp
	}

	switch args[0] {
	case "-h", "--help", "help":
		printProviderAttachUsage(os.Stderr)
		return flag.ErrHelp
	case "list":
		return runProviderAttachList(args[1:])
	case "show":
		return runProviderAttachShow(args[1:])
	case "detach":
		return runProviderAttachDetach(args[1:])
	default:
		return fmt.Errorf("unknown provider attach command %q", args[0])
	}
}

func runProviderAttachList(args []string) error {
	opts, remaining, err := parseProviderAttachFlags("list", args, printProviderAttachListUsage)
	if err != nil {
		return err
	}
	if len(remaining) > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(remaining, " "))
	}
	client, err := providerAttachClient(opts)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	attachments, err := client.ListAttachments(ctx)
	if err != nil {
		return err
	}
	printProviderAttachList(os.Stdout, attachments)
	return nil
}

func runProviderAttachShow(args []string) error {
	opts, remaining, err := parseProviderAttachFlags("show", args, printProviderAttachShowUsage)
	if err != nil {
		return err
	}
	if len(remaining) != 1 {
		return errorsForProviderAttachID("show", remaining)
	}
	client, err := providerAttachClient(opts)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	attachment, err := client.GetAttachment(ctx, remaining[0])
	if err != nil {
		return err
	}
	printProviderAttachShow(os.Stdout, attachment)
	return nil
}

func runProviderAttachDetach(args []string) error {
	opts, remaining, err := parseProviderAttachFlags("detach", args, printProviderAttachDetachUsage)
	if err != nil {
		return err
	}
	if len(remaining) != 1 {
		return errorsForProviderAttachID("detach", remaining)
	}
	client, err := providerAttachClient(opts)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.CloseSession(ctx, remaining[0]); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "Detached provider-dev attachment %s\n", remaining[0])
	return nil
}

func parseProviderAttachFlags(command string, args []string, usage func(io.Writer)) (providerAttachCommandOptions, []string, error) {
	fs := flag.NewFlagSet("gestaltd provider attach "+command, flag.ContinueOnError)
	fs.Usage = func() { usage(fs.Output()) }
	remoteFlag := fs.String("remote", "", "remote gestaltd base URL")
	remoteTokenFlag := fs.String("remote-token", "", "bearer token for the remote server")
	if err := fs.Parse(args); err != nil {
		return providerAttachCommandOptions{}, nil, err
	}
	opts := providerAttachCommandOptions{
		Remote:      *remoteFlag,
		RemoteToken: *remoteTokenFlag,
	}
	if strings.TrimSpace(opts.Remote) == "" {
		return providerAttachCommandOptions{}, nil, errorsForProviderAttachRemote(command)
	}
	return opts, fs.Args(), nil
}

func providerAttachClient(opts providerAttachCommandOptions) (providerdev.Client, error) {
	token, err := resolveProviderAttachToken(opts)
	if err != nil {
		return providerdev.Client{}, err
	}
	return providerdev.Client{
		BaseURL: opts.Remote,
		Token:   token,
	}, nil
}

func resolveProviderAttachToken(opts providerAttachCommandOptions) (string, error) {
	return resolveProviderRemoteTokenWithErrors(opts.Remote, opts.RemoteToken, providerRemoteTokenErrors{
		AuthMissing:              providerAttachAuthMissingError,
		StoredCredentialUnscoped: providerAttachStoredCredentialUnscopedError,
		StoredCredentialMismatch: providerAttachStoredCredentialMismatchError,
		StoredCredentialMissingToken: func(credentialPath string) error {
			return fmt.Errorf("stored Gestalt CLI credential in %s is missing api_token; pass --remote-token with a user API token for this server", credentialPath)
		},
	})
}

func printProviderAttachList(w io.Writer, attachments []providerdev.AttachmentInfo) {
	if len(attachments) == 0 {
		_, _ = fmt.Fprintln(w, "No provider-dev attachments found.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ATTACH ID\tPROVIDERS\tUI\tLAST SEEN\tIDLE TIMEOUT")
	for _, attachment := range attachments {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%ds\n",
			attachment.AttachID,
			providerAttachProviderNames(attachment.Providers),
			providerAttachUIProviders(attachment.Providers),
			formatProviderAttachTime(attachment.LastSeenAt),
			attachment.IdleTimeoutSeconds,
		)
	}
	_ = tw.Flush()
}

func printProviderAttachShow(w io.Writer, attachment *providerdev.AttachmentInfo) {
	if attachment == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "Attach ID: %s\n", attachment.AttachID)
	_, _ = fmt.Fprintf(w, "Created: %s\n", formatProviderAttachTime(attachment.CreatedAt))
	_, _ = fmt.Fprintf(w, "Last seen: %s\n", formatProviderAttachTime(attachment.LastSeenAt))
	_, _ = fmt.Fprintf(w, "Idle timeout: %ds\n", attachment.IdleTimeoutSeconds)
	_, _ = fmt.Fprintln(w, "Providers:")
	if len(attachment.Providers) == 0 {
		_, _ = fmt.Fprintln(w, "  - none")
		return
	}
	for _, provider := range attachment.Providers {
		_, _ = fmt.Fprintf(w, "  - %s", provider.Name)
		if strings.TrimSpace(provider.Source) != "" {
			_, _ = fmt.Fprintf(w, " source=%s", provider.Source)
		}
		if provider.UI {
			uiPath := strings.TrimSpace(provider.UIPath)
			if uiPath == "" {
				uiPath = "<mounted>"
			}
			_, _ = fmt.Fprintf(w, " ui=%s", uiPath)
		}
		_, _ = fmt.Fprintln(w)
	}
}

func providerAttachProviderNames(providers []providerdev.AttachmentProviderInfo) string {
	names := make([]string, 0, len(providers))
	for _, provider := range providers {
		if name := strings.TrimSpace(provider.Name); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "-"
	}
	slices.Sort(names)
	return strings.Join(names, ",")
}

func providerAttachUIProviders(providers []providerdev.AttachmentProviderInfo) string {
	names := make([]string, 0, len(providers))
	for _, provider := range providers {
		if !provider.UI {
			continue
		}
		name := strings.TrimSpace(provider.Name)
		if name == "" {
			continue
		}
		if path := strings.TrimSpace(provider.UIPath); path != "" {
			name += ":" + path
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return "-"
	}
	slices.Sort(names)
	return strings.Join(names, ",")
}

func formatProviderAttachTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.UTC().Format(time.RFC3339)
}

func errorsForProviderAttachID(command string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("gestaltd provider attach %s requires ATTACH_ID", command)
	}
	return fmt.Errorf("unexpected arguments: %s", strings.Join(args[1:], " "))
}

func errorsForProviderAttachRemote(command string) error {
	return fmt.Errorf("gestaltd provider attach %s requires --remote URL", command)
}

func providerAttachAuthMissingError(remoteOrigin string) error {
	return fmt.Errorf(`provider attach requires authentication.

Remote server:
  %s

No --remote-token or %s was provided, and no stored Gestalt CLI credential for this server was found.

Pass a user API token for this server:

  gestaltd provider attach list --remote %s --remote-token <token>

You can also set %s for this command`, remoteOrigin, gestaltAPIKeyEnv, remoteOrigin, gestaltAPIKeyEnv)
}

func providerAttachStoredCredentialUnscopedError(remoteOrigin, credentialPath string) error {
	return fmt.Errorf(`provider attach could not use the stored Gestalt CLI credential.

Remote server:
  %s

Stored credential:
  server: <missing>
  file: %s

The stored credential does not record which Gestalt server it belongs to, so it was not sent.

Pass a user API token for this server:

  gestaltd provider attach list --remote %s --remote-token <token>`, remoteOrigin, credentialPath, remoteOrigin)
}

func providerAttachStoredCredentialMismatchError(remoteOrigin, storedOrigin, credentialPath string) error {
	return fmt.Errorf(`provider attach could not use the stored Gestalt CLI credential.

Remote server:
  %s

Stored credential:
  server: %s
  file: %s

The stored credential is scoped to a different Gestalt server, so it was not sent.

Pass a user API token for this server:

  gestaltd provider attach list --remote %s --remote-token <token>

You can also set %s for this command`, remoteOrigin, storedOrigin, credentialPath, remoteOrigin, gestaltAPIKeyEnv)
}

func printProviderAttachUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd provider attach <command> [flags]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Commands:")
	writeUsageLine(w, "  list       List your remote provider-dev attachments")
	writeUsageLine(w, "  show       Show one remote provider-dev attachment")
	writeUsageLine(w, "  detach     Detach one remote provider-dev attachment")
	writeUsageLine(w, "")
	writeUsageLine(w, "Run these against the same remote gestaltd URL used by provider dev --remote.")
}

func printProviderAttachListUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd provider attach list --remote URL [--remote-token TOKEN]")
	writeUsageLine(w, "")
	writeUsageLine(w, "List remote provider-dev attachments owned by the authenticated caller.")
	writeProviderAttachFlagsUsage(w)
}

func printProviderAttachShowUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd provider attach show --remote URL [--remote-token TOKEN] ATTACH_ID")
	writeUsageLine(w, "")
	writeUsageLine(w, "Show one remote provider-dev attachment owned by the authenticated caller.")
	writeProviderAttachFlagsUsage(w)
}

func printProviderAttachDetachUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd provider attach detach --remote URL [--remote-token TOKEN] ATTACH_ID")
	writeUsageLine(w, "")
	writeUsageLine(w, "Detach one remote provider-dev attachment owned by the authenticated caller.")
	writeProviderAttachFlagsUsage(w)
}

func writeProviderAttachFlagsUsage(w io.Writer) {
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --remote        Remote gestaltd base URL")
	writeUsageLine(w, "  --remote-token  Bearer token for the remote server (defaults to GESTALT_API_KEY or matching stored Gestalt CLI credential)")
}
