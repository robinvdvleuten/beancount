package parser

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
	"golang.org/x/exp/slices"
)

type Directives []Directive

func (d Directives) Len() int           { return len(d) }
func (d Directives) Swap(i, j int)      { d[i], d[j] = d[j], d[i] }
func (d Directives) Less(i, j int) bool { return d[i].date().Before(time.Time(d[j].date().Time)) }

type AST struct {
	Directives Directives `parser:"(@@"`
	Options    []*Option  `parser:"| @@"`
	Includes   []*Include `parser:"| @@ | ~ignore)*"`
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

	date() *Date
	Directive() string
}

type Commodity struct {
	Date     *Date  `parser:"@Date 'commodity'"`
	Currency string `parser:"@Ident"`

	withMetadata
}

var _ Directive = &Commodity{}

func (c *Commodity) date() *Date       { return c.Date }
func (c *Commodity) Directive() string { return "commodity" }

type Open struct {
	Date                 *Date    `parser:"@Date 'open'"`
	Account              Account  `parser:"@Account"`
	ConstraintCurrencies []string `parser:"(@Ident (',' @Ident)*)?"`
	BookingMethod        string   `parser:"@('STRICT' | 'NONE')?"`

	withMetadata `parser:""`
}

var _ Directive = &Open{}

func (o *Open) date() *Date       { return o.Date }
func (o *Open) Directive() string { return "open" }

type Close struct {
	Date    *Date   `parser:"@Date 'close'"`
	Account Account `parser:"@Account"`

	withMetadata
}

var _ Directive = &Close{}

func (c *Close) date() *Date       { return c.Date }
func (c *Close) Directive() string { return "close" }

type Balance struct {
	Date    *Date   `parser:"@Date 'balance'"`
	Account Account `parser:"@Account"`
	Amount  *Amount `parser:"@@"`

	withMetadata
}

var _ Directive = &Balance{}

func (b *Balance) date() *Date       { return b.Date }
func (b *Balance) Directive() string { return "balance" }

type Pad struct {
	Date       *Date   `parser:"@Date 'pad'"`
	Account    Account `parser:"@Account"`
	AccountPad Account `parser:"@Account"`

	withMetadata
}

var _ Directive = &Pad{}

func (p *Pad) date() *Date       { return p.Date }
func (p *Pad) Directive() string { return "pad" }

type Note struct {
	Date        *Date   `parser:"@Date 'note'"`
	Account     Account `parser:"@Account"`
	Description string  `parser:"@String"`

	withMetadata
}

var _ Directive = &Note{}

func (n *Note) date() *Date       { return n.Date }
func (n *Note) Directive() string { return "note" }

type Document struct {
	Date           *Date   `parser:"@Date 'document'"`
	Account        Account `parser:"@Account"`
	PathToDocument string  `parser:"@String"`

	withMetadata
}

var _ Directive = &Document{}

func (d *Document) date() *Date       { return d.Date }
func (d *Document) Directive() string { return "document" }

type Price struct {
	Date      *Date   `parser:"@Date 'price'"`
	Commodity string  `parser:"@Ident"`
	Amount    *Amount `parser:"@@"`

	withMetadata
}

var _ Directive = &Price{}

func (p *Price) date() *Date       { return p.Date }
func (p *Price) Directive() string { return "price" }

type Event struct {
	Date  *Date  `parser:"@Date 'event'"`
	Name  string `parser:"@String"`
	Value string `parser:"@String"`

	withMetadata
}

var _ Directive = &Event{}

func (e *Event) date() *Date       { return e.Date }
func (e *Event) Directive() string { return "event" }

type Transaction struct {
	Date      *Date  `parser:"@Date ('txn' | "`
	Flag      string `parser:"@('*' | '!' | 'P') )"`
	Payee     string `parser:"@(String (?= String))?"`
	Narration string `parser:"@String?"`

	withMetadata

	Postings []*Posting `parser:"@@*"`
}

var _ Directive = &Transaction{}

func (t *Transaction) date() *Date       { return t.Date }
func (t *Transaction) Directive() string { return "transaction" }

type Posting struct {
	Flag       string  `parser:"@('*' | '!')?"`
	Account    Account `parser:"@Account"`
	Amount     *Amount `parser:"(@@"`
	PriceTotal bool    `parser:"(('@' | @'@@')"`
	Price      *Amount `parser:"@@)?"`
	Cost       *Amount `parser:"('{' @@ '}')?)?"`

	withMetadata
}

type Amount struct {
	Value    string `parser:"@Number"`
	Currency string `parser:"@Ident"`
}

type Account string

var allowedAccountTypes = []string{"Assets", "Liabilities", "Equity", "Income", "Expenses"}

func (a *Account) Capture(values []string) error {
	t, _, _ := strings.Cut(values[0], ":")
	if !slices.Contains(allowedAccountTypes, t) {
		return fmt.Errorf(`unexpected account type "%s"`, t)
	}

	*a = Account(values[0])
	return nil
}

type Date struct {
	time.Time
}

func (d *Date) Capture(values []string) error {
	t, err := time.Parse("2006-01-02", values[0])
	if err != nil {
		return err
	}

	d.Time = t
	return nil
}

type Metadata struct {
	Key   string `parser:"@Ident ':'"`
	Value string `parser:"@(~'\\n'+)"`
}

type Option struct {
	Name  string `parser:"'option' @String"`
	Value string `parser:"@String"`
}

type Include struct {
	Filename  string `parser:"'include' @String"`
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
		participle.Union[Directive](
			&Commodity{},
			&Open{},
			&Close{},
			&Balance{},
			&Pad{},
			&Note{},
			&Document{},
			&Price{},
			&Event{},
			&Transaction{},
		),
		participle.UseLookahead(2),
	)
)

// Parse AST from an io.Reader.
func Parse(r io.Reader) (*AST, error) {
	ast, err := parser.Parse("", r)
	if err != nil {
		return nil, err
	}

	return ast, SortDirectives(ast)
}

// ParseString parses AST from a string.
func ParseString(str string) (*AST, error) {
	ast, err := parser.ParseString("", str)
	if err != nil {
		return nil, err
	}

	return ast, SortDirectives(ast)
}

// ParseBytes parses AST from bytes.
func ParseBytes(data []byte) (*AST, error) {
	ast, err := parser.ParseBytes("", data)
	if err != nil {
		return nil, err
	}

	return ast, SortDirectives(ast)
}

// SortDirectives sort all directives by their parsed date.
//
// This is called automatically during Parse*(), but can be called on a manually constructed AST.
func SortDirectives(ast *AST) error {
	sort.Sort(ast.Directives)
	return nil
}
