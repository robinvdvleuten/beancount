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

// compareDirectives compares two directives by their date, then by type priority,
// then by source position (line number). Returns -1 if a < b, 0 if a == b, 1 if a > b.
// This implements a stable sort that preserves source order for same-date, same-type
// directives, matching the behavior of the official Python beancount implementation.
//
// For same-date directives, the processing order is:
//  1. Open (accounts must be opened before use)
//  2. Close (process closes before transactions that might use closed accounts)
//  3. All other directives (transactions, balance, pad, etc.)
//  4. Within same type, sort by line number (preserves source order)
func compareDirectives(a, b Directive) int {
	// First compare by date
	if a.GetDate().Before(b.GetDate().Time) {
		return -1
	} else if a.GetDate().After(b.GetDate().Time) {
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
	Comments   []*Comment
	BlankLines []*BlankLine
}

// WithMetadata is an interface for AST nodes that can have metadata attached.
type WithMetadata interface {
	AddMetadata(...*Metadata)
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

	GetDate() *Date
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

// ApplyPushPopDirectives applies pushtag/poptag and pushmeta/popmeta directives
// to transactions and other directives in file order (before date sorting).
func ApplyPushPopDirectives(ast *AST) error {
	// Collect all positioned items
	var items []positionedItem

	for i := range ast.Directives {
		items = append(items, positionedItem{
			pos:       ast.Directives[i].Position(),
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
					rawStr := NewRawString(value)
					withMeta.AddMetadata(&Metadata{Key: key, Value: &MetadataValue{StringValue: &rawStr}})
				}
			}
		}
	}

	return nil
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
