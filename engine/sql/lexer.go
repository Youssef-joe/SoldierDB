package sql

import (
	"fmt"
	"strings"
)

// TokenKind classifies a lexical token.
type TokenKind int

const (
	// TokenEOF marks the end of input.
	TokenEOF TokenKind = iota
	// TokenIdent is an identifier.
	TokenIdent
	// TokenKeyword is a reserved keyword.
	TokenKeyword
	// TokenInt is an integer literal.
	TokenInt
	// TokenFloat is a floating point literal.
	TokenFloat
	// TokenString is a string literal with escapes decoded.
	TokenString
	// TokenSymbol is an operator or punctuation.
	TokenSymbol
)

func (k TokenKind) String() string {
	switch k {
	case TokenEOF:
		return "EOF"
	case TokenIdent:
		return "identifier"
	case TokenKeyword:
		return "keyword"
	case TokenInt:
		return "integer"
	case TokenFloat:
		return "float"
	case TokenString:
		return "string"
	case TokenSymbol:
		return "symbol"
	default:
		return "unknown"
	}
}

// Token is a single lexical token.
type Token struct {
	Kind TokenKind
	Text string
	Pos  int
}

func (t Token) String() string {
	if t.Kind == TokenEOF {
		return "end of input"
	}
	return fmt.Sprintf("%q", t.Text)
}

// SyntaxError is a lexer or parser error with a byte position.
type SyntaxError struct {
	Pos int
	Msg string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("sql: syntax error at position %d: %s", e.Pos, e.Msg)
}

var keywords = map[string]struct{}{
	"ALL": {}, "AND": {}, "AS": {}, "ASC": {}, "BEGIN": {}, "BETWEEN": {},
	"BOOL": {}, "BOOLEAN": {}, "BY": {}, "CHAR": {}, "COMMIT": {},
	"CREATE": {}, "DEFAULT": {}, "DELETE": {}, "DESC": {}, "DESCRIBE": {},
	"DISTINCT": {}, "DOUBLE": {}, "DROP": {}, "EXISTS": {}, "FALSE": {},
	"FLOAT": {}, "FROM": {}, "GROUP": {}, "HAVING": {}, "IF": {}, "IN": {},
	"INDEX": {}, "INSERT": {}, "INT": {}, "INTEGER": {}, "INTO": {}, "IS": {},
	"KEY": {}, "LIKE": {}, "LIMIT": {}, "NOT": {},
	"NULL": {}, "OFFSET": {}, "ON": {}, "OR": {}, "ORDER": {}, "PRIMARY": {},
	"REAL": {}, "ROLLBACK": {}, "SELECT": {}, "SET": {}, "SHOW": {},
	"SMALLINT": {}, "STRING": {}, "TABLE": {}, "TABLES": {},
	"TEXT": {}, "TRANSACTION": {}, "TRUE": {}, "UNIQUE": {}, "UPDATE": {},
	"VALUES": {}, "VARCHAR": {}, "WHERE": {}, "BIGINT": {},
}

// IsKeyword reports whether name is reserved.
func IsKeyword(name string) bool {
	_, ok := keywords[strings.ToUpper(name)]
	return ok
}

// Lexer turns SQL text into tokens.
type Lexer struct {
	input string
	pos   int
}

// NewLexer creates a lexer for input.
func NewLexer(input string) *Lexer {
	return &Lexer{input: input}
}

// Next returns the next token.
func (l *Lexer) Next() (Token, error) {
	l.skipSpaceAndComments()
	if l.pos >= len(l.input) {
		return Token{Kind: TokenEOF, Pos: l.pos}, nil
	}

	start := l.pos
	c := l.input[l.pos]

	switch {
	case c == '\'':
		text, err := l.scanString()
		if err != nil {
			return Token{}, err
		}
		return Token{Kind: TokenString, Text: text, Pos: start}, nil
	case c == '"':
		text, err := l.scanQuotedIdent()
		if err != nil {
			return Token{}, err
		}
		return Token{Kind: TokenIdent, Text: text, Pos: start}, nil
	case isDigit(c):
		return l.scanNumber()
	case isIdentStart(c):
		return l.scanIdent(), nil
	}

	if op, ok := l.scanSymbol(); ok {
		return op, nil
	}
	return Token{}, &SyntaxError{Pos: start, Msg: fmt.Sprintf("unexpected character %q", c)}
}

func (l *Lexer) skipSpaceAndComments() {
	for l.pos < len(l.input) {
		c := l.input[l.pos]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v' {
			l.pos++
			continue
		}
		if c == '-' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '-' {
			l.pos += 2
			for l.pos < len(l.input) && l.input[l.pos] != '\n' {
				l.pos++
			}
			continue
		}
		if c == '/' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '*' {
			end := strings.Index(l.input[l.pos+2:], "*/")
			if end < 0 {
				l.pos = len(l.input)
				return
			}
			l.pos += 2 + end + 2
			continue
		}
		return
	}
}

func (l *Lexer) scanString() (string, error) {
	start := l.pos
	l.pos++ // opening quote
	var b strings.Builder
	for l.pos < len(l.input) {
		c := l.input[l.pos]
		if c == '\'' {
			if l.pos+1 < len(l.input) && l.input[l.pos+1] == '\'' {
				b.WriteByte('\'')
				l.pos += 2
				continue
			}
			l.pos++
			return b.String(), nil
		}
		b.WriteByte(c)
		l.pos++
	}
	return "", &SyntaxError{Pos: start, Msg: "unterminated string literal"}
}

func (l *Lexer) scanQuotedIdent() (string, error) {
	start := l.pos
	l.pos++
	var b strings.Builder
	for l.pos < len(l.input) {
		c := l.input[l.pos]
		if c == '"' {
			if l.pos+1 < len(l.input) && l.input[l.pos+1] == '"' {
				b.WriteByte('"')
				l.pos += 2
				continue
			}
			l.pos++
			return b.String(), nil
		}
		b.WriteByte(c)
		l.pos++
	}
	return "", &SyntaxError{Pos: start, Msg: "unterminated quoted identifier"}
}

func (l *Lexer) scanNumber() (Token, error) {
	start := l.pos
	isFloat := false
	for l.pos < len(l.input) && isDigit(l.input[l.pos]) {
		l.pos++
	}
	if l.pos < len(l.input) && l.input[l.pos] == '.' {
		// Only treat the dot as part of the number when a digit follows.
		if l.pos+1 < len(l.input) && isDigit(l.input[l.pos+1]) {
			isFloat = true
			l.pos++
			for l.pos < len(l.input) && isDigit(l.input[l.pos]) {
				l.pos++
			}
		}
	}
	if l.pos < len(l.input) && (l.input[l.pos] == 'e' || l.input[l.pos] == 'E') {
		save := l.pos
		l.pos++
		if l.pos < len(l.input) && (l.input[l.pos] == '+' || l.input[l.pos] == '-') {
			l.pos++
		}
		if l.pos < len(l.input) && isDigit(l.input[l.pos]) {
			isFloat = true
			for l.pos < len(l.input) && isDigit(l.input[l.pos]) {
				l.pos++
			}
		} else {
			l.pos = save
		}
	}
	kind := TokenInt
	if isFloat {
		kind = TokenFloat
	}
	return Token{Kind: kind, Text: l.input[start:l.pos], Pos: start}, nil
}

func (l *Lexer) scanIdent() Token {
	start := l.pos
	for l.pos < len(l.input) && isIdentPart(l.input[l.pos]) {
		l.pos++
	}
	text := l.input[start:l.pos]
	if _, ok := keywords[strings.ToUpper(text)]; ok {
		return Token{Kind: TokenKeyword, Text: strings.ToUpper(text), Pos: start}
	}
	return Token{Kind: TokenIdent, Text: text, Pos: start}
}

func (l *Lexer) scanSymbol() (Token, bool) {
	start := l.pos
	if l.pos+1 < len(l.input) {
		two := l.input[l.pos : l.pos+2]
		switch two {
		case "<=", ">=", "<>", "!=", "||":
			l.pos += 2
			return Token{Kind: TokenSymbol, Text: two, Pos: start}, true
		}
	}
	switch l.input[l.pos] {
	case '=', '<', '>', '+', '-', '*', '/', '%', '(', ')', ',', ';', '.':
		l.pos++
		return Token{Kind: TokenSymbol, Text: string(l.input[start]), Pos: start}, true
	}
	return Token{}, false
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isIdentStart(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
func isIdentPart(c byte) bool { return isIdentStart(c) || isDigit(c) }

// Tokenize returns all tokens, including the final EOF token.
func Tokenize(input string) ([]Token, error) {
	l := NewLexer(input)
	var tokens []Token
	for {
		tok, err := l.Next()
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, tok)
		if tok.Kind == TokenEOF {
			return tokens, nil
		}
	}
}
