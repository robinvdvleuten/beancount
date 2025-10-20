// Package ast declares the types used to represent syntax trees for Beancount files.
//
// These types represent the structure of Beancount directives, transactions, and related
// elements that make up a Beancount ledger file. The AST (Abstract Syntax Tree) can be
// created by parsing a Beancount file using the parser package, or constructed
// programmatically for generating Beancount output.
package ast

import (
	"golang.org/x/exp/slices"
)

// Directives is a slice of Directive that implements sort.Interface.
type Directives []Directive

func (d Directives) Len() int           { return len(d) }
func (d Directives) Swap(i, j int)      { d[i], d[j] = d[j], d[i] }
func (d Directives) Less(i, j int) bool { return compareDirectives(d[i], d[j]) < 0 }

// compareDirectives compares two directives by their date, then by type priority.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
//
// For same-date directives, the processing order is:
//  1. Open (accounts must be opened before use)
//  2. Close (process closes before transactions that might use closed accounts)
//  3. All other directives (transactions, balance, pad, etc.)
func compareDirectives(a, b Directive) int {
	// First compare by date
	if a.date().Before(b.date().Time) {
		return -1
	} else if a.date().After(b.date().Time) {
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

	return 0
}

// directiveTypePriority returns the processing priority for a directive type.
// Lower numbers are processed first.
func directiveTypePriority(d Directive) int {
	switch d.(type) {
	case *Open:
		return 0 // Process opens first
	case *Close:
		return 1 // Process closes second
	default:
		return 2 // All others (transactions, balance, pad, note, etc.)
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
}

// WithMetadata is an interface for AST nodes that can have metadata attached.
type WithMetadata interface {
	AddMetadata(...*Metadata)
}

// withMetadata is an embeddable struct that implements WithMetadata.
type withMetadata struct {
	Metadata []*Metadata
}

func (w *withMetadata) AddMetadata(m ...*Metadata) {
	w.Metadata = append(w.Metadata, m...)
}

// Directive is the interface implemented by all Beancount directive types.
type Directive interface {
	WithMetadata

	date() *Date
	Directive() string
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

// getDirectivePos extracts the position from any directive type.
func getDirectivePos(d Directive) Position {
	switch v := d.(type) {
	case *Commodity:
		return v.Pos
	case *Open:
		return v.Pos
	case *Close:
		return v.Pos
	case *Balance:
		return v.Pos
	case *Pad:
		return v.Pos
	case *Note:
		return v.Pos
	case *Document:
		return v.Pos
	case *Price:
		return v.Pos
	case *Event:
		return v.Pos
	case *Custom:
		return v.Pos
	case *Transaction:
		return v.Pos
	default:
		return Position{}
	}
}

// ApplyPushPopDirectives applies pushtag/poptag and pushmeta/popmeta directives
// to transactions and other directives in file order (before date sorting).
func ApplyPushPopDirectives(ast *AST) error {
	// Collect all positioned items
	var items []positionedItem

	for i := range ast.Directives {
		items = append(items, positionedItem{
			pos:       getDirectivePos(ast.Directives[i]),
			directive: ast.Directives[i],
		})
	}

	for _, pt := range ast.Pushtags {
		items = append(items, positionedItem{pos: pt.Pos, pushtag: pt})
	}

	for _, pt := range ast.Poptags {
		items = append(items, positionedItem{pos: pt.Pos, poptag: pt})
	}

	for _, pm := range ast.Pushmetas {
		items = append(items, positionedItem{pos: pm.Pos, pushmeta: pm})
	}

	for _, pm := range ast.Popmetas {
		items = append(items, positionedItem{pos: pm.Pos, popmeta: pm})
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
	var activeTags []Tag
	activeMetadata := make(map[string]string)

	// Process items in file order
	for _, item := range items {
		switch {
		case item.pushtag != nil:
			activeTags = append(activeTags, item.pushtag.Tag)

		case item.poptag != nil:
			// Remove tag from slice
			for i, tag := range activeTags {
				if tag == item.poptag.Tag {
					activeTags = append(activeTags[:i], activeTags[i+1:]...)
					break
				}
			}

		case item.pushmeta != nil:
			activeMetadata[item.pushmeta.Key] = item.pushmeta.Value

		case item.popmeta != nil:
			delete(activeMetadata, item.popmeta.Key)

		case item.directive != nil:
			// Apply active tags to transactions (preserving order)
			if txn, ok := item.directive.(*Transaction); ok {
				txn.Tags = append(txn.Tags, activeTags...)
			}

			// Apply active metadata to all directives with metadata
			if withMeta, ok := item.directive.(WithMetadata); ok {
				for key, value := range activeMetadata {
					withMeta.AddMetadata(&Metadata{Key: key, Value: &MetadataValue{StringValue: &value}})
				}
			}
		}
	}

	return nil
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
