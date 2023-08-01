//go:generate peg -inline -switch grammar.peg

package beancount

type AST struct {
	Directives []Directive
	Options    []*Option
	Includes   []*Include
}

type WithMetadata interface {
	AddMetadata(...*Metadata)
}

type withMetadata struct {
	Metadata []*Metadata
}

func (w *withMetadata) AddMetadata(m ...*Metadata) {
	w.Metadata = append(w.Metadata, m...)
}

type Directive interface {
	WithMetadata

	Directive() string
}

type Commodity struct {
	withMetadata

	Date     string
	Currency string
}

var _ Directive = &Commodity{}

func (c *Commodity) Directive() string {
	return "commodity"
}

type Open struct {
	withMetadata

	Date                 string
	Account              string
	ConstraintCurrencies []string
	BookingMethod        string
}

var _ Directive = &Open{}

func (o *Open) Directive() string {
	return "open"
}

type Close struct {
	withMetadata

	Date    string
	Account string
}

var _ Directive = &Close{}

func (c *Close) Directive() string {
	return "close"
}

type Transaction struct {
	withMetadata

	Date      string
	Flag      string
	Payee     string
	Narration string

	Postings []*Posting
}

var _ Directive = &Transaction{}

func (t *Transaction) Directive() string {
	return "transaction"
}

type Posting struct {
	withMetadata

	Account string
	Flag    string
	Amount  *Amount
	Price   *Price
	Cost    *Amount
}

type Amount struct {
	Value    string
	Currency string
}

type Price struct {
	Amount

	Total bool
}

type Metadata struct {
	Key   string
	Value string
}

type Option struct {
	Name  string
	Value string
}

type Include struct {
	Filename string
}
