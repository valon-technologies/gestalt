package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

const (
	managedSecretsAdminPath  = "/admin/api/v1/managed-secrets"
	managedSecretMaxBytes    = 64 * 1024
	managedSecretHTTPTimeout = 30 * time.Second
)

type managedSecretClient struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

func runSecrets(args []string) error {
	if len(args) == 0 {
		printSecretsUsage(os.Stderr)
		return flag.ErrHelp
	}

	switch args[0] {
	case "-h", "--help", "help":
		printSecretsUsage(os.Stdout)
		return nil
	case "create":
		return runManagedSecretWrite(args[1:], "create")
	case "rotate":
		return runManagedSecretWrite(args[1:], "rotate")
	case "list":
		return runManagedSecretList(args[1:])
	case "describe":
		return runManagedSecretDescribe(args[1:])
	case "retire":
		return runManagedSecretRetire(args[1:])
	default:
		return fmt.Errorf("unknown secrets command %q", args[0])
	}
}

func printSecretsUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd secrets <command> [flags]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Commands:")
	writeUsageLine(w, "  create     Create a new managed secret version; fails if the name already exists")
	writeUsageLine(w, "  rotate     Rotate an existing managed secret by creating a new version")
	writeUsageLine(w, "  list       List managed secret metadata; values are never returned")
	writeUsageLine(w, "  describe   Show one secret's metadata and recent audit history")
	writeUsageLine(w, "  retire     Mark a secret retired while retaining history")
	writeUsageLine(w, "")
	writeUsageLine(w, "Commands use GESTALT_URL/GESTALT_API_KEY or credentials from `gestalt init` and `gestalt auth login`.")
	writeUsageLine(w, "Values are never accepted as command-line arguments.")
}

type managedSecretWriteArgs struct {
	name     string
	ownerApp string
	reason   string
}

func runManagedSecretWrite(args []string, operation string) error {
	fs := flag.NewFlagSet("gestaltd secrets "+operation, flag.ContinueOnError)
	fs.Usage = func() { printManagedSecretWriteUsage(fs.Output(), operation) }
	opts := managedSecretWriteArgs{}
	fs.StringVar(&opts.ownerApp, "owner-app", "", "app that owns this secret")
	fs.StringVar(&opts.reason, "reason", "", "why this value is being written")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	positionals := fs.Args()
	if len(positionals) != 1 {
		return fmt.Errorf("secret name is required")
	}
	opts.name = positionals[0]
	if strings.TrimSpace(opts.reason) == "" {
		return fmt.Errorf("--reason is required")
	}
	value, err := readManagedSecretValue(opts)
	if err != nil {
		return err
	}

	body := map[string]any{
		"name":      opts.name,
		"operation": operation,
		"ownerApp":  opts.ownerApp,
		"reason":    opts.reason,
		"source":    "gestaltd-cli",
	}
	if value == "" {
		body["generate"] = true
	} else {
		body["value"] = value
	}

	client, err := newManagedSecretClient()
	if err != nil {
		return err
	}
	var response managedSecretWriteResponse
	if err := client.post(context.Background(), managedSecretsAdminPath, body, &response); err != nil {
		return fmt.Errorf("failed to write managed secret: %w", err)
	}
	printManagedSecretWriteResponse(os.Stdout, response)
	return nil
}

func printManagedSecretWriteResponse(w io.Writer, response managedSecretWriteResponse) {
	_, _ = fmt.Fprintf(w, "Secret: %s\n", response.Secret.Name)
	if response.Secret.OwnerApp != "" {
		_, _ = fmt.Fprintf(w, "Owner app: %s\n", response.Secret.OwnerApp)
	}
	_, _ = fmt.Fprintf(w, "Version: %d\n", response.Version.Version)
	_, _ = fmt.Fprintf(w, "Created at: %s\n", response.Version.CreatedAt)
	if response.GeneratedValue != "" {
		_, _ = fmt.Fprintln(w, "Generated value (shown once):")
		_, _ = fmt.Fprintln(w, response.GeneratedValue)
	}
	if response.RolloutRequired {
		_, _ = fmt.Fprintln(w, "Rollout required: yes")
		_, _ = fmt.Fprintln(w, "Gestalt resolves this secret during startup; roll out the runtime to load the new version.")
	} else {
		_, _ = fmt.Fprintln(w, "Rollout required: no")
	}
}

func printManagedSecretWriteUsage(w io.Writer, operation string) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd secrets "+operation+" NAME --reason TEXT")
	writeUsageLine(w, "")
	writeUsageLine(w, "A value is read from piped stdin or a hidden terminal prompt.")
	writeUsageLine(w, "With no value, the server generates a secret and prints it once.")
	writeUsageLine(w, "Values are never accepted as command-line arguments.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --owner-app     App that owns this app-scoped secret")
	writeUsageLine(w, "  --reason        Why this value is being written (required)")
}

func readManagedSecretValue(opts managedSecretWriteArgs) (string, error) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		first, err := readManagedSecretPassword("Secret value: ")
		if err != nil {
			return "", err
		}
		second, err := readManagedSecretPassword("Confirm secret value: ")
		if err != nil {
			return "", err
		}
		if first != second {
			return "", errors.New("secret values do not match")
		}
		return validateManagedSecretValue([]byte(first))
	}
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("failed to read secret value from stdin: %w", err)
	}
	return validateManagedSecretValue(raw)
}

func validateManagedSecretValue(raw []byte) (string, error) {
	if len(raw) > managedSecretMaxBytes {
		return "", fmt.Errorf("secret value exceeds %d bytes", managedSecretMaxBytes)
	}
	value := string(raw)
	if !utf8.ValidString(value) {
		return "", errors.New("secret value must be valid UTF-8")
	}
	return value, nil
}

func runManagedSecretList(args []string) error {
	fs := flag.NewFlagSet("gestaltd secrets list", flag.ContinueOnError)
	fs.Usage = func() { printSimpleUsage(fs.Output(), "gestaltd secrets list") }
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if positionals := fs.Args(); len(positionals) > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(positionals, " "))
	}

	client, err := newManagedSecretClient()
	if err != nil {
		return err
	}
	var all []managedSecretSummary
	if err := client.get(context.Background(), managedSecretsAdminPath, &all); err != nil {
		return fmt.Errorf("failed to list managed secrets: %w", err)
	}
	printManagedSecretRows(os.Stdout, all)
	return nil
}

func runManagedSecretDescribe(args []string) error {
	fs := flag.NewFlagSet("gestaltd secrets describe", flag.ContinueOnError)
	fs.Usage = func() { printSimpleUsage(fs.Output(), "gestaltd secrets describe NAME") }
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	positionals := fs.Args()
	if len(positionals) != 1 {
		return fmt.Errorf("secret name is required")
	}
	name := positionals[0]

	client, err := newManagedSecretClient()
	if err != nil {
		return err
	}
	var response managedSecretDetail
	if err := client.get(context.Background(), managedSecretsAdminPath+"/"+url.PathEscape(name), &response); err != nil {
		return fmt.Errorf("failed to describe managed secret %s: %w", name, err)
	}
	printManagedSecretDescribeResponse(os.Stdout, response)
	return nil
}

func printManagedSecretDescribeResponse(w io.Writer, response managedSecretDetail) {
	_, _ = fmt.Fprintf(w, "Name: %s\n", response.Name)
	_, _ = fmt.Fprintf(w, "Owner app: %s\n", response.OwnerApp)
	_, _ = fmt.Fprintf(w, "Scope: %s\n", response.Scope)
	if response.Description != "" {
		_, _ = fmt.Fprintf(w, "Description: %s\n", response.Description)
	}
	_, _ = fmt.Fprintf(w, "Current version: %d\n", response.CurrentVersion)
	_, _ = fmt.Fprintf(w, "Created at: %s\n", response.CreatedAt)
	_, _ = fmt.Fprintf(w, "Updated at: %s\n", response.UpdatedAt)
	if response.RetiredAt != nil {
		_, _ = fmt.Fprintf(w, "Retired at: %s\n", *response.RetiredAt)
	}
	printManagedSecretAudit(w, response.Audit)
}

func runManagedSecretRetire(args []string) error {
	fs := flag.NewFlagSet("gestaltd secrets retire", flag.ContinueOnError)
	fs.Usage = func() { printSimpleUsage(fs.Output(), "gestaltd secrets retire NAME --reason TEXT") }
	reason := fs.String("reason", "", "why the secret is being retired")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	positionals := fs.Args()
	if len(positionals) != 1 {
		return fmt.Errorf("secret name is required")
	}
	name := positionals[0]
	if strings.TrimSpace(*reason) == "" {
		return fmt.Errorf("--reason is required")
	}

	client, err := newManagedSecretClient()
	if err != nil {
		return err
	}
	path := fmt.Sprintf("%s/%s/retire", managedSecretsAdminPath, url.PathEscape(name))
	var response managedSecretSummary
	if err := client.post(context.Background(), path, map[string]any{"reason": *reason}, &response); err != nil {
		return fmt.Errorf("failed to retire managed secret %s: %w", name, err)
	}
	_, _ = fmt.Fprintf(os.Stdout, "Retired %s\n", name)
	return nil
}

func clientLoginSupported(baseURL string) (bool, error) {
	var payload struct {
		LoginSupported bool `json:"loginSupported"`
	}
	client := &managedSecretClient{BaseURL: strings.TrimRight(baseURL, "/")}
	if err := client.get(context.Background(), "/api/v1/auth/info", &payload); err != nil {
		return false, fmt.Errorf("failed to determine authentication mode: %w", err)
	}
	return payload.LoginSupported, nil
}

func newManagedSecretClient() (*managedSecretClient, error) {
	baseURL, err := config.ResolveGestaltCLIURL()
	if err != nil {
		return nil, err
	}
	token, err := config.ResolveGestaltCLIToken()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(baseURL) == "" {
		return nil, errors.New("gestalt URL is required; set GESTALT_URL or run `gestalt init`")
	}
	if strings.TrimSpace(token) == "" {
		loginSupported, authErr := clientLoginSupported(baseURL)
		if authErr != nil {
			return nil, authErr
		}
		if loginSupported {
			return nil, errors.New("gestalt credentials are required; set GESTALT_API_KEY or run `gestalt auth login`")
		}
	}
	return &managedSecretClient{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		Token:   strings.TrimSpace(token),
	}, nil
}

func (c *managedSecretClient) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	if c == nil {
		return nil, errors.New("managed secrets client is not configured")
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return nil, err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if reader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: managedSecretHTTPTimeout}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s response: %w", method, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := strings.TrimSpace(string(respBody))
		var payload struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(respBody, &payload) == nil && strings.TrimSpace(payload.Error) != "" {
			message = strings.TrimSpace(payload.Error)
		}
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		return nil, fmt.Errorf("managed secrets API returned %d: %s", resp.StatusCode, message)
	}
	return respBody, nil
}

func (c *managedSecretClient) get(ctx context.Context, path string, out any) error {
	body, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	return decodeManagedSecretResponse(body, out)
}

func (c *managedSecretClient) post(ctx context.Context, path string, requestBody any, out any) error {
	body, err := c.do(ctx, http.MethodPost, path, requestBody)
	if err != nil {
		return err
	}
	return decodeManagedSecretResponse(body, out)
}

func decodeManagedSecretResponse(body []byte, out any) error {
	if out == nil {
		return nil
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

type managedSecretSummary struct {
	Name           string  `json:"name"`
	OwnerApp       string  `json:"ownerApp"`
	Scope          string  `json:"scope"`
	Description    string  `json:"description"`
	CurrentVersion int64   `json:"currentVersion"`
	CreatedAt      string  `json:"createdAt"`
	UpdatedAt      string  `json:"updatedAt"`
	RetiredAt      *string `json:"retiredAt,omitempty"`
}

type managedSecretDetail struct {
	managedSecretSummary
	Audit []managedSecretAuditRow `json:"audit"`
}

type managedSecretAuditRow struct {
	Action    string `json:"action"`
	Version   int64  `json:"version"`
	Actor     string `json:"actor"`
	CreatedAt string `json:"createdAt"`
}

type managedSecretVersionSummary struct {
	Version   int64  `json:"version"`
	CreatedAt string `json:"createdAt"`
}

type managedSecretWriteResponse struct {
	Secret          managedSecretSummary        `json:"secret"`
	Version         managedSecretVersionSummary `json:"version"`
	GeneratedValue  string                      `json:"generatedValue,omitempty"`
	RolloutRequired bool                        `json:"rolloutRequired"`
}

func printManagedSecretRows(w io.Writer, rows []managedSecretSummary) {
	if len(rows) == 0 {
		_, _ = fmt.Fprintln(w, "No managed secrets")
		return
	}
	_, _ = fmt.Fprintf(w, "%-40s %-30s %-10s %8s\n", "NAME", "OWNER APP", "SCOPE", "VERSION")
	for i := range rows {
		_, _ = fmt.Fprintf(w, "%-40s %-30s %-10s %8d\n", rows[i].Name, rows[i].OwnerApp, rows[i].Scope, rows[i].CurrentVersion)
	}
}

func printManagedSecretAudit(w io.Writer, rows []managedSecretAuditRow) {
	if len(rows) == 0 {
		return
	}
	_, _ = fmt.Fprintln(w, "Audit:")
	for i := range rows {
		_, _ = fmt.Fprintf(w, "  %s v%d by %s: %s\n", rows[i].CreatedAt, rows[i].Version, rows[i].Actor, rows[i].Action)
	}
}

func printSimpleUsage(w io.Writer, usage string) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  "+usage)
}

// readManagedSecretPassword reads a hidden value from a terminal.
func readManagedSecretPassword(label string) (string, error) {
	fmt.Fprint(os.Stderr, label)
	value, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(value), nil
}
