package main

import (
	"github.com/alecthomas/kong"
	"github.com/robinvdvleuten/beancount"
)

var (
	cli struct {
		beancount.Commands
	}
)

func main() {
	ctx := kong.Parse(&cli)

	err := ctx.Run()
	ctx.FatalIfErrorf(err)
}
