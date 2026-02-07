package database

import "testing"

func TestQuoteIdent(t *testing.T) {
	got := QuoteIdent("user")
	if got != `"user"` {
		t.Fatalf("unexpected quote: %s", got)
	}
	got = QuoteIdent(`a"b`)
	if got != `"a""b"` {
		t.Fatalf("unexpected escaped quote: %s", got)
	}
}
