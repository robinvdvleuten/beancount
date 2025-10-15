package parser

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
	"golang.org/x/exp/slices"
)

type Directives []Directive

func (d Directives) Len() int           { return len(d) }
func (d Directives) Swap(i, j int)      { d[i], d[j] = d[j], d[i] }
func (d Directives) Less(i, j int) bool { return compareDirectives(d[i], d[j]) < 0 }

// compareDirectives compares two directives by their date.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func compareDirectives(a, b Directive) int {
	if a.date().Before(b.date().Time) {
		return -1
	} else if a.date().After(b.date().Time) {
		return 1
	}
	return 0
}

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
	Links     []Link `parser:"@Link*"`
	Tags      []Tag  `parser:"@Tag*"`

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
	Cost       *Cost   `parser:"@@?"`
	PriceTotal bool    `parser:"(('@' | @'@@')"`
	Price      *Amount `parser:"@@)?)?"`

	withMetadata
}

type Amount struct {
	Value    string `parser:"@Number"`
	Currency string `parser:"@Ident"`
}

type Cost struct {
	Amount *Amount `parser:"'{' @@"`
	Date   *Date   `parser:"(',' @Date)?"`
	Label  string  `parser:"(',' @String)? '}'"`
}

type Account string

func (a *Account) Capture(values []string) error {
	parts := strings.Split(values[0], ":")

	// Validate first segment (account type)
	if len(parts) < 2 {
		return fmt.Errorf("account must have at least two segments: %s", values[0])
	}

	t := parts[0]
	switch t {
	case "Assets", "Liabilities", "Equity", "Income", "Expenses":
	default:
		return fmt.Errorf(`unexpected account type "%s"`, t)
	}

	// Validate subsequent segments
	for i := 1; i < len(parts); i++ {
		if !isValidAccountSegment(parts[i]) {
			return fmt.Errorf("invalid account segment at position %d: %s", i, parts[i])
		}
	}

	*a = Account(values[0])
	return nil
}

// isValidAccountSegment checks if an account segment (after first) is valid.
// Must start with uppercase letter or digit, can contain alphanumerics and hyphens.
func isValidAccountSegment(segment string) bool {
	if len(segment) == 0 {
		return false
	}

	// First character must be uppercase or digit
	first := segment[0]
	if (first < 'A' || first > 'Z') && (first < '0' || first > '9') {
		return false
	}

	// Rest can be alphanumeric or hyphen
	for i := 1; i < len(segment); i++ {
		ch := segment[i]
		if (ch < 'A' || ch > 'Z') && (ch < 'a' || ch > 'z') &&
			(ch < '0' || ch > '9') && ch != '-' {
			return false
		}
	}

	return true
}

type Date struct {
	time.Time
}

func (d *Date) Capture(values []string) error {
	s := values[0]
	// Lexer guarantees format \d{4}-\d{2}-\d{2}, so we can parse directly

	// Parse year (positions 0-3)
	year := int(s[0]-'0')*1000 + int(s[1]-'0')*100 +
		int(s[2]-'0')*10 + int(s[3]-'0')

	// Parse month (positions 5-6)
	month := int(s[5]-'0')*10 + int(s[6]-'0')

	// Parse day (positions 8-9)
	day := int(s[8]-'0')*10 + int(s[9]-'0')

	// Basic validation (time.Date will normalize edge cases)
	if month < 1 || month > 12 || day < 1 || day > 31 {
		return fmt.Errorf("invalid date: %s", s)
	}

	d.Time = time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	return nil
}

type Link string

func (l *Link) Capture(values []string) error {
	// Lexer guarantees format \^[A-Za-z0-9_-]+, so we can skip first character
	*l = Link(values[0][1:])
	return nil
}

type Tag string

func (t *Tag) Capture(values []string) error {
	// Lexer guarantees format #[A-Za-z0-9_-]+, so we can skip first character
	*t = Tag(values[0][1:])
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
	Filename string `parser:"'include' @String"`
}

var (
	lex = lexer.MustSimple([]lexer.SimpleRule{
		{"Date", `\d{4}-\d{2}-\d{2}`},
		{"Account", `[A-Z][A-Za-z]*:[A-Za-z0-9][A-Za-z0-9:-]*`},
		{"String", `"[^"]*"`},
		{"Number", `[-+]?(\d*\.)?\d+`},
		{"Link", `\^[A-Za-z0-9_-]+`},
		{"Tag", `#[A-Za-z0-9_-]+`},
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

// isSorted checks if directives are already sorted by date.
func isSorted(d Directives) bool {
	for i := 1; i < len(d); i++ {
		if d.Less(i, i-1) {
			return false
		}
	}
	return true
}

// SortDirectives sort all directives by their parsed date.
//
// This is called automatically during Parse*(), but can be called on a manually constructed AST.
func SortDirectives(ast *AST) error {
	// Skip sorting if already sorted (common case for well-maintained files)
	if isSorted(ast.Directives) {
		return nil
	}

	// Use pdqsort for better performance when sorting is needed
	slices.SortFunc(ast.Directives, compareDirectives)
	return nil
}
