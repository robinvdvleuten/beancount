// Package ast declares the types used to represent syntax trees for Beancount files.
//
// These types represent the structure of Beancount directives, transactions, and related
// elements that make up a Beancount ledger file. The AST (Abstract Syntax Tree) can be
// created by parsing a Beancount file using the parser package, or constructed
// programmatically for generating Beancount output.
package ast

import (
	"fmt"
	"slices"
	"strings"
)

// Directives is a slice of Directive that implements sort.Interface.
type Directives []Directive

func (d Directives) Len() int           { return len(d) }
func (d Directives) Swap(i, j int)      { d[i], d[j] = d[j], d[i] }
func (d Directives) Less(i, j int) bool { return compareDirectives(d[i], d[j]) < 0 }

// compareDirectives compares two directives by their date, then by type priority,
// then by source position (line number). Returns -1 if a < b, 0 if a == b, 1 if a > b.
// This matches the same-date processing order of the official Python beancount
// implementation.
//
// For same-date directives, the processing order is:
//  1. Open (accounts must be opened before use)
//  2. Balance (assertions apply at the beginning of the day)
//  3. All other directives (transactions, pad, note, price, etc.)
//  4. Document
//  5. Close (processed last)
//  6. Within the same priority, sort by line number
func compareDirectives(a, b Directive) int {
	// First compare by date
	if a.Date().Before(b.Date().Time) {
		return -1
	} else if a.Date().After(b.Date().Time) {
		return 1
	}

	// Same date - compare by type priority
	aPriority := directiveTypePriority(a)
	bPriority := directiveTypePriority(b)
	if aPriority < bPriority {
		return -1
	} else if aPriority > bPriority {
		return 1
	}

	// Same date and type - compare by line number to preserve source order
	aLine := getDirectiveLine(a)
	bLine := getDirectiveLine(b)
	if aLine < bLine {
		return -1
	} else if aLine > bLine {
		return 1
	}

	return 0
}

// getDirectiveLine extracts the line number from a directive for stable sorting.
func getDirectiveLine(d Directive) int {
	return d.Position().Line
}

// directiveTypePriority returns the processing priority for a directive type.
// Lower numbers are processed first.
func directiveTypePriority(d Directive) int {
	switch d.(type) {
	case *Open:
		return -2
	case *Balance:
		return -1
	case *Document:
		return 1
	case *Close:
		return 2
	default:
		return 0
	}
}

// AST represents a parsed Beancount file containing directives, options, includes,
// and other top-level elements.
type AST struct {
	Directives Directives
	Options    []*Option
	Includes   []*Include
	Plugins    []*Plugin
	Pushtags   []*Pushtag
	Poptags    []*Poptag
	Pushmetas  []*Pushmeta
	Popmetas   []*Popmeta
	Comments   []*Comment
	BlankLines []*BlankLine

	pushPopApplied bool
}

// WithMetadata is an interface for AST nodes that can have metadata attached.
type WithMetadata interface {
	AddMetadata(...*Metadata)
	GetMetadata() []*Metadata
}

// WithComment is an interface for AST nodes that can have an inline comment attached.
type WithComment interface {
	GetComment() *Comment
	SetComment(*Comment)
}

// withMetadata is an embeddable struct that implements WithMetadata.
type withMetadata struct {
	Metadata []*Metadata
}

func (w *withMetadata) AddMetadata(m ...*Metadata) {
	w.Metadata = append(w.Metadata, m...)
}

func (w *withMetadata) GetMetadata() []*Metadata { return w.Metadata }

func (w *withMetadata) HasMetadata() bool {
	return len(w.Metadata) > 0
}

// withComment is an embeddable struct that implements WithComment.
type withComment struct {
	InlineComment *Comment // Attached inline comment at end of directive line
}

func (w *withComment) GetComment() *Comment {
	return w.InlineComment
}

func (w *withComment) SetComment(c *Comment) {
	w.InlineComment = c
}

// Directive is the interface implemented by all Beancount directive types.
type Directive interface {
	WithMetadata
	WithComment
	Positioned

	Date() *Date
	Kind() DirectiveKind
}

// positionedItem represents any AST item that has a position in the source file.
type positionedItem struct {
	pos       Position
	directive Directive
	pushtag   *Pushtag
	poptag    *Poptag
	pushmeta  *Pushmeta
	popmeta   *Popmeta
}

// PushPopError reports a pushed tag or metadata key left open at the end of
// a file, or a pop of one that is not pushed.
type PushPopError struct {
	Pos     Position
	Message string
}

func (e *PushPopError) Error() string {
	return fmt.Sprintf("%s:%d: %s", e.Pos.Filename, e.Pos.Line, e.Message)
}

// GetPosition returns the position of the offending push or pop directive.
func (e *PushPopError) GetPosition() Position { return e.Pos }

// ApplyPushPopDirectives applies pushtag/poptag and pushmeta/popmeta directives
// to transactions and other directives in file order (before date sorting).
// Like beancount, pushed tags form a list and pushed metadata a stack per
// key; it returns an error for each pop of something not pushed and each
// push still open at the end, which beancount reports per file.
func ApplyPushPopDirectives(ast *AST) []error {
	if ast.pushPopApplied {
		return nil
	}

	ast.pushPopApplied = true

	// Collect all positioned items
	var items []positionedItem

	for i := range ast.Directives {
		items = append(items, positionedItem{
			pos:       ast.Directives[i].Position(),
			directive: ast.Directives[i],
		})
	}

	for _, pt := range ast.Pushtags {
		items = append(items, positionedItem{pos: pt.Position(), pushtag: pt})
	}

	for _, pt := range ast.Poptags {
		items = append(items, positionedItem{pos: pt.Position(), poptag: pt})
	}

	for _, pm := range ast.Pushmetas {
		items = append(items, positionedItem{pos: pm.Position(), pushmeta: pm})
	}

	for _, pm := range ast.Popmetas {
		items = append(items, positionedItem{pos: pm.Position(), popmeta: pm})
	}

	// Sort by file position
	slices.SortFunc(items, func(a, b positionedItem) int {
		if a.pos.Line != b.pos.Line {
			if a.pos.Line < b.pos.Line {
				return -1
			}
			return 1
		}
		if a.pos.Column != b.pos.Column {
			if a.pos.Column < b.pos.Column {
				return -1
			}
			return 1
		}
		if a.pos.Offset < b.pos.Offset {
			return -1
		}
		if a.pos.Offset > b.pos.Offset {
			return 1
		}
		return 0
	})

	// Track active state - use slices to preserve order
	var activeTags []*Pushtag
	activeMetadata := make(map[string][]*Pushmeta)
	var metadataKeys []string // push order of keys, for deterministic output
	var errs []error

	// Process items in file order
	for _, item := range items {
		switch {
		case item.pushtag != nil:
			activeTags = append(activeTags, item.pushtag)

		case item.poptag != nil:
			popped := false
			for i, pushed := range activeTags {
				if pushed.Tag == item.poptag.Tag {
					activeTags = slices.Delete(activeTags, i, i+1)
					popped = true
					break
				}
			}
			if !popped {
				errs = append(errs, &PushPopError{
					Pos:     item.poptag.Position(),
					Message: fmt.Sprintf("Attempting to pop absent tag: '%s'", string(item.poptag.Tag)),
				})
			}

		case item.pushmeta != nil:
			key := item.pushmeta.Key
			if len(activeMetadata[key]) == 0 {
				metadataKeys = append(metadataKeys, key)
			}
			activeMetadata[key] = append(activeMetadata[key], item.pushmeta)

		case item.popmeta != nil:
			key := item.popmeta.Key
			if stack := activeMetadata[key]; len(stack) > 0 {
				activeMetadata[key] = stack[:len(stack)-1]
			} else {
				errs = append(errs, &PushPopError{
					Pos:     item.popmeta.Position(),
					Message: fmt.Sprintf("Attempting to pop absent metadata key: '%s'", key),
				})
			}

		case item.directive != nil:
			// Apply active tags to transactions (preserving order)
			if txn, ok := item.directive.(*Transaction); ok {
				for _, pushed := range activeTags {
					txn.Tags = append(txn.Tags, pushed.Tag)
				}
			}

			// Like beancount, only transactions receive pushed metadata:
			// each key's innermost value, unless set explicitly.
			if txn, ok := item.directive.(*Transaction); ok {
				for _, key := range metadataKeys {
					stack := activeMetadata[key]
					if len(stack) == 0 || slices.ContainsFunc(txn.Metadata, func(m *Metadata) bool { return m.Key == key }) {
						continue
					}
					txn.AddMetadata(&Metadata{Key: key, Value: stack[len(stack)-1].metadataValue()})
				}
			}
		}
	}

	for _, pushed := range activeTags {
		errs = append(errs, &PushPopError{
			Pos:     pushed.Position(),
			Message: fmt.Sprintf("Unbalanced pushed tag: '%s'", string(pushed.Tag)),
		})
	}
	for _, key := range metadataKeys {
		stack := activeMetadata[key]
		if len(stack) == 0 {
			continue
		}
		values := make([]string, len(stack))
		for i, pushed := range stack {
			values[i] = pushed.metadataValue().String()
		}
		errs = append(errs, &PushPopError{
			Pos:     stack[0].Position(),
			Message: fmt.Sprintf("Unbalanced metadata key '%s'; leftover metadata '%s'", key, strings.Join(values, ", ")),
		})
	}

	return errs
}

// MarkPushPopDirectivesApplied marks a tree as already containing derived push/pop effects.
func MarkPushPopDirectivesApplied(ast *AST) {
	ast.pushPopApplied = true
}

// LinesWithMultipleItems returns a set of line numbers (1-indexed) that contain
// multiple AST items (directives, options, includes, comments, blank lines, etc.).
// This is useful for tools that need to preserve or reconstruct source lines safely.
//
// Lines with multiple items cannot be safely preserved verbatim during formatting
// because they may contain partial content from multiple semantic items.
//
// Example:
//
//	2024-01-01 open Assets:Checking  ; Comment on same line
//	^--- This line has both an Open directive and a Comment
func LinesWithMultipleItems(tree *AST) map[int]bool {
	lineCounts := make(map[int]int)

	// Count items on each line
	for _, opt := range tree.Options {
		lineCounts[opt.Position().Line]++
	}
	for _, inc := range tree.Includes {
		lineCounts[inc.Position().Line]++
	}
	for _, plugin := range tree.Plugins {
		lineCounts[plugin.Position().Line]++
	}
	for _, tag := range tree.Pushtags {
		lineCounts[tag.Position().Line]++
	}
	for _, tag := range tree.Poptags {
		lineCounts[tag.Position().Line]++
	}
	for _, meta := range tree.Pushmetas {
		lineCounts[meta.Position().Line]++
	}
	for _, meta := range tree.Popmetas {
		lineCounts[meta.Position().Line]++
	}
	for _, dir := range tree.Directives {
		lineCounts[dir.Position().Line]++
	}
	for _, comment := range tree.Comments {
		lineCounts[comment.Position().Line]++
	}
	for _, blank := range tree.BlankLines {
		lineCounts[blank.Position().Line]++
	}

	// Build set of lines with multiple items
	result := make(map[int]bool)
	for line, count := range lineCounts {
		if count > 1 {
			result[line] = true
		}
	}

	return result
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

// SortDirectives sorts all directives by their parsed date for semantic processing.
func SortDirectives(ast *AST) error {
	// Skip sorting if already sorted (common case for well-maintained files)
	if isSorted(ast.Directives) {
		return nil
	}

	// Use pdqsort for better performance when sorting is needed
	slices.SortFunc(ast.Directives, compareDirectives)
	return nil
}
