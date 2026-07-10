package ledger

import (
	"github.com/robinvdvleuten/beancount/ast"
	sharedconfig "github.com/robinvdvleuten/beancount/config"
)

// Config aliases the shared processing configuration for compatibility.
type Config = sharedconfig.Config

// AccountNamesConfig aliases the shared account-name configuration.
type AccountNamesConfig = sharedconfig.AccountNames

// NewConfig returns configuration populated with official defaults.
func NewConfig() *Config { return sharedconfig.New() }

func configFromAST(tree *ast.AST) (*Config, error) { return sharedconfig.FromAST(tree) }

func configFromOptions(options map[string][]string) (*Config, error) {
	return sharedconfig.FromOptions(options)
}
