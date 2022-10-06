package main

import (
	"fmt"
	"os"

	"github.com/alecthomas/kong"
	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
	"github.com/alecthomas/repr"
)

type Beancount struct {
	Commodities  []*Commodity   `(@@`
	Accounts     []*Account     ` | @@`
	Transactions []*Transaction ` | @@ | ~ignore)*`
}

type Commodity struct {
	Date      string `@Date`
	Directive string `"commodity"`
	Currency  string `@Ident`
}

type Account struct {
	Date                 string   `@Date`
	Directive            string   `@("close" | "open")`
	Name                 string   `@(Ident (":" Ident)*)`
	ConstraintCurrencies []string `@Ident*`
}

type Transaction struct {
	Date      string `@Date`
	Directive string `( "txn"`
	Flag      string ` | @("*" | "!") )`
	Payee     string `@(String (?= String))?`
	Narration string `@String?`

	Postings *[]Posting `@@*`
}

type Posting struct {
	Account string  `@(Ident (":" Ident)*)`
	Amount  *Amount `@@`
	Price   *Amount `( "@" "@"? @@ )?`
}

type Amount struct {
	Value    float64 `@Number`
	Currency string  `@Ident`
}

var (
	cli struct {
		EBNF bool   `help"Dump EBNF."`
		File string `help:"Beancount file to parse." arg:"" type: "existingfile"`
	}

	beancountLexer = lexer.MustSimple([]lexer.SimpleRule{
		{"Date", `\d\d\d\d-\d\d-\d\d`},
		{"Ident", `[a-zA-Z](\w(-\w)?)*`},
		{"Number", `[-+]?(\d*\.)?\d+`},
		{"String", `"[^"]*"`},
		{"Punct", `[-.,:;*!@#({)}]`},
		{"whitespace", `[\s\n]+`},
		{"ignore", `[\s\S]*`},
	})

	parser = participle.MustBuild[Beancount](
		participle.Lexer(beancountLexer),
	)
)

func main() {
	ctx := kong.Parse(&cli)
	if cli.EBNF {
		fmt.Println(parser.String())
		ctx.Exit(0)
	}

	file, err := os.Open(cli.File)
	ctx.FatalIfErrorf(err)
	defer file.Close()

	ast, err := parser.Parse(cli.File, file)
	ctx.FatalIfErrorf(err)

	repr.Println(ast)
}
