import { syntaxHighlighting, HighlightStyle } from "@codemirror/language";
import { EditorView } from "@codemirror/view";
import { tags as t } from "@lezer/highlight";

export const editorTheme = EditorView.theme({
  "&": {
    backgroundColor: "var(--color-base-100)",
    color: "color-mix(in oklab, var(--color-base-content) 85%, transparent)",
    height: "100%",
  },
  ".cm-content": {
    caretColor: "var(--color-base-content)",
  },
  ".cm-cursor, .cm-dropCursor": {
    borderLeftColor: "var(--color-base-content)",
  },
  "&.cm-focused .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection": {
    backgroundColor: "color-mix(in oklab, var(--color-base-content) 8%, transparent)",
  },
  ".cm-activeLine": {
    backgroundColor: "color-mix(in oklab, var(--color-base-content) 3%, transparent)",
  },
  ".cm-selectionMatch": {
    backgroundColor: "color-mix(in oklab, var(--color-base-content) 6%, transparent)",
  },
  ".cm-gutters": {
    backgroundColor: "var(--color-base-100)",
    color: "color-mix(in oklab, var(--color-base-content) 25%, transparent)",
    borderRight: "1px solid color-mix(in oklab, var(--color-base-content) 12%, transparent)",
  },
  ".cm-activeLineGutter": {
    backgroundColor: "color-mix(in oklab, var(--color-base-content) 3%, transparent)",
    color: "color-mix(in oklab, var(--color-base-content) 50%, transparent)",
  },
  ".cm-tooltip": {
    backgroundColor: "var(--color-base-100)",
    color: "var(--color-base-content)",
    border: "1px solid color-mix(in oklab, var(--color-base-content) 12%, transparent)",
    boxShadow: "0 12px 40px color-mix(in oklab, var(--color-base-content) 10%, transparent)",
  },
  ".cm-tooltip-autocomplete": {
    padding: 0,
  },
  ".cm-tooltip-autocomplete ul": {
    backgroundColor: "var(--color-base-100)",
    color: "var(--color-base-content)",
  },
  ".cm-tooltip-autocomplete li": {
    padding: "0.35rem 0.75rem",
    color: "color-mix(in oklab, var(--color-base-content) 80%, transparent)",
  },
  ".cm-tooltip-autocomplete li[aria-selected]": {
    backgroundColor: "color-mix(in oklab, var(--color-base-content) 6%, transparent)",
    color: "var(--color-base-content)",
  },
  ".cm-completionMatchedText": {
    color: "color-mix(in oklab, var(--color-base-content) 60%, transparent)",
  },
  ".cm-tooltip-lint": {
    backgroundColor: "var(--color-base-100)",
    color: "var(--color-base-content)",
    border: "1px solid color-mix(in oklab, var(--color-base-content) 12%, transparent)",
    boxShadow: "0 12px 40px color-mix(in oklab, var(--color-base-content) 10%, transparent)",
  },
  ".cm-diagnostic": {
    borderLeft: "4px solid color-mix(in oklab, var(--color-base-content) 10%, transparent)",
  },
  ".cm-diagnostic-error": {
    borderLeftColor: "color-mix(in oklab, var(--color-error, #f31260) 65%, transparent)",
  },
});

const highlightStyle = HighlightStyle.define([
  {
    tag: t.literal,
    color: "color-mix(in oklab, var(--color-base-content) 90%, transparent)",
    fontWeight: "500",
  },
  {
    tag: t.modifier,
    color: "color-mix(in oklab, var(--color-base-content) 85%, transparent)",
    fontWeight: "600",
  },
  {
    tag: t.keyword,
    color: "color-mix(in oklab, var(--color-base-content) 80%, transparent)",
    fontWeight: "500",
  },
  {
    tag: t.variableName,
    color: "color-mix(in oklab, var(--color-base-content) 70%, transparent)",
  },
  {
    tag: t.number,
    color: "color-mix(in oklab, var(--color-base-content) 75%, transparent)",
  },
  {
    tag: t.unit,
    color: "color-mix(in oklab, var(--color-base-content) 55%, transparent)",
    fontWeight: "500",
  },
  {
    tag: t.string,
    color: "color-mix(in oklab, var(--color-base-content) 60%, transparent)",
  },
  {
    tag: t.heading,
    color: "color-mix(in oklab, var(--color-base-content) 75%, transparent)",
    fontWeight: "600",
  },
  {
    tag: t.propertyName,
    color: "color-mix(in oklab, var(--color-base-content) 45%, transparent)",
    fontStyle: "italic",
  },
  {
    tag: t.name,
    color: "color-mix(in oklab, var(--color-base-content) 50%, transparent)",
  },
  {
    tag: t.bool,
    color: "color-mix(in oklab, var(--color-base-content) 55%, transparent)",
  },
  {
    tag: t.tagName,
    color: "color-mix(in oklab, var(--color-base-content) 40%, transparent)",
  },
  {
    tag: t.link,
    color: "color-mix(in oklab, var(--color-base-content) 40%, transparent)",
    textDecoration: "underline",
    textUnderlineOffset: "2px",
    textDecorationColor: "color-mix(in oklab, var(--color-base-content) 20%, transparent)",
  },
  {
    tag: t.lineComment,
    color: "color-mix(in oklab, var(--color-base-content) 35%, transparent)",
    fontStyle: "italic",
  },
  {
    tag: t.arithmeticOperator,
    color: "color-mix(in oklab, var(--color-base-content) 35%, transparent)",
  },
  {
    tag: t.operator,
    color: "color-mix(in oklab, var(--color-base-content) 35%, transparent)",
  },
  {
    tag: t.special(t.operator),
    color: "color-mix(in oklab, var(--color-base-content) 40%, transparent)",
  },
  {
    tag: t.brace,
    color: "color-mix(in oklab, var(--color-base-content) 30%, transparent)",
  },
  {
    tag: t.paren,
    color: "color-mix(in oklab, var(--color-base-content) 30%, transparent)",
  },
  {
    tag: t.separator,
    color: "color-mix(in oklab, var(--color-base-content) 30%, transparent)",
  },
  {
    tag: t.punctuation,
    color: "color-mix(in oklab, var(--color-base-content) 30%, transparent)",
  },
]);

export const beancountSyntaxHighlighting = syntaxHighlighting(highlightStyle);
