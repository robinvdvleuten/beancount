// Large Beancount File Generator
//
// This tool generates a large beancount file for performance testing and profiling.
// It creates realistic transactions with various features to stress-test the parser and ledger.
//
// Usage:
//
//	go run main.go > large.beancount
//	go run main.go 20000000 > large.beancount  # Specify target size in bytes
package main

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"time"
)

const (
	defaultTargetSize = 10 * 1024 * 1024 // 10MB
)

var (
	accounts = []string{
		"Assets:Bank:Checking",
		"Assets:Bank:Savings",
		"Assets:Brokerage:Cash",
		"Assets:Brokerage:AAPL",
		"Assets:Brokerage:MSFT",
		"Assets:Brokerage:GOOGL",
		"Assets:Brokerage:VTI",
		"Assets:Brokerage:VXUS",
		"Assets:Crypto:BTC",
		"Assets:Crypto:ETH",
		"Liabilities:CreditCard:Visa",
		"Liabilities:CreditCard:Amex",
		"Income:Salary",
		"Income:Bonus",
		"Income:Investments:Dividends",
		"Income:Investments:Interest",
		"Expenses:Food:Groceries",
		"Expenses:Food:Restaurant",
		"Expenses:Housing:Rent",
		"Expenses:Housing:Utilities",
		"Expenses:Transport:Gas",
		"Expenses:Transport:Transit",
		"Expenses:Shopping:Clothing",
		"Expenses:Shopping:Electronics",
		"Expenses:Entertainment:Movies",
		"Expenses:Entertainment:Concerts",
		"Expenses:Healthcare:Medical",
		"Expenses:Healthcare:Dental",
		"Expenses:Taxes:Federal",
		"Expenses:Taxes:State",
		"Expenses:Commissions",
		"Equity:Opening-Balances",
	}

	payees = []string{
		"Whole Foods", "Safeway", "Trader Joe's", "Costco",
		"Shell Gas", "Chevron", "BART", "Uber",
		"Landlord", "PG&E", "Comcast", "AT&T",
		"Amazon", "Target", "Best Buy", "Apple Store",
		"Netflix", "Spotify", "AMC Theaters",
		"Employer Inc", "Fidelity", "Vanguard",
	}

	narrations = []string{
		"Grocery shopping", "Fuel purchase", "Rent payment",
		"Salary deposit", "Stock purchase", "Utility bill",
		"Online purchase", "Restaurant dinner", "Coffee",
		"Monthly subscription", "Medical appointment",
		"Investment contribution", "Dividend payment",
		"Tax payment", "Insurance premium", "Gift",
	}

	tags = []string{
		"personal", "business", "vacation", "tax-deductible",
		"reimbursable", "investment", "savings",
	}

	links = []string{
		"invoice-2023-001", "receipt-march", "annual-review",
		"rebalance-q1", "tax-2023", "bonus-cycle",
	}

	currencies = []string{"USD", "EUR", "GBP", "CAD"}
	stocks     = []string{"AAPL", "MSFT", "GOOGL", "TSLA", "AMZN", "VTI", "VXUS"}
)

func main() {
	targetSize := defaultTargetSize
	if len(os.Args) > 1 {
		if size, err := strconv.Atoi(os.Args[1]); err == nil {
			targetSize = size
		}
	}

	// Note: rand.Seed is no longer needed in Go 1.20+
	// The global random generator is automatically seeded

	// Write header
	writeHeader()

	// Generate directives until we reach target size
	startDate := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	currentDate := startDate

	bytesWritten := 0
	transactionCount := 0

	for bytesWritten < targetSize {
		// Mix different types of directives
		switch rand.Intn(10) {
		case 0, 1: // 20% - Simple transaction
			output := generateSimpleTransaction(currentDate)
			fmt.Print(output)
			bytesWritten += len(output)
			transactionCount++

		case 2, 3: // 20% - Transaction with metadata
			output := generateTransactionWithMetadata(currentDate)
			fmt.Print(output)
			bytesWritten += len(output)
			transactionCount++

		case 4, 5: // 20% - Investment transaction with cost
			output := generateInvestmentTransaction(currentDate)
			fmt.Print(output)
			bytesWritten += len(output)
			transactionCount++

		case 6: // 10% - Multi-currency transaction
			output := generateMultiCurrencyTransaction(currentDate)
			fmt.Print(output)
			bytesWritten += len(output)
			transactionCount++

		case 7: // 10% - Complex transaction with tags and links
			output := generateComplexTransaction(currentDate)
			fmt.Print(output)
			bytesWritten += len(output)
			transactionCount++

		case 8: // 10% - Balance assertion
			output := generateBalanceAssertion(currentDate)
			fmt.Print(output)
			bytesWritten += len(output)

		case 9: // 10% - Price directive
			output := generatePriceDirective(currentDate)
			fmt.Print(output)
			bytesWritten += len(output)
		}

		// Advance date by 1-5 days
		currentDate = currentDate.AddDate(0, 0, rand.Intn(5)+1)
	}

	fmt.Fprintf(os.Stderr, "\nGenerated %d bytes with %d transactions\n", bytesWritten, transactionCount)
}

func writeHeader() {
	fmt.Println("; Large Beancount File for Performance Testing")
	fmt.Println("; Generated:", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Println()
	fmt.Println("option \"title\" \"Performance Test Ledger\"")
	fmt.Println("option \"operating_currency\" \"USD\"")
	fmt.Println()

	// Open all accounts
	fmt.Println("; Account declarations")
	openDate := "2020-01-01"
	for _, account := range accounts {
		fmt.Printf("%s open %s\n", openDate, account)
	}
	fmt.Println()
}

func generateSimpleTransaction(date time.Time) string {
	dateStr := date.String()
	payee := payees[rand.Intn(len(payees))]
	narration := narrations[rand.Intn(len(narrations))]
	amount := randAmount(10, 500)

	// Pick two accounts
	acc1 := accounts[rand.Intn(len(accounts))]
	acc2 := accounts[rand.Intn(len(accounts))]

	return fmt.Sprintf(`%s * "%s" "%s"
  %s  %s USD
  %s  %s USD

`, dateStr, payee, narration, acc1, amount, acc2, negateAmount(amount))
}

func generateTransactionWithMetadata(date time.Time) string {
	dateStr := date.String()
	payee := payees[rand.Intn(len(payees))]
	narration := narrations[rand.Intn(len(narrations))]
	amount := randAmount(50, 1000)

	acc1 := accounts[rand.Intn(len(accounts))]
	acc2 := accounts[rand.Intn(len(accounts))]

	return fmt.Sprintf(`%s * "%s" "%s"
  invoice: "INV-%d"
  category: "shopping"
  %s  %s USD
    note: "Purchase from vendor"
  %s  %s USD

`, dateStr, payee, narration, rand.Intn(10000), acc1, amount, acc2, negateAmount(amount))
}

func generateInvestmentTransaction(date time.Time) string {
	dateStr := date.String()
	stock := stocks[rand.Intn(len(stocks))]
	shares := rand.Intn(50) + 1
	pricePerShare := randAmount(50, 500)
	totalCost := calculateTotal(shares, pricePerShare)
	commission := "9.99"

	return fmt.Sprintf(`%s * "Buy %s"
  Assets:Brokerage:Cash  -%s USD
  Assets:Brokerage:%s  %d %s {%s USD}
  Expenses:Commissions  %s USD

`, dateStr, stock, addAmounts(totalCost, commission), stock, shares, stock, pricePerShare, commission)
}

func generateMultiCurrencyTransaction(date time.Time) string {
	dateStr := date.String()
	amount1 := randAmount(100, 2000)
	currency1 := currencies[0]
	currency2 := currencies[rand.Intn(len(currencies))]
	exchangeRate := randAmount(1, 2)
	amount2 := fmt.Sprintf("%.2f", parseAmount(amount1)*parseAmount(exchangeRate))

	return fmt.Sprintf(`%s * "Currency exchange"
  Assets:Bank:Checking  -%s %s @ %s %s
  Assets:Bank:Savings  %s %s

`, dateStr, amount1, currency1, exchangeRate, currency2, amount2, currency2)
}

func generateComplexTransaction(date time.Time) string {
	dateStr := date.String()
	payee := payees[rand.Intn(len(payees))]
	narration := narrations[rand.Intn(len(narrations))]

	tag1 := tags[rand.Intn(len(tags))]
	tag2 := tags[rand.Intn(len(tags))]
	link := links[rand.Intn(len(links))]

	amounts := []string{
		randAmount(100, 500),
		randAmount(50, 200),
		randAmount(20, 100),
	}

	total := addAmounts(amounts[0], addAmounts(amounts[1], amounts[2]))

	return fmt.Sprintf(`%s * "%s" "%s" ^%s #%s #%s
  receipt: "RCP-%d"
  Expenses:Food:Restaurant  %s USD
  Expenses:Food:Groceries  %s USD
  Expenses:Transport:Gas  %s USD
  Assets:Bank:Checking  -%s USD

`, dateStr, payee, narration, link, tag1, tag2, rand.Intn(100000), amounts[0], amounts[1], amounts[2], total)
}

func generateBalanceAssertion(date time.Time) string {
	dateStr := date.String()
	account := accounts[rand.Intn(len(accounts))]
	balance := randAmount(1000, 50000)

	return fmt.Sprintf("%s balance %s  %s USD\n\n", dateStr, account, balance)
}

func generatePriceDirective(date time.Time) string {
	dateStr := date.String()
	stock := stocks[rand.Intn(len(stocks))]
	price := randAmount(50, 500)

	return fmt.Sprintf("%s price %s %s USD\n\n", dateStr, stock, price)
}

// Helper functions

func randAmount(min, max float64) string {
	amount := min + rand.Float64()*(max-min)
	return fmt.Sprintf("%.2f", amount)
}

func parseAmount(amountStr string) float64 {
	val, _ := strconv.ParseFloat(amountStr, 64)
	return val
}

func negateAmount(amountStr string) string {
	val := parseAmount(amountStr)
	return fmt.Sprintf("%.2f", -val)
}

func addAmounts(a, b string) string {
	return fmt.Sprintf("%.2f", parseAmount(a)+parseAmount(b))
}

func calculateTotal(shares int, pricePerShare string) string {
	price := parseAmount(pricePerShare)
	return fmt.Sprintf("%.2f", float64(shares)*price)
}
