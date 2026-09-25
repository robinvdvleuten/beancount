// Command fixture is an Importer for the importer package's tests. The
// FIXTURE_MODE environment variable picks how it misbehaves.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/importer"
)

type fixture struct{}

func (fixture) Identify(_ context.Context, path string) (bool, error) {
	return filepath.Ext(path) == ".csv", nil
}

func (fixture) Extract(_ context.Context, path string) ([]ast.Directive, error) {
	log.Printf("log line from %s", filepath.Base(path))
	fmt.Fprintln(os.Stderr, "stderr line")

	date, _ := ast.NewDate("2024-01-15")
	nextDay, _ := ast.NewDate("2024-01-16")
	checking, _ := ast.NewAccount("Assets:Checking")
	food, _ := ast.NewAccount("Expenses:Food")

	switch os.Getenv("FIXTURE_MODE") {
	case "extract-error":
		return nil, errors.New("statement is truncated")
	case "wrong-kind":
		return []ast.Directive{ast.NewPrice(date, "HOOL", ast.NewAmount("1", "USD"))}, nil
	case "bad-tag":
		return []ast.Directive{ast.NewTransaction(date, "Latte",
			ast.WithFlag("*"),
			ast.WithTags("bad tag"),
			ast.WithPostings(
				ast.NewPosting(food, ast.WithAmount("4.50", "USD")),
				ast.NewPosting(checking),
			),
		)}, nil
	case "unbalanced":
		return []ast.Directive{ast.NewTransaction(date, "Latte",
			ast.WithFlag("*"),
			ast.WithPostings(
				ast.NewPosting(food, ast.WithAmount("4.50", "USD")),
				ast.NewPosting(checking, ast.WithAmount("-4.00", "USD")),
			),
		)}, nil
	}

	return []ast.Directive{
		ast.NewTransaction(date, "Latte",
			ast.WithFlag("*"),
			ast.WithPayee("Coffee Shop"),
			ast.WithTransactionMetadata(ast.NewMetadata("import-id", "TX-001")),
			ast.WithPostings(
				ast.NewPosting(food, ast.WithAmount("4.50", "USD")),
				ast.NewPosting(checking),
			),
		),
		ast.NewBalance(nextDay, checking, ast.NewAmount("-4.50", "USD")),
	}, nil
}

// futurePlugin stands in for a protocol version this beancount does not speak.
type futurePlugin struct{ plugin.NetRPCUnsupportedPlugin }

func (futurePlugin) GRPCServer(*plugin.GRPCBroker, *grpc.Server) error { return nil }
func (futurePlugin) GRPCClient(context.Context, *plugin.GRPCBroker, *grpc.ClientConn) (any, error) {
	return nil, nil
}

func main() {
	switch os.Getenv("FIXTURE_MODE") {
	case "not-importer":
		fmt.Println("hello")
		return
	case "protocol-2":
		plugin.Serve(&plugin.ServeConfig{
			HandshakeConfig:  importer.Handshake,
			VersionedPlugins: map[int]plugin.PluginSet{2: {"importer": futurePlugin{}}},
			GRPCServer:       plugin.DefaultGRPCServer,
		})
		return
	}
	importer.Serve(fixture{})
}
