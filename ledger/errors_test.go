package ledger

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
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
		{NewAccountNotOpenError(txn, account), "AccountNotOpenError", 10, txn},
		{NewAccountAlreadyOpenError(open, date), "AccountAlreadyOpenError", 10, open},
		{NewInvalidAccountNameError(open, NewConfig()), "InvalidAccountNameError", 10, open},
		{NewAccountAlreadyClosedError(closeDirective, date), "AccountAlreadyClosedError", 10, closeDirective},
		{NewAccountNotClosedError(closeDirective), "AccountNotClosedError", 10, closeDirective},
		{NewDuplicateCommodityError(commodity), "DuplicateCommodityError", 10, commodity},
		{NewBalanceCurrencyError(balance), "BalanceCurrencyError", 10, balance},
		{NewDuplicateBalanceError(balance), "DuplicateBalanceError", 10, balance},
		{NewNegativeCostError(txn, posting, decimal.NewFromInt(-1), "USD"), "NegativeCostError", 11, txn},
		{NewMergeCostError(txn, posting), "MergeCostError", 11, txn},
		{NewCurrencyGroupError(txn, posting, "Too many missing numbers"), "CurrencyGroupError", 11, txn},
		{NewNegativePriceError(txn, pricedPosting), "NegativePriceError", 11, txn},
		{NewTotalPriceWithoutUnitsError(txn, pricedPosting), "TotalPriceWithoutUnitsError", 11, txn},
		{NewInvalidBookingMethodError(open), "InvalidBookingMethodError", 10, open},
		{NewTransactionNotBalancedError(txn, map[string]string{"USD": "1"}), "TransactionNotBalancedError", 10, txn},
		{NewInvalidAmountError(txn, account, "x", details), "InvalidAmountError", 10, txn},
		{NewBalanceMismatchError(balance, decimal.NewFromInt(1), decimal.NewFromInt(2)), "BalanceMismatchError", 10, balance},
		{NewInvalidCostError(txn, account, 0, "{x USD}", details), "InvalidCostError", 10, txn},
		{NewTotalCostError(txn, posting, "cannot use total cost with zero quantity"), "TotalCostError", 10, txn},
		{NewInvalidPriceError(txn, account, 0, "@ x USD", details), "InvalidPriceError", 10, txn},
		{NewInvalidMetadataError(txn, account, "k", nil, "empty value"), "InvalidMetadataError", 10, txn},
		{NewInsufficientInventoryError(txn, account, details), "InsufficientInventoryError", 10, txn},
		{NewAmbiguousBookingError(txn, account, details), "AmbiguousBookingError", 10, txn},
		{NewCurrencyConstraintError(txn, account, "EUR", []string{"USD"}), "CurrencyConstraintError", 10, txn},
		{NewUnusedPadWarning(pad), "UnusedPadWarning", 10, pad},
		{NewDocumentFileError(document, "/x/a.pdf"), "DocumentFileError", 10, document},
		{NewInvalidDirectivePriceError("price currency cannot be empty", price), "InvalidDirectivePriceError", 10, price},
		{NewPluginConfigError(plugin), "PluginConfigError", 10, nil},
	} {
		t.Run(tt.kind, func(t *testing.T) {
			assert.Equal(t, tt.kind, kindOf(tt.err))

			prefix := "test.bean:" + map[int]string{10: "10", 11: "11"}[tt.line] + ": "
			assert.True(t, strings.HasPrefix(tt.err.Error(), prefix), tt.err.Error())

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
	err := NewInsufficientInventoryError(txn, "Assets:Checking", errors.New("details"))
	assert.Equal(t, "2024-01-15: Insufficient inventory (account Assets:Checking): details", err.Error())
}
