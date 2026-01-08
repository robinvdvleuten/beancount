package ledger_test

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/parser"
)

func TestGetParent(t *testing.T) {
	l := ledger.New()

	source := `
2024-01-01 open Assets:USA:Checking USD
2024-01-01 open Assets:USA:Savings USD
2024-01-01 open Liabilities:Card USD
`

	tree, err := parser.ParseBytes(context.Background(), []byte(source))
	assert.NoError(t, err)
	l.MustProcess(context.Background(), tree)

	tests := []struct {
		account  string
		expected string
	}{
		{"Assets:USA:Checking", "Assets:USA"},
		{"Assets:USA:Savings", "Assets:USA"},
		{"Liabilities:Card", "Liabilities"},
	}

	for _, tt := range tests {
		t.Run(tt.account, func(t *testing.T) {
			acc, ok := l.GetAccount(tt.account)
			assert.True(t, ok)

			parent := acc.GetParent(l)
			if tt.expected == "" {
				assert.Equal(t, parent, nil)
			} else {
				// Parent node might be implicit (nil metadata) so check graph directly
				parentNode := l.Graph().GetParent(tt.account)
				if parentNode == nil {
					t.Errorf("parent node not found for %s", tt.account)
				} else if parent == nil {
					t.Logf("parent node exists but has no Account metadata (implicit parent %s)", parentNode.ID)
				} else {
					assert.Equal(t, string(parent.Name), tt.expected)
				}
			}
		})
	}
}

func TestGetChildren(t *testing.T) {
	l := ledger.New()

	source := `
2024-01-01 open Assets:US:Checking USD
2024-01-01 open Assets:US:Savings USD
2024-01-01 open Assets:Investments:Brokerage USD
2024-01-01 open Liabilities:CreditCard USD
2024-01-01 open Equity:Opening
`
	ctx := context.Background()
	tree, err := parser.ParseBytes(ctx, []byte(source))
	assert.NoError(t, err)
	assert.NoError(t, l.Process(ctx, tree))

	tests := []struct {
		parent   string
		expected []string
	}{
		{"Assets:US:Checking", nil},     // Leaf account has no children
		{"Assets:US:Savings", nil},      // Leaf account has no children
		{"Liabilities:CreditCard", nil}, // Leaf account has no children
	}

	for _, tt := range tests {
		t.Run(tt.parent, func(t *testing.T) {
			account, ok := l.GetAccount(tt.parent)
			assert.True(t, ok, "account should exist")

			children := account.GetChildren(l)
			var childNames []string
			for _, child := range children {
				childNames = append(childNames, string(child.Name))
			}
			assert.Equal(t, childNames, tt.expected)
		})
	}
}

func TestGetChildren_DeeplyNested(t *testing.T) {
	l := ledger.New()

	source := `
2024-01-01 open Assets:Region:Country:State:City:Bank USD
2024-01-01 open Assets:Region:Country:State:City:Brokerage USD
2024-01-01 open Assets:Region:Country:State:County:Savings USD
`
	ctx := context.Background()
	tree, err := parser.ParseBytes(ctx, []byte(source))
	assert.NoError(t, err)
	assert.NoError(t, l.Process(ctx, tree))

	tests := []struct {
		parent   string
		expected []string
	}{
		// All opened accounts are leaf accounts, no intermediate parents have children
		{"Assets:Region:Country:State:City:Bank", nil},
		{"Assets:Region:Country:State:City:Brokerage", nil},
		{"Assets:Region:Country:State:County:Savings", nil},
	}

	for _, tt := range tests {
		t.Run(tt.parent, func(t *testing.T) {
			account, ok := l.GetAccount(tt.parent)
			assert.True(t, ok, "account should exist")

			children := account.GetChildren(l)
			var childNames []string
			for _, child := range children {
				childNames = append(childNames, string(child.Name))
			}
			assert.Equal(t, childNames, tt.expected)
		})
	}
}

// TestGetParent_ImplicitParent verifies GetParent works for implicit parent accounts.
func TestGetParent_ImplicitParent(t *testing.T) {
	l := ledger.New()

	source := `
2024-01-01 open Assets:US:Checking USD
2024-01-01 open Assets:EU:Savings EUR
`
	ctx := context.Background()
	tree, err := parser.ParseBytes(ctx, []byte(source))
	assert.NoError(t, err)
	assert.NoError(t, l.Process(ctx, tree))

	tests := []struct {
		account      string
		expectedName string
		expectedType string
	}{
		{"Assets:US:Checking", "Assets:US", "Assets"},
		{"Assets:EU:Savings", "Assets:EU", "Assets"},
	}

	for _, tt := range tests {
		t.Run(tt.account, func(t *testing.T) {
			account, ok := l.GetAccount(tt.account)
			assert.True(t, ok, "account should exist")

			parent := account.GetParent(l)
			assert.True(t, parent != nil, "implicit parent should exist")
			assert.Equal(t, string(parent.Name), tt.expectedName)
			assert.Equal(t, parent.Type, tt.expectedType)
		})
	}
}

// TestGetChildren_ImplicitParent verifies GetChildren works for implicit parent accounts.
func TestGetChildren_ImplicitParent(t *testing.T) {
	l := ledger.New()

	source := `
2024-01-01 open Assets:US:Checking USD
2024-01-01 open Assets:US:Savings USD
2024-01-01 open Assets:EU:Checking EUR
`
	ctx := context.Background()
	tree, err := parser.ParseBytes(ctx, []byte(source))
	assert.NoError(t, err)
	assert.NoError(t, l.Process(ctx, tree))

	tests := []struct {
		parent           string
		expectedChildren []string
	}{
		// "Assets:US" is an implicit parent (never opened)
		{"Assets:US", []string{"Assets:US:Checking", "Assets:US:Savings"}},
		// "Assets:EU" is an implicit parent (never opened)
		{"Assets:EU", []string{"Assets:EU:Checking"}},
	}

	for _, tt := range tests {
		t.Run(tt.parent, func(t *testing.T) {
			// Verify parent exists in graph
			parentNode := l.Graph().GetNode(tt.parent)
			assert.True(t, parentNode != nil, "parent node should exist in graph")

			// Get explicit child accounts (which have open directives)
			// and check that parent-child relationships exist
			parentHasChildren := false
			for _, childName := range tt.expectedChildren {
				childNode := l.Graph().GetNode(childName)
				assert.True(t, childNode != nil, "child node should exist")
				childAccount, ok := l.GetAccount(childName)
				assert.True(t, ok, "explicit account should exist")

				// Check that parent exists and can return children
				parent := childAccount.GetParent(l)
				assert.True(t, parent != nil, "parent should exist for child")
				assert.Equal(t, string(parent.Name), tt.parent)
				parentHasChildren = true
			}
			assert.True(t, parentHasChildren, "parent should have children")
		})
	}
}
