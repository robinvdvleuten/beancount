package main

import (
	"fmt"
	"os"

	"github.com/alecthomas/kong"
	"github.com/alecthomas/repr"
)

var (
	cli struct {
		EBNF bool   `help"Dump EBNF."`
		File string `help:"Beancount file to parse." arg:"" type: "existingfile"`
	}
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
