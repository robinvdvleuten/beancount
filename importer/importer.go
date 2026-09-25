package importer

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/importer/internal/pb"
	"github.com/robinvdvleuten/beancount/telemetry"
)

// ProtocolVersion is the version of the protocol this package speaks.
const ProtocolVersion = 1

// Handshake is the go-plugin handshake both sides of protocol v1 agree on.
// It keeps beancount from talking to a binary that is not an Importer.
var Handshake = plugin.HandshakeConfig{
	ProtocolVersion:  ProtocolVersion,
	MagicCookieKey:   "BEANCOUNT_IMPORTER",
	MagicCookieValue: "8a3f3c52-6f0e-4b8e-9d0a-3c1e5b2f7d41",
}

const pluginName = "importer"

// Importer turns a Statement into Extracted directives. Implement it and
// pass it to Serve from an Importer's main.
type Importer interface {
	// Identify reports whether the Importer can read the Statement at path.
	Identify(ctx context.Context, path string) (bool, error)
	// Extract returns the Statement's transactions and balance assertions.
	// An import-id metadata string becomes the transaction's Import ID.
	Extract(ctx context.Context, path string) ([]ast.Directive, error)
}

// Serve runs impl as an Importer until beancount disconnects. Call it from
// main; it does not return.
func Serve(impl Importer) {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig:  Handshake,
		VersionedPlugins: map[int]plugin.PluginSet{ProtocolVersion: {pluginName: &grpcPlugin{impl: impl, stderr: os.Stderr}}},
		GRPCServer:       plugin.DefaultGRPCServer,
		Logger:           hclog.NewNullLogger(),
	})
}

// Client is a running Importer, started by Open.
type Client struct {
	plugin *plugin.Client
	rpc    pb.ImporterClient
}

var _ Importer = (*Client)(nil)

// Open starts the Importer binary at path and completes the handshake. The
// Importer's own output goes to stderr. Close the Client when done.
func Open(ctx context.Context, path string, stderr io.Writer) (*Client, error) {
	timer := telemetry.FromContext(ctx).Start("importer.open " + filepath.Base(path))
	defer timer.End()

	pc := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig:  Handshake,
		VersionedPlugins: map[int]plugin.PluginSet{ProtocolVersion: {pluginName: &grpcPlugin{}}},
		Cmd:              exec.CommandContext(ctx, path),
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
		Logger:           hclog.NewNullLogger(),
		Stderr:           stderr,
		SyncStdout:       stderr,
		SyncStderr:       stderr,
	})
	rpcClient, err := pc.Client()
	if err != nil {
		pc.Kill()
		return nil, fmt.Errorf("failed to start %s as a beancount Importer (protocol version %d): %w", path, ProtocolVersion, err)
	}
	raw, err := rpcClient.Dispense(pluginName)
	if err != nil {
		pc.Kill()
		return nil, fmt.Errorf("failed to start %s as a beancount Importer: %w", path, err)
	}
	return &Client{plugin: pc, rpc: raw.(pb.ImporterClient)}, nil
}

// Identify asks the Importer whether it can read the Statement at path.
func (c *Client) Identify(ctx context.Context, path string) (bool, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}
	resp, err := c.rpc.Identify(ctx, &pb.IdentifyRequest{StatementPath: abs})
	if err != nil {
		return false, rpcError(err)
	}
	return resp.GetMatches(), nil
}

// Extract asks the Importer for the Statement's Extracted directives.
func (c *Client) Extract(ctx context.Context, path string) ([]ast.Directive, error) {
	timer := telemetry.FromContext(ctx).Start("importer.extract " + filepath.Base(path))
	defer timer.End()

	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	resp, err := c.rpc.Extract(ctx, &pb.ExtractRequest{StatementPath: abs})
	if err != nil {
		return nil, rpcError(err)
	}
	return decodeDirectives(resp.GetDirectives())
}

// Close stops the Importer.
func (c *Client) Close() {
	c.plugin.Kill()
}

// rpcError returns the message an Importer failed with, without gRPC's
// status wrapping.
func rpcError(err error) error {
	if s, ok := status.FromError(err); ok {
		return fmt.Errorf("%s", s.Message())
	}
	return err
}

// grpcPlugin connects an Importer to go-plugin's gRPC transport.
type grpcPlugin struct {
	plugin.NetRPCUnsupportedPlugin
	impl   Importer
	stderr *os.File
}

func (p *grpcPlugin) GRPCServer(_ *plugin.GRPCBroker, s *grpc.Server) error {
	pb.RegisterImporterServer(s, &server{impl: p.impl, stderr: p.stderr})
	return nil
}

func (p *grpcPlugin) GRPCClient(_ context.Context, _ *plugin.GRPCBroker, c *grpc.ClientConn) (any, error) {
	return pb.NewImporterClient(c), nil
}

// server answers beancount's calls with an Importer.
type server struct {
	pb.UnimplementedImporterServer
	impl Importer

	stderr        *os.File
	restoreStderr sync.Once
}

// useProcessStderr points os.Stderr back at the process's own stderr.
// plugin.Serve redirects it into a gRPC stream that Client.Close does not
// drain, so output written just before Close would be lost; the process's
// stderr is read until the Importer exits. Serve swaps os.Stderr before it
// accepts calls, so the first call can swap it back.
func (s *server) useProcessStderr() {
	s.restoreStderr.Do(func() { os.Stderr = s.stderr })
}

func (s *server) Identify(ctx context.Context, req *pb.IdentifyRequest) (*pb.IdentifyResponse, error) {
	s.useProcessStderr()
	matches, err := s.impl.Identify(ctx, req.GetStatementPath())
	if err != nil {
		return nil, err
	}
	return &pb.IdentifyResponse{Matches: matches}, nil
}

func (s *server) Extract(ctx context.Context, req *pb.ExtractRequest) (*pb.ExtractResponse, error) {
	s.useProcessStderr()
	directives, err := s.impl.Extract(ctx, req.GetStatementPath())
	if err != nil {
		return nil, err
	}
	msgs, err := encodeDirectives(directives)
	if err != nil {
		return nil, err
	}
	return &pb.ExtractResponse{Directives: msgs}, nil
}
