package ledger

import (
	"github.com/robinvdvleuten/beancount/ast"
	sharedconfig "github.com/robinvdvleuten/beancount/config"
)

func configFromAST(tree *ast.AST) (*sharedconfig.Config, []error) {
	return sharedconfig.ParseOptions(tree)
}

// fallBackBookingMethods reports each open whose booking method beancount
// does not know and, as beancount's parser does, replaces it with a copy
// holding the booking_method option in effect on its line, which booking,
// printing and queries then read.
func fallBackBookingMethods(tree *ast.AST) []error {
	var errs []error
	for i, directive := range tree.Directives {
		open, ok := directive.(*ast.Open)
		if !ok || open.BookingMethod == "" || sharedconfig.IsBookingMethod(open.BookingMethod) {
			continue
		}
		fallback := *open
		fallback.BookingMethod = sharedconfig.OptionsAt(tree.Options, open.Position()).BookingMethod
		tree.Directives[i] = &fallback
		errs = append(errs, newInvalidBookingMethodError(&fallback, open.BookingMethod))
	}
	return errs
}
