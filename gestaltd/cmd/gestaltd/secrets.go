package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/zalando/go-keyring"
	"golang.org/x/term"
)

const defaultKeychainService = "gestaltd"

func runSecrets(args []string) error {
	if len(args) == 0 {
		printSecretsUsage(os.Stderr)
		return flag.ErrHelp
	}

	switch args[0] {
	case "set":
		return runSecretsSet(args[1:])
	case "get":
		return runSecretsGet(args[1:])
	case "delete":
		return runSecretsDelete(args[1:])
	case "-h", "--help", "help":
		printSecretsUsage(os.Stderr)
		return flag.ErrHelp
	default:
		return fmt.Errorf("unknown secrets command %q", args[0])
	}
}

func runSecretsSet(args []string) error {
	fs := flag.NewFlagSet("gestaltd secrets set", flag.ContinueOnError)
	fs.Usage = func() { printSecretsSetUsage(fs.Output()) }
	service := fs.String("service", defaultKeychainService, "keychain service name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("expected exactly one secret name")
	}
	name := fs.Arg(0)

	value, err := readSecretValue()
	if err != nil {
		return err
	}
	if value == "" {
		return fmt.Errorf("secret value cannot be empty")
	}

	if err := keyring.Set(*service, name, value); err != nil {
		return fmt.Errorf("storing secret in keychain: %w", err)
	}
	fmt.Fprintf(os.Stderr, "stored secret %q in keychain (service=%q)\n", name, *service)
	return nil
}

func runSecretsGet(args []string) error {
	fs := flag.NewFlagSet("gestaltd secrets get", flag.ContinueOnError)
	fs.Usage = func() { printSecretsGetUsage(fs.Output()) }
	service := fs.String("service", defaultKeychainService, "keychain service name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("expected exactly one secret name")
	}
	name := fs.Arg(0)

	val, err := keyring.Get(*service, name)
	if err != nil {
		if err == keyring.ErrNotFound {
			return fmt.Errorf("secret %q not found in keychain (service=%q)", name, *service)
		}
		return fmt.Errorf("reading secret from keychain: %w", err)
	}
	fmt.Print(val)
	return nil
}

func runSecretsDelete(args []string) error {
	fs := flag.NewFlagSet("gestaltd secrets delete", flag.ContinueOnError)
	fs.Usage = func() { printSecretsDeleteUsage(fs.Output()) }
	service := fs.String("service", defaultKeychainService, "keychain service name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("expected exactly one secret name")
	}
	name := fs.Arg(0)

	if err := keyring.Delete(*service, name); err != nil {
		if err == keyring.ErrNotFound {
			return fmt.Errorf("secret %q not found in keychain (service=%q)", name, *service)
		}
		return fmt.Errorf("deleting secret from keychain: %w", err)
	}
	fmt.Fprintf(os.Stderr, "deleted secret %q from keychain (service=%q)\n", name, *service)
	return nil
}

func readSecretValue() (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("reading from stdin: %w", err)
		}
		return strings.TrimRight(string(data), "\n\r"), nil
	}

	fmt.Fprint(os.Stderr, "Enter secret value: ")
	data, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("reading secret: %w", err)
	}
	return string(data), nil
}

func printSecretsUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd secrets <command> [flags]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Manage secrets in the OS keychain for use with the keychain secret provider.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Commands:")
	writeUsageLine(w, "  set <name>      Store a secret (reads from stdin or prompts interactively)")
	writeUsageLine(w, "  get <name>      Print a secret value to stdout")
	writeUsageLine(w, "  delete <name>   Remove a secret from the keychain")
}

func printSecretsSetUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd secrets set [--service NAME] <name>")
	writeUsageLine(w, "")
	writeUsageLine(w, "Store a secret in the OS keychain. Reads the value from stdin (piped)")
	writeUsageLine(w, "or prompts interactively when connected to a terminal.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Example:")
	writeUsageLine(w, "  gestaltd secrets set encryption-key")
	writeUsageLine(w, "  echo 'my-value' | gestaltd secrets set my-secret")
}

func printSecretsGetUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd secrets get [--service NAME] <name>")
	writeUsageLine(w, "")
	writeUsageLine(w, "Print a secret value from the OS keychain to stdout.")
}

func printSecretsDeleteUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd secrets delete [--service NAME] <name>")
	writeUsageLine(w, "")
	writeUsageLine(w, "Remove a secret from the OS keychain.")
}
