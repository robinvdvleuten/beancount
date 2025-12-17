package parser

import "github.com/robinvdvleuten/beancount/ast"

// Re-export StringMetadata and EscapeType for backward compatibility
type StringMetadata = ast.StringMetadata
type EscapeType = ast.EscapeType

const (
	EscapeTypeUnknown = ast.EscapeTypeUnknown
	EscapeTypeNone    = ast.EscapeTypeNone
	EscapeTypeCStyle  = ast.EscapeTypeCStyle
)
