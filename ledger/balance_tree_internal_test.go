package ledger

import (
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
)

// testAccount builds an account holding the given postings, each a date
// and a USD amount, in its inventory and its posting history.
func testAccount(name string, postings ...[2]string) *Account {
	account := &Account{Name: ast.Account(name), Type: ast.Account(name).Root(), Inventory: NewInventory()}
	for _, p := range postings {
		posting := ast.NewPosting(account.Name, ast.WithAmount(p[1], "USD"))
		txn := ast.NewTransaction(newTestDate(p[0]), "t", ast.WithPostings(posting))
		account.Inventory.AddLot("USD", mustParseDec(p[1]), nil)
		account.Postings = append(account.Postings, &AccountPosting{Transaction: txn, Posting: posting})
	}
	return account
}

func testAccounts(accounts ...*Account) map[string]*Account {
	byName := make(map[string]*Account, len(accounts))
	for _, account := range accounts {
		byName[string(account.Name)] = account
	}
	return byName
}

// treeLines renders each node as "name balance" for comparing tree shapes.
func treeLines(nodes []*BalanceNode) []string {
	var lines []string
	var walk func(node *BalanceNode)
	walk = func(node *BalanceNode) {
		lines = append(lines, node.Name+" "+node.Balance.Get("USD").String())
		for _, child := range node.Children {
			walk(child)
		}
	}
	for _, node := range nodes {
		walk(node)
	}
	return lines
}

func TestNewBalanceTreeNesting(t *testing.T) {
	accounts := testAccounts(
		testAccount("Assets:US:Checking", [2]string{"2024-01-01", "100"}),
		testAccount("Assets:US:Savings", [2]string{"2024-01-01", "50"}),
		testAccount("Assets:Cash", [2]string{"2024-01-01", "5"}),
		testAccount("Income:Salary", [2]string{"2024-01-01", "-155"}),
	)

	tree, err := newBalanceTree(accounts, NewConfig(), nil, nil, nil)
	assert.NoError(t, err)
	assert.Equal(t, []string{
		"Assets 155",
		"Assets:Cash 5",
		"Assets:US 150",
		"Assets:US:Checking 100",
		"Assets:US:Savings 50",
		"Income -155",
		"Income:Salary -155",
	}, treeLines(tree.Roots))
	assert.Equal(t, []string{"USD"}, tree.Currencies)
}

// An intermediate account with a single open descendant must still be
// linked to its own parent, so the descendant counts toward the root.
func TestNewBalanceTreeSingleLeafUnderIntermediates(t *testing.T) {
	accounts := testAccounts(
		testAccount("Expenses:Health:Dental:Insurance", [2]string{"2024-01-01", "7"}),
		testAccount("Expenses:Food", [2]string{"2024-01-01", "3"}),
		testAccount("Liabilities:US:Chase:Slate", [2]string{"2024-01-01", "-10"}),
	)

	tree, err := newBalanceTree(accounts, NewConfig(), nil, nil, nil)
	assert.NoError(t, err)
	assert.Equal(t, []string{
		"Liabilities -10",
		"Liabilities:US -10",
		"Liabilities:US:Chase -10",
		"Liabilities:US:Chase:Slate -10",
		"Expenses 10",
		"Expenses:Food 3",
		"Expenses:Health 7",
		"Expenses:Health:Dental 7",
		"Expenses:Health:Dental:Insurance 7",
	}, treeLines(tree.Roots))
}

func TestNewBalanceTreeTypeWithoutAccounts(t *testing.T) {
	accounts := testAccounts(testAccount("Assets:Cash", [2]string{"2024-01-01", "5"}))

	tree, err := newBalanceTree(accounts, NewConfig(), []ast.AccountType{ast.AccountTypeAssets, ast.AccountTypeLiabilities}, nil, nil)
	assert.NoError(t, err)
	assert.Equal(t, []string{"Assets 5", "Assets:Cash 5"}, treeLines(tree.Roots))
}

func TestNewBalanceTreeConfiguredTypeNames(t *testing.T) {
	cfg := NewConfig()
	cfg.AccountNames.Assets = "Vermoegen"
	accounts := testAccounts(
		testAccount("Vermoegen:Kasse", [2]string{"2024-01-01", "5"}),
		testAccount("Assets:Cash", [2]string{"2024-01-01", "7"}),
	)

	tree, err := newBalanceTree(accounts, cfg, []ast.AccountType{ast.AccountTypeAssets}, nil, nil)
	assert.NoError(t, err)
	assert.Equal(t, []string{"Vermoegen 5", "Vermoegen:Kasse 5"}, treeLines(tree.Roots))
}

func TestNewBalanceTreeDateRange(t *testing.T) {
	accounts := testAccounts(testAccount("Expenses:Food",
		[2]string{"2024-01-05", "10"},
		[2]string{"2024-02-05", "20"},
		[2]string{"2024-03-05", "40"},
	))

	tree, err := newBalanceTree(accounts, NewConfig(), nil, newTestDate("2024-02-01"), newTestDate("2024-02-29"))
	assert.NoError(t, err)
	assert.Equal(t, []string{"Expenses 20", "Expenses:Food 20"}, treeLines(tree.Roots))
	assert.Equal(t, "2024-02-01", *tree.StartDate)
	assert.Equal(t, "2024-02-29", *tree.EndDate)

	tree, err = newBalanceTree(accounts, NewConfig(), nil, newTestDate("2024-02-05"), newTestDate("2024-02-05"))
	assert.NoError(t, err)
	assert.Equal(t, []string{"Expenses 20", "Expenses:Food 20"}, treeLines(tree.Roots))

	tree, err = newBalanceTree(accounts, NewConfig(), nil, nil, newTestDate("2024-02-05"))
	assert.NoError(t, err)
	assert.Equal(t, []string{"Expenses 30", "Expenses:Food 30"}, treeLines(tree.Roots))
	assert.Equal(t, (*string)(nil), tree.StartDate)
	assert.Equal(t, "2024-02-05", *tree.EndDate)

	_, err = newBalanceTree(accounts, NewConfig(), nil, newTestDate("2024-03-01"), newTestDate("2024-02-01"))
	assert.Error(t, err)
}
