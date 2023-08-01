package main

import (
	"os"

	"github.com/alecthomas/kong"
	"github.com/alecthomas/repr"
	"github.com/robinvdvleuten/beancount"
)

var (
	cli struct {
		File string `help:"Beancount file to parse." arg:"" type: "existingfile"`
	}
)

func main() {
	ctx := kong.Parse(&cli)

	raw, err := os.ReadFile(cli.File)
	ctx.FatalIfErrorf(err)

	b, err := beancount.Parse(string(raw))
	ctx.FatalIfErrorf(err)

	repr.Println(b)
}
