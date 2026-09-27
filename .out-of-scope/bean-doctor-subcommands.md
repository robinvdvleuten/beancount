# bean-doctor subcommands other than lex and missing_open

`beancount doctor` implements `lex` and `missing_open` (the latter tracked in
#481). We don't plan to add bean-doctor's other subcommands.

## Why this is out of scope

bean-doctor is a grab bag of debugging aids for beancount's own Python
internals. Most of them only make sense against that implementation, and the
rest are already covered by commands we have. Per subcommand (probed against
bean-doctor 2.3.6):

- **`context`** prints the balances before and after one transaction, its
  unbooked and booked forms, and its residual. Parity would mean copying
  beancount's MD5 entry hash (`Hash:c01bcacc…`, computed over its Python
  data model) and a layout of internals nobody parses. That is a lot of
  quirk-matching for a debugging view; `query` already gives balances up to
  any date.
- **`linked`** lists the transactions sharing a link with the one at a line.
  `query "select date, narration, position where 'trip' in links"` does this
  already.
- **`parse`** and **`roundtrip`** exercise beancount's Python parser and
  printer. Our parser has its own fuzz tests and compliance suites, and
  `format` is checked against `bean-format`.
- **`dump_lexer`** is an alias of `lex`.
- **`list_options`**, **`print_options`** and **`display_context`** print
  Python option defaults and the inferred decimal precision, both internals.
- **`directories`** checks a `document` directory tree against account names,
  a rarely used workflow.
- **`deps`**, **`checkdeps`** and **`validate_html`** check Python packages
  and bean-web output, and have no counterpart here.

Reopen a subcommand if users ask for it for a concrete debugging need that
`check` and `query` don't meet.

## Prior requests

- #394: "doctor: missing bean-doctor subcommands"
