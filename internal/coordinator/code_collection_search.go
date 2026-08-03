package coordinator

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"codeflux.dev/codeflux/internal/transport"

	_ "modernc.org/sqlite"
)

// atomSearchIndex is a full-text index over the atoms in one collected
// repository.
//
// It exists because an atom's name is the least interesting thing about it.
// What a person searching for an atom actually asks is "which one of these
// says it is not safe to retry", "which one takes a credential", "what covers
// the empty-cart case" — questions whose answers live in the documented
// promises, and a substring match over declaration names can answer none of
// them.
//
// The index is held in memory beside the parsed map, not in the coordinator's
// database. Atoms are derived from the working tree at an exact revision and
// are already recomputed whenever that revision moves; persisting them would
// create a second account of what the code says, and the two would eventually
// disagree. This index has exactly the lifetime of the map it indexes.
type atomSearchIndex struct {
	database *sql.DB
}

// atomSearchDocument is one atom as the index sees it.
type atomSearchDocument struct {
	Key string
	// Name, Receiver, ImportPath and Path are what a person types when they
	// know what they are looking for.
	Name       string
	Receiver   string
	ImportPath string
	Path       string
	// Documentation is the atom's opening sentence and every documented field,
	// which is what a person types when they do not.
	Documentation string
}

// newAtomSearchIndex builds an index over the supplied atoms.
//
// A failure to build is returned rather than swallowed, but the caller treats
// it as "no index": search then falls back to matching names, which is what it
// did before this existed. A search surface that stops working because an
// index could not be built would be worse than one that searches less well.
func newAtomSearchIndex(documents []atomSearchDocument) (*atomSearchIndex, error) {
	if len(documents) == 0 {
		return nil, nil
	}
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("open atom search index: %w", err)
	}
	// One connection: the index is small, single-writer, and read by one
	// request at a time, and an in-memory database is per-connection.
	database.SetMaxOpenConns(1)
	if _, err := database.Exec(atomSchemaStatement); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("create atom search index: %w", err)
	}
	transaction, err := database.Begin()
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("begin atom search index: %w", err)
	}
	statement, err := transaction.Prepare(
		`INSERT INTO atoms (key, name, receiver, import_path, path, documentation)
		 VALUES (?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		_ = transaction.Rollback()
		_ = database.Close()
		return nil, fmt.Errorf("prepare atom search index: %w", err)
	}
	for _, document := range documents {
		if _, err := statement.Exec(
			document.Key,
			// A Go identifier is one token to FTS5, so ReserveStock is not
			// found by "stock". The name is indexed both whole and split at its
			// case boundaries, which is how people actually search for it.
			document.Name+" "+splitIdentifierWords(document.Name),
			document.Receiver+" "+splitIdentifierWords(document.Receiver),
			document.ImportPath, document.Path, document.Documentation,
		); err != nil {
			_ = statement.Close()
			_ = transaction.Rollback()
			_ = database.Close()
			return nil, fmt.Errorf("write atom search index: %w", err)
		}
	}
	_ = statement.Close()
	if err := transaction.Commit(); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("commit atom search index: %w", err)
	}
	return &atomSearchIndex{database: database}, nil
}

// Close releases the index.
func (index *atomSearchIndex) Close() {
	if index == nil || index.database == nil {
		return
	}
	_ = index.database.Close()
}

// atomSearchHit is one match and the reason it is one.
//
// A result list that cannot say why a row is in it is a list a person has to
// take on faith: searching "stock" returns every atom whose name contains it,
// which is correct and looks exactly like a search that did nothing. The
// reason travels with the row.
type atomSearchHit struct {
	Key string
	// MatchedName reports that the term is in the declaration's own name.
	MatchedName bool
	// Promise is the piece of documented text that matched, when the match came
	// from what the atom promises rather than from what it is called.
	Promise string
}

// Search returns the matches for a query, best match first, each carrying why
// it matched.
//
// Ranking is bm25 with the name weighted above the documentation: somebody who
// types an atom's name means that atom, and somebody who types a phrase from a
// promise means whichever atom makes it.
func (index *atomSearchIndex) Search(query string, limit int) ([]atomSearchHit, error) {
	if index == nil || index.database == nil {
		return nil, nil
	}
	expression := atomSearchExpression(query)
	if expression == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = transport.MaximumCodePage
	}
	rows, err := index.database.Query(
		atomSearchStatement,
		atomMatchOpen, atomMatchClose, atomMatchOpen, atomMatchClose, expression, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("search atoms: %w", err)
	}
	defer rows.Close()
	hits := make([]atomSearchHit, 0, limit)
	for rows.Next() {
		var key, name, documentation string
		if err := rows.Scan(&key, &name, &documentation); err != nil {
			return nil, fmt.Errorf("read atom search result: %w", err)
		}
		hits = append(hits, atomSearchHit{
			Key:         key,
			MatchedName: strings.Contains(name, atomMatchOpen),
			Promise:     matchedPromise(documentation),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read atom search results: %w", err)
	}
	return hits, nil
}

// atomSearchStatement is the query the search runs, and atomSelectStatement is
// the one that reads a single atom back.
//
// They are constants so the interface can show a person the exact query that
// produced what they are looking at. A query rewritten for display is a query
// that drifts from the one that ran, and then the console is teaching
// something that is no longer true.
const (
	atomSearchStatement = `SELECT key,
       highlight(atoms, 1, ?, ?) AS matched_name,
       highlight(atoms, 5, ?, ?) AS matched_documentation
  FROM atoms
 WHERE atoms MATCH ?
 ORDER BY bm25(atoms, 0.0, 8.0, 4.0, 2.0, 1.0, 1.0)
 LIMIT ?`

	atomSelectStatement = `SELECT name, receiver, import_path, path, documentation
  FROM atoms
 WHERE key = ?`

	atomSchemaStatement = `CREATE VIRTUAL TABLE atoms USING fts5(
    key UNINDEXED, name, receiver, import_path, path, documentation,
    tokenize = 'unicode61'
)`
)

// AtomSearchSQL renders the search this query performed, with the match
// expression and the bound written in, so a person can read or run exactly
// what the coordinator ran.
func AtomSearchSQL(query string, limit int) string {
	expression := atomSearchExpression(query)
	if expression == "" {
		return ""
	}
	if limit <= 0 {
		limit = transport.MaximumCodePage
	}
	statement := strings.ReplaceAll(atomSearchStatement, "highlight(atoms, 1, ?, ?)",
		"highlight(atoms, 1, '<', '>')")
	statement = strings.ReplaceAll(statement, "highlight(atoms, 5, ?, ?)",
		"highlight(atoms, 5, '<', '>')")
	statement = strings.Replace(statement, "atoms MATCH ?",
		"atoms MATCH '"+strings.ReplaceAll(expression, "'", "''")+"'", 1)
	statement = strings.Replace(statement, "LIMIT ?",
		"LIMIT "+strconv.Itoa(limit), 1)
	return statement + ";"
}

// AtomSelectSQL renders the read of one atom by its key.
func AtomSelectSQL(key string) string {
	if strings.TrimSpace(key) == "" {
		return ""
	}
	return strings.Replace(atomSelectStatement, "key = ?",
		"key = '"+strings.ReplaceAll(key, "'", "''")+"'", 1) + ";"
}

// AtomSchemaSQL is the table the two queries above run against.
func AtomSchemaSQL() string { return atomSchemaStatement + ";" }

// atomMatchOpen and atomMatchClose bracket a matched term inside a highlighted
// column. They are characters no Go declaration or documented sentence
// contains, so finding one is proof of a match rather than a guess.
const (
	atomMatchOpen  = "\x02"
	atomMatchClose = "\x03"
)

// matchedPromise renders the sentence a documentation match landed in.
//
// The whole documented text would bury the row; the first sentence containing
// the match is what a person needs to see to know the result belongs. The
// markers are removed once they have done their job of locating it.
func matchedPromise(highlighted string) string {
	index := strings.Index(highlighted, atomMatchOpen)
	if index < 0 {
		return ""
	}
	cleaned := strings.NewReplacer(atomMatchOpen, "", atomMatchClose, "").Replace(highlighted)
	position := len([]rune(strings.NewReplacer(
		atomMatchOpen, "", atomMatchClose, "",
	).Replace(highlighted[:index])))
	runes := []rune(cleaned)
	const window = 90
	start := position - window/3
	if start < 0 {
		start = 0
	}
	end := start + window
	if end > len(runes) {
		end = len(runes)
	}
	// Both ends move to a space so the fragment starts and finishes on whole
	// words. A quotation that begins mid-word reads as a rendering fault
	// rather than as the sentence it came from.
	start = wordBoundaryAfter(runes, start)
	end = wordBoundaryBefore(runes, end, start)
	fragment := strings.TrimSpace(strings.Join(strings.Fields(string(runes[start:end])), " "))
	if start > 0 {
		fragment = "…" + fragment
	}
	if end < len(runes) {
		fragment += "…"
	}
	return fragment
}

// atomSearchExpression turns what a person typed into an FTS5 query.
//
// Every term is quoted and given a prefix wildcard, so typing part of a word
// finds it and nothing a person types can be read as FTS5 syntax. A bare query
// string passed through would let a stray quote or NEAR turn a search into an
// error the person cannot see the cause of.
func atomSearchExpression(query string) string {
	terms := strings.Fields(strings.ToLower(strings.TrimSpace(query)))
	quoted := make([]string, 0, len(terms))
	for _, term := range terms {
		cleaned := strings.Map(func(character rune) rune {
			switch {
			case character >= 'a' && character <= 'z',
				character >= '0' && character <= '9':
				return character
			default:
				return -1
			}
		}, term)
		if cleaned == "" {
			continue
		}
		quoted = append(quoted, `"`+cleaned+`"*`)
	}
	if len(quoted) == 0 {
		return ""
	}
	// Terms are joined with AND: a person typing two words means both.
	return strings.Join(quoted, " AND ")
}

// splitIdentifierWords renders a Go identifier as the words it is made of.
//
// FTS5 sees ReserveStockUntilCheckoutExpires as one token, so a search for
// "checkout" would not find it. Indexing the split form alongside the whole
// one means both the exact name and any word inside it match.
func splitIdentifierWords(identifier string) string {
	if identifier == "" {
		return ""
	}
	var builder strings.Builder
	runes := []rune(identifier)
	for index, character := range runes {
		upper := character >= 'A' && character <= 'Z'
		if upper && index > 0 {
			previous := runes[index-1]
			previousLower := previous >= 'a' && previous <= 'z'
			previousDigit := previous >= '0' && previous <= '9'
			nextLower := index+1 < len(runes) &&
				runes[index+1] >= 'a' && runes[index+1] <= 'z'
			if previousLower || previousDigit || nextLower {
				builder.WriteRune(' ')
			}
		}
		if character == '_' || character == '.' || character == '-' {
			builder.WriteRune(' ')
			continue
		}
		builder.WriteRune(character)
	}
	return builder.String()
}

// wordBoundaryAfter moves an index forward to the start of the next whole
// word, unless it already sits at the beginning of the text.
func wordBoundaryAfter(runes []rune, index int) int {
	if index <= 0 {
		return 0
	}
	for index < len(runes) && !isWordBreak(runes[index]) {
		index++
	}
	for index < len(runes) && isWordBreak(runes[index]) {
		index++
	}
	return index
}

// wordBoundaryBefore moves an index back to the end of the last whole word,
// never behind the start it was given.
func wordBoundaryBefore(runes []rune, index, start int) int {
	if index >= len(runes) {
		return len(runes)
	}
	for index > start && !isWordBreak(runes[index]) {
		index--
	}
	if index <= start {
		return len(runes)
	}
	return index
}

func isWordBreak(character rune) bool {
	switch character {
	case ' ', '\n', '\t':
		return true
	default:
		return false
	}
}
