package beancount

import (
	"os"

	"github.com/alecthomas/kong"
	"github.com/alecthomas/repr"
	"github.com/robinvdvleuten/beancount/formatter"
	"github.com/robinvdvleuten/beancount/parser"
)

type CheckCmd struct {
	File []byte `help:"Beancount input filename." arg:"" type:"filecontent"`
}

func (cmd *CheckCmd) Run(ctx *kong.Context) error {
	ast, err := parser.ParseBytes(cmd.File)
	if err != nil {
		return err
	}

	repr.Println(ast)

	return nil
}

type FormatCmd struct {
	File           []byte `help:"Beancount input filename." arg:"" type:"filecontent"`
	CurrencyColumn int    `help:"Column for currency alignment (overrides prefix-width and num-width if set, auto if 0)." default:"0"`
	PrefixWidth    int    `help:"Width in characters for account names (auto if 0)." default:"0"`
	NumWidth       int    `help:"Width for numbers (auto if 0)." default:"0"`
}

func (cmd *FormatCmd) Run(ctx *kong.Context) error {
	// Parse the input file
	ast, err := parser.ParseBytes(cmd.File)
	if err != nil {
		return err
	}

	// Create formatter with options
	var opts []formatter.Option
	if cmd.CurrencyColumn > 0 {
		opts = append(opts, formatter.WithCurrencyColumn(cmd.CurrencyColumn))
	}
	if cmd.PrefixWidth > 0 {
		opts = append(opts, formatter.WithPrefixWidth(cmd.PrefixWidth))
	}
	if cmd.NumWidth > 0 {
		opts = append(opts, formatter.WithNumWidth(cmd.NumWidth))
	}
	f := formatter.New(opts...)

	// Format and output to stdout
	if err := f.Format(ast, cmd.File, os.Stdout); err != nil {
		return err
	}

	return nil
}

type Commands struct {
	Check  CheckCmd  `cmd:"" help:"Parse, check and realize a beancount input file."`
	Format FormatCmd `cmd:"" help:"Format a beancount file to align numbers and currencies."`
}
