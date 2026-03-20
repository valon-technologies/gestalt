package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/valon-technologies/toolshed/core/crypto"
	"github.com/valon-technologies/toolshed/internal/server"
)

const (
	defaultWebPort     = 3000
	healthPollInterval = 500 * time.Millisecond
	healthPollTimeout  = 15 * time.Second
	webShutdownTimeout = 5 * time.Second
)

func runDev(args []string) error {
	fs := flag.NewFlagSet("dev", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config file")
	apiPort := fs.Int("api-port", 0, "API server port (overrides config)")
	webPort := fs.Int("web-port", defaultWebPort, "web UI port")
	apiOnly := fs.Bool("api-only", false, "start API server without web UI")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var bootstrapArgs []string
	if *configPath != "" {
		bootstrapArgs = []string{"-config", *configPath}
	}

	env, err := setupBootstrap("dev", bootstrapArgs)
	if err != nil {
		return err
	}
	defer env.Close()

	if *apiPort != 0 {
		env.Config.Server.Port = *apiPort
	}

	if err := startPlugins(env); err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
		defer cancel()
		shutdownPlugins(ctx, env)
	}()

	srv, err := server.New(server.Config{
		Auth:        env.Result.Auth,
		Datastore:   env.Result.Datastore,
		Providers:   env.Result.Providers,
		Runtimes:    env.Result.Runtimes,
		Bindings:    env.Result.Bindings,
		Invoker:     env.Result.Invoker,
		DevMode:     env.Result.DevMode,
		StateSecret: crypto.DeriveKey(env.Config.Server.EncryptionKey),
	})
	if err != nil {
		return fmt.Errorf("creating server: %w", err)
	}

	addr := fmt.Sprintf(":%d", env.Config.Server.Port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}

	listenErr := make(chan error, 1)
	go func() {
		log.Printf("API server listening on %s", addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			listenErr <- err
		}
	}()

	if err := waitForHealth(fmt.Sprintf("http://localhost:%d/health", env.Config.Server.Port), listenErr); err != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		return err
	}

	if env.Result.DevMode {
		log.Printf("dev mode enabled — use 'Dev Login' on the login page")
	}

	var webCmd *exec.Cmd
	if !*apiOnly {
		webDir, err := findWebDir()
		if err != nil {
			log.Printf("web UI directory not found, running API only: %v", err)
		} else {
			var startErr error
			webCmd, startErr = startWebUI(webDir, env.Config.Server.Port, *webPort)
			if startErr != nil {
				log.Printf("failed to start web UI: %v", startErr)
			} else {
				log.Printf("web UI starting on http://localhost:%d", *webPort)
				log.Printf("API proxy: /api/v1/* -> http://localhost:%d", env.Config.Server.Port)
			}
		}
	}

	var serverErr error
	select {
	case err := <-listenErr:
		serverErr = fmt.Errorf("http server: %v", err)
	case <-env.Ctx.Done():
	}

	if webCmd != nil && webCmd.Process != nil {
		_ = syscall.Kill(-webCmd.Process.Pid, syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _ = webCmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(webShutdownTimeout):
			_ = syscall.Kill(-webCmd.Process.Pid, syscall.SIGKILL)
		}
	}

	log.Println("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("server shutdown: %v", err)
	}

	log.Println("shutdown complete")
	return serverErr
}

func waitForHealth(url string, listenErr <-chan error) error {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(healthPollTimeout)
	for time.Now().Before(deadline) {
		select {
		case err := <-listenErr:
			return fmt.Errorf("http server: %v", err)
		default:
		}
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(healthPollInterval)
	}
	return fmt.Errorf("server did not become healthy within %s", healthPollTimeout)
}

func findWebDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		exe, _ = os.Getwd()
	}

	candidates := []string{
		filepath.Join(filepath.Dir(exe), "..", "web"),
		"web",
		filepath.Join("toolshed", "web"),
	}

	for _, dir := range candidates {
		pkg := filepath.Join(dir, "package.json")
		if _, err := os.Stat(pkg); err == nil {
			return dir, nil
		}
	}
	return "", fmt.Errorf("could not find web/ directory with package.json")
}

func startWebUI(webDir string, apiPort, webUIPort int) (*exec.Cmd, error) {
	cmd := exec.Command("npx", "next", "dev", "--port", fmt.Sprintf("%d", webUIPort))
	cmd.Dir = webDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("TOOLSHED_API_URL=http://localhost:%d", apiPort),
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}
