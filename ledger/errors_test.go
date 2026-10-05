package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/diagnostic"
	"github.com/robinvdvleuten/beancount/parser"
	"github.com/shopspring/decimal"
)

// kindOf returns a ledger error's kind, or "" for any other error.
func kindOf(err error) string {
	var kinded interface{ Kind() string }
	if errors.As(err, &kinded) {
		return kinded.Kind()
	}
	return ""
}

func TestErrorKinds(t *testing.T) {
	date, _ := ast.NewDate("2024-01-15")
	pos := ast.Position{Filename: "test.bean", Line: 10}
	postingPos := ast.Position{Filename: "test.bean", Line: 11}
	account := ast.Account("Assets:Cash")
	at := func(d ast.Directive) ast.Directive {
		d.(interface{ SetPosition(ast.Position) }).SetPosition(pos)
		return d
	}

	posting := ast.NewPosting(account, ast.WithAmount("1", "USD"))
	posting.SetPosition(postingPos)
	pricedPosting := ast.NewPosting(account, ast.WithTotalPrice(ast.NewAmount("-10", "USD")))
	pricedPosting.SetPosition(postingPos)
	txn := at(ast.NewTransaction(date, "x", ast.WithPostings(posting))).(*ast.Transaction)
	open := at(ast.NewOpen(date, account, nil, "")).(*ast.Open)
	closeDirective := at(ast.NewClose(date, account)).(*ast.Close)
	balance := at(ast.NewBalance(date, account, ast.NewAmount("1", "USD"))).(*ast.Balance)
	pad := at(ast.NewPad(date, account, "Equity:O")).(*ast.Pad)
	commodity := at(ast.NewCommodity(date, "USD")).(*ast.Commodity)
	document := at(ast.NewDocument(date, account, "a.pdf")).(*ast.Document)
	price := at(ast.NewPrice(date, "HOOL", ast.NewAmount("1", "USD"))).(*ast.Price)
	plugin := &ast.Plugin{Name: ast.NewRawString("beancount.plugins.auto_accounts")}
	plugin.SetPosition(pos)
	details := errors.New("details")

	for _, tt := range []struct {
		err       error
		kind      string
		line      int
		directive ast.Directive
	}{
		{newAccountNotOpenError(txn, account), "AccountNotOpenError", 10, txn},
		{newAccountAlreadyOpenError(open, date), "AccountAlreadyOpenError", 10, open},
		{newAccountAlreadyClosedError(closeDirective, date), "AccountAlreadyClosedError", 10, closeDirective},
		{newAccountNotClosedError(closeDirective), "AccountNotClosedError", 10, closeDirective},
		{newDuplicateCommodityError(commodity), "DuplicateCommodityError", 10, commodity},
		{newBalanceCurrencyError(balance), "BalanceCurrencyError", 10, balance},
		{newDuplicateBalanceError(balance), "DuplicateBalanceError", 10, balance},
		{newNegativeCostError(txn, posting, decimal.NewFromInt(-1), "USD"), "NegativeCostError", 11, nil},
		{newZeroAmountError(txn, posting), "ZeroAmountError", 11, nil},
		{newMergeCostError(txn, posting), "MergeCostError", 11, nil},
		{newDuplicateCostComponentError(txn, posting, &ast.Cost{Label: "b"}), "DuplicateCostComponentError", 11, txn},
		{newCurrencyGroupError(txn, posting, "Failed to categorize posting 1"), "CurrencyGroupError", 11, txn},
		{newInterpolationError(txn, posting, "Too many missing numbers"), "InterpolationError", 11, nil},
		{newNegativePriceError(txn, pricedPosting), "NegativePriceError", 11, txn},
		{newTotalPriceWithoutUnitsError(txn, pricedPosting), "TotalPriceWithoutUnitsError", 11, txn},
		{newCostPriceCurrencyError(txn, pricedPosting, "EUR"), "CostPriceCurrencyError", 11, nil},
		{newInvalidBookingMethodError(open, "BOGUS"), "InvalidBookingMethodError", 10, open},
		{newUnbookedTransactionError(txn), "UnbookedTransactionError", 10, txn},
		{newTransactionNotBalancedError(txn, []residual{{"USD", decimal.NewFromInt(1)}}), "TransactionNotBalancedError", 10, txn},
		{newInvalidAmountError(txn, account, "x", details), "InvalidAmountError", 10, txn},
		{newBalanceMismatchError(balance, decimal.NewFromInt(1), decimal.NewFromInt(2)), "BalanceMismatchError", 10, balance},
		{newInvalidCostError(txn, account, 0, "{x USD}", details), "InvalidCostError", 10, txn},
		{newTotalCostError(txn, posting, "cannot use total cost with zero quantity"), "TotalCostError", 10, txn},
		{newInvalidPriceError(txn, account, 0, "@ x USD", details), "InvalidPriceError", 10, txn},
		{newInvalidMetadataError(txn, account, "k", nil, "duplicate key"), "InvalidMetadataError", 10, txn},
		{newInsufficientInventoryError(txn, account, details), "InsufficientInventoryError", 10, txn},
		{newAmbiguousBookingError(txn, account, details), "AmbiguousBookingError", 10, txn},
		{newCurrencyConstraintError(txn, account, "EUR"), "CurrencyConstraintError", 10, txn},
		{newUnusedPadWarning(pad), "UnusedPadWarning", 10, pad},
		{newDocumentFileError(document), "DocumentFileError", 10, document},
		{newInvalidDirectivePriceError("price currency cannot be empty", price), "InvalidDirectivePriceError", 10, price},
		{newPluginConfigError(plugin), "PluginConfigError", 10, nil},
		{newPluginImportError(plugin), "PluginImportError", 10, nil},
	} {
		t.Run(tt.kind, func(t *testing.T) {
			assert.Equal(t, tt.kind, kindOf(tt.err))

			prefix := "test.bean:" + map[int]string{10: "10", 11: "11"}[tt.line] + ": "
			assert.Equal(t, prefix+tt.err.(diagnostic.Positioned).Message(), tt.err.Error())

			positioned := tt.err.(interface {
				GetPosition() ast.Position
				GetDirective() ast.Directive
			})
			assert.Equal(t, tt.line, positioned.GetPosition().Line)
			assert.Equal(t, tt.directive, positioned.GetDirective())

			data, err := json.Marshal(tt.err)
			assert.NoError(t, err)
			var rendered map[string]any
			assert.NoError(t, json.Unmarshal(data, &rendered))
			assert.Equal[any](t, tt.kind, rendered["type"])
			assert.Equal[any](t, tt.err.Error(), rendered["message"])
			assert.NotZero(t, rendered["position"])
		})
	}
}

func TestErrorWithoutFilenameUsesTheDate(t *testing.T) {
	date, _ := ast.NewDate("2024-01-15")
	txn := ast.NewTransaction(date, "x")
	err := newInsufficientInventoryError(txn, "Assets:Checking", errors.New("details"))
	assert.Equal(t, "2024-01-15: details", err.Error())
}

func TestMergeCostErrorReadsAsBeancounts(t *testing.T) {
	// A merge cost is reported in bean-check's words, then booked like {}.
	tree := parser.MustParseString(context.Background(), `
2020-01-01 open Assets:I
2020-01-01 open Assets:C
2020-01-01 open Income:G
2020-01-01 * "buy"
  Assets:I  5 HOOL {10.00 EUR}
  Assets:C
2020-02-07 * "merge"
  Assets:I  -5 HOOL {*}
  Income:G
`)
	l := New()
	_, validationErrors := processDiagnostics(t, l, tree)
	assert.Equal(t, 1, len(validationErrors))
	assert.Equal(t, "MergeCostError", kindOf(validationErrors[0]))
	assert.Equal(t, "Cost merging is not supported yet", validationErrors[0].(*Diagnostic).message)
}

func TestCurrencyConstraintErrorReadsAsBeancounts(t *testing.T) {
	// A posting in a currency its account's open does not allow is reported
	// in bean-check's words, without the allowed currencies.
	tree := parser.MustParseString(context.Background(), `
2020-01-01 open Assets:Euro EUR
2020-01-01 open Equity:O
2020-01-10 * "x"
  Assets:Euro  5 USD
  Equity:O
`)
	l := New()
	_, validationErrors := processDiagnostics(t, l, tree)
	assert.Equal(t, 1, len(validationErrors))
	assert.Equal(t, "CurrencyConstraintError", kindOf(validationErrors[0]))
	assert.Equal(t, "Invalid currency USD for account 'Assets:Euro'", validationErrors[0].(*Diagnostic).message)
}
