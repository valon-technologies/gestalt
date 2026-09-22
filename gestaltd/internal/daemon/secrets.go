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

	var (
		client *managedSecretClient
		err    error
	)
	clientOnce := func() (*managedSecretClient, error) {
		if client == nil {
			client, err = newManagedSecretClient()
		}
		return client, err
	}

	switch args[0] {
	case "-h", "--help", "help":
		printSecretsUsage(os.Stdout)
		return nil
	case "create":
		return runManagedSecretWrite(args[1:], "create", clientOnce)
	case "rotate":
		return runManagedSecretWrite(args[1:], "rotate", clientOnce)
	case "list":
		return runManagedSecretList(args[1:], clientOnce)
	case "describe":
		return runManagedSecretDescribe(args[1:], clientOnce)
	case "audit":
		return runManagedSecretAudit(args[1:], clientOnce)
	case "preflight":
		return runManagedSecretPreflight(args[1:], clientOnce)
	case "retire":
		return runManagedSecretRetire(args[1:], clientOnce)
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
	writeUsageLine(w, "  audit      Show audit history for one secret")
	writeUsageLine(w, "  preflight  Check configured references against stored secrets; reads JSON from stdin")
	writeUsageLine(w, "  retire     Mark a secret retired while retaining history")
	writeUsageLine(w, "")
	writeUsageLine(w, "Commands use GESTALT_URL/GESTALT_API_KEY or credentials from `gestalt init` and `gestalt auth login`.")
	writeUsageLine(w, "Values are never accepted as command-line arguments.")
}

type managedSecretWriteArgs struct {
	name        string
	ownerApp    string
	scope       string
	description string
	reason      string
	requestID   string
	valueFile   string
	generate    bool
}

func runManagedSecretWrite(args []string, operation string, clientOnce func() (*managedSecretClient, error)) error {
	fs := flag.NewFlagSet("gestaltd secrets "+operation, flag.ContinueOnError)
	fs.Usage = func() { printManagedSecretWriteUsage(fs.Output(), operation) }
	opts := managedSecretWriteArgs{}
	fs.StringVar(&opts.ownerApp, "owner-app", "", "app that owns this secret")
	fs.StringVar(&opts.scope, "scope", "", "secret scope: app or shared")
	fs.StringVar(&opts.description, "description", "", "human-readable description")
	fs.StringVar(&opts.reason, "reason", "", "why this value is being written")
	fs.StringVar(&opts.requestID, "request-id", "", "stable request identifier for automation retries")
	fs.StringVar(&opts.valueFile, "file", "", "read exact value bytes from this file")
	fs.BoolVar(&opts.generate, "generate", false, "generate a 48-byte base64url secret and print it once")
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
		"name":        opts.name,
		"operation":   operation,
		"ownerApp":    opts.ownerApp,
		"scope":       opts.scope,
		"description": opts.description,
		"reason":      opts.reason,
		"source":      "gestaltd-cli",
		"requestId":   opts.requestID,
	}
	if opts.generate {
		body["generate"] = true
	} else {
		body["value"] = value
	}

	client, err := clientOnce()
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
	writeUsageLine(w, "  gestaltd secrets "+operation+" NAME [--owner-app APP] [--scope app|shared] [--description TEXT] --reason TEXT [--request-id ID] [--file PATH] [--generate]")
	writeUsageLine(w, "")
	writeUsageLine(w, "The value is read from a file, piped stdin, or a hidden terminal prompt.")
	writeUsageLine(w, "It is never accepted as a command-line argument.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --owner-app     App that owns this secret; required for the default app scope")
	writeUsageLine(w, "  --scope         Secret scope: app or shared")
	writeUsageLine(w, "  --description   Human-readable description")
	writeUsageLine(w, "  --reason        Why this value is being written (required)")
	writeUsageLine(w, "  --request-id    Stable request identifier for automation retries")
	writeUsageLine(w, "  --file          Read exact value bytes from PATH")
	writeUsageLine(w, "  --generate      Generate a 48-byte base64url secret and print it once")
}

func readManagedSecretValue(opts managedSecretWriteArgs) (string, error) {
	if opts.generate {
		if opts.valueFile != "" {
			return "", errors.New("--generate and --file are mutually exclusive")
		}
		return "", nil
	}
	if opts.valueFile != "" {
		raw, err := os.ReadFile(opts.valueFile)
		if err != nil {
			return "", fmt.Errorf("failed to read %s: %w", opts.valueFile, err)
		}
		return validateManagedSecretValue(raw)
	}
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
	if value == "" {
		return "", errors.New("secret value is required")
	}
	return value, nil
}

func runManagedSecretList(args []string, clientOnce func() (*managedSecretClient, error)) error {
	fs := flag.NewFlagSet("gestaltd secrets list", flag.ContinueOnError)
	fs.Usage = func() { printSimpleUsage(fs.Output(), "gestaltd secrets list [--owner-app APP]") }
	ownerApp := fs.String("owner-app", "", "filter by owning app")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if positionals := fs.Args(); len(positionals) > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(positionals, " "))
	}

	client, err := clientOnce()
	if err != nil {
		return err
	}
	var all []managedSecretSummary
	if err := client.get(context.Background(), managedSecretsAdminPath, &all); err != nil {
		return fmt.Errorf("failed to list managed secrets: %w", err)
	}
	rows := make([]managedSecretSummary, 0, len(all))
	for i := range all {
		if strings.TrimSpace(*ownerApp) == "" || all[i].OwnerApp == strings.TrimSpace(*ownerApp) {
			rows = append(rows, all[i])
		}
	}
	printManagedSecretRows(os.Stdout, rows)
	return nil
}

func runManagedSecretDescribe(args []string, clientOnce func() (*managedSecretClient, error)) error {
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

	client, err := clientOnce()
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

func runManagedSecretAudit(args []string, clientOnce func() (*managedSecretClient, error)) error {
	fs := flag.NewFlagSet("gestaltd secrets audit", flag.ContinueOnError)
	fs.Usage = func() { printSimpleUsage(fs.Output(), "gestaltd secrets audit NAME [--limit N]") }
	limit := fs.Int("limit", 50, "maximum audit rows to return")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	positionals := fs.Args()
	if len(positionals) != 1 {
		return fmt.Errorf("secret name is required")
	}
	name := positionals[0]
	if *limit < 1 || *limit > 200 {
		return fmt.Errorf("--limit must be between 1 and 200")
	}

	client, err := clientOnce()
	if err != nil {
		return err
	}
	path := fmt.Sprintf("%s/%s/audit?limit=%d", managedSecretsAdminPath, url.PathEscape(name), *limit)
	var rows []managedSecretAuditRow
	if err := client.get(context.Background(), path, &rows); err != nil {
		return fmt.Errorf("failed to list managed secret audit for %s: %w", name, err)
	}
	printManagedSecretAudit(os.Stdout, rows)
	return nil
}

func runManagedSecretPreflight(args []string, clientOnce func() (*managedSecretClient, error)) error {
	fs := flag.NewFlagSet("gestaltd secrets preflight", flag.ContinueOnError)
	fs.Usage = func() { printSimpleUsage(fs.Output(), "gestaltd secrets preflight") }
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if positionals := fs.Args(); len(positionals) > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(positionals, " "))
	}
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("failed to read preflight references from stdin: %w", err)
	}
	var references any
	if err := json.Unmarshal(raw, &references); err != nil {
		return fmt.Errorf("failed to parse preflight references: %w", err)
	}

	client, err := clientOnce()
	if err != nil {
		return err
	}
	var response managedSecretPreflightResponse
	if err := client.post(context.Background(), managedSecretsAdminPath+"/preflight", references, &response); err != nil {
		return fmt.Errorf("failed to run managed secret preflight: %w", err)
	}
	printManagedSecretPreflightResponse(os.Stdout, response)
	if !response.OK {
		return exitCodeError{code: 1}
	}
	return nil
}

func printManagedSecretPreflightResponse(w io.Writer, response managedSecretPreflightResponse) {
	status := "FAIL"
	if response.OK {
		status = "PASS"
	}
	_, _ = fmt.Fprintf(w, "Status: %s\n", status)
	for _, row := range response.Missing {
		_, _ = fmt.Fprintf(w, "Missing: %s\n", row.Name)
		if row.App != "" {
			_, _ = fmt.Fprintf(w, "  App: %s\n", row.App)
		}
		if row.Field != "" {
			_, _ = fmt.Fprintf(w, "  Field: %s\n", row.Field)
		}
	}
	for _, row := range response.Unreferenced {
		_, _ = fmt.Fprintf(w, "Unreferenced: %s\n", row.Name)
	}
}

func runManagedSecretRetire(args []string, clientOnce func() (*managedSecretClient, error)) error {
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

	client, err := clientOnce()
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
	req.Header.Set("Authorization", "Bearer "+c.Token)
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
	CreatedBy      string  `json:"createdBy"`
	UpdatedBy      string  `json:"updatedBy"`
	RetiredAt      *string `json:"retiredAt,omitempty"`
	RetiredReason  string  `json:"retiredReason,omitempty"`
}

type managedSecretDetail struct {
	managedSecretSummary
	Audit []managedSecretAuditRow `json:"audit"`
}

type managedSecretAuditRow struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	Version   int64  `json:"version"`
	Actor     string `json:"actor"`
	Reason    string `json:"reason"`
	Source    string `json:"source,omitempty"`
	RequestID string `json:"requestId,omitempty"`
	KMSKey    string `json:"kmsKey,omitempty"`
	CreatedAt string `json:"createdAt"`
	Result    string `json:"result"`
}

type managedSecretVersionSummary struct {
	Version   int64  `json:"version"`
	State     string `json:"state"`
	CreatedAt string `json:"createdAt"`
	CreatedBy string `json:"createdBy"`
	Reason    string `json:"reason"`
	KMSKey    string `json:"kmsKey,omitempty"`
}

type managedSecretWriteResponse struct {
	Secret          managedSecretSummary        `json:"secret"`
	Version         managedSecretVersionSummary `json:"version"`
	GeneratedValue  string                      `json:"generatedValue,omitempty"`
	RolloutRequired bool                        `json:"rolloutRequired"`
}

type managedSecretReference struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
	App      string `json:"app,omitempty"`
	Field    string `json:"field,omitempty"`
}

type managedSecretPreflightResponse struct {
	OK           bool                     `json:"ok"`
	Missing      []managedSecretReference `json:"missing"`
	Unreferenced []managedSecretReference `json:"unreferenced"`
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
