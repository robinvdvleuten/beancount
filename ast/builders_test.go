package ast

import (
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"
)

func TestNewAmount(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		currency string
	}{
		{"Positive", "100.50", "USD"},
		{"Negative", "-42.00", "EUR"},
		{"Zero", "0.00", "GBP"},
		{"Large", "1234567.89", "JPY"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			amount := NewAmount(tt.value, tt.currency)
			assert.Equal(t, tt.value, amount.Value)
			assert.Equal(t, tt.currency, amount.Currency)
		})
	}
}

func TestNewDate(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"Valid", "2024-01-15", false},
		{"LeapYear", "2024-02-29", false},
		{"Invalid", "2024-13-01", true},
		{"BadFormat", "01/15/2024", true},
		{"Empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			date, err := NewDate(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				assert.True(t, date == nil)
			} else {
				assert.NoError(t, err)
				assert.True(t, date != nil)
			}
		})
	}
}

func TestNewDateFromTime(t *testing.T) {
	now := time.Now()
	date := NewDateFromTime(now)

	assert.Equal(t, now.Year(), date.Year())
	assert.Equal(t, now.Month(), date.Month())
	assert.Equal(t, now.Day(), date.Day())
}

func TestNewAccount(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"Assets", "Assets:US:BofA:Checking", false},
		{"Liabilities", "Liabilities:CreditCard", false},
		{"Equity", "Equity:Opening-Balances", false},
		{"Income", "Income:Salary", false},
		{"Expenses", "Expenses:Groceries", false},
		{"CustomType", "Foo:Bar", false}, // Parser allows custom types - ledger validates against configured names
		{"SingleSegment", "Assets", true},
		{"Empty", "", true},

		// Unicode support - matching official beancount behavior
		{"French", "Assets:Bank:Société-Générale", false},
		{"German", "Expenses:Café:München", false},
		{"Spanish", "Liabilities:Préstamos", false},
		{"Chinese", "Assets:银行:中国", false},
		{"Japanese", "Income:会社:給料", false},
		{"Korean", "Assets:은행:계좌", false},
		{"Cyrillic", "Expenses:Кафе:Москва", false},
		{"Greek", "Assets:Τράπεζα:Αθήνα", false},
		{"Arabic", "Assets:بنك:حساب", false},
		{"Mixed", "Assets:Café:München:中国", false},

		// Custom root names (allowed by parser, validated in ledger)
		{"CustomRoot", "Vermoegen:Checking", false},
		{"CustomRootUnicode", "Vermögen:Konto", false},

		// Edge cases
		{"WithDigit", "Assets:Bank123:Account", false},
		{"StartWithDigit", "Assets:3Bank:Account", false},
		{"MultipleHyphens", "Assets:My-Big-Long-Account-Name", false},

		// Invalid cases
		{"LowercaseStart", "Assets:bank:Account", true},
		{"SpecialChar", "Assets:Bank$Account", true},
		{"Space", "Assets:Bank Account", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account, err := NewAccount(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, Account(tt.input), account)
			}
		})
	}
}

func TestNewLink(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  Link
	}{
		{"WithCaret", "^invoice-001", "invoice-001"},
		{"WithoutCaret", "invoice-001", "invoice-001"},
		{"Empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link := NewLink(tt.input)
			assert.Equal(t, tt.want, link)
		})
	}
}

func TestNewTag(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  Tag
	}{
		{"WithHash", "#groceries", "groceries"},
		{"WithoutHash", "groceries", "groceries"},
		{"Empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tag := NewTag(tt.input)
			assert.Equal(t, tt.want, tag)
		})
	}
}

func TestNewMetadata(t *testing.T) {
	meta := NewMetadata("invoice", "INV-2024-001")
	assert.Equal(t, "invoice", meta.Key)
	assert.Equal(t, "string", meta.Value.Type())
	assert.Equal(t, "INV-2024-001", meta.Value.String())
}

func TestNewTransaction(t *testing.T) {
	date, _ := NewDate("2024-01-15")

	t.Run("MinimalTransaction", func(t *testing.T) {
		txn := NewTransaction(date, "Test transaction")
		assert.Equal(t, date, txn.Date())
		assert.Equal(t, "Test transaction", txn.Narration.Value)
		assert.Equal(t, "", txn.Flag)
		assert.Equal(t, "", txn.Payee.Value)
		assert.Equal(t, 0, len(txn.Postings))
	})

	t.Run("WithFlag", func(t *testing.T) {
		txn := NewTransaction(date, "Test", WithFlag("*"))
		assert.Equal(t, "*", txn.Flag)
	})

	t.Run("WithPayee", func(t *testing.T) {
		txn := NewTransaction(date, "Test", WithPayee("Amazon"))
		assert.Equal(t, "Amazon", txn.Payee.Value)
	})

	t.Run("WithTags", func(t *testing.T) {
		txn := NewTransaction(date, "Test", WithTags("food", "groceries"))
		assert.Equal(t, 2, len(txn.Tags))
		assert.Equal(t, Tag("food"), txn.Tags[0])
		assert.Equal(t, Tag("groceries"), txn.Tags[1])
	})

	t.Run("WithLinks", func(t *testing.T) {
		txn := NewTransaction(date, "Test", WithLinks("invoice-001", "receipt-002"))
		assert.Equal(t, 2, len(txn.Links))
		assert.Equal(t, Link("invoice-001"), txn.Links[0])
		assert.Equal(t, Link("receipt-002"), txn.Links[1])
	})

	t.Run("WithTransactionMetadata", func(t *testing.T) {
		meta1 := NewMetadata("key1", "value1")
		meta2 := NewMetadata("key2", "value2")
		txn := NewTransaction(date, "Test", WithTransactionMetadata(meta1, meta2))
		assert.Equal(t, 2, len(txn.Metadata))
	})

	t.Run("CompleteTransaction", func(t *testing.T) {
		account1, _ := NewAccount("Expenses:Groceries")
		account2, _ := NewAccount("Assets:Checking")

		txn := NewTransaction(date, "Buy groceries",
			WithFlag("*"),
			WithPayee("Whole Foods"),
			WithTags("food", "shopping"),
			WithLinks("receipt-001"),
			WithPostings(
				NewPosting(account1, WithAmount("45.60", "USD")),
				NewPosting(account2),
			),
		)

		assert.Equal(t, "*", txn.Flag)
		assert.Equal(t, "Whole Foods", txn.Payee.Value)
		assert.Equal(t, 2, len(txn.Tags))
		assert.Equal(t, 1, len(txn.Links))
		assert.Equal(t, 2, len(txn.Postings))
	})
}

func TestNewPosting(t *testing.T) {
	account, _ := NewAccount("Assets:Checking")

	t.Run("MinimalPosting", func(t *testing.T) {
		posting := NewPosting(account)
		assert.Equal(t, account, posting.Account)
		assert.True(t, posting.Amount == nil)
		assert.True(t, posting.Cost == nil)
	})

	t.Run("WithAmount", func(t *testing.T) {
		posting := NewPosting(account, WithAmount("100.00", "USD"))
		assert.Equal(t, "100.00", posting.Amount.Value)
		assert.Equal(t, "USD", posting.Amount.Currency)
	})

	t.Run("WithCost", func(t *testing.T) {
		cost := NewCost(NewAmount("1.35", "EUR"))
		posting := NewPosting(account, WithCost(cost))
		assert.Equal(t, cost, posting.Cost)
	})

	t.Run("WithPrice", func(t *testing.T) {
		price := NewAmount("1.35", "EUR")
		posting := NewPosting(account, WithPrice(price))
		assert.Equal(t, price, posting.Price)
		assert.False(t, posting.PriceTotal)
	})

	t.Run("WithTotalPrice", func(t *testing.T) {
		price := NewAmount("135.00", "EUR")
		posting := NewPosting(account, WithTotalPrice(price))
		assert.Equal(t, price, posting.Price)
		assert.True(t, posting.PriceTotal)
	})

	t.Run("WithPostingFlag", func(t *testing.T) {
		posting := NewPosting(account, WithPostingFlag("!"))
		assert.Equal(t, "!", posting.Flag)
	})

	t.Run("CompletePosting", func(t *testing.T) {
		cost := NewCost(NewAmount("520.00", "USD"))
		price := NewAmount("525.00", "USD")
		meta := NewMetadata("note", "test")

		posting := NewPosting(account,
			WithAmount("10", "HOOL"),
			WithCost(cost),
			WithPrice(price),
			WithPostingFlag("*"),
			WithPostingMetadata(meta),
		)

		assert.Equal(t, "10", posting.Amount.Value)
		assert.Equal(t, "HOOL", posting.Amount.Currency)
		assert.Equal(t, cost, posting.Cost)
		assert.Equal(t, price, posting.Price)
		assert.Equal(t, "*", posting.Flag)
		assert.Equal(t, 1, len(posting.Metadata))
	})
}

func TestNewCost(t *testing.T) {
	t.Run("SimpleCost", func(t *testing.T) {
		amount := NewAmount("518.73", "USD")
		cost := NewCost(amount)
		assert.Equal(t, amount, cost.Amount)
		assert.True(t, cost.Date == nil)
		assert.Equal(t, "", cost.Label)
		assert.False(t, cost.IsMerge)
	})

	t.Run("CostWithDate", func(t *testing.T) {
		amount := NewAmount("518.73", "USD")
		date, _ := NewDate("2024-01-15")
		cost := NewCostWithDate(amount, date)
		assert.Equal(t, amount, cost.Amount)
		assert.Equal(t, date, cost.Date)
		assert.Equal(t, "", cost.Label)
	})

	t.Run("CostWithLabel", func(t *testing.T) {
		amount := NewAmount("518.73", "USD")
		date, _ := NewDate("2024-01-15")
		cost := NewCostWithLabel(amount, date, "first-lot")
		assert.Equal(t, amount, cost.Amount)
		assert.Equal(t, date, cost.Date)
		assert.Equal(t, "first-lot", cost.Label)
	})

	t.Run("EmptyCost", func(t *testing.T) {
		cost := NewEmptyCost()
		assert.True(t, cost.IsEmpty())
		assert.False(t, cost.IsMergeCost())
	})

	t.Run("MergeCost", func(t *testing.T) {
		cost := NewMergeCost()
		assert.True(t, cost.IsMergeCost())
		assert.False(t, cost.IsEmpty())
	})
}

func TestNewClearedTransaction(t *testing.T) {
	date, _ := NewDate("2024-01-15")
	account1, _ := NewAccount("Expenses:Groceries")
	account2, _ := NewAccount("Assets:Checking")

	txn := NewClearedTransaction(date, "Buy groceries",
		NewPosting(account1, WithAmount("45.60", "USD")),
		NewPosting(account2),
	)

	assert.Equal(t, "*", txn.Flag)
	assert.Equal(t, "Buy groceries", txn.Narration.Value)
	assert.Equal(t, 2, len(txn.Postings))
}

func TestNewPendingTransaction(t *testing.T) {
	date, _ := NewDate("2024-01-15")
	account1, _ := NewAccount("Assets:Savings")
	account2, _ := NewAccount("Assets:Checking")

	txn := NewPendingTransaction(date, "Pending transfer",
		NewPosting(account1, WithAmount("1000.00", "USD")),
		NewPosting(account2),
	)

	assert.Equal(t, "!", txn.Flag)
	assert.Equal(t, "Pending transfer", txn.Narration.Value)
	assert.Equal(t, 2, len(txn.Postings))
}

func TestNewOpen(t *testing.T) {
	date, _ := NewDate("2024-01-01")
	account, _ := NewAccount("Assets:US:BofA:Checking")

	t.Run("WithCurrencies", func(t *testing.T) {
		open := NewOpen(date, account, []string{"USD"}, "")
		assert.Equal(t, date, open.Date())
		assert.Equal(t, account, open.Account)
		assert.Equal(t, []string{"USD"}, open.ConstraintCurrencies)
		assert.Equal(t, "", open.BookingMethod)
	})

	t.Run("WithBookingMethod", func(t *testing.T) {
		open := NewOpen(date, account, nil, "FIFO")
		assert.Equal(t, "FIFO", open.BookingMethod)
	})
}

func TestNewClose(t *testing.T) {
	date, _ := NewDate("2024-12-31")
	account, _ := NewAccount("Liabilities:CreditCard:OldCard")

	close := NewClose(date, account)
	assert.Equal(t, date, close.Date())
	assert.Equal(t, account, close.Account)
}

func TestNewBalance(t *testing.T) {
	date, _ := NewDate("2024-01-31")
	account, _ := NewAccount("Assets:Checking")
	amount := NewAmount("1250.00", "USD")

	balance := NewBalance(date, account, amount)
	assert.Equal(t, date, balance.Date())
	assert.Equal(t, account, balance.Account)
	assert.Equal(t, amount, balance.Amount)
}

func TestNewPad(t *testing.T) {
	date, _ := NewDate("2024-01-01")
	account, _ := NewAccount("Assets:Checking")
	padAccount, _ := NewAccount("Equity:Opening-Balances")

	pad := NewPad(date, account, padAccount)
	assert.Equal(t, date, pad.Date())
	assert.Equal(t, account, pad.Account)
	assert.Equal(t, padAccount, pad.AccountPad)
}

func TestNewNote(t *testing.T) {
	date, _ := NewDate("2024-01-15")
	account, _ := NewAccount("Assets:Checking")

	note := NewNote(date, account, "Opened new checking account")
	assert.Equal(t, date, note.Date())
	assert.Equal(t, account, note.Account)
	assert.Equal(t, "Opened new checking account", note.Description.Value)
}

func TestNewDocument(t *testing.T) {
	date, _ := NewDate("2024-01-15")
	account, _ := NewAccount("Assets:Checking")

	doc := NewDocument(date, account, "/path/to/statement.pdf")
	assert.Equal(t, date, doc.Date())
	assert.Equal(t, account, doc.Account)
	assert.Equal(t, "/path/to/statement.pdf", doc.PathToDocument.Value)
}

func TestNewCommodity(t *testing.T) {
	date, _ := NewDate("2024-01-01")
	commodity := NewCommodity(date, "USD")
	assert.Equal(t, date, commodity.Date())
	assert.Equal(t, "USD", commodity.Currency)
}

func TestNewPrice(t *testing.T) {
	date, _ := NewDate("2024-01-15")
	amount := NewAmount("520.50", "USD")

	price := NewPrice(date, "HOOL", amount)
	assert.Equal(t, date, price.Date())
	assert.Equal(t, "HOOL", price.Commodity)
	assert.Equal(t, amount, price.Amount)
}

func TestNewEvent(t *testing.T) {
	date, _ := NewDate("2024-01-01")
	event := NewEvent(date, "location", "New York, USA")
	assert.Equal(t, date, event.Date())
	assert.Equal(t, "location", event.Name.Value)
	assert.Equal(t, "New York, USA", event.Value.Value)
}

// Example_csvImporter demonstrates how to use the builders to create
// transactions from CSV data, such as a bank statement import.
func Example_csvImporter() {
	// Example: Parse a CSV row from a bank statement
	// CSV format: date,payee,amount
	// Row: "2024-01-15,Whole Foods,-45.60"

	// Parse the CSV data (simplified for example)
	date, _ := NewDate("2024-01-15")
	payee := "Whole Foods"
	amount := "-45.60"

	// Create accounts
	expensesAccount, _ := NewAccount("Expenses:Groceries")
	checkingAccount, _ := NewAccount("Assets:Checking")

	// Build the transaction
	txn := NewTransaction(date, "Groceries",
		WithFlag("*"),
		WithPayee(payee),
		WithTags("food"),
		WithPostings(
			NewPosting(expensesAccount, WithAmount("45.60", "USD")), // Positive for expense
			NewPosting(checkingAccount, WithAmount(amount, "USD")),  // Negative for withdrawal
		),
	)

	// Now you can format and output the transaction
	_ = txn // Use formatter.FormatTransaction(txn, os.Stdout)
}

// Example_investmentTransaction demonstrates building a transaction
// with cost basis for investment tracking.
func Example_investmentTransaction() {
	date, _ := NewDate("2024-01-15")

	// Buy 10 shares of HOOL at $520 per share
	brokerageAccount, _ := NewAccount("Assets:Investments:Brokerage")
	cashAccount, _ := NewAccount("Assets:Investments:Cash")

	cost := NewCost(NewAmount("520.00", "USD"))

	txn := NewTransaction(date, "Buy HOOL shares",
		WithFlag("*"),
		WithPayee("Vanguard"),
		WithTags("investment", "stocks"),
		WithPostings(
			NewPosting(brokerageAccount,
				WithAmount("10", "HOOL"),
				WithCost(cost)),
			NewPosting(cashAccount,
				WithAmount("-5200.00", "USD")),
		),
	)

	_ = txn // Use formatter.FormatTransaction(txn, os.Stdout)
}

func TestAccountType(t *testing.T) {
	tests := []struct {
		account Account
		want    AccountType
	}{
		{"Assets:Checking", AccountTypeAssets},
		{"Liabilities:CreditCard", AccountTypeLiabilities},
		{"Equity:Opening-Balances", AccountTypeEquity},
		{"Income:Salary", AccountTypeIncome},
		{"Expenses:Rent", AccountTypeExpenses},
	}

	for _, tt := range tests {
		t.Run(string(tt.account), func(t *testing.T) {
			got := tt.account.Type()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAccountTypePanicsOnInvalidPrefix(t *testing.T) {
	assert.Panics(t, func() {
		Account("Invalid:Account").Type()
	})
}

func TestAccountTypePanicsOnMissingColon(t *testing.T) {
	assert.Panics(t, func() {
		Account("Assets").Type()
	})
}

func TestAccountTypeStringPanicsOnInvalid(t *testing.T) {
	assert.Panics(t, func() {
		_ = AccountType(0).String()
	})
}

func TestAccountTypeString(t *testing.T) {
	tests := []struct {
		accountType AccountType
		want        string
	}{
		{AccountTypeAssets, "Assets"},
		{AccountTypeLiabilities, "Liabilities"},
		{AccountTypeEquity, "Equity"},
		{AccountTypeIncome, "Income"},
		{AccountTypeExpenses, "Expenses"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.accountType.String())
		})
	}
}
