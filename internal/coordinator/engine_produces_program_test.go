package coordinator

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/pipeline"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// TestTheEngineProducesProgramsThatBuildAndRun is the whole point of the
// product, checked end to end against a real model.
//
// Everything else in this package asserts that rows were written and that
// files appeared. This asks the only question a person actually cares about:
// does the thing it built compile, and does the executable do what was asked?
// A run that writes a file full of plausible Go and stops short of that has
// not built anything.
//
// The cases run from least to most that can go wrong. A fixed line proves the
// path works at all. Reading arguments, branching, accumulating over input,
// and finally parsing structured input and aggregating it each add a way for a
// program to compile perfectly and still be wrong — which is the failure this
// exists to catch, because it is the one a green build hides.
//
// It calls a real provider, so it costs money and needs a key. Without one it
// skips rather than passing quietly: a green run that never reached a model
// would be the most misleading result this file could produce.
func TestTheEngineProducesProgramsThatBuildAndRun(t *testing.T) {
	key := ReadProviderKey(repositoryRootForTest(t))
	if key == "" {
		t.Skip("no provider key: set OPENAI_API_KEY or put it in .env")
	}

	for _, program := range []generatedProgram{
		{
			name:        "1 prints a fixed line",
			packagePath: "cmd/greet",
			requirement: "Create cmd/greet/main.go: a Go program in package main " +
				"whose main function prints exactly this one line and nothing " +
				"else: Hello from CodeFlux",
			expected: "Hello from CodeFlux",
		},
		{
			name:        "2 computes over its arguments",
			packagePath: "cmd/total",
			requirement: "Create cmd/total/main.go: a Go program in package main " +
				"that reads every command-line argument as an integer, adds them " +
				"up, and prints only the sum on one line.",
			arguments: []string{"7", "11", "24"},
			expected:  "42",
		},
		{
			name:        "3 branches on an argument",
			packagePath: "cmd/fizzbuzz",
			requirement: "Create cmd/fizzbuzz/main.go: a Go program in package " +
				"main that reads one integer N from the first command-line " +
				"argument and prints the numbers 1 to N, one per line, except " +
				"that multiples of 3 print Fizz, multiples of 5 print Buzz, and " +
				"multiples of both print FizzBuzz. Print nothing else.",
			arguments: []string{"15"},
			expected: "1\n2\nFizz\n4\nBuzz\nFizz\n7\n8\nFizz\nBuzz\n11\n" +
				"Fizz\n13\n14\nFizzBuzz",
		},
		{
			name:        "4 aggregates and orders its input",
			packagePath: "cmd/wordfreq",
			requirement: "Create cmd/wordfreq/main.go: a Go program in package " +
				"main that reads all of standard input, splits it into " +
				"whitespace-separated words, and prints one line per distinct " +
				"word in the form 'word count'. Order the lines by descending " +
				"count, and order words with the same count alphabetically. " +
				"Print nothing else.",
			stdin:    "b a c b a b\n",
			expected: "b 3\na 2\nc 1",
		},
		{
			name:        "5 parses structured input and reports on it",
			packagePath: "cmd/ledger",
			requirement: "Create cmd/ledger/main.go: a Go program in package " +
				"main that reads a JSON array from standard input where each " +
				"element is an object with a string field 'name' and an integer " +
				"field 'amount'. Print one line per distinct name in the form " +
				"'name total', ordered alphabetically by name, where total is " +
				"the sum of that name's amounts. Then print a final line " +
				"'TOTAL total' with the sum of every amount. Print nothing else.",
			stdin: `[{"name":"rent","amount":1200},{"name":"food","amount":300},` +
				`{"name":"rent","amount":100},{"name":"bus","amount":50}]`,
			expected: "bus 50\nfood 300\nrent 1300\nTOTAL 1650",
		},
		{
			name:        "6 evaluates an expression with a stack",
			packagePath: "cmd/rpn",
			requirement: "Create cmd/rpn/main.go: a Go program in package main " +
				"that treats its command-line arguments as a reverse Polish " +
				"notation expression over integers, supporting the operators " +
				"+, -, * and /, evaluates it with a stack, and prints only the " +
				"integer result on one line.",
			arguments: []string{"3", "4", "+", "2", "*", "7", "-"},
			expected:  "7",
		},
		{
			name:        "7 summarises tabular input by column",
			packagePath: "cmd/csvstat",
			requirement: "Create cmd/csvstat/main.go: a Go program in package " +
				"main that reads CSV from standard input where the first row is " +
				"a header. For every column whose values are all integers, " +
				"print one line 'column min max sum' using that column's " +
				"values. Keep the columns in header order and print nothing else.",
			stdin:    "name,score,age\na,10,30\nb,20,40\nc,30,50\n",
			expected: "score 10 30 60\nage 30 50 120",
		},
		{
			name:        "8 runs a real algorithm over its input",
			packagePath: "cmd/intervals",
			requirement: "Create cmd/intervals/main.go: a Go program in package " +
				"main that reads lines from standard input, each holding two " +
				"integers 'start end' separated by a space, merges every " +
				"overlapping or touching interval, and prints the merged " +
				"intervals in ascending order, one per line as 'start end'. " +
				"Print nothing else.",
			stdin:    "1 3\n2 6\n8 10\n15 18\n",
			expected: "1 6\n8 10\n15 18",
		},
		{
			name:        "9 interprets a small language",
			packagePath: "cmd/stackvm",
			requirement: "Create cmd/stackvm/main.go: a Go program in package " +
				"main that reads a program from standard input, one " +
				"instruction per line, and executes it on an integer stack. " +
				"The instructions are 'PUSH n' which pushes integer n, 'ADD' " +
				"and 'MUL' which pop two values and push their sum or product, " +
				"'DUP' which duplicates the top value, and 'PRINT' which pops " +
				"the top value and prints it on its own line. Print nothing else.",
			stdin:    "PUSH 2\nPUSH 3\nADD\nDUP\nMUL\nPRINT\n",
			expected: "25",
		},
		{
			name:        "10 searches a grid for a shortest path",
			packagePath: "cmd/maze",
			requirement: "Create cmd/maze/main.go: a Go program in package main " +
				"that reads a rectangular grid from standard input, one row per " +
				"line, where '#' is a wall, '.' is open, 'S' is the start and " +
				"'E' is the end. Moving only up, down, left or right through " +
				"non-wall cells, print the number of moves in a shortest path " +
				"from S to E, or print 'no path' if none exists. Print nothing " +
				"else.",
			stdin:    "S.#\n..#\n#.E\n",
			expected: "4",
		},
		{
			name:        "11 spreads one program across two files",
			packagePath: "cmd/bank",
			requirement: "Create cmd/bank/main.go and cmd/bank/account.go: a Go " +
				"program in package main split across those two files, where " +
				"account.go holds an Account type with Deposit, Withdraw and " +
				"Balance methods and main.go reads commands from standard " +
				"input. The commands are 'DEPOSIT n', 'WITHDRAW n' and " +
				"'BALANCE'. BALANCE prints the current balance on its own " +
				"line. A WITHDRAW larger than the balance must change nothing " +
				"and print 'insufficient' on its own line. Print nothing else.",
			stdin:    "DEPOSIT 100\nWITHDRAW 30\nBALANCE\nWITHDRAW 200\nBALANCE\n",
			expected: "70\ninsufficient\n70",
		},
		{
			name:        "12 parses infix arithmetic with precedence",
			packagePath: "cmd/calc",
			requirement: "Create cmd/calc/main.go: a Go program in package main " +
				"that evaluates the single infix arithmetic expression given as " +
				"its first command-line argument. It must support +, -, * and / " +
				"over integers with the usual precedence, parentheses, and " +
				"left-to-right association, using integer division. Print only " +
				"the integer result on one line.",
			arguments: []string{"2+3*4-(5-1)/2"},
			expected:  "12",
		},
		{
			name:        "13 simulates a cache with an eviction policy",
			packagePath: "cmd/lru",
			requirement: "Create cmd/lru/main.go: a Go program in package main " +
				"that simulates a least-recently-used cache whose capacity is " +
				"the first command-line argument. It reads commands from " +
				"standard input: 'PUT key value' stores a value, and 'GET key' " +
				"prints the stored value on its own line or -1 if it is absent. " +
				"Both PUT and a successful GET count as using a key. When a PUT " +
				"exceeds capacity, evict the least recently used key. After all " +
				"commands, print a final line 'hits misses' holding the number " +
				"of successful and unsuccessful GETs. Print nothing else.",
			arguments: []string{"2"},
			stdin:     "PUT a 1\nPUT b 2\nGET a\nPUT c 3\nGET b\nGET c\n",
			expected:  "1\n-1\n3\n2 1",
		},
		{
			name:        "14 finds a cheapest route through a weighted graph",
			packagePath: "cmd/route",
			requirement: "Create cmd/route/main.go: a Go program in package main " +
				"that reads a weighted directed graph from standard input. The " +
				"first line holds four integers 'N M start end': the number of " +
				"nodes, the number of edges, the start node and the end node. " +
				"Each of the next M lines holds 'u v w', a directed edge from u " +
				"to v of positive weight w. Print the total weight of a " +
				"cheapest path from start to end, or 'unreachable' if there is " +
				"none. Print nothing else.",
			stdin:    "4 4 0 3\n0 1 1\n1 2 2\n0 2 5\n2 3 1\n",
			expected: "4",
		},
		{
			name:        "15 fully justifies text to a width",
			packagePath: "cmd/justify",
			requirement: "Create cmd/justify/main.go: a Go program in package " +
				"main that reads whitespace-separated words from standard input " +
				"and prints them wrapped to the line width given as the first " +
				"command-line argument. Pack as many words onto each line as " +
				"fit, separated by at least one space. Pad every line except " +
				"the last so that it is exactly the given width, distributing " +
				"the extra spaces between words as evenly as possible and " +
				"giving the leftmost gaps the extra space when they do not " +
				"divide evenly. Left-justify the last line. Print nothing else.",
			arguments: []string{"16"},
			stdin:     "This is an example of text justification.\n",
			expected:  "This    is    an\nexample  of text\njustification.",
		},
		{
			name:        "16 spans two packages with a real import",
			packagePath: "cmd/stats",
			requirement: "Create internal/stats/stats.go and cmd/stats/main.go. " +
				"The module is codeflux.test/workspace. internal/stats/stats.go " +
				"is package stats and exports Mean and Max, each taking a slice " +
				"of int and returning an int, where Mean truncates toward zero. " +
				"cmd/stats/main.go is package main, imports " +
				"codeflux.test/workspace/internal/stats, reads its command-line " +
				"arguments as integers, and prints only one line holding the " +
				"mean and the max separated by a space.",
			arguments: []string{"1", "2", "3", "4"},
			expected:  "2 4",
			alsoWritten: []string{
				"internal/stats/stats.go", "cmd/stats/main.go",
			},
		},
		{
			// The same ledger as rung 5, asked for as a functional core behind
			// an imperative shell. Rung 5 produced one imperative main with no
			// function boundary in it and therefore nothing testable; this asks
			// whether the engine can produce the shape on request, and the
			// check is mechanical rather than stylistic: the core must not
			// touch the outside world.
			name:        "17 keeps a pure core behind an imperative shell",
			packagePath: "cmd/fpledger",
			requirement: "Create internal/ledger/ledger.go and " +
				"cmd/fpledger/main.go. The module is codeflux.test/workspace. " +
				"internal/ledger/ledger.go is package ledger and must be pure: " +
				"it may not read input, print, or touch os, fmt, or any other " +
				"side effect, and it exports an Entry struct with Name string " +
				"and Amount int, and a function Totals taking a slice of Entry " +
				"and returning a slice of the per-name totals ordered " +
				"alphabetically by name together with the overall total. " +
				"cmd/fpledger/main.go is package main, imports " +
				"codeflux.test/workspace/internal/ledger, and does all of the " +
				"input and output: it decodes a JSON array of entries from " +
				"standard input, calls Totals, prints one line per name as " +
				"'name total' and then a final line 'TOTAL total'. Print " +
				"nothing else.",
			stdin: `[{"name":"rent","amount":1200},{"name":"food","amount":300},` +
				`{"name":"rent","amount":100},{"name":"bus","amount":50}]`,
			expected:    "bus 50\nfood 300\nrent 1300\nTOTAL 1650",
			alsoWritten: []string{"internal/ledger/ledger.go"},
			pureCore:    []string{"internal/ledger/ledger.go"},
		},
		{
			// The hardest rung: a generic Result monad, atomic pure functions
			// composed through it, and the monad laws asserted in the
			// repository's own tests. The tests matter for a second reason —
			// every other rung leaves the engine running "go test ./..."
			// against a repository with no tests in it, so its own validation
			// passes vacuously. This is the first case where the engine's
			// safety net has something to catch.
			name:        "18 composes atomic functions through a Result monad",
			packagePath: "cmd/fpipeline",
			requirement: "Create internal/fp/result.go, internal/fp/result_test.go, " +
				"internal/pipeline/pipeline.go and cmd/fpipeline/main.go. The " +
				"module is codeflux.test/workspace. internal/fp/result.go is " +
				"package fp and must be pure: it defines a generic type " +
				"Result[T any] holding either a value or an error, constructors " +
				"Ok and Err, a method IsOk, a method Unwrap returning the value " +
				"and the error, and because Go methods cannot take type " +
				"parameters it also defines package-level generic functions " +
				"Map[A any, B any](Result[A], func(A) B) Result[B] and " +
				"AndThen[A any, B any](Result[A], func(A) Result[B]) Result[B]. " +
				"internal/fp/result_test.go is package fp and tests the three " +
				"monad laws for Result: left identity, right identity and " +
				"associativity. internal/pipeline/pipeline.go is package " +
				"pipeline and must be pure: it imports " +
				"codeflux.test/workspace/internal/fp and exports two small " +
				"functions, ParseInts taking a slice of string and returning " +
				"fp.Result of a slice of int, and Mean taking a slice of int " +
				"and returning fp.Result of an int using integer division that " +
				"fails on an empty slice. cmd/fpipeline/main.go is package main " +
				"and does all input and output: it composes ParseInts and Mean " +
				"over its command-line arguments using fp.AndThen, then prints " +
				"only the integer mean on one line, or the word failed on one " +
				"line if the result holds an error.",
			arguments: []string{"10", "20", "30"},
			expected:  "20",
			alsoWritten: []string{
				"internal/fp/result.go", "internal/fp/result_test.go",
				"internal/pipeline/pipeline.go",
			},
			pureCore: []string{
				"internal/fp/result.go", "internal/pipeline/pipeline.go",
			},
			mustContain: map[string][]string{
				"internal/fp/result.go": {
					"type Result[", "func Ok[", "func Err[",
					"func Map[", "func AndThen[",
				},
				"internal/pipeline/pipeline.go": {
					"func ParseInts(", "func Mean(",
				},
				"cmd/fpipeline/main.go": {"fp.AndThen("},
			},
		},
		{
			name:        "19 orders a dependency graph and detects a cycle",
			packagePath: "cmd/toposort",
			requirement: "Create cmd/toposort/main.go: a Go program in package " +
				"main that reads lines from standard input, each holding two " +
				"names separated by a space meaning the first must come before " +
				"the second. Print the names in an order that respects every " +
				"constraint, one per line, choosing the alphabetically first " +
				"available name whenever more than one is ready. If no such " +
				"order exists print only the word cycle. Print nothing else.",
			stdin:    "a b\na c\nb d\nc d\n",
			expected: "a\nb\nc\nd",
		},
		{
			name:        "20 implements a regular expression matcher",
			packagePath: "cmd/regex",
			requirement: "Create cmd/regex/main.go: a Go program in package " +
				"main that takes a pattern as its first command-line argument " +
				"and a text as its second, and decides whether the pattern " +
				"matches the whole text. Implement the matching yourself " +
				"without using the regexp package. The pattern supports a " +
				"literal character, a dot meaning any single character, a star " +
				"meaning zero or more of the preceding element, a leading caret " +
				"anchoring the start, and a trailing dollar anchoring the end. " +
				"Print only the word match or the words no match on one line.",
			arguments: []string{"^a.*c$", "abbbc"},
			expected:  "match",
		},
		{
			name:        "21 multiplies numbers too large for a machine word",
			packagePath: "cmd/bignum",
			requirement: "Create cmd/bignum/main.go: a Go program in package " +
				"main that multiplies the two decimal integers given as its " +
				"command-line arguments and prints only the product on one " +
				"line. The numbers are far larger than any built-in integer " +
				"type, so do the arithmetic on the digits yourself and do not " +
				"use the math/big package.",
			arguments: []string{
				"99999999999999999999", "99999999999999999999",
			},
			expected: "9999999999999999999800000000000000000001",
		},
		{
			name:        "22 builds an optimal prefix code",
			packagePath: "cmd/huffman",
			requirement: "Create cmd/huffman/main.go: a Go program in package " +
				"main that reads lines from standard input, each holding a " +
				"symbol and its frequency separated by a space, builds an " +
				"optimal binary prefix code for those frequencies, and prints " +
				"only the total number of bits the encoded input would occupy " +
				"on one line.",
			stdin:    "a 5\nb 9\nc 12\nd 13\ne 16\nf 45\n",
			expected: "224",
		},
		{
			name:        "23 parses JSON without the standard decoder",
			packagePath: "cmd/jsonparse",
			requirement: "Create cmd/jsonparse/main.go: a Go program in package " +
				"main that reads one JSON value from standard input and prints " +
				"it back in a canonical form: no whitespace, and every object's " +
				"keys in ascending order. Write the parser yourself and do not " +
				"use the encoding/json package. Print nothing else.",
			stdin:    `{"b":1,"a":[2,3],"c":{"y":true,"x":null}}`,
			expected: `{"a":[2,3],"b":1,"c":{"x":null,"y":true}}`,
		},
		{
			name:        "24 solves a linear system exactly",
			packagePath: "cmd/gauss",
			requirement: "Create cmd/gauss/main.go: a Go program in package " +
				"main that reads a linear system from standard input. The " +
				"first line holds the number of unknowns N. Each of the next N " +
				"lines holds N+1 integers: the coefficients of one equation " +
				"followed by its right-hand side. Solve the system exactly, " +
				"without floating point, and print the values of the unknowns " +
				"in order on one line separated by spaces. Print nothing else.",
			stdin:    "2\n2 1 5\n1 -1 1\n",
			expected: "2 1",
		},
		{
			name:        "25 reports the difference between two texts",
			packagePath: "cmd/textdiff",
			requirement: "Create cmd/textdiff/main.go: a Go program in package " +
				"main that reads two blocks of lines from standard input " +
				"separated by a line holding exactly three hyphens. Print the " +
				"difference between them using a longest common subsequence: a " +
				"line present in both is printed with a leading space, a line " +
				"only in the first is printed with a leading minus, and a line " +
				"only in the second is printed with a leading plus. Keep the " +
				"original order and print nothing else.",
			stdin:    "a\nb\nc\n---\na\nx\nc\n",
			expected: " a\n-b\n+x\n c",
		},
		{
			name:        "26 reduces a lambda expression to normal form",
			packagePath: "cmd/lambda",
			requirement: "Create cmd/lambda/main.go: a Go program in package " +
				"main that reads one lambda-calculus expression as its first " +
				"command-line argument and prints its normal form on one line. " +
				"An abstraction is written L followed by a variable, a dot, and " +
				"a body. Application is written by juxtaposition and associates " +
				"to the left. Parentheses group. Reduce by beta-reduction until " +
				"no redex remains, renaming bound variables where needed to " +
				"avoid capture. Print nothing else.",
			arguments: []string{"(Lx.Ly.x) a b"},
			expected:  "a",
		},
		{
			name:        "27 answers stabbing queries over intervals",
			packagePath: "cmd/intervaltree",
			requirement: "Create cmd/intervaltree/main.go: a Go program in " +
				"package main that reads from standard input a list of closed " +
				"integer intervals, one per line as two integers separated by a " +
				"space, then a line holding a single question mark, then a list " +
				"of integer queries one per line. For each query print only the " +
				"number of intervals that contain it, one per line. Print " +
				"nothing else.",
			stdin:    "1 5\n3 8\n10 12\n?\n4\n9\n11\n",
			expected: "2\n0\n1",
		},
		{
			name:        "28 decides satisfiability by search",
			packagePath: "cmd/sat",
			requirement: "Create cmd/sat/main.go: a Go program in package main " +
				"that reads a boolean formula in conjunctive normal form from " +
				"standard input. The first line holds the number of variables " +
				"and the number of clauses. Each following line holds one " +
				"clause as non-zero integers terminated by a zero, where a " +
				"positive integer means that variable and a negative integer " +
				"means its negation. Decide whether any assignment satisfies " +
				"every clause and print only the word SAT or the word UNSAT on " +
				"one line.",
			stdin:    "2 4\n1 2 0\n1 -2 0\n-1 2 0\n-1 -2 0\n",
			expected: "UNSAT",
		},
		{
			name:        "29 interprets a language with closures and recursion",
			packagePath: "cmd/lisp",
			requirement: "Create cmd/lisp/main.go: a Go program in package main " +
				"that reads a Lisp-like program as its first command-line " +
				"argument, evaluates it, and prints the value of the last " +
				"expression on one line. Support integer literals, the special " +
				"forms define, if and lambda, function definition in the form " +
				"(define (name args) body), recursion, and the primitives +, -, " +
				"* and = which compares two integers. Print nothing else.",
			arguments: []string{
				"(define (fact n) (if (= n 0) 1 (* n (fact (- n 1))))) (fact 10)",
			},
			expected: "3628800",
		},
		{
			name:        "30 compiles a pattern into an automaton",
			packagePath: "cmd/nfa",
			requirement: "Create cmd/nfa/main.go: a Go program in package main " +
				"that takes a regular expression as its first command-line " +
				"argument, compiles it into a nondeterministic automaton and " +
				"then into a deterministic one by subset construction, and uses " +
				"that automaton to decide each line of standard input. The " +
				"expression supports concatenation, alternation written with a " +
				"vertical bar, the star, and parentheses. Print match or no " +
				"match for each line, one per line, and nothing else.",
			arguments: []string{"(a|b)*abb"},
			stdin:     "abb\naabb\nbabb\nabab\n",
			expected:  "match\nmatch\nmatch\nno match",
		},
		{
			name:        "31 raises a matrix to a power to reach a large term",
			packagePath: "cmd/matexp",
			requirement: "Create cmd/matexp/main.go: a Go program in package " +
				"main that computes the nth Fibonacci number, where n is its " +
				"first command-line argument, by repeated squaring of the two " +
				"by two matrix rather than by iteration or recursion. Fibonacci " +
				"numbers start 1, 1, 2, 3. Print only the result on one line.",
			arguments: []string{"90"},
			expected:  "2880067194370816120",
		},
		{
			name:        "32 keeps every earlier version of a structure alive",
			packagePath: "cmd/persistent",
			requirement: "Create cmd/persistent/main.go: a Go program in package " +
				"main that maintains a persistent immutable map from string to " +
				"integer. It reads commands from standard input: 'set key value' " +
				"produces a new version and prints its number starting from 1, " +
				"and 'get version key' prints the value that key had in that " +
				"version, or the word absent. Setting a key must not change any " +
				"earlier version. Print nothing else.",
			stdin:    "set a 1\nset b 2\nset a 3\nget 1 a\nget 2 a\nget 3 a\nget 1 b\n",
			expected: "1\n2\n3\n1\n1\n3\nabsent",
		},
		{
			name:        "33 searches a large constrained space",
			packagePath: "cmd/queens",
			requirement: "Create cmd/queens/main.go: a Go program in package " +
				"main that counts the arrangements of N queens on an N by N " +
				"board with no two attacking each other, where N is its first " +
				"command-line argument. Print only the count on one line.",
			arguments: []string{"10"},
			expected:  "724",
		},
		{
			name:        "34 compiles to bytecode and executes it",
			packagePath: "cmd/bytecode",
			requirement: "Create cmd/bytecode/main.go: a Go program in package " +
				"main that compiles the infix arithmetic expression given as its " +
				"first command-line argument into a stack bytecode and then runs " +
				"that bytecode. The instructions are PUSH n, ADD, SUB, MUL and " +
				"DIV, and the compiler emits them in postfix order. Print each " +
				"instruction on its own line in the order emitted, then the " +
				"result of running them on its own line. Support + - * / and " +
				"parentheses with the usual precedence and integer division.",
			arguments: []string{"2+3*4"},
			expected:  "PUSH 2\nPUSH 3\nPUSH 4\nMUL\nADD\n14",
		},
		{
			name:        "35 chooses optimally under a constraint",
			packagePath: "cmd/knapsack",
			requirement: "Create cmd/knapsack/main.go: a Go program in package " +
				"main that reads a capacity on the first line of standard input " +
				"and then one item per line as a weight and a value separated by " +
				"a space. Each item may be taken at most once. Print only the " +
				"greatest total value that fits within the capacity, on one line.",
			stdin:    "5\n2 3\n3 4\n4 5\n5 6\n",
			expected: "7",
		},
		{
			name:        "36 finds the shortest edit script between two sequences",
			packagePath: "cmd/editscript",
			requirement: "Create cmd/editscript/main.go: a Go program in package " +
				"main that reads two blocks of lines from standard input " +
				"separated by a line holding exactly three hyphens, and prints " +
				"only the smallest number of line insertions and deletions that " +
				"turns the first block into the second, on one line.",
			stdin:    "a\nb\nc\nd\n---\na\nc\nd\ne\n",
			expected: "2",
		},
		{
			name:        "37 decides what is still reachable",
			packagePath: "cmd/marksweep",
			requirement: "Create cmd/marksweep/main.go: a Go program in package " +
				"main that reads a heap from standard input. The first line " +
				"holds the number of objects, numbered from 1. Each following " +
				"line until a line holding a single question mark describes one " +
				"reference as two object numbers separated by a space. The " +
				"remaining line lists the root objects separated by spaces. " +
				"Print the number of objects reachable from the roots and the " +
				"number that are not, separated by a space, on one line.",
			stdin:    "6\n1 2\n2 3\n4 5\n?\n1\n",
			expected: "3 3",
		},
		{
			name:        "38 finds the longest path through a weighted graph",
			packagePath: "cmd/critpath",
			requirement: "Create cmd/critpath/main.go: a Go program in package " +
				"main that reads a directed acyclic graph of tasks from standard " +
				"input. The first line holds the number of tasks, numbered from " +
				"1. Each following line holds a task number and its duration. " +
				"Then a line holding a single question mark, then one dependency " +
				"per line as two task numbers meaning the first must finish " +
				"before the second starts. Print only the earliest time all " +
				"tasks can be complete, on one line.",
			stdin:    "4\n1 3\n2 2\n3 4\n4 1\n?\n1 2\n1 3\n2 4\n3 4\n",
			expected: "8",
		},
		{
			name:        "39 answers a query language over tabular data",
			packagePath: "cmd/sqlquery",
			requirement: "Create cmd/sqlquery/main.go: a Go program in package " +
				"main that takes a query as its first command-line argument and " +
				"reads a CSV table with a header row from standard input. " +
				"Support SELECT of named columns and of SUM over a column, an " +
				"optional WHERE comparing a column to an integer with one of " +
				"< > =, an optional GROUP BY of one column, and an optional " +
				"ORDER BY of one column ascending. Print the header of the " +
				"result and then one row per line, values separated by commas. " +
				"Print nothing else.",
			arguments: []string{
				"SELECT name, SUM(amount) FROM t WHERE amount > 40 GROUP BY name ORDER BY name",
			},
			stdin:    "name,amount\nrent,1200\nfood,30\nrent,100\nbus,50\nfood,300\n",
			expected: "name,SUM(amount)\nbus,50\nfood,300\nrent,1300",
		},
		{
			name:        "40 unifies terms and searches for a proof",
			packagePath: "cmd/prolog",
			requirement: "Create cmd/prolog/main.go: a Go program in package " +
				"main that reads a logic program from standard input, one clause " +
				"per line, then a line holding a single question mark, then one " +
				"query. A clause is either a fact like parent(a,b). or a rule " +
				"like ancestor(X,Y) :- parent(X,Y). with a comma-separated body. " +
				"Names beginning with a capital letter are variables. Resolve the " +
				"query by unification with backtracking and print each distinct " +
				"solution's bindings as Var=value pairs separated by spaces, one " +
				"solution per line, in the order found. Print nothing else.",
			stdin: "parent(a,b).\nparent(b,c).\nancestor(X,Y) :- parent(X,Y).\n" +
				"ancestor(X,Y) :- parent(X,Z), ancestor(Z,Y).\n?\nancestor(a,Y)\n",
			expected: "Y=b\nY=c",
		},
		{
			name:        "41 divides to arbitrary precision without floating point",
			packagePath: "cmd/decimal",
			requirement: "Create cmd/decimal/main.go: a Go program in package " +
				"main that divides the first command-line argument by the second " +
				"and prints the result to the number of decimal places given by " +
				"the third, truncating rather than rounding. Do the arithmetic " +
				"on digits yourself: no floating point and no math/big. Print " +
				"only the result on one line.",
			arguments: []string{"1", "7", "50"},
			expected:  "0.14285714285714285714285714285714285714285714285714",
		},
		{
			name:        "42 solves an exact cover by systematic search",
			packagePath: "cmd/exactcover",
			requirement: "Create cmd/exactcover/main.go: a Go program in package " +
				"main that reads a zero-one matrix from standard input, one row " +
				"per line of digits with no separators, and counts the ways to " +
				"choose a set of rows such that every column is covered by " +
				"exactly one chosen row. Print only the count on one line.",
			stdin:    "0010110\n1001001\n0110010\n1001000\n0100001\n0001101\n",
			expected: "1",
		},
		{
			name:        "43 merges two divergent edits of the same text",
			packagePath: "cmd/threeway",
			requirement: "Create cmd/threeway/main.go: a Go program in package " +
				"main that reads three blocks of lines from standard input " +
				"separated by lines holding exactly three hyphens: the common " +
				"ancestor, then one edit of it, then another. Produce the merged " +
				"result, taking a line from whichever side changed it. If both " +
				"sides changed the same line differently, print only the word " +
				"conflict. Print nothing else.",
			stdin:    "a\nb\nc\n---\na\nX\nc\n---\na\nb\nY\n",
			expected: "a\nX\nY",
		},
		{
			name:        "44 proves a formula has no solution at all",
			packagePath: "cmd/pigeonhole",
			requirement: "Create cmd/pigeonhole/main.go: a Go program in package " +
				"main that reads a formula in conjunctive normal form from " +
				"standard input in the same shape as DIMACS: a first line of the " +
				"variable and clause counts, then one clause per line as " +
				"non-zero integers terminated by a zero. Decide satisfiability " +
				"using unit propagation and backtracking search, and print only " +
				"the word SAT or the word UNSAT on one line.",
			stdin: "6 9\n1 2 0\n3 4 0\n5 6 0\n-1 -3 0\n-1 -5 0\n-3 -5 0\n" +
				"-2 -4 0\n-2 -6 0\n-4 -6 0\n",
			expected: "UNSAT",
		},
		{
			name:        "45 encodes and decodes its own compression",
			packagePath: "cmd/canonhuff",
			requirement: "Create cmd/canonhuff/main.go: a Go program in package " +
				"main that reads a line of text from standard input, builds an " +
				"optimal prefix code for its characters, encodes the text with " +
				"it, decodes the result back using only the code it built, and " +
				"then prints the decoded text on one line followed by the number " +
				"of bits the encoding occupied on the next. Print nothing else.",
			stdin:    "abracadabra\n",
			expected: "abracadabra\n23",
		},
		{
			name:        "46 evaluates higher-order functions over closures",
			packagePath: "cmd/closures",
			requirement: "Create cmd/closures/main.go: a Go program in package " +
				"main that evaluates the Lisp-like expression given as its first " +
				"command-line argument and prints its value on one line. Support " +
				"integer literals, lambda, application, and the primitives + - " +
				"and *. A lambda must capture the environment where it was " +
				"written, so a function passed to another function still sees " +
				"its own bindings. Print nothing else.",
			arguments: []string{"((lambda (f) (f (f 3))) (lambda (x) (* x x)))"},
			expected:  "81",
		},
		{
			name:        "47 runs a small expression language with state",
			packagePath: "cmd/exprlang",
			requirement: "Create cmd/exprlang/main.go: a Go program in package " +
				"main that reads a program from standard input as statements " +
				"separated by semicolons. A statement is either an assignment of " +
				"an expression to a name, or an expression. Expressions support " +
				"+ - * / with the usual precedence, parentheses, unary minus, " +
				"integer literals, previously assigned names, and the functions " +
				"min, max and abs of two arguments except abs which takes one. " +
				"Print the value of the last statement on one line, and nothing " +
				"else.",
			stdin:    "x = 3; y = -4; max(x * x + abs(y), 20)\n",
			expected: "25",
		},
		{
			name:        "48 divides numbers larger than any machine word",
			packagePath: "cmd/bigdiv",
			requirement: "Create cmd/bigdiv/main.go: a Go program in package " +
				"main that divides the first command-line argument by the second, " +
				"both decimal integers far larger than any built-in type, and " +
				"prints the quotient and the remainder separated by a space on " +
				"one line. Do the long division on the digits yourself and do " +
				"not use the math/big package.",
			arguments: []string{"1000000000000000000000", "7"},
			expected:  "142857142857142857142 6",
		},
	} {
		t.Run(program.name, func(t *testing.T) {
			program.buildAndRun(t, key)
		})
	}
}

// generatedProgram is one program the engine is asked to write, and what
// running it must print.
type generatedProgram struct {
	name        string
	packagePath string
	requirement string
	arguments   []string
	stdin       string
	expected    string
	// pureCore, when set, names files that must compute without reaching
	// outside themselves. It is empty for programs where that is not asked for.
	pureCore []string
	// mustContain are fragments each named file has to hold, used where the
	// shape of the code is the point rather than only its output.
	mustContain map[string][]string
	// alsoWritten are files the program needs besides its own main.go.
	//
	// A program that spans packages is only really spanning them if the other
	// package exists on disk and is imported: a single file that quietly
	// inlines everything would compile, run, print the right answer, and prove
	// nothing about whether the engine can build across packages.
	alsoWritten []string
}

// buildAndRun drives the engine, then compiles and runs what it wrote.
func (program generatedProgram) buildAndRun(t *testing.T, key string) {
	t.Helper()
	// A factory rather than one model, so the ladder runs the escalation path
	// the product runs. Handing over a single built model would leave the run
	// unable to escalate, and every rung would then be evidence about a
	// configuration nobody ships.
	//
	// The default style and default ladder, because the ladder is the
	// pipeline's own evidence and judging it under a non-default setting would
	// say nothing about what a person gets out of the box.
	style := pipeline.DefaultSettings().CodeStyle
	engine := startEscalatingEngineFixture(t, func(named string) (agentloop.FixedModel, error) {
		return newDefaultAgentModel(key, named, style)
	})
	// The engine is shown the same example the test will judge it by. Judging
	// a run against a criterion it was never told is not a test of the run: it
	// cannot refine toward something it cannot see, and every failure of that
	// kind was unfixable by any number of attempts.
	worktree, taskID := engine.carryOut(
		t, program.requirement+program.acceptanceBlock(), program.packagePath)

	source := filepath.Join(worktree, filepath.FromSlash(program.packagePath), "main.go")
	written, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("the program the run reported writing is not there: %v", err)
	}
	t.Logf("the model wrote %s:\n%s", program.packagePath, written)

	// The worktree is temporary; the record is not. What the run produced has
	// to be readable back out of the database afterwards, or the only copy of
	// the work disappears with the directory it was built in.
	stored, err := engine.repositories.ListTaskArtifacts(context.Background(), taskID)
	if err != nil {
		t.Fatalf("reading what the run produced failed: %v", err)
	}
	switch {
	case storedSomewhere(stored, written):
	case storedRedacted(stored):
		// The record says it is not byte-identical, which is a true statement
		// about a file whose contents tripped the redaction pipeline. The
		// artifact is still evidence; it is simply evidence that says so.
		t.Logf("the record holds a redacted copy: %s",
			describeStoredMismatch(stored, written))
	default:
		t.Errorf("the program is on disk but not in the record: %s",
			describeStoredMismatch(stored, written))
	}

	program.verifyPureCore(t, worktree)

	for _, declared := range program.alsoWritten {
		if _, err := os.Stat(
			filepath.Join(worktree, filepath.FromSlash(declared)),
		); err != nil {
			t.Fatalf("the program was asked to span packages and %s is missing: %v",
				declared, err)
		}
	}

	// The plan the run recorded is checked against the worktree it left behind,
	// so a run that declared what it would produce and then produced something
	// else is caught here rather than downstream.
	engine.verifyPlanThesis(t, taskID, worktree)

	// 1. It has to compile. This is the first thing that cannot be faked: a
	//    file of plausible Go that does not build is not a program.
	binary := filepath.Join(t.TempDir(), filepath.Base(program.packagePath))
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.CommandContext(t.Context(),
		"go", "build", "-o", binary, "./"+program.packagePath)
	build.Dir = worktree
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the generated program does not compile: %v\n%s\n--- source ---\n%s",
			err, output, written)
	}
	if _, err := os.Stat(binary); err != nil {
		t.Fatalf("the build reported success but produced no executable: %v", err)
	}

	// 2. It has to run, and do what was asked. A program that compiles and
	//    prints the wrong thing has still not carried out the requirement.
	run := exec.CommandContext(t.Context(), binary, program.arguments...)
	if program.stdin != "" {
		run.Stdin = strings.NewReader(program.stdin)
	}
	produced, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated executable failed to run: %v\n%s", err, produced)
	}
	if got := normalizeProgramOutput(string(produced)); got != program.expected {
		t.Fatalf("the executable printed:\n%s\nwant:\n%s\n--- source ---\n%s",
			got, program.expected, written)
	}
	t.Logf("built %s, ran it with %v, and it printed exactly what was asked",
		filepath.Base(binary), program.arguments)

	// 3. And it has to survive input nobody intended. Everything above uses
	//    the one input the requirement described, which is the input least
	//    likely to break anything.
	program.probeAdversarially(t, binary)
}

// verifyPlanThesis checks the recorded plan against the worktree it produced.
//
// A plan states what it will produce and what would make it done. Those claims
// are the run's own thesis, and a run is only finished if they became true: a
// plan naming files it never wrote, or declaring a validation command the
// worktree then fails, has reported success it did not earn.
func (engine engineFixture) verifyPlanThesis(
	t *testing.T,
	taskID domain.TaskID,
	worktree string,
) {
	t.Helper()
	plan, err := engine.repositories.GetCurrentPlanRevision(context.Background(), taskID)
	if err != nil {
		t.Fatalf("the run recorded no plan to check its work against: %v", err)
	}
	if len(plan.Plan.ExpectedFiles) == 0 {
		t.Fatal("the plan declared no files, so nothing it did can be checked")
	}
	for _, declared := range plan.Plan.ExpectedFiles {
		path := filepath.Join(worktree, filepath.FromSlash(declared))
		if _, err := os.Stat(path); err != nil {
			t.Errorf("the plan declared %s and the run did not produce it", declared)
		}
	}
	if len(plan.Plan.CompletionCriteria) == 0 {
		t.Error("the plan states no completion criteria, so it cannot be judged done")
	}

	// The plan's own validation command is run against the worktree. This is
	// the thesis in its strongest form: not "a file appeared" but "the check
	// this run said it would be judged by actually passes".
	validation := exec.CommandContext(t.Context(), "go", "test", "./...")
	validation.Dir = worktree
	if output, err := validation.CombinedOutput(); err != nil {
		t.Errorf("the plan's own validation fails in the worktree it produced: %v\n%s",
			err, output)
	}
	t.Logf("plan revision %d held: %d declared file(s) present, validation passes",
		plan.Revision, len(plan.Plan.ExpectedFiles))
}

// carryOut drives one requirement through the engine and returns the worktree
// the work landed in.
func (engine engineFixture) carryOut(
	t *testing.T,
	requirement string,
	packagePath string,
) (string, domain.TaskID) {
	t.Helper()
	ctx := context.Background()
	requestID := engine.request(t, requirement)
	created, err := engine.lifecycle.CreateTaskFromRequirement(ctx, transport.CreateTaskCommand{
		ThreadID:                 engine.threadID,
		RequestMessageID:         &requestID,
		Requirement:              requirement,
		TaskClass:                string(fingerprint.TaskClassFeature),
		RepositoryRevision:       strings.Repeat("1", 40),
		BaselineModelRevision:    "engine-program",
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "profile-v1",
		AffectedPackages:         []string{packagePath},
		IdempotencyKey:           "engine-program",
	})
	if err != nil {
		t.Fatalf("intake refused the requirement: %v", err)
	}
	readyRevision := driveTaskToReady(t, engine.repositories, created.TaskID, created.Revision)
	preflight, err := engine.application.TaskPreflightService().BindExecution(
		ctx, created.TaskID, readyRevision,
		ForecastedTask{
			Policy:   storage.ExecutionPolicyRevision{Revision: created.PolicyRevision},
			Forecast: storage.EffortForecastRevision{Revision: created.ForecastRevision},
		},
		"engine-program-bind",
	)
	if err != nil {
		t.Fatalf("binding the approved preflight failed: %v", err)
	}
	if _, err := engine.lifecycle.StartPreparedTask(ctx, transport.StartTaskCommand{
		TaskID:            created.TaskID,
		ExpectedRevision:  readyRevision,
		PreflightRevision: preflight.Revision,
		IdempotencyKey:    "engine-program-start",
	}); err != nil {
		t.Fatalf("starting the approved task failed: %v", err)
	}
	binding, err := engine.repositories.GetWorktreeBinding(ctx, created.TaskID)
	if err != nil {
		t.Fatalf("starting a task must create its worktree: %v", err)
	}

	// The run is waited on, not the file. A file appears the moment the first
	// edit lands, and the refinement rounds that follow are exactly where a
	// first draft that compiles but misbehaves gets corrected.
	engine.waitFor(t, "the run to finish", func() bool {
		return strings.Contains(engine.narration(), "Finished:")
	})
	// What the run said is logged even when everything passes. A green result
	// with no record of how it was reached is the kind of evidence that stops
	// being worth anything the moment somebody doubts it.
	t.Logf("the run reported:\n%s", engine.narration())
	engine.reportFlow(t, created.TaskID)
	return binding.WorktreePath, created.TaskID
}

// normalizeProgramOutput compares what a program printed, not how the platform
// ends its lines.
func normalizeProgramOutput(output string) string {
	return strings.TrimSpace(strings.ReplaceAll(output, "\r\n", "\n"))
}

// repositoryRootForTest finds the checkout this test is running inside.
//
// The provider key lives beside the module, and a test's working directory is
// its own package, so walking up to the module root is what finds it.
func repositoryRootForTest(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return ""
		}
		directory = parent
	}
}

// The scripted fixture is a model like any other; this keeps that true.
var _ agentloop.FixedModel = (*scriptedEngineModel)(nil)

// storedSomewhere reports whether the record holds exactly these bytes.
//
// A run writes more than one file, and the order they were stored in is not
// something a caller should have to know, so this asks the only question that
// matters: is this content in the record at all.
func storedSomewhere(stored []storage.Artifact, content []byte) bool {
	for _, artifact := range stored {
		if string(artifact.Content) == string(content) {
			return true
		}
	}
	return false
}

// verifyPureCore checks that a core asked to be pure actually is.
//
// Purity is checked mechanically rather than by reading the code: the core may
// not import the packages through which a Go program reaches the outside
// world. That is a narrower claim than "functional" and it is the part that
// matters here — a core that cannot print, read, or touch the clock is one
// whose output depends only on its arguments, which is what makes it testable
// and composable. A core that merely looks tidy proves nothing.
func (program generatedProgram) verifyPureCore(t *testing.T, worktree string) {
	t.Helper()
	for _, core := range program.pureCore {
		body, err := os.ReadFile(
			filepath.Join(worktree, filepath.FromSlash(core)))
		if err != nil {
			t.Fatalf("a pure core was asked for at %s and none was written: %v",
				core, err)
		}
		source := string(body)
		t.Logf("the core it wrote (%s):\n%s", core, source)
		for _, forbidden := range []string{
			`"fmt"`, `"os"`, `"bufio"`, `"log"`, `"net/http"`, `"time"`,
			`"math/rand"`,
		} {
			if strings.Contains(source, forbidden) {
				t.Errorf("%s imports %s, so its result does not depend on its "+
					"arguments alone", core, forbidden)
			}
		}
	}
	// Some programs are asked for a particular shape rather than only a
	// particular answer. Checking for the shape is crude, and it is still the
	// difference between "it printed the right number" and "it was built the
	// way it was asked to be built".
	for file, fragments := range program.mustContain {
		body, err := os.ReadFile(
			filepath.Join(worktree, filepath.FromSlash(file)))
		if err != nil {
			t.Fatalf("%s was required and is missing: %v", file, err)
		}
		for _, fragment := range fragments {
			if !strings.Contains(string(body), fragment) {
				t.Errorf("%s does not contain %q, so the shape asked for was "+
					"not produced", file, fragment)
			}
		}
	}
}

// reportFlow prints how far the run actually got through the delivery flow.
//
// Every rung in this suite ends green, and green here means the executable
// printed the right answer — not that the work was checked in any of the ways
// the flow describes. Printing the ledger beside the result keeps those two
// claims from being read as one.
func (engine engineFixture) reportFlow(t *testing.T, taskID domain.TaskID) {
	t.Helper()
	recorded, err := engine.repositories.ListPipelineStages(
		context.Background(), taskID, 1)
	if err != nil || len(recorded) == 0 {
		t.Logf("the run left no flow ledger: %v", err)
		return
	}
	counts := map[pipeline.State]int{}
	var performed []string
	for _, record := range recorded {
		counts[record.State]++
		if record.State != pipeline.StateNotImplemented {
			performed = append(performed,
				fmt.Sprintf("%d:%s=%s", record.Stage, record.Name, record.State))
		}
	}
	t.Logf("flow: %d/%d stages performed [%s]; %d not implemented",
		len(performed), len(recorded), strings.Join(performed, " "),
		counts[pipeline.StateNotImplemented])

	// A failed gate fails the rung.
	//
	// This test used to assert that the produced program compiles, runs, and
	// prints the right bytes, and stop there. So a run whose own ledger
	// recorded three failed gates and refused to deliver was reported as a
	// passing rung — the engine blocked the work and the evidence about the
	// engine said it had succeeded. That is precisely the confusion the ledger
	// exists to prevent, reproduced one level up in the thing that reads it.
	//
	// The bytes are still checked, and separately: a program that prints the
	// wrong answer and a program whose tests cannot detect a defect are
	// different failures and a reader needs to know which one happened.
	var failed []string
	for _, record := range recorded {
		if record.State == pipeline.StateFailed {
			failed = append(failed, fmt.Sprintf("%d:%s — %s",
				record.Stage, record.Name, record.DetailRedacted))
		}
	}
	if len(failed) > 0 {
		t.Errorf("the run did not satisfy its own flow; %d gate(s) failed:\n  %s",
			len(failed), strings.Join(failed, "\n  "))
	}
	if os.Getenv("CODEFLUX_FLOW_DETAIL") == "" {
		return
	}
	// The whole ledger, stage by stage. It is long, which is the honest shape
	// of the answer: most of this flow is declared and gated and performed by
	// nothing, and a summary line makes that easier to skim past than it
	// should be.
	var table strings.Builder
	for _, record := range recorded {
		fmt.Fprintf(&table, "\n%2d %-22s %-16s %s",
			record.Stage, record.Name, record.State, record.DetailRedacted)
		if record.EvidenceJSON != "{}" {
			fmt.Fprintf(&table, "\n      evidence: %s", record.EvidenceJSON)
		}
		fmt.Fprintf(&table, "\n      gate: %s", record.Gate)
	}
	t.Logf("the full flow ledger:%s", table.String())
}

// describeStoredMismatch says how the record differs from the file.
//
// "not in the record" is true and useless. What a person needs is whether
// anything was stored at all, and if so where the stored bytes and the file
// first diverge — the difference between "nothing persisted" and "persisted
// but altered on the way in" is two entirely different defects.
func describeStoredMismatch(stored []storage.Artifact, written []byte) string {
	if len(stored) == 0 {
		return "nothing at all was stored"
	}
	var report strings.Builder
	fmt.Fprintf(&report, "%d artifact(s) stored; file is %d bytes",
		len(stored), len(written))
	for _, artifact := range stored {
		fmt.Fprintf(&report, "\n  stored %d bytes, media %s, first difference at %d",
			len(artifact.Content), artifact.MediaType,
			firstDifference(artifact.Content, written))
	}
	return report.String()
}

// firstDifference reports the byte offset where two versions diverge.
func firstDifference(stored, written []byte) int {
	limit := len(stored)
	if len(written) < limit {
		limit = len(written)
	}
	for index := range limit {
		if stored[index] != written[index] {
			return index
		}
	}
	return limit
}

// storedRedacted reports whether the record admits it holds an altered copy.
func storedRedacted(stored []storage.Artifact) bool {
	for _, artifact := range stored {
		if strings.HasSuffix(artifact.Type, "-redacted") {
			return true
		}
	}
	return false
}

// hostileInput is one way of using a program that nobody intended.
type hostileInput struct {
	name      string
	arguments []string
	stdin     string
	// invalid says this input cannot be served. A program given it must fail
	// visibly — non-zero exit, or something on stderr. Printing nothing and
	// reporting success is the failure mode this exists to catch, because a
	// caller has no way to tell it apart from a correct empty answer.
	invalid bool
}

// probeAdversarially runs the built program against input it was not designed
// for and reports what it does.
//
// Every other check in this file uses the one input the requirement described,
// which is the input least likely to break anything. A program is not correct
// because it handles the example; it is correct when it also refuses, clearly,
// everything it cannot handle.
func (program generatedProgram) probeAdversarially(t *testing.T, binary string) {
	t.Helper()
	var findings []string
	for _, hostile := range []hostileInput{
		{name: "nothing at all", invalid: program.needsInput()},
		{name: "unparseable text", arguments: []string{"!!!"},
			stdin: "this is not the input you wanted\n", invalid: true},
		{name: "empty but present", stdin: "\n", invalid: program.needsInput()},
		{name: "a very long line", arguments: []string{strings.Repeat("9", 4096)},
			stdin: strings.Repeat("x ", 20000) + "\n", invalid: true},
	} {
		findings = append(findings,
			program.observeHostileRun(t, binary, hostile)...)
	}
	if len(findings) == 0 {
		t.Logf("adversarial: %s survived every hostile input", program.packagePath)
		return
	}
	t.Errorf("adversarial findings for %s:\n  %s",
		program.packagePath, strings.Join(findings, "\n  "))
}

// needsInput reports whether this program was given input at all.
func (program generatedProgram) needsInput() bool {
	return program.stdin != "" || len(program.arguments) > 0
}

// observeHostileRun runs one hostile input and reports what went wrong.
func (program generatedProgram) observeHostileRun(
	t *testing.T,
	binary string,
	hostile hostileInput,
) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	run := exec.CommandContext(ctx, binary, hostile.arguments...)
	if hostile.stdin != "" {
		run.Stdin = strings.NewReader(hostile.stdin)
	}
	output, err := run.CombinedOutput()

	var findings []string
	if ctx.Err() != nil {
		return append(findings, hostile.name+": did not terminate within 10s")
	}
	if strings.Contains(string(output), "panic:") ||
		strings.Contains(string(output), "goroutine ") {
		return append(findings, hostile.name+": panicked — "+
			normalizeProgramOutput(string(output)))
	}
	if !hostile.invalid {
		return findings
	}
	// The input could not be served, so the program has to say so. Exiting
	// zero with nothing to show is indistinguishable from having done the job.
	exited := run.ProcessState != nil && run.ProcessState.ExitCode() == 0
	silent := strings.TrimSpace(string(output)) == ""
	if err == nil && exited && silent {
		findings = append(findings, hostile.name+
			": accepted bad input, printed nothing, and exited 0")
	}
	return findings
}

// acceptanceBlock renders this program's expected behaviour in the form the
// engine parses.
//
// It is appended to the requirement rather than passed beside it so the model
// is shown exactly what it will be judged against. Hiding the criterion and
// then failing a run for missing it would be a worse product than not
// checking: the run cannot refine toward something it cannot see.
func (program generatedProgram) acceptanceBlock() string {
	var block strings.Builder
	block.WriteString("\n\n<<<ACCEPTANCE\n")
	if len(program.arguments) > 0 {
		fmt.Fprintf(&block, "args: %s\n", strings.Join(program.arguments, " "))
	}
	if program.stdin != "" {
		escaped := strings.ReplaceAll(program.stdin, "\n", `\n`)
		escaped = strings.ReplaceAll(escaped, "\t", `\t`)
		fmt.Fprintf(&block, "stdin: %s\n", escaped)
	}
	fmt.Fprintf(&block, "expected:\n%s\n>>>", program.expected)
	return block.String()
}
