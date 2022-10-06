//go:generate peg -inline -switch grammar.peg

package beancount

import (
	"io"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
)

type AST struct {
	Directives []Directive `parser:"(@@"`
	Options    []*Option   `parser:"| @@ | ~ignore)*"`
}

type WithMetadata interface {
	AddMetadata(...*Metadata)
}

type withMetadata struct {
	Metadata []*Metadata `parser:"@@*"`
}

func (w *withMetadata) AddMetadata(m ...*Metadata) {
	w.Metadata = append(w.Metadata, m...)
}

type Directive interface {
	WithMetadata

	Directive() string
}

type Commodity struct {
	Date     string `parser:"@Date 'commodity'"`
	Currency string `parser:"@Ident"`

	withMetadata
}

var _ Directive = &Commodity{}

func (c *Commodity) Directive() string {
	return "commodity"
}

type Open struct {
	Date                 string   `parser:"@Date 'open'"`
	Account              string   `parser:"@Account"`
	ConstraintCurrencies []string `parser:"@Ident (',' @Ident)*"`
	BookingMethod        string   `parser:"@('STRICT' | 'NONE')?"`

	withMetadata `parser:""`
}

var _ Directive = &Open{}

func (o *Open) Directive() string {
	return "open"
}

type Close struct {
	Date    string `parser:"@Date 'close'"`
	Account string `parser:"@Account"`

	withMetadata
}

var _ Directive = &Close{}

func (c *Close) Directive() string {
	return "close"
}

type Transaction struct {
	Date      string `parser:"@Date ('txn' | "`
	Flag      string `parser:"@('*' | '!') )"`
	Payee     string `parser:"@(String (?= String))?"`
	Narration string `parser:"@String?"`

	withMetadata

	Postings []*Posting `parser:"@@*"`
}

var _ Directive = &Transaction{}

func (t *Transaction) Directive() string {
	return "transaction"
}

type Posting struct {
	Flag    string  `parser:"@('*' | '!')?"`
	Account string  `parser:"@Account"`
	Amount  *Amount `parser:"@@?"`
	Price   *Price  `parser:"@@?"`
	Cost    *Amount `parser:"('{' @@ '}')?"`

	withMetadata
}

type Amount struct {
	Value    string `parser:"@Number"`
	Currency string `parser:"@Ident"`
}

type Price struct {
	Total bool `parser:"('@' | @'@@')"`

	Amount
}

type Metadata struct {
	Key   string `parser:"@Ident ':'"`
	Value string `parser:"@(~'\\n'+)"`
}

type Option struct {
	Name  string `parser:"'option' @String"`
	Value string `parser:"@String"`
}

var (
	lex = lexer.MustSimple([]lexer.SimpleRule{
		{"Date", `\d{4}-\d{2}-\d{2}`},
		{"Account", `[A-Z][[:alpha:]]*(:[0-9A-Z][[:alnum:]]+(-[[:alnum:]]+)?)+`},
		{"String", `"[^"]*"`},
		{"Number", `[-+]?(\d*\.)?\d+`},
		{"Ident", `[A-Za-z][0-9A-Za-z_-]*`},
		{"Punct", `[!*:,@{}]+`},
		{"Comment", `;[^\n]*\n`},
		{"Whitespace", `[[:space:]]`},
		{"ignore", `[\s\S]*`},
	})

	parser = participle.MustBuild[AST](
		participle.Lexer(lex),
		participle.Unquote("String"),
		participle.Elide("Comment", "Whitespace"),
		participle.Union[Directive](&Commodity{}, &Open{}, &Close{}, &Transaction{}),
		participle.UseLookahead(2),
	)
)

func Parse(r io.Reader) (*AST, error) {
	ast, err := parser.Parse("", r)
	if err != nil {
		return nil, err
	}

	return ast, nil
}

// ParseString parses telegram from a string.
func ParseString(str string) (*AST, error) {
	ast, err := parser.ParseString("", str)
	if err != nil {
		return nil, err
	}

	return ast, nil
}

// ParseBytes parses telegram from bytes.
func ParseBytes(data []byte) (*AST, error) {
	ast, err := parser.ParseBytes("", data)
	if err != nil {
		return nil, err
	}

	return ast, nil
}
