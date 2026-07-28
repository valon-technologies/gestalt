package server

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	coredata "github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/tunnel"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
	"github.com/valon-technologies/gestalt/server/services/remotemanagement"
	"github.com/valon-technologies/gestalt/server/services/remotepublish"
)

const defaultReverseLeaseDuration = 5 * time.Minute

type reverseRemoteSetup struct {
	remoteManagement   proto.RemoteManagementServer
	publisher          *remotepublish.Publisher
	frps               *tunnel.Server
	frpsHandler        http.Handler
	frpsConnectHandler http.Handler
	// groups is the publication plan computed once from config. It is passed
	// to startReversePublisher so there is a single source of truth.
	groups []remotepublish.PublicationGroup
	// publicationError is set if startReversePublisher fails.
	publicationError string
	mu               sync.Mutex
}

func setupReverseRemoteUpstream(ctx context.Context, cfg *config.Config, services *coredata.Services, authz core.AuthorizationProvider) (result *reverseRemoteSetup, err error) {
	result = &reverseRemoteSetup{}
	defer func() {
		if err != nil {
			result.shutdown(context.Background())
			result = nil
		}
	}()

	// Compute publication groups once from config. This is the single source
	// of truth for both readiness (via publicationPlan) and the publisher.
	result.groups, err = remotepublish.BuildPublicationGroups(cfg)
	if err != nil {
		return nil, err
	}

	// Only start the embedded frps + RemoteManagement service when this
	// instance may act as an upstream (i.e. when it has coredata). A typical
	// gestaltd with no remotes and no apps to publish still gets the API
	// surface so other gestaltds can register, but frps only runs when
	// RemoteRegistrations is available.
	if services == nil || services.RemoteRegistrations == nil {
		return result, nil
	}

	result.frps, err = tunnel.StartServer(ctx)
	if err != nil {
		return nil, err
	}
	result.frpsHandler = result.frps.HTTPHandler()
	result.frpsConnectHandler = result.frps.ConnectHandler()

	var identity *tunnel.Identity
	identity, err = tunnel.NewIdentity()
	if err != nil {
		return nil, err
	}

	validator := remotepublish.NewEndpointValidator(remotepublish.EndpointValidatorConfig{
		ConnectAddr:    result.frps.ConnectAddr(),
		ClientIdentity: identity,
	})

	result.remoteManagement, err = remotemanagement.New(
		services.RemoteRegistrations,
		authz,
		services.Users,
		validator,
		remotemanagement.Config{
			ServerIdentity: &proto.ServerIdentity{
				ClientSpkiSha256: identity.SPKISHA256,
			},
			Tunnel: &proto.TunnelBootstrap{
				FrpsAddress:   result.frps.ServerURL(advertisedURL(cfg)),
				LeaseDuration: durationpb.New(defaultReverseLeaseDuration),
			},
			LeaseDuration: defaultReverseLeaseDuration,
			ConnectURL:    connectURLForPod(cfg),
		},
	)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func startReversePublisher(ctx context.Context, providers *registry.ProviderMap[core.Provider], setup *reverseRemoteSetup, logger *slog.Logger) error {
	if setup == nil || len(setup.groups) == 0 {
		return nil
	}
	publisher := remotepublish.NewPublisher(remotepublish.PublisherConfig{
		Groups:    setup.groups,
		Providers: providers,
		Logger:    logger,
	})
	if err := publisher.Start(ctx); err != nil {
		setup.mu.Lock()
		setup.publicationError = err.Error()
		setup.mu.Unlock()
		return err
	}
	setup.mu.Lock()
	setup.publisher = publisher
	setup.mu.Unlock()
	return nil
}

func connectURLForPod(cfg *config.Config) string {
	podIP := strings.TrimSpace(os.Getenv("GESTALTD_POD_IP"))
	if podIP == "" {
		return ""
	}
	return "http://" + net.JoinHostPort(podIP, strconv.Itoa(cfg.Server.PublicListener().Port))
}

func advertisedURL(cfg *config.Config) string {
	if raw := strings.TrimSpace(cfg.Server.BaseURL); raw != "" {
		if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
			scheme := "wss"
			if parsed.Scheme == "http" {
				scheme = "ws"
			}
			return scheme + "://" + parsed.Host
		}
	}
	listener := cfg.Server.PublicListener()
	host := strings.TrimSpace(listener.Host)
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "localhost"
	}
	return "ws://" + net.JoinHostPort(host, strconv.Itoa(listener.Port))
}

// readinessReason returns a non-empty string while any planned publication
// group has not yet published or if publication failed to start.
func (s *reverseRemoteSetup) readinessReason() string {
	if s == nil || len(s.groups) == 0 {
		return ""
	}
	s.mu.Lock()
	pubErr := s.publicationError
	publisher := s.publisher
	s.mu.Unlock()
	if pubErr != "" {
		return "reverse publication failed to start: " + pubErr
	}
	if publisher == nil {
		return "reverse publication starting"
	}
	return publisher.ReadinessReason()
}

func (s *reverseRemoteSetup) shutdown(ctx context.Context) {
	if s == nil {
		return
	}
	s.mu.Lock()
	publisher := s.publisher
	s.mu.Unlock()
	if publisher != nil {
		publisher.Shutdown(ctx)
	}
	if s.frps != nil {
		s.frps.Close()
	}
}
