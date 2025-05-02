// MANUAL patch to add Host() support to generated client
package dagger

import (
	"context"
	"encoding/json"

	"dagger.io/dagger/querybuilder"
)

type HostID string

// Queries the host environment.
func (r *Client) Host() *Host {
	q := r.query.Select("host")

	return &Host{
		query: q,
	}
}

type Host struct {
	query *querybuilder.Selection

	id *HostID
}

func (r *Host) WithGraphQLQuery(q *querybuilder.Selection) *Host {
	return &Host{
		query: q,
	}
}
func (r *Host) Directory(path string, opts ...HostDirectoryOpts) *Directory {
	q := r.query.Select("directory")
	for i := len(opts) - 1; i >= 0; i-- {
		// `exclude` optional argument
		if !querybuilder.IsZeroValue(opts[i].Exclude) {
			q = q.Arg("exclude", opts[i].Exclude)
		}
		// `include` optional argument
		if !querybuilder.IsZeroValue(opts[i].Include) {
			q = q.Arg("include", opts[i].Include)
		}
	}
	q = q.Arg("path", path)

	return &Directory{
		query: q,
	}
}

// HostDirectoryOpts contains options for Host.Directory
type HostDirectoryOpts struct {
	// Exclude artifacts that match the given pattern (e.g., ["node_modules/", ".git*"]).
	Exclude []string
	// Include only artifacts that match the given pattern (e.g., ["app/", "package.*"]).
	Include []string
}

// HostServiceOpts contains options for Host.Service
type HostServiceOpts struct {
	// Upstream host to forward traffic to.
	//
	// Default: "localhost"
	Host string
}

// HostTunnelOpts contains options for Host.Tunnel
type HostTunnelOpts struct {
	// Map each service port to the same port on the host, as if the service were running natively.
	//
	// Note: enabling may result in port conflicts.
	Native bool
	// Configure explicit port forwarding rules for the tunnel.
	//
	// If a port's frontend is unspecified or 0, a random port will be chosen by the host.
	//
	// If no ports are given, all of the service's ports are forwarded. If native is true, each port maps to the same port on the host. If native is false, each port maps to a random port chosen by the host.
	//
	// If ports are given and native is true, the ports are additive.
	Ports []PortForward
}

func (r *Host) File(path string) *File {
	q := r.query.Select("file")
	q = q.Arg("path", path)

	return &File{
		query: q,
	}
}
func (r *Host) ID(ctx context.Context) (HostID, error) {
	if r.id != nil {
		return *r.id, nil
	}
	q := r.query.Select("id")

	var response HostID

	q = q.Bind(&response)
	return response, q.Execute(ctx)
}
func (r *Host) XXX_GraphQLType() string {
	return "Host"
}
func (r *Host) XXX_GraphQLIDType() string {
	return "HostID"
}
func (r *Host) XXX_GraphQLID(ctx context.Context) (string, error) {
	id, err := r.ID(ctx)
	if err != nil {
		return "", err
	}
	return string(id), nil
}
func (r *Host) MarshalJSON() ([]byte, error) {
	id, err := r.ID(marshalCtx)
	if err != nil {
		return nil, err
	}
	return json.Marshal(id)
}
func (r *Host) Service(ports []PortForward, opts ...HostServiceOpts) *Service {
	q := r.query.Select("service")
	for i := len(opts) - 1; i >= 0; i-- {
		// `host` optional argument
		if !querybuilder.IsZeroValue(opts[i].Host) {
			q = q.Arg("host", opts[i].Host)
		}
	}
	q = q.Arg("ports", ports)

	return &Service{
		query: q,
	}
}
func (r *Host) SetSecretFile(name string, path string) *Secret {
	q := r.query.Select("setSecretFile")
	q = q.Arg("name", name)
	q = q.Arg("path", path)

	return &Secret{
		query: q,
	}
}
func (r *Host) Tunnel(service *Service, opts ...HostTunnelOpts) *Service {
	assertNotNil("service", service)
	q := r.query.Select("tunnel")
	for i := len(opts) - 1; i >= 0; i-- {
		// `native` optional argument
		if !querybuilder.IsZeroValue(opts[i].Native) {
			q = q.Arg("native", opts[i].Native)
		}
		// `ports` optional argument
		if !querybuilder.IsZeroValue(opts[i].Ports) {
			q = q.Arg("ports", opts[i].Ports)
		}
	}
	q = q.Arg("service", service)

	return &Service{
		query: q,
	}
}
func (r *Host) UnixSocket(path string) *Socket {
	q := r.query.Select("unixSocket")
	q = q.Arg("path", path)

	return &Socket{
		query: q,
	}
}
