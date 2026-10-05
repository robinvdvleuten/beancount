package ledger

import (
	"github.com/robinvdvleuten/beancount/ast"
	sharedconfig "github.com/robinvdvleuten/beancount/config"
)

func configFromAST(tree *ast.AST) (*sharedconfig.Config, []error) {
	return sharedconfig.ParseOptions(tree)
}
