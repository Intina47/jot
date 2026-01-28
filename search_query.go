package main

import (
	"fmt"
	"strings"
	"unicode"
)

type tokenKind int

const (
	tokenTerm tokenKind = iota
	tokenPhrase
	tokenAnd
	tokenOr
	tokenNot
	tokenLParen
	tokenRParen
)

type token struct {
	kind  tokenKind
	value string
}

func parseQuery(query string) ([]token, error) {
	lexerTokens, err := lexQuery(query)
	if err != nil {
		return nil, err
	}
	if len(lexerTokens) == 0 {
		return nil, fmt.Errorf("empty query")
	}

	var tokens []token
	var prev *token
	for _, tok := range lexerTokens {
		if prev != nil && needsImplicitAnd(*prev, tok) {
			tokens = append(tokens, token{kind: tokenAnd})
		}
		tokens = append(tokens, tok)
		prev = &tok
	}

	return tokens, nil
}

func needsImplicitAnd(prev, next token) bool {
	prevIsTerm := prev.kind == tokenTerm || prev.kind == tokenPhrase || prev.kind == tokenRParen
	nextIsTerm := next.kind == tokenTerm || next.kind == tokenPhrase || next.kind == tokenLParen || next.kind == tokenNot
	return prevIsTerm && nextIsTerm
}

func lexQuery(input string) ([]token, error) {
	var tokens []token
	runes := []rune(strings.TrimSpace(input))
	for i := 0; i < len(runes); {
		ch := runes[i]
		switch {
		case unicode.IsSpace(ch):
			i++
		case ch == '(':
			tokens = append(tokens, token{kind: tokenLParen})
			i++
		case ch == ')':
			tokens = append(tokens, token{kind: tokenRParen})
			i++
		case ch == '"':
			phrase, next, err := readPhrase(runes, i+1)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(phrase) == "" {
				return nil, fmt.Errorf("empty phrase")
			}
			tokens = append(tokens, token{kind: tokenPhrase, value: strings.ToLower(phrase)})
			i = next
		default:
			word, next := readWord(runes, i)
			lower := strings.ToLower(word)
			switch lower {
			case "and":
				tokens = append(tokens, token{kind: tokenAnd})
			case "or":
				tokens = append(tokens, token{kind: tokenOr})
			case "not":
				tokens = append(tokens, token{kind: tokenNot})
			default:
				tokens = append(tokens, token{kind: tokenTerm, value: lower})
			}
			i = next
		}
	}

	return tokens, nil
}

func readPhrase(runes []rune, start int) (string, int, error) {
	var builder strings.Builder
	for i := start; i < len(runes); i++ {
		if runes[i] == '"' {
			return builder.String(), i + 1, nil
		}
		builder.WriteRune(runes[i])
	}
	return "", len(runes), fmt.Errorf("unterminated phrase")
}

func readWord(runes []rune, start int) (string, int) {
	var builder strings.Builder
	for i := start; i < len(runes); i++ {
		ch := runes[i]
		if unicode.IsSpace(ch) || ch == '(' || ch == ')' {
			return builder.String(), i
		}
		builder.WriteRune(ch)
	}
	return builder.String(), len(runes)
}

func toRPN(tokens []token) ([]token, error) {
	var output []token
	var stack []token

	for _, tok := range tokens {
		switch tok.kind {
		case tokenTerm, tokenPhrase:
			output = append(output, tok)
		case tokenNot, tokenAnd, tokenOr:
			for len(stack) > 0 {
				top := stack[len(stack)-1]
				if top.kind == tokenLParen {
					break
				}
				if precedence(top.kind) >= precedence(tok.kind) {
					output = append(output, top)
					stack = stack[:len(stack)-1]
				} else {
					break
				}
			}
			stack = append(stack, tok)
		case tokenLParen:
			stack = append(stack, tok)
		case tokenRParen:
			found := false
			for len(stack) > 0 {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if top.kind == tokenLParen {
					found = true
					break
				}
				output = append(output, top)
			}
			if !found {
				return nil, fmt.Errorf("unbalanced parentheses")
			}
		default:
			return nil, fmt.Errorf("unsupported token")
		}
	}

	for len(stack) > 0 {
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if top.kind == tokenLParen || top.kind == tokenRParen {
			return nil, fmt.Errorf("unbalanced parentheses")
		}
		output = append(output, top)
	}

	return output, nil
}

func precedence(kind tokenKind) int {
	switch kind {
	case tokenNot:
		return 3
	case tokenAnd:
		return 2
	case tokenOr:
		return 1
	default:
		return 0
	}
}
