package main

import (
	"context"
	"dagger/discord/internal/dagger"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel/codes"
	"golang.org/x/sync/errgroup"
)

func New(
	token *dagger.Secret,
) *Discord {
	return &Discord{
		Token: token,
	}
}

type Discord struct {
	Token *dagger.Secret
}

func (m *Discord) Servers(ctx context.Context) ([]*Server, error) {
	out, err := m.Exporter().Run(ctx, []string{"guilds"})
	if err != nil {
		return nil, err
	}

	lines := strings.Split(out, "\n")
	var servers []*Server

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		parts := strings.SplitN(line, " | ", 2)
		if len(parts) != 2 {
			continue
		}

		discordId := strings.TrimSpace(parts[0])
		name := strings.TrimSpace(parts[1])

		server := &Server{
			Account:   m,
			DiscordId: discordId,
			Name:      name,
		}
		servers = append(servers, server)
	}

	return servers, nil
}

func (m *Discord) Exporter() *Exporter {
	return &Exporter{
		Account: m,
		State: dag.Container().
			From("tyrrrz/discordchatexporter:stable").
			WithSecretVariable("DISCORD_TOKEN", m.Token),
	}
}

type Exporter struct {
	Account *Discord
	State   *dagger.Container
}

func (e *Exporter) WithCommand(args []string) *Exporter {
	e.State = e.State.
		WithExec(args, dagger.ContainerWithExecOpts{
			UseEntrypoint: true,
		})
	return e
}

func (e *Exporter) Run(ctx context.Context, args []string) (string, error) {
	return e.WithCommand(args).State.Stdout(ctx)
}

func (m *Discord) Server(ctx context.Context, name string) (*Server, error) {
	servers, err := m.Servers(ctx)
	if err != nil {
		return nil, err
	}
	for _, server := range servers {
		if server.Name == name {
			return &Server{
				Account:   m,
				DiscordId: server.DiscordId,
			}, nil
		}
	}
	return nil, fmt.Errorf("no available server named %q", name)
}

type Server struct {
	Account   *Discord
	DiscordId string
	Name      string
}

func (s *Server) Export(
	ctx context.Context,
	// +optional
	filter string,
	// +default=1
	parallel int,
) (*dagger.Directory, error) {
	channels, err := s.Channels(ctx, filter)
	if err != nil {
		return nil, err
	}
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(parallel)

	// Pre-allocate directories array
	dirs := make([]*dagger.Directory, len(channels))

	for i, channel := range channels {
		i, channel := i, channel // capture loop variables
		g.Go(func() error {
			ctx, span := Tracer().Start(ctx, fmt.Sprintf("exporting channel %q", channel.Name))
			// FIXME: add boilerplate for passing error in custom span
			defer span.End()
			dir, err := channel.Export(ctx, 1).Sync(ctx)
			if err != nil {
				span.SetStatus(codes.Error, "export failed: "+err.Error())
				// Ignore failed exports (FIXME: make this configurable)
				return nil
			}
			dirs[i] = dir
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	mergedDir := dag.Directory()
	for _, dir := range dirs {
		mergedDir = mergedDir.WithDirectory(".", dir)
	}
	return mergedDir, nil
}

func (s *Server) Channels(
	ctx context.Context,
	// +optional
	filter string,
) ([]*Channel, error) {
	output, err := s.Account.Exporter().Run(ctx, []string{
		"channels", "-g", s.DiscordId,
	})
	if err != nil {
		return nil, err
	}

	lines := strings.Split(output, "\n")
	var channels []*Channel

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		parts := strings.SplitN(line, " | ", 2)
		if len(parts) != 2 {
			continue
		}

		discordId := strings.TrimSpace(parts[0])
		name := strings.TrimSpace(parts[1])

		channel := &Channel{
			Server:    s,
			DiscordId: discordId,
			Name:      name,
		}
		if filter != "" {
			matched, err := regexp.MatchString(filter, name)
			if err != nil {
				return nil, fmt.Errorf("invalid filter regexp %q: %w", filter, err)
			}
			if !matched {
				continue
			}
		}
		channels = append(channels, channel)
	}

	return channels, nil
}

func (s *Server) Channel(ctx context.Context, name string) (*Channel, error) {
	channels, err := s.Channels(ctx, "")
	if err != nil {
		return nil, err
	}
	for _, channel := range channels {
		if channel.Name == name {
			return &Channel{
				Server:    s,
				DiscordId: channel.DiscordId,
				Name:      channel.Name,
			}, nil
		}
	}
	return nil, fmt.Errorf("no available channel named %q", name)
}

type Channel struct {
	Server    *Server
	DiscordId string
	Name      string
}

func (c *Channel) Export(
	ctx context.Context,
	// +default=1
	parallel int,
) *dagger.Directory {
	return c.Server.Account.Exporter().
		WithCommand([]string{
			"export",
			"-c", c.DiscordId,
			"--include-threads", "All",
			"--media", "True",
			"--reuse-media", "True",
			"--parallel", fmt.Sprintf("%d", parallel),
			"-o", "./discord/channels/%G/%T/%C.html",
			"--media-dir", "./discord/media",
		}).
		State.
		Directory(".")
}
