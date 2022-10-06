package main

import (
	"testing"

	require "github.com/alecthomas/assert/v2"
	"github.com/alecthomas/repr"
)

func TestExe(t *testing.T) {
	ast, err := parser.ParseString("", `
* Commodities

1792-01-01 commodity USD
	export: "CASH"
	name: "US Dollar"

* Accounts

2019-01-01 open Equity:Opening-Balances USD

* Transactions

2019-01-01 txn
	Assets:US:BofA:Checking  4135.73 USD
	Equity:Opening-Balances -4135.73 USD

2019-01-01 *
	Liabilities:CreditCard:CapitalOne -37.45 USD

2019-01-01 !
	Assets:ETrade:IVV                +10 IVV

2019-01-01 txn "Lamb tagine with wine"
	Assets:US:BofA:Checking 4135.73 USD @ 412 EUR

2019-01-01 * "Lamb tagine with wine"
	Assets:US:BofA:Checking 4135.73 USD @@ 488.22 EUR

2019-01-01 * "Cafe Mogador" "Lamb tagine with wine"

2019-01-01 * "Cafe Mogador" ""
	`)

	require.NoError(t, err)
	repr.Println(ast)
}
