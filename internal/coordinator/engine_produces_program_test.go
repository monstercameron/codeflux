package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
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
// The ladder has three bands, and each one adds a way to be wrong that the
// band below it could not express. Rungs 1 to 48 are single programs that have
// to compute the right answer. Rungs 49 to 100 are systems: work split across
// packages that may not import each other freely, cores that have to stay
// pure, state shared between goroutines, and data that has to survive a
// restart — where a program can print the right answer and still be built
// wrong. Rungs 101 to 150 are HTTP services, where the answer includes the
// status code, the headers and what happens to a request nobody wanted. Rungs
// 151 to 200 are the work a company actually ships: several packages, a real
// SQLite database with migrations, constraints and query plans, and a
// specification with acceptance criteria that interact, checked by a journey
// rather than by one call. Rungs 201 to 250 are that work built on somebody
// else's code, where the difficulty is no longer what to write but where to
// let a dependency reach.
//
// No rung names a file, a package or a function. The layout is the run's to
// decide and part of what is being judged, so the checks ask about properties
// instead: how many packages the work was separated into, whether the package
// an idea landed in can reach the outside world, how far a library was allowed
// to spread, whether there is an interface to substitute at, whether the same
// input twice gives the same output, and whether anything was left on disk.
//
// It calls a real provider, so it costs money and needs a key. Without one it
// skips rather than passing quietly: a green run that never reached a model
// would be the most misleading result this file could produce.
//
// How the rungs relate to each other is chosen with CODEFLUX_LADDER; see
// startLadder for what the two modes are for.
func TestTheEngineProducesProgramsThatBuildAndRun(t *testing.T) {
	// Asked for explicitly, or not at all.
	//
	// A provider key is a reason this test *can* run, and it was for a while
	// the only thing standing between it and every `go test ./...` in the
	// repository — including the one the pre-push hook runs. That is two
	// hundred and fifty live model runs charged to whoever happened to have a
	// key in their .env, triggered by a command nobody would expect to spend
	// money. The mode has to be named on purpose.
	mode := strings.TrimSpace(os.Getenv("CODEFLUX_LADDER"))
	if mode == "" {
		t.Skip("the ladder calls a real provider on every rung and costs real " +
			"money: set CODEFLUX_LADDER=isolated or CODEFLUX_LADDER=shared to " +
			"run it")
	}
	key := ReadProviderKey(repositoryRootForTest(t))
	if key == "" {
		t.Skip("no provider key: set OPENAI_API_KEY or put it in .env")
	}
	ladder := startLadder(t, key)

	for _, program := range ladderRungs() {
		t.Run(program.name, func(t *testing.T) {
			program.buildAndRun(t, ladder)
		})
	}
}

// ladderRungs is the ladder itself, in order.
//
// It is a function rather than a literal inside the test so that the checks
// which need no provider key can read it: that the rungs are numbered without
// a gap, that no two share the identity their requests are keyed by, and that
// no requirement names a file. Those are properties of the table, and a
// property of the table that can only be checked by spending money on two
// hundred and fifty model runs is one nobody checks.
func ladderRungs() []generatedProgram {
	return []generatedProgram{
		{
			name: "1 prints a fixed line",
			requirement: "Write a command-line program whose main function prints exactly " +
				"this one line and nothing else: Hello from CodeFlux",
			expected: "Hello from CodeFlux",
		},
		{
			name: "2 computes over its arguments",
			requirement: "Write a command-line program that reads every command-line " +
				"argument as an integer, adds them up, and prints only the sum on " +
				"one line.",
			arguments: []string{"7", "11", "24"},
			expected:  "42",
		},
		{
			name: "3 branches on an argument",
			requirement: "Write a command-line program that reads one integer N from the " +
				"first command-line argument and prints the numbers 1 to N, one " +
				"per line, except that multiples of 3 print Fizz, multiples of 5 " +
				"print Buzz, and multiples of both print FizzBuzz. Print nothing " +
				"else.",
			arguments: []string{"15"},
			expected: "1\n2\nFizz\n4\nBuzz\nFizz\n7\n8\nFizz\nBuzz\n11\n" +
				"Fizz\n13\n14\nFizzBuzz",
		},
		{
			name: "4 aggregates and orders its input",
			requirement: "Write a command-line program that reads all of standard input, " +
				"splits it into whitespace-separated words, and prints one line " +
				"per distinct word in the form 'word count'. Order the lines by " +
				"descending count, and order words with the same count " +
				"alphabetically. Print nothing else.",
			stdin:    "b a c b a b\n",
			expected: "b 3\na 2\nc 1",
		},
		{
			name: "5 parses structured input and reports on it",
			requirement: "Write a command-line program that reads a JSON array from " +
				"standard input where each element is an object with a string " +
				"field 'name' and an integer field 'amount'. Print one line per " +
				"distinct name in the form 'name total', ordered alphabetically " +
				"by name, where total is the sum of that name's amounts. Then " +
				"print a final line 'TOTAL total' with the sum of every amount. " +
				"Print nothing else.",
			stdin: `[{"name":"rent","amount":1200},{"name":"food","amount":300},` +
				`{"name":"rent","amount":100},{"name":"bus","amount":50}]`,
			expected: "bus 50\nfood 300\nrent 1300\nTOTAL 1650",
		},
		{
			name: "6 evaluates an expression with a stack",
			requirement: "Write a command-line program that treats its command-line " +
				"arguments as a reverse Polish notation expression over integers, " +
				"supporting the operators +, -, * and /, evaluates it with a " +
				"stack, and prints only the integer result on one line.",
			arguments: []string{"3", "4", "+", "2", "*", "7", "-"},
			expected:  "7",
		},
		{
			name: "7 summarises tabular input by column",
			requirement: "Write a command-line program that reads CSV from standard input " +
				"where the first row is a header. For every column whose values " +
				"are all integers, print one line 'column min max sum' using that " +
				"column's values. Keep the columns in header order and print " +
				"nothing else.",
			stdin:    "name,score,age\na,10,30\nb,20,40\nc,30,50\n",
			expected: "score 10 30 60\nage 30 50 120",
		},
		{
			name: "8 runs a real algorithm over its input",
			requirement: "Write a command-line program that reads lines from standard " +
				"input, each holding two integers 'start end' separated by a " +
				"space, merges every overlapping or touching interval, and prints " +
				"the merged intervals in ascending order, one per line as 'start " +
				"end'. Print nothing else.",
			stdin:    "1 3\n2 6\n8 10\n15 18\n",
			expected: "1 6\n8 10\n15 18",
		},
		{
			name: "9 interprets a small language",
			requirement: "Write a command-line program that reads a program from standard " +
				"input, one instruction per line, and executes it on an integer " +
				"stack. The instructions are 'PUSH n' which pushes integer n, " +
				"'ADD' and 'MUL' which pop two values and push their sum or " +
				"product, 'DUP' which duplicates the top value, and 'PRINT' which " +
				"pops the top value and prints it on its own line. Print nothing " +
				"else.",
			stdin:    "PUSH 2\nPUSH 3\nADD\nDUP\nMUL\nPRINT\n",
			expected: "25",
		},
		{
			name: "10 searches a grid for a shortest path",
			requirement: "Write a command-line program that reads a rectangular grid from " +
				"standard input, one row per line, where '#' is a wall, '.' is " +
				"open, 'S' is the start and 'E' is the end. Moving only up, down, " +
				"left or right through non-wall cells, print the number of moves " +
				"in a shortest path from S to E, or print 'no path' if none " +
				"exists. Print nothing else.",
			stdin:    "S.#\n..#\n#.E\n",
			expected: "4",
		},
		{
			name: "11 spreads one program across two files",
			requirement: "Write a command-line program split across two files in one " +
				"package: one holds an Account type with Deposit, Withdraw and " +
				"Balance methods, and the other reads commands from standard " +
				"input. The commands are 'DEPOSIT n', 'WITHDRAW n' and 'BALANCE'. " +
				"BALANCE prints the current balance on its own line. A WITHDRAW " +
				"larger than the balance must change nothing and print " +
				"'insufficient' on its own line. Print nothing else.",
			stdin:    "DEPOSIT 100\nWITHDRAW 30\nBALANCE\nWITHDRAW 200\nBALANCE\n",
			expected: "70\ninsufficient\n70",
		},
		{
			name: "12 parses infix arithmetic with precedence",
			requirement: "Write a command-line program that evaluates the single infix " +
				"arithmetic expression given as its first command-line argument. " +
				"It must support +, -, * and / over integers with the usual " +
				"precedence, parentheses, and left-to-right association, using " +
				"integer division. Print only the integer result on one line.",
			arguments: []string{"2+3*4-(5-1)/2"},
			expected:  "12",
		},
		{
			name: "13 simulates a cache with an eviction policy",
			requirement: "Write a command-line program that simulates a " +
				"least-recently-used cache whose capacity is the first " +
				"command-line argument. It reads commands from standard input: " +
				"'PUT key value' stores a value, and 'GET key' prints the stored " +
				"value on its own line or -1 if it is absent. Both PUT and a " +
				"successful GET count as using a key. When a PUT exceeds " +
				"capacity, evict the least recently used key. After all commands, " +
				"print a final line 'hits misses' holding the number of " +
				"successful and unsuccessful GETs. Print nothing else.",
			arguments: []string{"2"},
			stdin:     "PUT a 1\nPUT b 2\nGET a\nPUT c 3\nGET b\nGET c\n",
			expected:  "1\n-1\n3\n2 1",
		},
		{
			name: "14 finds a cheapest route through a weighted graph",
			requirement: "Write a command-line program that reads a weighted directed " +
				"graph from standard input. The first line holds four integers 'N " +
				"M start end': the number of nodes, the number of edges, the " +
				"start node and the end node. Each of the next M lines holds 'u v " +
				"w', a directed edge from u to v of positive weight w. Print the " +
				"total weight of a cheapest path from start to end, or " +
				"'unreachable' if there is none. Print nothing else.",
			stdin:    "4 4 0 3\n0 1 1\n1 2 2\n0 2 5\n2 3 1\n",
			expected: "4",
		},
		{
			name: "15 fully justifies text to a width",
			requirement: "Write a command-line program that reads whitespace-separated " +
				"words from standard input and prints them wrapped to the line " +
				"width given as the first command-line argument. Pack as many " +
				"words onto each line as fit, separated by at least one space. " +
				"Pad every line except the last so that it is exactly the given " +
				"width, distributing the extra spaces between words as evenly as " +
				"possible and giving the leftmost gaps the extra space when they " +
				"do not divide evenly. Left-justify the last line. Print nothing " +
				"else.",
			arguments: []string{"16"},
			stdin:     "This is an example of text justification.\n",
			expected:  "This    is    an\nexample  of text\njustification.",
		},
		{
			// The bar is what this requirement describes: one package that
			// exports, and the command that imports it. It asked for three,
			// which is one more package than the prose ever mentions, and the
			// two directories it counts include the command — so a run that
			// grouped the work exactly as written was failed for it.
			//
			// The name is where the confusion came from: "two packages" reads
			// as two libraries, and the requirement names one. Rung 17 sets two
			// for the same shape (one pure package and a command) and rung 18
			// sets three for a requirement that genuinely describes two
			// libraries, so two is what this rung's siblings already mean by it.
			//
			// Cam settled this on 2026-08-04 after the engine produced
			// cmd/stats and stats on two independent runs, having produced one
			// package on every run before the planner chose its own layout.
			name: "16 spans a package and its command with a real import",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. One package exports Mean and Max, each taking a slice " +
				"of int and returning an int, where Mean truncates toward zero. " +
				"The command imports that package, reads its command-line " +
				"arguments as integers, and prints only one line holding the mean " +
				"and the max separated by a space.",
			arguments:   []string{"1", "2", "3", "4"},
			expected:    "2 4",
			minPackages: 2,
		},
		{
			// The same ledger as rung 5, asked for as a functional core behind
			// an imperative shell. Rung 5 produced one imperative main with no
			// function boundary in it and therefore nothing testable; this asks
			// whether the engine can produce the shape on request, and the
			// check is mechanical rather than stylistic: the core must not
			// touch the outside world.
			name: "17 keeps a pure core behind an imperative shell",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. One package must be pure: it may not read input, " +
				"print, or touch os, fmt, or any other side effect, and it " +
				"exports an Entry struct with Name string and Amount int, and a " +
				"function Totals taking a slice of Entry and returning a slice of " +
				"the per-name totals ordered alphabetically by name together with " +
				"the overall total. The command imports that package, and does " +
				"all of the input and output: it decodes a JSON array of entries " +
				"from standard input, calls Totals, prints one line per name as " +
				"'name total' and then a final line 'TOTAL total'. Print nothing " +
				"else.",
			stdin: `[{"name":"rent","amount":1200},{"name":"food","amount":300},` +
				`{"name":"rent","amount":100},{"name":"bus","amount":50}]`,
			expected:     "bus 50\nfood 300\nrent 1300\nTOTAL 1650",
			minPackages:  2,
			purePackages: 1,
			pureSymbols:  []string{"Totals"},
		},
		{
			// The hardest rung: a generic Result monad, atomic pure functions
			// composed through it, and the monad laws asserted in the
			// repository's own tests. The tests matter for a second reason —
			// every other rung leaves the engine running "go test ./..."
			// against a repository with no tests in it, so its own validation
			// passes vacuously. This is the first case where the engine's
			// safety net has something to catch.
			// "Pure" is spelled out here the way rung 17 spells it out.
			//
			// This asked for two pure packages and defined the word nowhere,
			// while the check is mechanical and strict: an import of fmt makes
			// a package impure, and fmt.Errorf is the idiomatic way to build
			// the error a parse failure returns. Rung 18 on 2026-08-04 produced
			// a correct program — the monad laws asserted, 31 of 37 stages
			// satisfied, built, run, printing exactly what was asked, surviving
			// every hostile input — and failed because mean/mean.go imports
			// fmt. errors.New is not on the list, so there is a clean way to do
			// it; the run simply had no way to know that was the difference.
			//
			// This states the bar rather than lowering it. Nothing about what
			// is checked changes.
			name: "18 composes atomic functions through a Result monad",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. A pure package here may not read input, print, or " +
				"import fmt, os, bufio, log, net, database/sql, math/rand or " +
				"time; use errors.New or a sentinel error rather than " +
				"fmt.Errorf. One package must be pure. It defines a generic type " +
				"Result[T any] holding either a value or an error, constructors " +
				"Ok and Err, a method IsOk, a method Unwrap returning the value " +
				"and the error, and because Go methods cannot take type " +
				"parameters it also defines package-level generic functions Map[A " +
				"any, B any](Result[A], func(A) B) Result[B] and AndThen[A any, B " +
				"any](Result[A], func(A) Result[B]) Result[B]. A test covers the " +
				"three monad laws for Result: left identity, right identity and " +
				"associativity. A second package must be pure. It imports the " +
				"result package and exports two small functions, ParseInts taking " +
				"a slice of string and returning fp.Result of a slice of int, and " +
				"Mean taking a slice of int and returning fp.Result of an int " +
				"using integer division that fails on an empty slice. The command " +
				"does all input and output: it composes ParseInts and Mean over " +
				"its command-line arguments using fp.AndThen, then prints only " +
				"the integer mean on one line, or the word failed on one line if " +
				"the result holds an error.",
			arguments:   []string{"10", "20", "30"},
			expected:    "20",
			minPackages: 3,
			mustAppear: []string{
				"type Result[", "func Ok[", "func Err[", "func Map[",
				"func AndThen[", "func ParseInts(", "func Mean(", ".AndThen(",
			},
			purePackages: 2,
			pureSymbols:  []string{"AndThen", "ParseInts"},
		},
		{
			name: "19 orders a dependency graph and detects a cycle",
			requirement: "Write a command-line program that reads lines from standard " +
				"input, each holding two names separated by a space meaning the " +
				"first must come before the second. Print the names in an order " +
				"that respects every constraint, one per line, choosing the " +
				"alphabetically first available name whenever more than one is " +
				"ready. If no such order exists print only the word cycle. Print " +
				"nothing else.",
			stdin:    "a b\na c\nb d\nc d\n",
			expected: "a\nb\nc\nd",
		},
		{
			name: "20 implements a regular expression matcher",
			requirement: "Write a command-line program that takes a pattern as its first " +
				"command-line argument and a text as its second, and decides " +
				"whether the pattern matches the whole text. Implement the " +
				"matching yourself without using the regexp package. The pattern " +
				"supports a literal character, a dot meaning any single " +
				"character, a star meaning zero or more of the preceding element, " +
				"a leading caret anchoring the start, and a trailing dollar " +
				"anchoring the end. Print only the word match or the words no " +
				"match on one line.",
			arguments: []string{"^a.*c$", "abbbc"},
			expected:  "match",
		},
		{
			name: "21 multiplies numbers too large for a machine word",
			requirement: "Write a command-line program that multiplies the two decimal " +
				"integers given as its command-line arguments and prints only the " +
				"product on one line. The numbers are far larger than any " +
				"built-in integer type, so do the arithmetic on the digits " +
				"yourself and do not use the math/big package.",
			arguments: []string{
				"99999999999999999999", "99999999999999999999",
			},
			expected: "9999999999999999999800000000000000000001",
		},
		{
			name: "22 builds an optimal prefix code",
			requirement: "Write a command-line program that reads lines from standard " +
				"input, each holding a symbol and its frequency separated by a " +
				"space, builds an optimal binary prefix code for those " +
				"frequencies, and prints only the total number of bits the " +
				"encoded input would occupy on one line.",
			stdin:    "a 5\nb 9\nc 12\nd 13\ne 16\nf 45\n",
			expected: "224",
		},
		{
			name: "23 parses JSON without the standard decoder",
			requirement: "Write a command-line program that reads one JSON value from " +
				"standard input and prints it back in a canonical form: no " +
				"whitespace, and every object's keys in ascending order. Write " +
				"the parser yourself and do not use the encoding/json package. " +
				"Print nothing else.",
			stdin:    `{"b":1,"a":[2,3],"c":{"y":true,"x":null}}`,
			expected: `{"a":[2,3],"b":1,"c":{"x":null,"y":true}}`,
		},
		{
			name: "24 solves a linear system exactly",
			requirement: "Write a command-line program that reads a linear system from " +
				"standard input. The first line holds the number of unknowns N. " +
				"Each of the next N lines holds N+1 integers: the coefficients of " +
				"one equation followed by its right-hand side. Solve the system " +
				"exactly, without floating point, and print the values of the " +
				"unknowns in order on one line separated by spaces. Print nothing " +
				"else.",
			stdin:    "2\n2 1 5\n1 -1 1\n",
			expected: "2 1",
		},
		{
			name: "25 reports the difference between two texts",
			requirement: "Write a command-line program that reads two blocks of lines from " +
				"standard input separated by a line holding exactly three " +
				"hyphens. Print the difference between them using a longest " +
				"common subsequence: a line present in both is printed with a " +
				"leading space, a line only in the first is printed with a " +
				"leading minus, and a line only in the second is printed with a " +
				"leading plus. Keep the original order and print nothing else.",
			// The expected transcript must not open with a line whose only
			// content is the leading-space "unchanged" marker: buildAndRun
			// compares against program.expected through normalizeProgramOutput,
			// which strings.TrimSpace()s the whole transcript, and the
			// acceptance block's own parser (parseOneExample, agent_acceptance.go)
			// does the same to what it reads back. Either way a leading space
			// that is the very first byte of the expected text is silently
			// eaten, so the rung's own correctness check could never pass even
			// for a correct program. Ordering the blocks so the first line
			// differs (a deletion, not a context line) keeps the assertion
			// meaningful.
			stdin:    "a\nb\nc\n---\nx\nb\nc\n",
			expected: "-a\n+x\n b\n c",
		},
		{
			name: "26 reduces a lambda expression to normal form",
			requirement: "Write a command-line program that reads one lambda-calculus " +
				"expression from standard input and prints its normal form on one " +
				"line. An abstraction is written L followed by a variable, a dot, " +
				"and a body. Application is written by juxtaposition and " +
				"associates to the left. Parentheses group. Reduce by " +
				"beta-reduction until no redex remains, renaming bound variables " +
				"where needed to avoid capture. Print nothing else.",
			stdin:    "(Lx.Ly.x) a b\n",
			expected: "a",
		},
		{
			name: "27 answers stabbing queries over intervals",
			requirement: "Write a command-line program that reads from standard input a " +
				"list of closed integer intervals, one per line as two integers " +
				"separated by a space, then a line holding a single question " +
				"mark, then a list of integer queries one per line. For each " +
				"query print only the number of intervals that contain it, one " +
				"per line. Print nothing else.",
			stdin:    "1 5\n3 8\n10 12\n?\n4\n9\n11\n",
			expected: "2\n0\n1",
		},
		{
			name: "28 decides satisfiability by search",
			requirement: "Write a command-line program that reads a boolean formula in " +
				"conjunctive normal form from standard input. The first line " +
				"holds the number of variables and the number of clauses. Each " +
				"following line holds one clause as non-zero integers terminated " +
				"by a zero, where a positive integer means that variable and a " +
				"negative integer means its negation. Decide whether any " +
				"assignment satisfies every clause and print only the word SAT or " +
				"the word UNSAT on one line.",
			stdin:    "2 4\n1 2 0\n1 -2 0\n-1 2 0\n-1 -2 0\n",
			expected: "UNSAT",
		},
		{
			name: "29 interprets a language with closures and recursion",
			requirement: "Write a command-line program that reads a Lisp-like program " +
				"from standard input, evaluates it, and prints the value of the " +
				"last expression on one line. Support integer literals, the " +
				"special forms define, if and lambda, function definition in the " +
				"form (define (name args) body), recursion, and the primitives +, " +
				"-, * and = which compares two integers. Print nothing else.",
			stdin: "(define (fact n) (if (= n 0) 1 (* n (fact (- n 1))))) " +
				"(fact 10)\n",
			expected: "3628800",
		},
		{
			name: "30 compiles a pattern into an automaton",
			requirement: "Write a command-line program that takes a regular expression as " +
				"its first command-line argument, compiles it into a " +
				"nondeterministic automaton and then into a deterministic one by " +
				"subset construction, and uses that automaton to decide each line " +
				"of standard input. The expression supports concatenation, " +
				"alternation written with a vertical bar, the star, and " +
				"parentheses. Print match or no match for each line, one per " +
				"line, and nothing else.",
			arguments: []string{"(a|b)*abb"},
			stdin:     "abb\naabb\nbabb\nabab\n",
			expected:  "match\nmatch\nmatch\nno match",
		},
		{
			name: "31 raises a matrix to a power to reach a large term",
			requirement: "Write a command-line program that computes the nth Fibonacci " +
				"number, where n is its first command-line argument, by repeated " +
				"squaring of the two by two matrix rather than by iteration or " +
				"recursion. Fibonacci numbers start 1, 1, 2, 3. Print only the " +
				"result on one line.",
			arguments: []string{"90"},
			expected:  "2880067194370816120",
		},
		{
			name: "32 keeps every earlier version of a structure alive",
			requirement: "Write a command-line program that maintains a persistent " +
				"immutable map from string to integer. It reads commands from " +
				"standard input: 'set key value' produces a new version and " +
				"prints its number starting from 1, and 'get version key' prints " +
				"the value that key had in that version, or the word absent. " +
				"Setting a key must not change any earlier version. Print nothing " +
				"else.",
			stdin:    "set a 1\nset b 2\nset a 3\nget 1 a\nget 2 a\nget 3 a\nget 1 b\n",
			expected: "1\n2\n3\n1\n1\n3\nabsent",
		},
		{
			name: "33 searches a large constrained space",
			requirement: "Write a command-line program that counts the arrangements of N " +
				"queens on an N by N board with no two attacking each other, " +
				"where N is its first command-line argument. Print only the count " +
				"on one line.",
			arguments: []string{"10"},
			expected:  "724",
		},
		{
			name: "34 compiles to bytecode and executes it",
			requirement: "Write a command-line program that compiles the infix arithmetic " +
				"expression given as its first command-line argument into a stack " +
				"bytecode and then runs that bytecode. The instructions are PUSH " +
				"n, ADD, SUB, MUL and DIV, and the compiler emits them in postfix " +
				"order. Print each instruction on its own line in the order " +
				"emitted, then the result of running them on its own line. " +
				"Support + - * / and parentheses with the usual precedence and " +
				"integer division.",
			arguments: []string{"2+3*4"},
			expected:  "PUSH 2\nPUSH 3\nPUSH 4\nMUL\nADD\n14",
		},
		{
			name: "35 chooses optimally under a constraint",
			requirement: "Write a command-line program that reads a capacity on the first " +
				"line of standard input and then one item per line as a weight " +
				"and a value separated by a space. Each item may be taken at most " +
				"once. Print only the greatest total value that fits within the " +
				"capacity, on one line.",
			stdin:    "5\n2 3\n3 4\n4 5\n5 6\n",
			expected: "7",
		},
		{
			name: "36 finds the shortest edit script between two sequences",
			requirement: "Write a command-line program that reads two blocks of lines from " +
				"standard input separated by a line holding exactly three " +
				"hyphens, and prints only the smallest number of line insertions " +
				"and deletions that turns the first block into the second, on one " +
				"line.",
			stdin:    "a\nb\nc\nd\n---\na\nc\nd\ne\n",
			expected: "2",
		},
		{
			name: "37 decides what is still reachable",
			requirement: "Write a command-line program that reads a heap from standard " +
				"input. The first line holds the number of objects, numbered from " +
				"1. Each following line until a line holding a single question " +
				"mark describes one reference as two object numbers separated by " +
				"a space. The remaining line lists the root objects separated by " +
				"spaces. Print the number of objects reachable from the roots and " +
				"the number that are not, separated by a space, on one line.",
			stdin:    "6\n1 2\n2 3\n4 5\n?\n1\n",
			expected: "3 3",
		},
		{
			name: "38 finds the longest path through a weighted graph",
			requirement: "Write a command-line program that reads a directed acyclic graph " +
				"of tasks from standard input. The first line holds the number of " +
				"tasks, numbered from 1. Each following line holds a task number " +
				"and its duration. Then a line holding a single question mark, " +
				"then one dependency per line as two task numbers meaning the " +
				"first must finish before the second starts. Print only the " +
				"earliest time all tasks can be complete, on one line.",
			stdin:    "4\n1 3\n2 2\n3 4\n4 1\n?\n1 2\n1 3\n2 4\n3 4\n",
			expected: "8",
		},
		{
			name: "39 answers a query language over tabular data",
			requirement: "Write a command-line program that reads a query on the first " +
				"line of standard input and then a CSV table with a header row on " +
				"the lines after it. Support SELECT of named columns and of SUM " +
				"over a column, an optional WHERE comparing a column to an " +
				"integer with one of < > =, an optional GROUP BY of one column, " +
				"and an optional ORDER BY of one column ascending. Print the " +
				"header of the result and then one row per line, values separated " +
				"by commas. Print nothing else.",
			stdin: "SELECT name, SUM(amount) FROM t WHERE amount > 40 " +
				"GROUP BY name ORDER BY name\n" +
				"name,amount\nrent,1200\nfood,30\nrent,100\nbus,50\nfood,300\n",
			expected: "name,SUM(amount)\nbus,50\nfood,300\nrent,1300",
		},
		{
			name: "40 unifies terms and searches for a proof",
			requirement: "Write a command-line program that reads a logic program from " +
				"standard input, one clause per line, then a line holding a " +
				"single question mark, then one query. A clause is either a fact " +
				"like parent(a,b). or a rule like ancestor(X,Y) :- parent(X,Y). " +
				"with a comma-separated body. Names beginning with a capital " +
				"letter are variables. Resolve the query by unification with " +
				"backtracking and print each distinct solution's bindings as " +
				"Var=value pairs separated by spaces, one solution per line, in " +
				"the order found. Print nothing else.",
			stdin: "parent(a,b).\nparent(b,c).\nancestor(X,Y) :- parent(X,Y).\n" +
				"ancestor(X,Y) :- parent(X,Z), ancestor(Z,Y).\n?\nancestor(a,Y)\n",
			expected: "Y=b\nY=c",
		},
		{
			name: "41 divides to arbitrary precision without floating point",
			requirement: "Write a command-line program that divides the first command-line " +
				"argument by the second and prints the result to the number of " +
				"decimal places given by the third, truncating rather than " +
				"rounding. Do the arithmetic on digits yourself: no floating " +
				"point and no math/big. Print only the result on one line.",
			arguments: []string{"1", "7", "50"},
			expected:  "0.14285714285714285714285714285714285714285714285714",
		},
		{
			name: "42 solves an exact cover by systematic search",
			requirement: "Write a command-line program that reads a zero-one matrix from " +
				"standard input, one row per line of digits with no separators, " +
				"and counts the ways to choose a set of rows such that every " +
				"column is covered by exactly one chosen row. Print only the " +
				"count on one line.",
			stdin:    "0010110\n1001001\n0110010\n1001000\n0100001\n0001101\n",
			expected: "1",
		},
		{
			name: "43 merges two divergent edits of the same text",
			requirement: "Write a command-line program that reads three blocks of lines " +
				"from standard input separated by lines holding exactly three " +
				"hyphens: the common ancestor, then one edit of it, then another. " +
				"Produce the merged result, taking a line from whichever side " +
				"changed it. If both sides changed the same line differently, " +
				"print only the word conflict. Print nothing else.",
			stdin:    "a\nb\nc\n---\na\nX\nc\n---\na\nb\nY\n",
			expected: "a\nX\nY",
		},
		{
			name: "44 proves a formula has no solution at all",
			requirement: "Write a command-line program that reads a formula in conjunctive " +
				"normal form from standard input in the same shape as DIMACS: a " +
				"first line of the variable and clause counts, then one clause " +
				"per line as non-zero integers terminated by a zero. Decide " +
				"satisfiability using unit propagation and backtracking search, " +
				"and print only the word SAT or the word UNSAT on one line.",
			stdin: "6 9\n1 2 0\n3 4 0\n5 6 0\n-1 -3 0\n-1 -5 0\n-3 -5 0\n" +
				"-2 -4 0\n-2 -6 0\n-4 -6 0\n",
			expected: "UNSAT",
		},
		{
			name: "45 encodes and decodes its own compression",
			requirement: "Write a command-line program that reads a line of text from " +
				"standard input, builds an optimal prefix code for its " +
				"characters, encodes the text with it, decodes the result back " +
				"using only the code it built, and then prints the decoded text " +
				"on one line followed by the number of bits the encoding occupied " +
				"on the next. Print nothing else.",
			stdin:    "abracadabra\n",
			expected: "abracadabra\n23",
		},
		{
			name: "46 evaluates higher-order functions over closures",
			requirement: "Write a command-line program that evaluates the Lisp-like " +
				"expression read from standard input and prints its value on one " +
				"line. Support integer literals, lambda, application, and the " +
				"primitives + - and *. A lambda must capture the environment where " +
				"it was written, so a function passed to another function still " +
				"sees its own bindings. Print nothing else.",
			stdin:    "((lambda (f) (f (f 3))) (lambda (x) (* x x)))\n",
			expected: "81",
		},
		{
			name: "47 runs a small expression language with state",
			requirement: "Write a command-line program that reads a program from standard " +
				"input as statements separated by semicolons. A statement is " +
				"either an assignment of an expression to a name, or an " +
				"expression. Expressions support + - * / with the usual " +
				"precedence, parentheses, unary minus, integer literals, " +
				"previously assigned names, and the functions min, max and abs of " +
				"two arguments except abs which takes one. Print the value of the " +
				"last statement on one line, and nothing else.",
			stdin:    "x = 3; y = -4; max(x * x + abs(y), 20)\n",
			expected: "25",
		},
		{
			name: "48 divides numbers larger than any machine word",
			requirement: "Write a command-line program that divides the first command-line " +
				"argument by the second, both decimal integers far larger than " +
				"any built-in type, and prints the quotient and the remainder " +
				"separated by a space on one line. Do the long division on the " +
				"digits yourself and do not use the math/big package.",
			arguments: []string{"1000000000000000000000", "7"},
			expected:  "142857142857142857142 6",
		},
		{
			name: "49 finds the longest repeated substring",
			requirement: "Write a command-line program that reads one line from standard " +
				"input, builds a suffix array and its longest-common-prefix " +
				"array, and prints the longest substring that occurs at least " +
				"twice, choosing the lexicographically smallest when several are " +
				"equally long. Print the word none if there is no such substring. " +
				"Print nothing else.",
			stdin:    "banana\n",
			expected: "ana",
		},
		{
			name: "50 matches many patterns in a single pass",
			requirement: "Write a command-line program that reads a count N, then N " +
				"patterns one per line, then a line holding a single question " +
				"mark, then one line of text. Build an Aho-Corasick automaton " +
				"with failure links and scan the text once. Print every " +
				"occurrence as the pattern and its zero-based start position " +
				"separated by a space, ordered by position and then by pattern. " +
				"Print nothing else.",
			stdin:    "3\nhe\nshe\nhis\n?\nushers\n",
			expected: "she 1\nhe 2",
		},
		{
			name: "51 answers range queries under range updates",
			requirement: "Write a command-line program that reads N on the first line and " +
				"N integers on the second, then one command per line. 'add l r v' " +
				"adds v to every element from l to r inclusive, counting from " +
				"one, and 'sum l r' prints the sum of that range on its own line. " +
				"Use a segment tree with lazy propagation so every command costs " +
				"logarithmic time. Print nothing else.",
			stdin:    "5\n1 2 3 4 5\nadd 2 4 10\nsum 1 5\nsum 2 3\n",
			expected: "45\n25",
		},
		{
			name: "52 builds a minimum spanning tree",
			requirement: "Write a command-line program that reads 'N M' and then M lines " +
				"'u v w' describing an undirected weighted graph on nodes " +
				"numbered from one. Print only the total weight of a minimum " +
				"spanning tree on one line, or the word disconnected if no " +
				"spanning tree exists.",
			stdin:    "4 5\n1 2 1\n2 3 2\n3 4 3\n4 1 4\n1 3 5\n",
			expected: "6",
		},
		{
			name: "53 finds a maximum flow",
			requirement: "Write a command-line program that reads 'N M source sink' and " +
				"then M lines 'u v capacity' describing a directed graph on nodes " +
				"numbered from one. Print only the value of a maximum flow from " +
				"the source to the sink, on one line.",
			stdin: "6 10 1 6\n1 2 16\n1 3 13\n2 3 10\n3 2 4\n2 4 12\n3 5 14\n" +
				"4 3 9\n5 4 7\n4 6 20\n5 6 4\n",
			expected: "23",
		},
		{
			name: "54 finds the cheapest maximum flow",
			requirement: "Write a command-line program that reads 'N M source sink' and " +
				"then M lines 'u v capacity cost' describing a directed graph on " +
				"nodes numbered from one, where cost is per unit of flow. Print " +
				"the value of a maximum flow and the least total cost that " +
				"achieves it, separated by a space on one line.",
			stdin:    "4 4 1 4\n1 2 2 1\n1 3 2 2\n2 4 2 2\n3 4 2 1\n",
			expected: "4 12",
		},
		{
			name: "55 assigns work at the least total cost",
			requirement: "Write a command-line program that reads N on the first line and " +
				"then an N by N cost matrix, one row of N integers per line, and " +
				"assigns each row to exactly one distinct column at the least " +
				"possible total cost. Print only that total on one line.",
			stdin:    "3\n4 1 3\n2 0 5\n3 2 2\n",
			expected: "5",
		},
		{
			name: "56 wraps a point set in its convex hull",
			requirement: "Write a command-line program that reads N and then N lines 'x y' " +
				"of integer points, and prints the vertices of their convex hull " +
				"counter-clockwise, one per line as 'x y', starting from the " +
				"point with the lowest y and then the lowest x. A point lying on " +
				"an edge is not a vertex. Print nothing else.",
			stdin:    "5\n0 0\n4 0\n4 4\n0 4\n2 2\n",
			expected: "0 0\n4 0\n4 4\n0 4",
		},
		{
			name: "57 answers nearest-neighbour queries from a k-d tree",
			requirement: "Write a command-line program that reads N and then N lines 'x y' " +
				"of integer points, then a line holding a single question mark, " +
				"then one query point per line. Build a two-dimensional k-d tree " +
				"and, for each query, print the nearest point as 'x y' on its own " +
				"line, breaking ties by the lower x and then the lower y. Print " +
				"nothing else.",
			stdin:    "6\n2 3\n5 4\n9 6\n4 7\n8 1\n7 2\n?\n9 2\n",
			expected: "8 1",
		},
		{
			name: "58 multiplies polynomials faster than the obvious way",
			requirement: "Write a command-line program that reads the degree-plus-one " +
				"count of the first polynomial, its integer coefficients from " +
				"lowest power upward on one line, and then the same for a second " +
				"polynomial. Multiply them in better than quadratic time — by " +
				"Karatsuba or by a number-theoretic transform, not by the " +
				"schoolbook double loop — and print the exact integer " +
				"coefficients of the product from lowest power upward, separated " +
				"by spaces on one line.",
			stdin:    "3\n1 2 3\n3\n4 5 6\n",
			expected: "4 13 28 27 18",
		},
		{
			name: "59 factors a number no trial division would reach",
			requirement: "Write a command-line program that factors the integer given as " +
				"its first command-line argument into primes and prints them in " +
				"ascending order separated by spaces on one line, repeating a " +
				"prime as often as it divides. The number is too large for trial " +
				"division to finish, so use a Miller-Rabin test and Pollard's " +
				"rho, and do the modular multiplication so that it cannot " +
				"overflow.",
			arguments: []string{"1000000016000000063"},
			expected:  "1000000007 1000000009",
		},
		{
			name: "60 solves congruences that share factors",
			requirement: "Write a command-line program that reads one congruence per line " +
				"as 'r m', meaning x is congruent to r modulo m. The moduli are " +
				"not guaranteed coprime. Print the smallest non-negative solution " +
				"and the modulus of the combined congruence separated by a space, " +
				"or the word none if the congruences contradict each other. Print " +
				"nothing else.",
			stdin:    "2 3\n3 5\n2 7\n",
			expected: "23 105",
		},
		{
			name: "61 exposes a generic balanced set across packages",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. One package must be pure. It defines a self-balancing " +
				"binary search tree Tree[T cmp.Ordered] with methods Insert, " +
				"Contains and Len, a method Height, and a package-level function " +
				"InOrder[T cmp.Ordered](*Tree[T]) []T returning the elements in " +
				"ascending order. Inserting already-sorted values must not " +
				"degrade it into a list. A test in it asserts that after " +
				"inserting one thousand ascending values the height stays below " +
				"twenty and InOrder is sorted. The command reads its command-line " +
				"arguments as integers, inserts them and prints them in ascending " +
				"order separated by spaces on one line.",
			arguments:    []string{"5", "3", "8", "1", "4"},
			expected:     "1 3 4 5 8",
			minPackages:  2,
			mustAppear:   []string{"type Tree[", "func InOrder[", ".InOrder("},
			purePackages: 1,
			pureSymbols:  []string{"InOrder"},
		},
		{
			name: "62 composes stream transformations through io.Reader",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. One package exports Upper(io.Reader) io.Reader and " +
				"Numbered(io.Reader) io.Reader. Upper upper-cases the bytes that " +
				"pass through it and Numbered prefixes each line with its " +
				"one-based number and a tab. Both must be true streaming " +
				"decorators: they may not read their whole input before " +
				"returning, and they may not import fmt, os or bufio. The command " +
				"copies standard input through Numbered and then Upper to " +
				"standard output with io.Copy.",
			stdin:        "alpha\nbeta\n",
			expected:     "1\tALPHA\n2\tBETA",
			minPackages:  2,
			mustAppear:   []string{"func Upper(", "func Numbered(", "io.Reader", "io.Copy("},
			purePackages: 1,
			pureSymbols:  []string{"Upper", "Numbered"},
		},
		{
			name: "63 decides what to do from the type of an error",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. One package defines a sentinel ErrMissing, a struct " +
				"type RangeError holding Field, Min and Max that implements " +
				"error, and WrapField(field string, err error) error whose result " +
				"keeps the cause reachable through errors.Unwrap. The command " +
				"reads one 'field=value' per line, where name must be non-empty " +
				"and age must be an integer from 1 to 150. For each line print " +
				"'ok field', or 'missing field' when the value is empty, or " +
				"'range field min max' when it is out of range. Decide which " +
				"message to print only with errors.Is and errors.As — never by " +
				"comparing error text.",
			stdin:       "name=ann\nname=\nage=200\nage=30\n",
			expected:    "ok name\nmissing name\nrange age 1 150\nok age",
			minPackages: 2,
			mustAppear:  []string{"ErrMissing", "type RangeError", "errors.Is(", "errors.As("},
		},
		{
			name: "64 fills typed configuration by reflection",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. One package exports Load(values map[string]string, " +
				"target any) error, which fills the exported fields of the struct " +
				"behind target from the map using each field's `conf` struct tag, " +
				"supporting string, int, bool and time.Duration, and returns an " +
				"error naming the field for anything it cannot parse and an error " +
				"when target is not a pointer to a struct. The command reads " +
				"'key=value' lines into a map, loads them into a struct whose " +
				"fields are Host string, Port int, Debug bool and Timeout " +
				"time.Duration tagged host, port, debug and timeout, and prints " +
				"the four values separated by spaces on one line.",
			stdin:       "host=localhost\nport=8080\ndebug=true\ntimeout=1500ms\n",
			expected:    "localhost 8080 true 1.5s",
			minPackages: 2,
			mustAppear:  []string{"reflect.", "Tag.Get("},
		},
		{
			name: "65 encodes and decodes its own binary format",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. One package defines Record with ID uint32, Name " +
				"string and Score int32, Marshal([]Record) []byte and " +
				"Unmarshal([]byte) ([]Record, error). The encoding is " +
				"little-endian throughout: a four-byte record count, then per " +
				"record the four-byte id, a two-byte name length followed by that " +
				"many bytes, and the four-byte score. Unmarshal must reject " +
				"truncated input with an error rather than panicking. A test " +
				"round-trips records and asserts that truncating the bytes " +
				"produces an error. The command reads 'id name score' lines, " +
				"marshals them, unmarshals the bytes back, prints each decoded " +
				"record as 'id name score' on its own line and then the number of " +
				"encoded bytes on the last line.",
			stdin:       "1 ann 10\n2 bob 20\n",
			expected:    "1 ann 10\n2 bob 20\n30",
			minPackages: 2,
			mustAppear:  []string{"encoding/binary", "func Marshal(", "func Unmarshal("},
		},
		{
			name: "66 hashes its input and verifies the digest",
			requirement: "Write a command-line program that reads all of standard input, " +
				"prints its SHA-256 digest as lowercase hexadecimal on one line, " +
				"then hashes the same bytes again and prints ok if the two " +
				"digests are identical or mismatch if they are not. Print nothing " +
				"else.",
			stdin: "hello",
			expected: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362" +
				"938b9824\nok",
		},
		{
			name: "67 signs a message and rejects a forgery",
			requirement: "Write a command-line program that takes a key as its first " +
				"command-line argument and a message as its second. Print the " +
				"HMAC-SHA256 of the message under that key as lowercase " +
				"hexadecimal on one line. Then verify that signature against the " +
				"original message with a constant-time comparison and print ok, " +
				"and verify the same signature against the message with its last " +
				"byte changed and print rejected. Print nothing else.",
			arguments: []string{"secret", "payload"},
			expected: "b82fcb791acec57859b989b430a826488ce2e479fdf92326bd0a2e83" +
				"75a42ba4\nok\nrejected",
			mustAppear: []string{"hmac.Equal("},
		},
		{
			name: "68 compresses, decompresses, and proves it lost nothing",
			requirement: "Write a command-line program that reads all of standard input, " +
				"compresses it with compress/flate at best compression, " +
				"decompresses the result, and prints the decompressed text on one " +
				"line, then identical or differs on the next, then smaller or " +
				"larger comparing the compressed size against the original. Print " +
				"nothing else.",
			stdin:    strings.Repeat("ab", 25) + "\n",
			expected: strings.Repeat("ab", 25) + "\nidentical\nsmaller",
		},
		{
			name: "69 spreads work across goroutines and keeps the order",
			requirement: "Write a command-line program that reads one integer per line, " +
				"computes for each the sum of its proper divisors using a pool of " +
				"four goroutines fed by one channel, and prints one line per " +
				"input in the order the inputs were given as 'n sum'. Every " +
				"goroutine must be waited for and every channel closed. Print " +
				"nothing else.",
			stdin:      "12\n28\n7\n",
			expected:   "12 16\n28 28\n7 1",
			mustAppear: []string{"sync.WaitGroup", "go func("},
		},
		{
			name: "70 stops work when its context is cancelled",
			requirement: "Write a command-line program that takes a duration as its first " +
				"command-line argument, starts a goroutine that would count " +
				"forever, and stops it with a context whose timeout is that " +
				"duration. Wait for the goroutine to return, then print cancelled " +
				"on one line and the context's error text on the next. The " +
				"program must exit well within twice the duration and must leave " +
				"no goroutine running. Print nothing else.",
			arguments:  []string{"50ms"},
			expected:   "cancelled\ncontext deadline exceeded",
			mustAppear: []string{"context.WithTimeout(", "ctx.Done()"},
		},
		{
			name: "71 fans work out across channels and back in",
			requirement: "Write a command-line program that reads one integer per line and " +
				"sends them down a single channel, squares them in three " +
				"concurrent stages all reading from that channel, merges the " +
				"results into one channel, and prints the total of the squares on " +
				"one line, then how many values were merged, then the word closed " +
				"once every channel has been drained and closed. Print nothing " +
				"else.",
			stdin:    "1\n2\n3\n4\n5\n",
			expected: "55\n5\nclosed",
		},
		{
			name: "72 keeps a counter exact under contention",
			requirement: "Write a command-line program that takes a number of goroutines " +
				"as its first command-line argument and a number of increments " +
				"per goroutine as its second. Have every goroutine increment one " +
				"counter guarded by a mutex and a second counter through " +
				"sync/atomic, wait for all of them, and print the two totals " +
				"separated by a space on one line. There must be no " +
				"unsynchronised access to either counter: the program has to be " +
				"correct under the race detector.",
			arguments:  []string{"8", "10000"},
			expected:   "80000 80000",
			mustAppear: []string{"sync.Mutex", "atomic."},
		},
		{
			name: "73 bounds what runs at once and proves the bound held",
			requirement: "Write a command-line program that takes a concurrency limit as " +
				"its first command-line argument and a number of jobs as its " +
				"second. Each job takes about five milliseconds. Run them " +
				"concurrently but never more than the limit at once, recording " +
				"under a mutex the greatest number in flight at any moment, and " +
				"print the number of jobs completed and that observed peak " +
				"separated by a space. The peak may never exceed the limit. Print " +
				"nothing else.",
			arguments: []string{"3", "50"},
			expected:  "50 3",
		},
		{
			name: "74 retries with backoff against a clock it controls",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. One package exports Do(attempts int, sleep " +
				"func(time.Duration), work func(attempt int) error) error, which " +
				"calls work with attempt numbers starting at one until it returns " +
				"nil or the attempts are used up, calling sleep between attempts " +
				"with a delay that starts at ten milliseconds and doubles. It " +
				"must not sleep after the final attempt or after a success. " +
				"Sleeping is injected rather than performed so the behaviour can " +
				"be tested without waiting. A test asserts the recorded delays " +
				"are 10ms, 20ms and 40ms for four failing attempts and that " +
				"nothing is slept after a success. The command takes the attempt " +
				"number that succeeds as its first command-line argument, records " +
				"the delays instead of sleeping, prints each recorded delay on " +
				"its own line and then the attempt that succeeded.",
			arguments:   []string{"3"},
			expected:    "10ms\n20ms\n3",
			minPackages: 2,
			mustAppear:  []string{"func Do("},
		},
		{
			name: "75 serves an LRU cache safely from many goroutines",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. One package exports a Cache with New(capacity int), " +
				"Get, Put and Len, evicting the least recently used entry, doing " +
				"every operation in constant time with a map and a linked list, " +
				"and safe for concurrent use. A test drives one cache from many " +
				"goroutines and asserts Len never exceeds the capacity. The " +
				"command takes the capacity as its first command-line argument, " +
				"has one hundred goroutines each put the keys a and b and get a, " +
				"waits for them, and prints the cache length. It then builds a " +
				"fresh cache of the same capacity, replays the 'PUT key value' " +
				"and 'GET key' commands read from standard input against it, and " +
				"prints each GET result on its own line, or -1 when the key is " +
				"absent.",
			arguments:   []string{"2"},
			stdin:       "PUT a 1\nPUT b 2\nGET a\nPUT c 3\nGET b\nGET c\n",
			expected:    "2\n1\n-1\n3",
			minPackages: 2,
		},
		{
			name: "76 writes a store to disk and reopens it",
			requirement: "Write a command-line program that creates a temporary directory " +
				"of its own, opens a key-value store backed by a file inside it, " +
				"and applies the 'set key value' and 'get key' commands read from " +
				"standard input, printing each get result on its own line or the " +
				"word absent. It then closes the store, prints reopened, opens " +
				"the same file again in a second store, replays only the get " +
				"commands against it and prints those results, and finally " +
				"removes the directory. Nothing may be written outside that " +
				"directory. Print nothing else.",
			stdin:    "set a 1\nset b 2\nget a\nget b\n",
			expected: "1\n2\nreopened\n1\n2",
		},
		{
			name: "77 recovers a store from its write-ahead log",
			requirement: "Write a command-line program that appends every mutation to a " +
				"write-ahead log file in a temporary directory of its own before " +
				"applying it to memory. It reads 'set key value' and 'delete key' " +
				"commands from standard input, then simulates a crash by " +
				"discarding the in-memory state without ever flushing a data " +
				"file, rebuilds the state by replaying the log, and prints the " +
				"recovered pairs as 'key value' one per line in ascending key " +
				"order followed by the number of log records replayed. Remove the " +
				"directory before exiting and print nothing else.",
			stdin:    "set a 1\nset b 2\nset a 3\ndelete b\n",
			expected: "a 3\n4",
		},
		{
			name: "78 stores records in a paged B-tree on disk",
			requirement: "Write a command-line program that keeps a B-tree of at least " +
				"order four in a file of fixed 4096-byte pages, in a temporary " +
				"directory of its own, behind a least-recently-used cache of " +
				"eight pages. Nodes must be read and written as whole pages " +
				"through that cache rather than held in memory as Go structures " +
				"for the life of the program. Insert the integers read one per " +
				"line from standard input, then print every key in ascending " +
				"order separated by spaces on one line, and on the next line the " +
				"keys a range scan from 3 to 7 inclusive returns, separated by " +
				"spaces. Remove the directory before exiting and print nothing " +
				"else.",
			stdin:    "5\n3\n9\n1\n7\n",
			expected: "1 3 5 7 9\n3 5 7",
		},
		{
			name: "79 commits and rolls back transactions",
			requirement: "Write a command-line program that holds a key-value store and " +
				"reads commands from standard input: 'begin' opens a transaction, " +
				"'set key value' writes, 'get key' prints the value visible now " +
				"or the word absent, 'commit' makes the transaction's writes " +
				"visible, and 'rollback' discards them. Writes inside an open " +
				"transaction must be visible to gets inside it and invisible " +
				"outside it until commit. Print nothing but the get results.",
			stdin: "set a 1\nbegin\nset a 2\nget a\nrollback\nget a\nbegin\n" +
				"set a 3\ncommit\nget a\n",
			expected: "2\n1\n3",
		},
		{
			name: "80 gives every reader a consistent snapshot",
			requirement: "Write a command-line program that keeps every version of every " +
				"key. Each 'set key value' creates a new version. 'snapshot' " +
				"fixes the current version and prints its number, counting from " +
				"one. 'get version key' reads the value as of that version, or " +
				"the word absent. 'gc' discards every version that no live " +
				"snapshot and no current read can see and prints how many " +
				"versions are retained. A write must never block a reader and " +
				"must never change what an existing snapshot sees. Print nothing " +
				"else.",
			stdin:    "set a 1\nsnapshot\nset a 2\nsnapshot\nset a 3\nget 1 a\nget 2 a\ngc\n",
			expected: "1\n2\n1\n2\n3",
		},
		{
			name: "81 indexes documents and answers boolean queries",
			requirement: "Write a command-line program that reads a count N, then N " +
				"documents one per line numbered from one, then a line holding a " +
				"single question mark, then one query per line. A query is terms " +
				"joined by AND, OR and AND NOT, evaluated left to right. Build an " +
				"inverted index and answer each query from it, printing the " +
				"matching document numbers in ascending order separated by spaces " +
				"on one line, or the word none. Print nothing else.",
			stdin: "3\nthe quick brown fox\nthe lazy dog\nquick dog runs\n?\n" +
				"quick AND dog\nfox OR dog\nquick AND NOT fox\n",
			expected: "3\n1 2 3\n3",
		},
		{
			name: "82 ranks documents by how much a term distinguishes them",
			requirement: "Write a command-line program that reads a count N, then N " +
				"documents one per line numbered from one, then a line holding a " +
				"single question mark, then one query per line. Score each " +
				"document as the sum over the query's terms of the term's count " +
				"in that document multiplied by the natural logarithm of N " +
				"divided by the number of documents holding the term. Print the " +
				"numbers of the documents with a positive score in descending " +
				"score, ties in ascending document order, separated by spaces on " +
				"one line, or the word none. Print nothing else.",
			stdin:    "3\ncat cat dog\ncat\ndog dog dog cat\n?\ndog\ncat\n",
			expected: "3 1\nnone",
		},
		{
			name: "83 compiles a template language and renders it",
			requirement: "Write a command-line program that reads a template, then a line " +
				"holding exactly three hyphens, then data as 'key=value' lines " +
				"where a key repeated more than once forms a list. Write the " +
				"template engine yourself and do not use text/template: parse the " +
				"template once into a tree and then render it. {{name}} " +
				"substitutes a value, {{#key}}...{{/key}} repeats its body once " +
				"per list item with {{key}} bound to the item, and " +
				"{{^key}}...{{/key}} renders its body only when the key is " +
				"absent. Print the rendered output and nothing else.",
			stdin: "Hello {{name}}!\n{{#item}}- {{item}}\n{{/item}}" +
				"{{^missing}}none{{/missing}}\n---\nname=ann\nitem=a\nitem=b\n",
			expected: "Hello ann!\n- a\n- b\nnone",
		},
		{
			name: "84 type-checks a program before it runs it",
			requirement: "Write a command-line program that reads one expression per line " +
				"in a small language with integer and boolean values, let " +
				"bindings, if, the arithmetic operators and the comparison <. For " +
				"each line print the type it infers, either int or bool, or a " +
				"message of the form 'type error: ' followed by what was wrong. " +
				"An if whose condition is not boolean reports 'type error: if " +
				"condition must be bool, got int'. Print nothing else.",
			stdin:    "let x = 1 in if x < 2 then x + 1 else 0\nif 1 then 2 else 3\n",
			expected: "int\ntype error: if condition must be bool, got int",
		},
		{
			name: "85 infers polymorphic types by unification",
			requirement: "Write a command-line program that reads one lambda-calculus term " +
				"per line, where a lambda is written with a backslash and let is " +
				"written 'let name = term in term', and infers its most general " +
				"type by Hindley-Milner unification with let-polymorphism. Name " +
				"the type variables a, b, c and so on in the order they first " +
				"appear in the printed type, write the function arrow as ' -> ' " +
				"associating to the right, and parenthesise only where the " +
				"meaning requires it. Print one type per line and nothing else.",
			stdin:    "\\x.x\nlet id = \\x.x in id id\n\\f.\\x.f (f x)\n",
			expected: "a -> a\na -> a\n(a -> a) -> a -> a",
		},
		{
			name: "86 collects the garbage in a heap it manages itself",
			requirement: "Write a command-line program that manages its own heap of named " +
				"objects and reads commands from standard input: 'alloc name' " +
				"allocates, 'link from to' makes one object reference another, " +
				"'root name' marks an object as a root, and 'collect' runs a " +
				"mark-and-sweep collection. On collect print the number of " +
				"objects still live and the number swept, separated by a space, " +
				"and on the next line the live objects' names in allocation order " +
				"separated by spaces. Print nothing else.",
			stdin:    "alloc a\nalloc b\nalloc c\nlink a b\nroot a\ncollect\n",
			expected: "2 1\na b",
		},
		{
			name: "87 compiles to a register machine and optimises it",
			requirement: "Write a command-line program that reads assignments of the form " +
				"'name = expression' and lines of the form 'print name', compiles " +
				"them to a register machine whose instructions are 'LOADI r n', " +
				"'ADD r r r', 'SUB r r r', 'MUL r r r', 'DIV r r r' and 'PRINT " +
				"r', then optimises the code by folding constants and removing " +
				"instructions whose results nothing reads, numbering the " +
				"registers it still needs from r0 upward in order of first " +
				"definition. Print the optimised instructions one per line, then " +
				"run them, which prints what PRINT produces. Print nothing else.",
			stdin:    "a = 2 + 3\nb = a * 0\nc = a + b\nprint c\n",
			expected: "LOADI r0 5\nPRINT r0\n5",
		},
		{
			name: "88 schedules dependent jobs and retries the ones that fail",
			requirement: "Write a command-line program that reads job definitions of the " +
				"form 'name' or 'name:dep,dep' until a line holding a single " +
				"question mark, then the names of the jobs that fail their first " +
				"attempt, one per line. Run every job after its dependencies, " +
				"taking the alphabetically first job whenever several are ready. " +
				"Print 'run name ok' or 'run name failed' as each job runs. A " +
				"failed job is retried once, after every job that was already " +
				"ready has run, and prints 'retry name ok'; jobs that depend on " +
				"it wait for the retry. Print nothing else.",
			stdin:    "a\nb:a\nc:a\nd:b,c\n?\nb\n",
			expected: "run a ok\nrun b failed\nrun c ok\nretry b ok\nrun d ok",
		},
		{
			name: "89 rebuilds an aggregate from its events and a snapshot",
			requirement: "Write a command-line program that reads 'deposit n', 'withdraw " +
				"n' and 'snapshot' commands from standard input, appending each " +
				"deposit and withdrawal to an event log and recording the running " +
				"balance whenever a snapshot is taken. Then print the balance " +
				"obtained by replaying every event from an empty aggregate, the " +
				"number of events that have to be replayed when starting from the " +
				"most recent snapshot instead, and the balance that rebuild " +
				"produces, one per line. The two balances must agree. Print " +
				"nothing else.",
			stdin:    "deposit 100\nwithdraw 30\nsnapshot\ndeposit 5\n",
			expected: "75\n1\n75",
		},
		{
			name: "90 validates documents against a schema it parses",
			requirement: "Write a command-line program that reads a schema, one field per " +
				"line as 'name:type' followed by space-separated constraints from " +
				"required, min= and max=, where type is string, int or 'list of " +
				"string'; then a line holding exactly three hyphens; then one " +
				"JSON object per line. For each object print ok, or the first " +
				"violation in schema field order as 'field: reason', where the " +
				"reasons are 'required', 'not a type', 'below minimum n' and " +
				"'above maximum n'. Print nothing else.",
			stdin: "name:string required\nage:int min=0 max=150\n" +
				"tags:list of string\n---\n" +
				`{"name":"ann","age":30,"tags":["a"]}` + "\n" +
				`{"age":200}` + "\n" +
				`{"name":"ann","age":200,"tags":[]}` + "\n",
			expected: "ok\nname: required\nage: above maximum 150",
		},
		{
			name: "91 diffs two documents and applies the patch it produced",
			requirement: "Write a command-line program that reads two JSON objects " +
				"separated by a line holding exactly three hyphens and computes " +
				"the smallest patch turning the first into the second. Print one " +
				"operation per line as 'add path value', 'remove path' or " +
				"'replace path value', where a path is slash-separated and rooted " +
				"at a slash, ordered by path ascending. Then apply the patch to " +
				"the first document and print identical if the result equals the " +
				"second document or differs if it does not. Print nothing else.",
			stdin: `{"a":1,"b":{"c":2},"d":3}` + "\n---\n" +
				`{"a":1,"b":{"c":4},"e":5}` + "\n",
			expected: "replace /b/c 4\nremove /d\nadd /e 5\nidentical",
		},
		{
			name: "92 converges two replicas whatever order they see",
			requirement: "Write a command-line program that reads operations of the form " +
				"'add element tag' and 'remove element tag' one per line and " +
				"applies all of them to two replicas of an observed-remove set — " +
				"the first replica in the order given and the second in the " +
				"reverse order — then merges the replicas. An element is present " +
				"when it has at least one added tag that has not been removed. " +
				"Print each replica's elements in ascending order separated by " +
				"spaces on its own line, then converged if the two agree or " +
				"diverged if they do not. Print nothing else.",
			stdin:    "add a t1\nadd b t2\nadd a t3\nremove a t1\n",
			expected: "a b\na b\nconverged",
		},
		{
			name: "93 commits a replicated log only on a quorum",
			requirement: "Write a command-line program that reads a replica count on the " +
				"first line and then one entry per line as 'name " +
				"acknowledgements'. An entry commits only when it and every entry " +
				"before it have been acknowledged by a strict majority of the " +
				"replicas. Print 'name committed' or 'name uncommitted' for each " +
				"entry in order, then the index of the last committed entry " +
				"counting from one, or zero if none committed. Print nothing " +
				"else.",
			stdin:    "5\nx 3\ny 2\nz 5\n",
			expected: "x committed\ny uncommitted\nz uncommitted\n1",
		},
		{
			name: "94 moves only the keys it has to when a node leaves",
			requirement: "Write a command-line program that reads a node count, then that " +
				"many node names one per line, then a key count, then a line " +
				"'remove name'. Place the keys key-0 up to key-count-minus-one on " +
				"a consistent hash ring with virtual nodes, record where each " +
				"lands, remove the named node, and place them again. Print the " +
				"word equal if the set of keys that changed node is exactly the " +
				"set that had been assigned to the removed node, or unequal, and " +
				"on the next line the number of keys that moved without having " +
				"been on the removed node. Print nothing else.",
			stdin:    "4\nred\ngreen\nblue\nyellow\n1000\nremove blue\n",
			expected: "equal\n0",
		},
		{
			name: "95 runs a language with modules and imports",
			requirement: "Write a command-line program that reads a program made of " +
				"modules introduced by 'module name', each holding function " +
				"definitions of the form 'fun name(args) = expression' where a " +
				"definition marked export is visible to other modules and one " +
				"that is not is private to its own. A module may import another " +
				"by name and then call its exported functions as module.name. " +
				"Evaluate the module called main, supporting recursion, if, " +
				"comparison and arithmetic, and print each of its print " +
				"statements' values one per line. Reject a call to a private " +
				"function with an error rather than resolving it. Print nothing " +
				"else.",
			stdin: "module math\nexport fun sq(x) = x * x\nmodule main\n" +
				"import math\nfun fact(n) = if n = 0 then 1 else n * fact(n - 1)\n" +
				"print math.sq(7)\nprint fact(5)\n",
			expected: "49\n120",
		},
		{
			name: "96 runs a bytecode machine that collects its own garbage",
			requirement: "Write a command-line program that compiles the program read from " +
				"standard input to bytecode and runs it on a virtual machine with " +
				"its own heap. The language has integers, let bindings, " +
				"assignment, function values that capture their environment, and " +
				"calls. The machine allocates closures and environments on its " +
				"heap and the command 'collect' runs a mark-and-sweep collection " +
				"over it from the stack and the globals. Print each print " +
				"statement's value on its own line, then collected if the " +
				"collection reclaimed at least one unreachable object or nothing " +
				"to collect if it did not. Print nothing else.",
			stdin: "fun makeCounter() = { let n = 0; fun() = { n = n + 1; n } }\n" +
				"let c = makeCounter()\nprint c()\nprint c()\nprint c()\ncollect\n",
			expected: "1\n2\n3\ncollected",
		},
		{
			name: "97 type-checks a source language and compiles it to that machine",
			requirement: "Write a command-line program that reads a program whose " +
				"functions carry declared parameter and result types, checks it — " +
				"rejecting any call whose argument types or arity do not match, " +
				"and any branch whose arms disagree — then compiles the checked " +
				"program to stack bytecode and runs it. The types are int and " +
				"bool. Print the declared type of main on one line and the value " +
				"it evaluates to on the next. Print nothing else.",
			stdin: "fun fib(n: int): int = if n < 2 then n else " +
				"fib(n - 1) + fib(n - 2)\nmain: int = fib(20)\n",
			expected: "int\n6765",
		},
		{
			name: "98 plans a query over its indexes and explains the plan",
			requirement: "Write a command-line program that reads table definitions of the " +
				"form 'table name(col,col)', index definitions of the form 'index " +
				"table.column', rows of the form 'insert table values', then a " +
				"line holding a single question mark, then one query joining two " +
				"tables with an equality condition and filtering one of them on " +
				"an indexed column. Choose a plan that uses an index wherever one " +
				"exists, print each plan step in execution order as 'step target' " +
				"where step is index-lookup, index-scan or table-scan, then a " +
				"line holding exactly three hyphens, then the result rows in the " +
				"order the plan produces them, values separated by spaces. Print " +
				"nothing else.",
			stdin: "table users(id,name)\ntable orders(user,total)\n" +
				"index users.id\nindex orders.user\n" +
				"insert users 1 bob\ninsert users 2 ann\n" +
				"insert orders 2 30\ninsert orders 1 5\ninsert orders 2 12\n?\n" +
				"select users.name, orders.total from users join orders on " +
				"orders.user = users.id where users.id = 2\n",
			expected: "index-lookup users.id\nindex-lookup orders.user\n---\n" +
				"ann 30\nann 12",
		},
		{
			name: "99 parses, validates, transforms and reports across packages",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. csvread turns CSV with a header row into records, " +
				"schema rejects a record whose amount is not an integer, " +
				"transform sums the amounts per name, and report renders the " +
				"result. Each of the four packages must be pure — no printing, no " +
				"reading, no clock — and the command does all of the input and " +
				"output. Print the number of data rows read as 'rows n', the " +
				"number that passed the schema as 'valid n', the number rejected " +
				"as 'rejected n', and then one 'name total' line per name in " +
				"ascending order. Print nothing else.",
			stdin:        "name,amount\nann,10\nbob,x\nann,5\n",
			expected:     "rows 3\nvalid 2\nrejected 1\nann 15",
			minPackages:  5,
			purePackages: 4,
		},
		{
			name: "100 assembles a checked toolchain across six packages",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. The packages form a one-directional pipeline — lexer " +
				"to parser to checker to optimiser to codegen to vm — and no " +
				"package may import one later in that order. Every package but vm " +
				"must be pure. The language has integer literals, arithmetic with " +
				"the usual precedence, named function definitions and calls, and " +
				"print statements. The command reads a program from standard " +
				"input, runs it through the whole pipeline, and prints each print " +
				"statement's value on its own line and nothing else.",
			stdin:        "fun add(a, b) = a + b\nprint add(2, 3) * 7\n",
			expected:     "35",
			minPackages:  7,
			purePackages: 5,
		},
		// From here the programs are HTTP services, and every one of them both
		// serves and calls itself.
		//
		// The harness runs a built executable, waits for it to exit, and compares
		// one exact stdout; the adversarial probe then fails anything still
		// running after ten seconds. A program that bound a port and blocked
		// would hang the suite and prove nothing, so each of these starts a real
		// net/http server on a port the operating system chooses, drives it with
		// a real client in the same process, prints a fixed transcript, and shuts
		// down. The server, the routing, the headers and the status codes are
		// genuine; only the lifetime is bounded.
		{
			name: "101 serves one route over HTTP and calls it",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving GET / " +
				"with the status 200 and the body hello. In the same process it " +
				"then requests that route with an http.Client, prints the status " +
				"code and the body separated by a space on one line, shuts the " +
				"server down and exits. Nothing may be written to standard error.",
			expected:   "200 hello",
			mustAppear: []string{"net/http", "http.Client"},
		},
		{
			name: "102 routes several paths and refuses the rest",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, with an " +
				"http.ServeMux serving /a with the body alpha, /b with the body " +
				"beta, and every other path with the status 404 and the body not " +
				"found. It then requests /a, /b and /c in that order, printing " +
				"the status code and the body separated by a space for each, one " +
				"per line, shuts the server down and exits. Nothing may be " +
				"written to standard error.",
			expected: "200 alpha\n200 beta\n404 not found",
		},
		{
			name: "103 distinguishes methods and reports what it allows",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving /items " +
				"so that GET returns 200 and the body list, POST returns 201 and " +
				"the body created, and any other method returns 405 with an Allow " +
				"header naming the methods it does serve. It then sends GET, POST " +
				"and DELETE in that order, printing for the first two the status " +
				"and the body separated by a space and for the third the status " +
				"and the Allow header, one per line, shuts the server down and " +
				"exits. Nothing may be written to standard error.",
			expected: "200 list\n201 created\n405 GET, POST",
		},
		{
			name: "104 reads query parameters and applies its defaults",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving " +
				"/search: it requires a q parameter and refuses a request without " +
				"one with the status 400 and the body 'q required', and it " +
				"accepts an optional limit parameter that defaults to 10, " +
				"answering with q and limit separated by a space. It then " +
				"requests /search with q=go and limit=2, with q=go alone, and " +
				"with nothing, printing the status and the body separated by a " +
				"space for each, one per line, shuts the server down and exits. " +
				"Nothing may be written to standard error.",
			expected: "200 go 2\n200 go 10\n400 q required",
		},
		{
			name: "105 matches parameters inside the path",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, whose " +
				"http.ServeMux registers the patterns 'GET /items/{id}' answering " +
				"with 'item' and the id, and 'GET /items/{id}/tags/{tag}' " +
				"answering with 'item' and the id and 'tag' and the tag, all " +
				"separated by spaces, with every other path answered 404 and the " +
				"body not found. Read the parameters with the request's PathValue " +
				"rather than by splitting the path yourself. It then requests " +
				"/items/7, /items/7/tags/red and /items, printing the status and " +
				"the body separated by a space for each, one per line, shuts the " +
				"server down and exits. Nothing may be written to standard error.",
			expected:   "200 item 7\n200 item 7 tag red\n404 not found",
			mustAppear: []string{"PathValue("},
		},
		{
			name: "106 reads the request's headers and sets its own",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, whose handler " +
				"echoes the request's X-Request-Id header as the body, sets an " +
				"X-Server header to codeflux, and declares a plain-text UTF-8 " +
				"content type. It then sends one request carrying X-Request-Id: " +
				"abc123 and prints, separated by spaces on one line, the status, " +
				"the body, the X-Server header and the Content-Type header, shuts " +
				"the server down and exits. Nothing may be written to standard " +
				"error.",
			expected: "200 abc123 codeflux text/plain; charset=utf-8",
		},
		{
			name: "107 exchanges JSON in both directions",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving POST " +
				"/sum: it decodes a JSON object with a values field holding an " +
				"array of integers and answers with a JSON object holding their " +
				"sum in a total field, and it answers a body that is not valid " +
				"JSON with the status 400 and a JSON object whose error field is " +
				"'invalid json'. It then posts the values 1, 2 and 3 and prints " +
				"the status and the decoded total separated by a space, posts a " +
				"malformed body and prints the status and the decoded error, " +
				"shuts the server down and exits. Nothing may be written to " +
				"standard error.",
			expected: "200 6\n400 invalid json",
		},
		{
			name: "108 answers a validation failure with a typed envelope",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving POST " +
				"/users: a request whose JSON body has no name is refused with " +
				"the status 422 and a JSON object whose error field is 'invalid' " +
				"and whose fields array names every field at fault, and a request " +
				"with a name is accepted with the status 201 and a JSON object " +
				"holding the new id, counting from one. It then posts a body with " +
				"no name and prints the status, the error and the single named " +
				"field separated by spaces, posts a valid body and prints the " +
				"status and the id, shuts the server down and exits. Nothing may " +
				"be written to standard error.",
			expected: "422 invalid name\n201 1",
		},
		{
			name: "109 wraps its handlers in middleware that logs them",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving GET " +
				"/ping with the body pong, wrapped in one middleware that gives " +
				"each request an increasing identifier counting from one and, " +
				"once the handler has returned, prints the method, the path, the " +
				"status it actually answered with and that identifier separated " +
				"by spaces. Capturing the status means wrapping the " +
				"ResponseWriter, not guessing. The program then makes two " +
				"requests in sequence, printing after each one the status and the " +
				"body separated by a space, shuts the server down and exits. " +
				"Nothing may be written to standard error.",
			expected: "GET /ping 200 1\n200 pong\nGET /ping 200 2\n200 pong",
		},
		{
			name: "110 proves the order its middleware runs in",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving one " +
				"route wrapped in three middlewares named A, B and C, composed so " +
				"that A is outermost and C innermost. Each prints 'enter' and its " +
				"name before calling the next and 'exit' and its name after it " +
				"returns, and the handler prints handle. The program makes one " +
				"request, then prints the status and the body ok separated by a " +
				"space, shuts the server down and exits. Nothing may be written " +
				"to standard error.",
			expected: "enter A\nenter B\nenter C\nhandle\nexit C\nexit B\n" +
				"exit A\n200 ok",
		},
		{
			name: "111 survives a handler that panics",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving /boom " +
				"with a handler that panics and /ok with the body ok, both " +
				"wrapped in a middleware that recovers from a panic and answers " +
				"500 with the body 'internal error'. The recovery must happen in " +
				"the middleware, so the server never logs the panic; give the " +
				"server an error log that discards, and write nothing at all to " +
				"standard error. The program requests /boom and then /ok, " +
				"printing the status and the body separated by a space for each, " +
				"shuts the server down and exits.",
			expected: "500 internal error\n200 ok",
		},
		{
			name: "112 carries a value through the request context",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, wrapped in a " +
				"middleware that reads an X-User header and puts the name into " +
				"the request's context under an unexported key type of its own, " +
				"never a string, so no other package could collide with it. The " +
				"handler reads the name back out of the context and answers with " +
				"it. The program makes one request carrying X-User: ann, prints " +
				"the status and the body separated by a space, shuts the server " +
				"down and exits. Nothing may be written to standard error.",
			expected:   "200 ann",
			mustAppear: []string{"context.WithValue(", "r.Context()"},
		},
		{
			name: "113 gives up on a handler that takes too long",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving /slow " +
				"with a handler that sleeps for two hundred milliseconds and " +
				"/fast with a handler that answers the body fast at once, both " +
				"behind a timeout of fifty milliseconds that answers a request " +
				"that overruns with the status 503 and the body timeout. The " +
				"program requests /slow and then /fast, printing the status and " +
				"the body separated by a space for each, shuts the server down " +
				"and exits. Nothing may be written to standard error.",
			expected: "503 timeout\n200 fast",
		},
		{
			name: "114 finishes the work in flight before it shuts down",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving a " +
				"handler that takes one hundred milliseconds and answers the body " +
				"done. It issues that request from a goroutine, waits until the " +
				"handler has certainly begun, and calls the server's Shutdown " +
				"while the request is still in flight. The in-flight request must " +
				"still complete: print its status and body separated by a space. " +
				"Then print 'shutdown complete' once Shutdown has returned, make " +
				"one more request, and print refused because it cannot be served. " +
				"Nothing may be written to standard error.",
			expected: "200 done\nshutdown complete\nrefused",
		},
		{
			name: "115 sets a cookie and reads it back",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving " +
				"/login, which sets an HttpOnly session cookie whose value is " +
				"abc, and /me, which answers with the name ann when that cookie " +
				"is present and with the status 401 and the body anonymous when " +
				"it is not. Using a client with a cookie jar it requests /login " +
				"and prints the status and the cookie as name=value separated by " +
				"a space, then requests /me and prints the status and the body, " +
				"then requests /me with a client that has no jar and prints the " +
				"status and the body, shuts the server down and exits. Nothing " +
				"may be written to standard error.",
			expected: "200 session=abc\n200 ann\n401 anonymous",
		},
		{
			name: "116 follows a redirect, and then declines to",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving /old " +
				"with a 302 redirect to /new and /new with the body new. It " +
				"requests /old with an ordinary client, which follows the " +
				"redirect, and prints the status and the body separated by a " +
				"space; then requests /old with a client whose redirect policy " +
				"returns the last response instead of following it, and prints " +
				"that status and the Location header; then shuts the server down " +
				"and exits. Nothing may be written to standard error.",
			expected: "200 new\n302 /new",
		},
		{
			name: "117 challenges for credentials and checks them safely",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving /me " +
				"behind basic authentication for the user ann with the password " +
				"opensesame. A request without credentials or with the wrong ones " +
				"is answered 401 with a WWW-Authenticate header naming the Basic " +
				"scheme and the realm codeflux; a correct one is answered with " +
				"the user's name. Compare the credentials in constant time. The " +
				"program requests /me with no credentials, then with the wrong " +
				"password, printing the status and the WWW-Authenticate header " +
				"for each, then with the right ones, printing the status and the " +
				"body, then shuts the server down and exits. Nothing may be " +
				"written to standard error.",
			expected: "401 Basic realm=\"codeflux\"\n401 Basic realm=\"codeflux\"\n" +
				"200 ann",
			mustAppear: []string{"subtle.ConstantTimeCompare("},
		},
		{
			name: "118 authorises a bearer token by its scope",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving /notes " +
				"behind bearer-token authorisation, where the token t-read " +
				"carries only the read scope. A GET with it is answered with the " +
				"body read; a POST with it is refused with the status 403 and the " +
				"body 'write required'; a request with no Authorization header is " +
				"refused with the status 401 and the body 'missing token'. The " +
				"program makes those three requests in that order, printing the " +
				"status and the body separated by a space for each, shuts the " +
				"server down and exits. Nothing may be written to standard error.",
			expected: "200 read\n403 write required\n401 missing token",
		},
		{
			name: "119 answers a cross-origin preflight",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving /items " +
				"for POST and answering a CORS preflight for the origin " +
				"https://app.example: an OPTIONS request carrying that origin and " +
				"asking for POST is answered 204 with the allowed methods and the " +
				"allowed origin, and the POST that follows carries the allowed " +
				"origin too. The program sends the preflight and prints the " +
				"status, the Access-Control-Allow-Methods header and the " +
				"Access-Control-Allow-Origin header separated by spaces, then " +
				"sends the POST and prints its status and allowed origin, then " +
				"shuts the server down and exits. Nothing may be written to " +
				"standard error.",
			expected: "204 POST https://app.example\n200 https://app.example",
		},
		{
			name: "120 chooses a representation the client will accept",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving /user, " +
				"which answers a request accepting application/json with a JSON " +
				"object holding the name ann, a request accepting text/plain with " +
				"just ann, and anything else with the status 406 and the body " +
				"'not acceptable'. The program sends the three Accept headers in " +
				"that order, printing the status and the body separated by a " +
				"space for each, shuts the server down and exits. Nothing may be " +
				"written to standard error.",
			expected: "200 {\"name\":\"ann\"}\n200 ann\n406 not acceptable",
		},
		{
			name: "121 compresses a response when the client asks for it",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving a body " +
				"of the letters ab repeated five hundred times, gzipped with the " +
				"Content-Encoding header set when the request's Accept-Encoding " +
				"allows it and sent unchanged when it does not. The program " +
				"requests it once with a transport that does not decompress for " +
				"it, so it sees the encoding and decodes the body itself, and " +
				"once asking for identity, printing for each the Content-Encoding " +
				"or the word identity when there is none, the status, and the " +
				"length of the decoded body, separated by spaces on one line. " +
				"Then it shuts the server down and exits. Nothing may be written " +
				"to standard error.",
			expected: "gzip 200 1000\nidentity 200 1000",
		},
		{
			name: "122 answers an unchanged resource with 304",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving /doc " +
				"with the body hello and a strong ETag of v1, and answering a " +
				"request whose If-None-Match matches that ETag with the status " +
				"304 and no body at all. The program requests /doc and prints the " +
				"status, the ETag header and the number of bytes in the body " +
				"separated by spaces, then requests it again with If-None-Match " +
				"set to what it was given and prints the status and the number of " +
				"bytes, then shuts the server down and exits. Nothing may be " +
				"written to standard error.",
			expected: "200 \"v1\" 5\n304 0",
		},
		{
			name: "123 answers on the modification time",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving /doc " +
				"with a Last-Modified of the first of January 2026 at midnight " +
				"UTC, answering 304 when the request's If-Modified-Since is that " +
				"instant or later and 200 when it is earlier. The program " +
				"requests /doc and prints the status and the Last-Modified header " +
				"separated by a space, then requests it with If-Modified-Since " +
				"set to that same instant and prints the status, then with an " +
				"instant one day earlier and prints the status, then shuts the " +
				"server down and exits. Nothing may be written to standard error.",
			expected: "200 Thu, 01 Jan 2026 00:00:00 GMT\n304\n200",
		},
		{
			name: "124 serves part of a resource on request",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving /doc " +
				"with the ten bytes 0 through 9, honouring a byte-range request " +
				"with the status 206, the requested bytes and a Content-Range " +
				"header, and refusing a range beyond the end with the status 416. " +
				"The program requests bytes 2 to 5 and prints the status, the " +
				"body and the Content-Range header separated by spaces, then " +
				"requests bytes 20 to 30 and prints the status, then shuts the " +
				"server down and exits. Nothing may be written to standard error.",
			expected: "206 2345 bytes 2-5/10\n416",
		},
		{
			name: "125 answers a HEAD with the headers and no body",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving /doc " +
				"with the body hello and an explicit Content-Length, so that a " +
				"HEAD request is answered with the same headers and no body at " +
				"all. The program sends a GET and then a HEAD, printing for each " +
				"the status, the number of bytes it actually received, and the " +
				"Content-Length header, separated by spaces on one line, then " +
				"shuts the server down and exits. Nothing may be written to " +
				"standard error.",
			expected: "200 5 5\n200 0 5",
		},
		{
			name: "126 serves files without letting a path escape",
			requirement: "Write a command-line program that creates a temporary directory " +
				"of its own holding a file a.txt whose contents are alpha, starts " +
				"a net/http server on 127.0.0.1 on a port the operating system " +
				"chooses serving that directory as a file system, and requests " +
				"/a.txt, then /missing.txt, then a path that tries to climb out " +
				"of the directory with an escaped ../ prefix. Print the status " +
				"and the body for the first and the status alone for the other " +
				"two, one per line, then remove the directory, shut the server " +
				"down and exit. Nothing outside that directory may be readable " +
				"through the server and nothing may be written to standard error.",
			expected: "200 alpha\n404\n404",
		},
		{
			name: "127 accepts a multipart upload",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving POST " +
				"/upload, which parses a multipart form holding a file field " +
				"called file and a text field called name and answers with the " +
				"uploaded file name, its size in bytes and the text field " +
				"separated by spaces. The program builds such a request itself " +
				"with multipart/writer, sending a file named note.txt whose " +
				"contents are alpha and a name of ann, prints the status and the " +
				"body separated by a space, shuts the server down and exits. " +
				"Nothing may be written to standard error.",
			expected: "200 note.txt 5 ann",
		},
		{
			name: "128 streams a response as it is produced",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, whose handler " +
				"writes three lines reading 'chunk' and the number, flushing " +
				"after each so the client can see it before the handler returns, " +
				"with about twenty milliseconds between them. The client reads " +
				"the response body incrementally and prints each line the moment " +
				"it arrives rather than after the response is complete, and then " +
				"prints the status on the last line. It then shuts the server " +
				"down and exits. Nothing may be written to standard error.",
			expected:   "chunk 1\nchunk 2\nchunk 3\n200",
			mustAppear: []string{"http.Flusher"},
		},
		{
			name: "129 delivers server-sent events",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving " +
				"/events as a server-sent event stream: the content type is " +
				"text/event-stream and it emits three events whose data is 1, 2 " +
				"and 3, each flushed as it is written and terminated by a blank " +
				"line. The client reads the stream, parsing the data lines rather " +
				"than assuming their byte offsets, prints 'event' and each value " +
				"as it arrives, closes the response and prints closed, then shuts " +
				"the server down and exits. Nothing may be written to standard " +
				"error.",
			expected: "event 1\nevent 2\nevent 3\nclosed",
		},
		{
			name: "130 refuses a body larger than it will hold",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving POST " +
				"/upload with the request body limited to 1024 bytes, answering " +
				"an acceptable body with the number of bytes it read and a body " +
				"over the limit with the status 413 and the body 'too large'. The " +
				"limit must be enforced while reading, not by trusting the " +
				"Content-Length header. The program posts ten bytes and then two " +
				"thousand, printing the status and the body separated by a space " +
				"for each, shuts the server down and exits. Nothing may be " +
				"written to standard error.",
			expected:   "200 10\n413 too large",
			mustAppear: []string{"http.MaxBytesReader("},
		},
		{
			name: "131 reports what is wrong with a submitted form",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving POST " +
				"/signup with a form-encoded body holding name and age. A name " +
				"that is empty is reported as 'name required' and an age that is " +
				"not a number as 'age not a number'; every fault is reported at " +
				"once, in ascending field order joined by a semicolon and a " +
				"space, with the status 400. A valid submission is answered with " +
				"the name and the age separated by a space. The program posts an " +
				"empty name with an age of abc and then a valid form, printing " +
				"the status and the body separated by a space for each, shuts the " +
				"server down and exits. Nothing may be written to standard error.",
			expected: "400 age not a number; name required\n200 ann 30",
		},
		{
			name: "132 implements create, read, update and delete",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving a JSON " +
				"resource at /items: POST creates and answers 201 with the new id " +
				"counting from one, GET /items/{id} answers with the item's name, " +
				"PUT /items/{id} replaces it and answers with the new name, GET " +
				"/items answers with how many items exist, DELETE /items/{id} " +
				"answers 204, and a GET for an id that is gone answers 404. The " +
				"program creates an item named ann, reads it, renames it to bob, " +
				"counts the collection, deletes it and reads it again, printing " +
				"the status and the body separated by a space for each and the " +
				"status alone where there is no body, one per line. It then shuts " +
				"the server down and exits. Nothing may be written to standard " +
				"error.",
			expected: "201 1\n200 ann\n200 bob\n200 1\n204\n404",
		},
		{
			name: "133 pages through a collection with a cursor",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, holding five " +
				"items with the ids 1 to 5 and serving GET /items with a limit " +
				"parameter and an optional cursor. A page answers with its ids " +
				"and the cursor for the next page, which is empty on the last " +
				"page; the cursor is the id after which the next page begins and " +
				"must not skip or repeat an item. The program reads every page " +
				"with a limit of two, following the cursor it is given until " +
				"there is none, and prints for each page the ids separated by " +
				"spaces followed by 'next=' and the cursor, one page per line. It " +
				"then shuts the server down and exits. Nothing may be written to " +
				"standard error.",
			expected: "1 2 next=2\n3 4 next=4\n5 next=",
		},
		{
			name: "134 replays its first answer for a repeated key",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving POST " +
				"/orders, which honours an Idempotency-Key header: the first " +
				"request with a given key creates an order and answers 201 with " +
				"its id, and a later request with the same key creates nothing " +
				"and answers 200 with the same id and a header marking it a " +
				"replay. It also serves GET /orders with the number of orders " +
				"that exist. The program posts with the key k1, posts again with " +
				"k1, posts with k2 and then counts, printing the status and the " +
				"id separated by a space for each post, adding the word replayed " +
				"where the header says so, and the count on the last line. It " +
				"then shuts the server down and exits. Nothing may be written to " +
				"standard error.",
			expected: "201 1\n200 1 replayed\n201 2\n2",
		},
		{
			name: "135 refuses an update built on a stale read",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving /doc, " +
				"whose GET answers with the current version as an ETag and whose " +
				"PUT requires an If-Match header: a PUT matching the current " +
				"version succeeds and answers with the new version, and a PUT " +
				"carrying a version that is no longer current is refused with the " +
				"status 409 and the body conflict. The program reads the " +
				"document, updates it with the version it was given, then tries " +
				"to update it again with that same now-stale version, printing " +
				"the status and the version or body separated by a space for " +
				"each, one per line. It then shuts the server down and exits. " +
				"Nothing may be written to standard error.",
			expected: "200 1\n200 2\n409 conflict",
		},
		{
			name: "136 sheds the requests past its rate limit",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving one " +
				"route behind a limit of three requests per one-second window, " +
				"refusing anything beyond it with the status 429 and a " +
				"Retry-After header holding the whole seconds until the window " +
				"resets. The limiter must be safe for concurrent use. The program " +
				"makes five requests in quick succession, printing the status for " +
				"each and, where the request was refused, the Retry-After header " +
				"after it, one per line. It then shuts the server down and exits. " +
				"Nothing may be written to standard error.",
			expected: "200\n200\n200\n429 1\n429 1",
		},
		{
			name: "137 counts correctly under concurrent requests",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving POST " +
				"/count, which increments one shared counter, and GET /count, " +
				"which answers with its value. The program issues one hundred " +
				"POSTs concurrently, waits for all of them, then reads the " +
				"counter and prints its value and the number of requests that " +
				"failed, separated by a space. The handler's state must be " +
				"correct under the race detector. It then shuts the server down " +
				"and exits. Nothing may be written to standard error.",
			expected: "100 0",
		},
		{
			name: "138 reuses one connection for many requests",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, counting every " +
				"new connection it accepts through the server's connection-state " +
				"hook. The program makes five requests in sequence with one " +
				"client, reading each body to completion and closing it so the " +
				"connection returns to the pool, then prints the number of " +
				"requests and the number of connections the server saw, separated " +
				"by a space. It then shuts the server down and exits. Nothing may " +
				"be written to standard error.",
			expected:   "5 1",
			mustAppear: []string{"ConnState"},
		},
		{
			name: "139 retries a dependency that is briefly failing",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving " +
				"/flaky, which answers 503 for its first two requests and then " +
				"200 with the body ok. The client retries a 503 up to five times " +
				"with a backoff that doubles, recording the delay rather than " +
				"sleeping it so the program does not wait, and gives up on any " +
				"other failure. Print the status of each attempt, with the body " +
				"after the one that succeeded, and then the number of attempts " +
				"followed by the word attempts. It then shuts the server down and " +
				"exits. Nothing may be written to standard error.",
			expected: "503\n503\n200 ok\n3 attempts",
		},
		{
			name: "140 opens a circuit and closes it again",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving a " +
				"route that fails for its first three requests and succeeds " +
				"afterwards, and calls it through a circuit breaker that opens " +
				"after three consecutive failures, refuses calls while open " +
				"without reaching the server at all, and after a short reset " +
				"window admits one trial call, closing on its success. Make six " +
				"calls: print fail, open or ok for each, then the breaker's " +
				"state, then the number of requests the server actually received. " +
				"It then shuts the server down and exits. Nothing may be written " +
				"to standard error.",
			expected: "fail\nfail\nfail\nopen\nopen\nok\nclosed\n4",
		},
		{
			name: "141 proxies a request to a service behind it",
			requirement: "Write a command-line program that starts two net/http servers on " +
				"127.0.0.1 on ports the operating system chooses: a backend " +
				"answering with the word backend and the path it was asked for, " +
				"and in front of it a reverse proxy from net/http/httputil " +
				"forwarding everything to the backend. The program requests /a " +
				"through the proxy, prints the status and the body separated by a " +
				"space, shuts both servers down and exits. Nothing may be written " +
				"to standard error.",
			expected: "200 backend /a",
		},
		{
			name: "142 forwards the caller's address and drops what it must",
			requirement: "Write a command-line program that starts a backend and a reverse " +
				"proxy in front of it, both on 127.0.0.1 on ports the operating " +
				"system chooses. The proxy adds an X-Forwarded-For header holding " +
				"the calling address's host and removes the hop-by-hop headers, " +
				"which may not be forwarded. The backend answers with 'xff=' and " +
				"the forwarded host, then 'connection=' and either present or " +
				"absent according to whether it received a Connection header. The " +
				"program sends one request through the proxy carrying a " +
				"Connection header, prints the status and the body separated by a " +
				"space, shuts both servers down and exits. Nothing may be written " +
				"to standard error.",
			expected: "200 xff=127.0.0.1 connection=absent",
		},
		{
			name: "143 signs a webhook that its receiver verifies",
			requirement: "Write a command-line program " +
				"that starts a receiver on 127.0.0.1 on a port the operating " +
				"system chooses, which accepts a POST only when its X-Signature " +
				"header is the HMAC-SHA256 of the exact request body under the " +
				"shared key s3cr3t, written as lowercase hexadecimal and compared " +
				"in constant time, answering 200 and verified, or 401 and 'bad " +
				"signature'. The sender posts the JSON body " +
				`{"id":1,"name":"ann"} ` + "and prints the signature it computed, " +
				"then the status and the body separated by a space; it then posts " +
				"a body altered by one byte under that same signature and prints " +
				"that status and body. It then shuts the server down and exits. " +
				"Nothing may be written to standard error.",
			expected: "abe548f4aa8ffce8cea22476db9c00a13dd6347c0ea6f651ffd4b62c1" +
				"0014291\n200 verified\n401 bad signature",
		},
		{
			name: "144 delivers from an outbox and suppresses the duplicates",
			requirement: "Write a command-line program that starts a receiver on 127.0.0.1 " +
				"on a port the operating system chooses, which accepts an event " +
				"only once per delivery identifier and answers a repeat as a " +
				"duplicate without storing it again. The sender holds three " +
				"events in an outbox, each with a delivery identifier that does " +
				"not change between attempts, sends them, and then sends exactly " +
				"the same batch a second time. Print 'delivered' and the count " +
				"sent, 'accepted' and the count the receiver stored, " +
				"'redelivered' and the count sent again, 'duplicates' and the " +
				"count it rejected as already seen, and finally the number of " +
				"distinct events the receiver holds, one per line. It then shuts " +
				"the server down and exits. Nothing may be written to standard " +
				"error.",
			expected: "delivered 3\naccepted 3\nredelivered 3\nduplicates 3\n3",
		},
		{
			name: "145 reports liveness and readiness as different questions",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving " +
				"/healthz, which answers 200 and ok whenever the process is " +
				"running, and /readyz, which answers 503 and 'db down' while its " +
				"declared dependency is unavailable and 200 and ready once it is " +
				"available. The program requests /healthz, then /readyz while the " +
				"dependency is still down, then marks it available and requests " +
				"/readyz again, printing the status and the body separated by a " +
				"space for each, one per line. It then shuts the server down and " +
				"exits. Nothing may be written to standard error.",
			expected: "200 ok\n503 db down\n200 ready",
		},
		{
			name: "146 logs every request as one structured record",
			requirement: "Write a command-line program that starts a net/http server on " +
				"127.0.0.1 on a port the operating system chooses, serving GET /a " +
				"with the status 200 and POST /b with the status 201, wrapped in " +
				"a middleware that writes one JSON object per request to standard " +
				"output holding method, path and status in that order and nothing " +
				"else. The program makes the two requests in that order and then " +
				"prints their two statuses separated by a space on the last line. " +
				"It then shuts the server down and exits. Nothing may be written " +
				"to standard error.",
			expected: "{\"method\":\"GET\",\"path\":\"/a\",\"status\":200}\n" +
				"{\"method\":\"POST\",\"path\":\"/b\",\"status\":201}\n200 201",
		},
		{
			name: "147 carries one trace across two services",
			requirement: "Write a command-line program that starts two net/http servers on " +
				"127.0.0.1 on ports the operating system chooses: a back service, " +
				"and a front service that calls it. The front service reads the " +
				"W3C traceparent header it is given, calls the back service with " +
				"a traceparent carrying the same trace identifier and a newly " +
				"generated span identifier, and each service prints its own name " +
				"and the trace identifier it saw. The program calls the front " +
				"service with the trace identifier " +
				"4bf92f3577b34da6a3ce929d0e0e4736 and the span identifier " +
				"00f067aa0ba902b7, then prints different if the two span " +
				"identifiers differ or same if they do not, then the status and " +
				"the body ok separated by a space. It then shuts both servers " +
				"down and exits. Nothing may be written to standard error.",
			expected: "front 4bf92f3577b34da6a3ce929d0e0e4736\n" +
				"back 4bf92f3577b34da6a3ce929d0e0e4736\ndifferent\n200 ok",
		},
		{
			name: "148 serves over TLS with a certificate it generates",
			requirement: "Write a command-line program that generates a self-signed " +
				"certificate for 127.0.0.1 in memory with crypto/x509, starts an " +
				"HTTPS server with it on 127.0.0.1 on a port the operating system " +
				"chooses, and requests it twice: once with a client whose root " +
				"pool holds that certificate, printing the status and the body " +
				"secure separated by a space, and once with a client using the " +
				"system roots, which must fail verification, printing untrusted. " +
				"Skipping verification is not an acceptable way to make the first " +
				"request succeed. It then shuts the server down and exits. " +
				"Nothing may be written to standard error.",
			expected:   "200 secure\nuntrusted",
			mustAppear: []string{"x509.CreateCertificate(", "RootCAs"},
		},
		{
			name: "149 requires the client to present a certificate too",
			requirement: "Write a command-line program that generates its own certificate " +
				"authority in memory, issues a server certificate for 127.0.0.1 " +
				"and a client certificate whose common name is ann, and starts an " +
				"HTTPS server that requires and verifies a client certificate " +
				"signed by that authority, answering with the common name it " +
				"verified. The program requests it with the client certificate, " +
				"printing the status and the body separated by a space, and then " +
				"with a client that presents none, printing refused. It then " +
				"shuts the server down and exits. Nothing may be written to " +
				"standard error.",
			expected:   "200 ann\nrefused",
			mustAppear: []string{"ClientAuth", "ClientCAs"},
		},
		{
			name: "150 assembles the whole service and proves it works",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. One package is a pure store of notes with versions " +
				"and must not import net/http. httpapi holds the routing and the " +
				"middleware: bearer-token authorisation, a limit of three " +
				"requests per second, gzip when the client accepts it, and JSON " +
				"errors. A test exercises the routes with net/http/httptest " +
				"rather than a real port. The command starts the server on " +
				"127.0.0.1 on a port the operating system chooses and drives it: " +
				"a GET without a token, a POST creating the note ann with a " +
				"token, a GET of that note, a PUT with a stale If-Match, a PUT " +
				"with the current one, enough further requests to be " +
				"rate-limited, and a GET accepting gzip. Print for them in that " +
				"order the status and body of the refused GET, the status and new " +
				"id, the status and name, the status and the word conflict, the " +
				"status and the new version, the status alone for the refusal, " +
				"and the content encoding with the status. Then shut down " +
				"gracefully and print 'shutdown complete'. Nothing may be written " +
				"to standard error.",
			expected: "401 missing token\n201 1\n200 ann\n409 conflict\n200 2\n" +
				"429\ngzip 200\nshutdown complete",
			minPackages:  3,
			mustAppear:   []string{"httptest."},
			purePackages: 1,
		},
		// From here a rung is a service with a specification, not a program with
		// an answer.
		//
		// Everything above asks for one behaviour and checks one result. A real
		// requirement arrives as a layout, a set of rules that interact, and a
		// journey through them, and most of what goes wrong lives between the
		// rules rather than inside any one of them: the second call with the same
		// key, the read that must not see the write, the error that must not say
		// which. So each of these states its acceptance criteria explicitly and
		// then prints one line per step of a journey that exercises them in
		// order. A run can satisfy any single line by accident; the transcript is
		// what it cannot.
		{
			name: "151 runs the whole sign-up and session journey",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. One package, which must be pure, it holds the account " +
				"state machine, the password rules and the verification token " +
				"rules, and it may not import net/http. One package holds " +
				"accounts and sessions. The routing layer serves POST /signup, " +
				"POST /verify, POST /login, GET /me and POST /logout. The " +
				"acceptance criteria are: an address that already has an account " +
				"is refused with 409; a login before verification is refused with " +
				"403; a verification token is single-use and a second use is " +
				"refused with 400; a wrong verification token is refused with 400 " +
				"and does not consume the real one; a wrong password is refused " +
				"with 401 and the message must not say whether the address " +
				"exists; a successful login sets a session cookie; GET /me " +
				"answers the account name for a valid session and 401 for none; " +
				"logout destroys the session so the same cookie no longer works. " +
				"Package store is a real SQLite database opened through " +
				"database/sql with the pure-Go driver modernc.org/sqlite, which " +
				"the repository already depends on: the schema is applied as a " +
				"migration at startup rather than assumed, every statement is " +
				"parameterised and no value is ever formatted into SQL, a unique " +
				"index on the address makes a second sign-up fail in the database " +
				"and not only in Go, and sessions live in their own table with a " +
				"foreign key to the account that cascades on delete with foreign " +
				"keys enabled. The program starts the server on 127.0.0.1 on a " +
				"port the operating system chooses and prints one line per step, " +
				"in this order: sign-up as ann, sign-up again with the same " +
				"address, login before verifying, verify with a wrong token, " +
				"verify with the right one, verify again with the same token, " +
				"login with a wrong password, login with the right one, GET /me, " +
				"logout, and GET /me again — each as the status and the one-word " +
				"or two-word result. It then shuts down and exits, writing " +
				"nothing to standard error.",
			expected: "201 pending\n409 taken\n403 unverified\n400 bad token\n" +
				"200 verified\n400 used\n401 invalid\n200 session\n200 ann\n204\n" +
				"401 anonymous",
			minPackages:  4,
			purePackages: 1,
		},
		{
			name: "152 stores a password so that it cannot be read back",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. Package credentials derives a password verifier with " +
				"a memory-hard or iterated key derivation over a per-account " +
				"random salt, and issues reset tokens that carry an expiry and " +
				"are stored only as a hash. The acceptance criteria are: the " +
				"stored account record contains neither the password nor anything " +
				"a comparison could reverse; two accounts with the same password " +
				"produce different verifiers; POST /forgot answers 202 whether or " +
				"not the address exists, so it cannot be used to discover " +
				"accounts; a reset token past its expiry is refused with 400; a " +
				"reset token is single-use; a successful reset invalidates every " +
				"existing session and the old password stops working. Storage is " +
				"a real SQLite database through database/sql with " +
				"modernc.org/sqlite, its schema applied as a migration at startup " +
				"and every statement parameterised: reset tokens are their own " +
				"table holding the token hash, the expiry and the account, and " +
				"consuming a token, changing the verifier and deleting the " +
				"account's sessions all happen in one transaction, so a failure " +
				"part-way cannot leave a spent token beside an unchanged " +
				"password. The program starts the server and prints one line per " +
				"step, in this order: sign up and print the status and the word " +
				"hashed if the stored record holds no plaintext, log in, ask to " +
				"reset an unknown address, ask to reset the known one, reset with " +
				"an expired token, reset with the valid one, reuse that token, " +
				"log in with the old password, log in with the new one, and " +
				"finally the word salted if two accounts sharing a password have " +
				"different verifiers. It then shuts down and exits, writing " +
				"nothing to standard error.",
			expected: "201 hashed\n200 ok\n202 accepted\n202 accepted\n400 expired\n" +
				"200 reset\n400 used\n401 invalid\n200 ok\nsalted",
			minPackages: 4,
		},
		{
			name: "153 issues, rotates and revokes signed tokens",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. One package builds and verifies signed JSON web " +
				"tokens with HMAC-SHA256 itself, without a third-party library, " +
				"and is pure apart from being given the current time. The " +
				"acceptance criteria are: a token whose payload was altered fails " +
				"verification; a token past its expiry is refused; a token " +
				"declaring the algorithm none is refused rather than accepted " +
				"unsigned; the refresh token is rotated on every use and the one " +
				"it replaced is invalidated; presenting an already-rotated " +
				"refresh token is treated as theft and revokes the whole token " +
				"family, including access tokens already issued from it; " +
				"verification compares signatures in constant time. Storage is a " +
				"real SQLite database through database/sql with " +
				"modernc.org/sqlite, its schema applied as a migration at startup " +
				"and every statement parameterised: a refresh token is a row " +
				"carrying its family identifier and whether it has been used, " +
				"with a unique index on the token hash, so revoking a family is " +
				"one UPDATE over that family rather than a loop in Go, and the " +
				"rotation reads and marks the old token in a single transaction " +
				"so two concurrent refreshes cannot both succeed. The program " +
				"starts the server and prints one line per step, in this order: " +
				"request a token pair, call GET /me with the access token, call " +
				"it with an altered signature, with an expired token, with an " +
				"unsigned token, refresh, refresh again with the token that was " +
				"just replaced, call GET /me with the access token from the " +
				"rotated pair, request a fresh pair, and call GET /me again. It " +
				"then shuts down and exits, writing nothing to standard error.",
			expected: "200 issued\n200 ann\n401 bad signature\n401 expired\n" +
				"401 bad algorithm\n200 rotated\n401 reused\n401 revoked\n" +
				"200 issued\n200 ann",
			minPackages:  4,
			mustAppear:   []string{"hmac.Equal("},
			purePackages: 1,
		},
		{
			name: "154 decides every request against a permission model",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. One package, which must be pure, holds roles, the " +
				"permissions each grants, and role inheritance: viewer may read, " +
				"editor inherits viewer and may write, admin inherits editor and " +
				"may delete and grant. The acceptance criteria are: every route " +
				"declares the permission it needs and the check is made in one " +
				"place rather than repeated in each handler; a refusal names the " +
				"permission that was missing and does not reveal anything about " +
				"the resource; granting a role takes effect for the next request " +
				"without a new login; an unknown role is refused at grant time " +
				"with 400 rather than silently ignored; a role's effective " +
				"permissions are its own and every inherited one, with no " +
				"duplicates. Roles, permissions and the grants between them are " +
				"tables in a real SQLite database through database/sql with " +
				"modernc.org/sqlite, its schema applied as a migration at startup " +
				"and every statement parameterised; the effective permissions of " +
				"an account are resolved by one recursive common table expression " +
				"that walks the inheritance, not by loading the roles and joining " +
				"them in Go, and a grant of a role that does not exist is refused " +
				"by a foreign key rather than only by a check in the handler. The " +
				"program starts the server and prints one line per step, in this " +
				"order: a viewer reads, a viewer writes, an editor writes, an " +
				"editor deletes, an admin deletes, a viewer grants, an admin " +
				"grants editor to the viewer, that account writes, an admin " +
				"grants a role that does not exist, and finally the promoted " +
				"account's effective permissions in ascending order separated by " +
				"commas. It then shuts down and exits, writing nothing to " +
				"standard error.",
			expected: "200 read\n403 write required\n201 written\n" +
				"403 delete required\n204\n403 grant required\n200 granted\n" +
				"201 written\n400 unknown role\nread,write",
			minPackages:  4,
			purePackages: 1,
		},
		{
			name: "155 hides the rows a caller has no business seeing",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. One package, which must be pure, decides visibility " +
				"from the caller and the row: an owner sees their own rows, an " +
				"administrator sees every row, and nobody else sees any. The " +
				"acceptance criteria are: a request for a row the caller may not " +
				"see is answered 404 and not 403, because a 403 tells the caller " +
				"the row exists; the same is true for update and delete; listing " +
				"is filtered by the same rule as reading, so a list can never " +
				"contain a row a direct read would hide; the filter is applied in " +
				"the query rather than by discarding rows after loading them; the " +
				"rule lives in authz and no handler repeats it. The rows are in a " +
				"real SQLite database through database/sql with " +
				"modernc.org/sqlite, its schema applied as a migration at startup " +
				"and every statement parameterised: the visibility rule becomes a " +
				"WHERE clause bound to the caller, there is an index on the owner " +
				"column so listing does not scan the table, and EXPLAIN QUERY " +
				"PLAN for the listing query must show that index being used " +
				"rather than a full scan. The program starts the server and " +
				"prints one line per step, in this order: ann creates a note, ann " +
				"reads it, bob reads it, bob updates it, bob deletes it, an " +
				"administrator reads it, ann lists, bob lists, the administrator " +
				"lists, and ann deletes her own note — each as the status and the " +
				"name or the count. It then shuts down and exits, writing nothing " +
				"to standard error.",
			expected: "201 1\n200 ann\n404 not found\n404 not found\n404 not found\n" +
				"200 ann\n200 1\n200 0\n200 1\n204",
			minPackages:  4,
			purePackages: 2,
		},
		{
			name: "156 keeps one tenant's data away from another's",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. Every request is resolved to a tenant from an " +
				"X-Tenant header, and every stored row carries its tenant. The " +
				"acceptance criteria are: a request that names no tenant is " +
				"refused 400 and a request naming an unknown one 404; identifiers " +
				"are allocated per tenant, so two tenants both hold an item " +
				"numbered one; a read of another tenant's identifier is answered " +
				"404; a tenant marked suspended is refused 403 on every route " +
				"except the ones that report its own state; a request whose body " +
				"carries a tenant that disagrees with the resolved one is refused " +
				"403 rather than trusting either; the tenant filter is applied by " +
				"the store, so no handler can forget it. Storage is a real SQLite " +
				"database through database/sql with modernc.org/sqlite, its " +
				"schema applied as a migration at startup and every statement " +
				"parameterised: every table carries a tenant column, every unique " +
				"index is scoped by tenant so two tenants may both hold item one, " +
				"and the per-tenant identifier comes from a counter row updated " +
				"in the same transaction as the insert rather than from a global " +
				"sequence. The program starts the server and prints one line per " +
				"step, in this order: a request with no tenant, a request for an " +
				"unknown tenant, acme creates an item, globex creates an item, " +
				"acme lists, globex lists, acme reads globex's identifier, a " +
				"suspended tenant makes a request, a request whose body names a " +
				"different tenant, and finally the total number of rows the store " +
				"holds. It then shuts down and exits, writing nothing to standard " +
				"error.",
			expected: "400 no tenant\n404 unknown tenant\n201 1\n201 1\n200 1\n" +
				"200 1\n404 not found\n403 suspended\n403 tenant mismatch\n2",
			minPackages: 4,
		},
		{
			name: "157 issues API keys it can rotate and revoke",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. Package apikey mints a key as a public identifier and " +
				"a secret, and stores only a hash of the secret. The acceptance " +
				"criteria are: the secret is returned exactly once, at creation, " +
				"and no route can read it back; the stored record contains no " +
				"value that could be presented as a key; a wrong secret is " +
				"refused 401 in constant time; the time a key was last used is " +
				"recorded without a write on every request turning into a " +
				"bottleneck; rotation issues a new secret and leaves the old one " +
				"working for a stated grace period; revoking ends the old secret " +
				"immediately even inside that period. Keys live in a real SQLite " +
				"database through database/sql with modernc.org/sqlite, its " +
				"schema applied as a migration at startup and every statement " +
				"parameterised: the secret hash carries a unique index and is the " +
				"only lookup key, the record has no column that could hold the " +
				"secret itself, and the last-used timestamp is written with an " +
				"UPDATE that changes nothing when the recorded second has not " +
				"moved, so a busy key does not write on every request. The " +
				"program starts the server and prints one line per step, in this " +
				"order: create a key and print the status and the words shown " +
				"once if the secret came back exactly once, the words stored " +
				"hashed if the stored record holds no usable secret, a call with " +
				"the key, a call with a wrong secret, the word used and the " +
				"recorded use count, a rotation, a call with the old secret " +
				"inside the grace period, revoking the old secret, a call with it " +
				"afterwards, and a call with the new one. It then shuts down and " +
				"exits, writing nothing to standard error.",
			expected: "201 shown once\nstored hashed\n200 ok\n401 invalid key\n" +
				"used 1\n200 rotated\n200 ok\n204\n401 revoked\n200 ok",
			minPackages: 4,
		},
		{
			name: "158 keeps an audit trail that shows if it was altered",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. One package, which must be pure, appends entries to a " +
				"chain: each entry holds the actor, the action, the target, the " +
				"request identifier and the hash of the entry before it. The " +
				"acceptance criteria are: every state-changing route writes " +
				"exactly one entry, written in the same transaction as the change " +
				"so neither can exist without the other; entries are append-only " +
				"and any route that would edit or delete one is refused 405; " +
				"verification recomputes the chain and names the first entry that " +
				"does not match; an entry records who acted, on what, and under " +
				"which request identifier, and holds no secret or password. The " +
				"trail is a table in a real SQLite database through database/sql " +
				"with modernc.org/sqlite, its schema applied as a migration at " +
				"startup and every statement parameterised: the append-only rule " +
				"is enforced by triggers that raise on UPDATE and on DELETE, so " +
				"it holds even against a statement the handlers never issue, and " +
				"the entry is inserted in the same transaction as the change it " +
				"records. The program starts the server and prints one line per " +
				"step, in this order: create a note, update it, delete it, read " +
				"the audit trail and print the status and the number of entries, " +
				"verify the chain, alter the second entry in the store and verify " +
				"again, attempt to delete an entry, and finally print the second " +
				"entry as its actor, action, target and request identifier " +
				"separated by spaces. It then shuts down and exits, writing " +
				"nothing to standard error.",
			expected: "201 1\n200 2\n204\n200 3\nchain intact\nchain broken at 2\n" +
				"405 append only\nann update note-1 req-2",
			minPackages:  4,
			purePackages: 1,
		},
		{
			name: "159 enforces the limits a plan actually paid for",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. A plan carries a per-second rate and a daily quota: " +
				"free allows three per second and ten per day, pro allows ten per " +
				"second and a hundred per day. The acceptance criteria are: every " +
				"answer carries an X-RateLimit-Remaining header holding the " +
				"requests left in the current second; exceeding the rate is " +
				"refused 429 with a Retry-After in whole seconds; exhausting the " +
				"daily quota is refused 402, which is a different condition from " +
				"being rate-limited and must not be reported as the same; limits " +
				"are counted per tenant, so one tenant cannot exhaust another's; " +
				"changing plan takes effect on the next request without a " +
				"restart; the counters are safe under concurrent requests. The " +
				"counters are rows in a real SQLite database through database/sql " +
				"with modernc.org/sqlite, its schema applied as a migration at " +
				"startup and every statement parameterised: an increment is one " +
				"INSERT with an ON CONFLICT clause that adds to the existing " +
				"count, never a read followed by a write, so two concurrent " +
				"requests cannot lose an increment, and the database is opened in " +
				"write-ahead-log mode with a busy timeout so a concurrent writer " +
				"waits instead of failing. The program starts the server and " +
				"prints one line per step, in this order: three requests on the " +
				"free plan, each as the status and the remaining header; a fourth " +
				"as the status and Retry-After; an upgrade to pro; a request as " +
				"the status and the remaining header; a request once the daily " +
				"quota is spent; and a request after the day rolls over. It then " +
				"shuts down and exits, writing nothing to standard error.",
			expected: "200 2\n200 1\n200 0\n429 1\n200 upgraded\n200 9\n" +
				"402 quota exceeded\n200 ok",
			minPackages: 4,
		},
		{
			name: "160 meters usage and bills it without floating point",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. Both meter and billing are pure. Money is counted in " +
				"minor units as integers and no amount may pass through a " +
				"floating-point type at any point. The price is one hundred minor " +
				"units per request and five minor units per hundred bytes. The " +
				"acceptance criteria are: metering records requests and bytes per " +
				"tenant and period; rolling a period up twice produces the same " +
				"invoice rather than two, so a retried job cannot double-bill; a " +
				"finalised invoice cannot be changed and an attempt is refused " +
				"409; an adjustment after finalisation is made as a separate " +
				"credit note; the balance is the invoice total less the credits. " +
				"Usage, invoices, line items and credit notes are tables in a " +
				"real SQLite database through database/sql with " +
				"modernc.org/sqlite, its schema applied as a migration at startup " +
				"and every statement parameterised: every amount is an INTEGER " +
				"column of minor units and no column is REAL, a unique index on " +
				"the tenant and period makes a second roll-up of the same period " +
				"fail rather than bill twice, and the invoice and its line items " +
				"are written in one transaction. The program starts the server " +
				"and prints one line per step, in this order: meter ten requests " +
				"carrying a thousand bytes in total and print the word metered " +
				"and the count; roll up and print the period, the request count " +
				"and the byte count; issue the invoice and print its number and " +
				"total; attempt to change it; issue a credit note of two hundred; " +
				"print the balance; print the words no floats if no monetary " +
				"value was ever held as a float; and print the word idempotent if " +
				"rolling the same period up again changed nothing. It then shuts " +
				"down and exits, writing nothing to standard error.",
			expected: "metered 10\nperiod 2026-01 requests 10 bytes 1000\n" +
				"invoice 1 total 1050\n409 finalised\ncredit 200\nbalance 850\n" +
				"no floats\nidempotent",
			minPackages:  4,
			purePackages: 2,
		},
		{
			name: "161 takes payment events in once and only once",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. Storage is a real SQLite database through " +
				"database/sql with the pure-Go driver modernc.org/sqlite, its " +
				"schema applied as a migration at startup and every statement " +
				"parameterised. The acceptance criteria are: the signature is " +
				"computed over the exact bytes received, before any decoding, and " +
				"a request that does not match is refused 401; a signature whose " +
				"timestamp is more than five minutes old is refused 400 even " +
				"though it verifies, so a captured request cannot be replayed " +
				"later; the provider's event identifier has a unique index and a " +
				"repeat is answered 200 as a duplicate without applying it again, " +
				"which must be decided by the insert failing rather than by a " +
				"preceding read; an event carrying a version older than the state " +
				"already stored is acknowledged and ignored; an event type " +
				"nothing handles is answered 202 rather than 400, so the provider " +
				"stops retrying it; reconciliation is a single query joining " +
				"charges to ledger entries and reporting the charges with no " +
				"entry. The program starts the server on 127.0.0.1 on a port the " +
				"operating system chooses and prints one line per step, in this " +
				"order: an unsigned event, a correctly signed event with an old " +
				"timestamp, a valid event, the same event again, an event with an " +
				"older version, an event of an unknown type, the word unmatched " +
				"and the reconciliation count, and that count again after the " +
				"missing ledger entry is written. It then shuts down and exits, " +
				"writing nothing to standard error.",
			expected: "401 bad signature\n400 stale\n200 accepted\n200 duplicate\n" +
				"200 stale event\n202 ignored\nunmatched 1\nunmatched 0",
			minPackages: 5,
		},
		{
			name: "162 moves a subscription only along legal transitions",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. One package, which must be pure, holds the state " +
				"machine: trialing, active, past_due, canceling and canceled, " +
				"with the transitions between them. Storage is a real SQLite " +
				"database through database/sql with modernc.org/sqlite, its " +
				"schema applied as a migration at startup and every statement " +
				"parameterised. The acceptance criteria are: the state column " +
				"carries a CHECK constraint naming the legal states, so a value " +
				"the domain would never produce still cannot be stored; every " +
				"transition inserts a row into a transitions table in the same " +
				"transaction as the update, and that table has a foreign key to " +
				"the subscription; a transition the machine forbids is refused " +
				"409 and leaves both tables untouched; the state and its " +
				"transition are written together or not at all; the history is " +
				"read back in order with an ORDER BY rather than sorted in Go. " +
				"The program starts the server and prints one line per step, in " +
				"this order: create the subscription, end the trial with a " +
				"payment, fail a payment, succeed on the dunning retry, ask to " +
				"cancel at the period end, let the period end, attempt to " +
				"reactivate the canceled subscription, and finally the whole " +
				"transition history as the states in order separated by spaces. " +
				"It then shuts down and exits, writing nothing to standard error.",
			expected: "201 trialing\n200 active\n200 past_due\n200 active\n" +
				"200 canceling\n200 canceled\n409 illegal transition\n" +
				"trialing active past_due active canceling canceled",
			minPackages:  4,
			purePackages: 1,
		},
		{
			name: "163 decides a feature flag the same way every time",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. One package, which must be pure, given a flag, its " +
				"rules and a subject it returns whether the flag is on and which " +
				"rule decided. Flags and rules are rows in a real SQLite database " +
				"through database/sql with modernc.org/sqlite, its schema applied " +
				"as a migration at startup and every statement parameterised, " +
				"with the rules ordered by an explicit priority column rather " +
				"than by insertion order. The acceptance criteria are: rules are " +
				"evaluated in priority order and the first that matches decides; " +
				"a percentage rollout buckets the subject by hashing the flag " +
				"name together with the subject, so the same subject always lands " +
				"in the same bucket and a subject in one process lands where it " +
				"would in another; a rollout of zero is never on and a rollout of " +
				"a hundred is always on; an unknown flag is refused 404 rather " +
				"than defaulting to off, because a silent default hides a typo; " +
				"the answer states the reason. The program starts the server and " +
				"prints one line per step, in this order: a flag that is off, a " +
				"flag on for everyone, a flag whose rule matches the pro plan " +
				"asked for a pro tenant, the same flag asked for a free tenant, a " +
				"rollout of zero, a rollout of a hundred, the word stable if the " +
				"same subject evaluated twice agreed, the word deterministic if a " +
				"second evaluator built from the stored rules agreed with the " +
				"first, an unknown flag as its status and message, and the reason " +
				"the pro tenant's answer gave. It then shuts down and exits, " +
				"writing nothing to standard error.",
			expected: "off\non\non\noff\noff\non\nstable\ndeterministic\n" +
				"404 unknown flag\nreason=rule:plan",
			minPackages:  4,
			purePackages: 1,
		},
		{
			name: "164 versions its configuration and can go back",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. Configuration versions are rows in a real SQLite " +
				"database through database/sql with modernc.org/sqlite, its " +
				"schema applied as a migration at startup and every statement " +
				"parameterised. The acceptance criteria are: the versions table " +
				"is append-only and a version is never edited in place; exactly " +
				"one version is current at any moment, enforced by a partial " +
				"unique index on the current flag rather than by convention; a " +
				"rollback publishes the old document as a new version, so the " +
				"history still shows what happened; a document that fails " +
				"validation is refused 422 and no version is written; a write " +
				"that carries a version number that is no longer current is " +
				"refused 409; reading the current configuration is one query and " +
				"does not sort in Go. The program starts the server and prints " +
				"one line per step, in this order: read the configuration, write " +
				"an invalid document, write a valid one, read it back, read the " +
				"number of versions in the history, roll back to the first " +
				"version, read the current version number together with its " +
				"value, and write again with the version number read before the " +
				"rollback. It then shuts down and exits, writing nothing to " +
				"standard error.",
			expected: "200 v1\n422 invalid\n200 v2\n200 v2\n200 2\n200 v3\n" +
				"200 v3 alpha\n409 conflict",
			minPackages: 4,
		},
		{
			name: "165 migrates its schema before it serves anything",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. The database is real SQLite through database/sql with " +
				"modernc.org/sqlite. The acceptance criteria are: applied " +
				"migrations are recorded in a schema_migrations table holding the " +
				"version, a checksum of the file and when it was applied; each " +
				"migration runs inside its own transaction, so a failure leaves " +
				"neither a half-applied schema nor a row claiming it succeeded; " +
				"starting again applies nothing; two instances starting at once " +
				"do not both migrate, because the migrator takes the write lock " +
				"with an immediate transaction and the second waits rather than " +
				"failing; a file whose contents changed after it was applied is " +
				"reported by version and refuses to start, since silently " +
				"ignoring it would leave two databases with the same recorded " +
				"version and different schemas; a down migration reverses the " +
				"last one; the readiness endpoint answers 503 while migrating and " +
				"200 afterwards. The program starts the server and prints one " +
				"line per step, in this order: the word applied and the count on " +
				"a fresh database, the same after a restart, the words lock held " +
				"if the second instance waited rather than migrating, the word " +
				"drift and the version whose checksum changed, the word reverted " +
				"and the count, the word applied and the count of reapplying it, " +
				"the word version and the schema version now recorded, and the " +
				"readiness status and body during and after migration. It then " +
				"shuts down and exits, writing nothing to standard error.",
			expected: "applied 3\napplied 0\nlock held\ndrift 002\nreverted 1\n" +
				"applied 1\nversion 3\n503 migrating\n200 ready",
			minPackages: 4,
			mustAppear:  []string{"embed", "schema_migrations"},
		},
		{
			name: "166 gives each request one transaction and means it",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. The database is real SQLite through database/sql with " +
				"modernc.org/sqlite, its schema applied as a migration at startup " +
				"and every statement parameterised. The acceptance criteria are: " +
				"a middleware opens one transaction per request, puts it in the " +
				"request context and commits it only if the handler answered " +
				"without error; a handler that fails part-way leaves nothing " +
				"behind, including the rows written before it failed; a service " +
				"calling another service joins the transaction already in the " +
				"context instead of opening a second one, which would deadlock " +
				"against the first; an optional step that may fail is wrapped in " +
				"a savepoint and rolled back to it without losing the work before " +
				"it; using the transaction after it has been committed is an " +
				"error rather than a silent no-op; a panic rolls back and answers " +
				"500; another connection sees none of the rows until the commit. " +
				"The program starts the server and prints one line per step, in " +
				"this order: the words rolled back and the row count after a " +
				"failing request, the words committed and the row count after a " +
				"successful one, the words one transaction if the nested call " +
				"joined rather than opened, the status and body of using the " +
				"transaction after commit, the status and name read inside the " +
				"transaction that wrote it, the counts another connection sees " +
				"before and after the commit as two numbers with the words before " +
				"and after, the status and body of a request whose handler " +
				"panics, and the word consistent if the store matches what the " +
				"transcript implies. It then shuts down and exits, writing " +
				"nothing to standard error.",
			expected: "rolled back 0\ncommitted 2\none transaction\n409 closed\n" +
				"200 ann\n0 before 1 after\n500 internal error\nconsistent",
			minPackages: 5,
		},
		{
			name: "167 keeps writing while the database is locked",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. The database is real SQLite through database/sql with " +
				"modernc.org/sqlite, in a temporary directory of its own. The " +
				"acceptance criteria are: the connection is opened in " +
				"write-ahead-log mode with foreign keys on and a busy timeout " +
				"set, and those are verified by reading the pragmas back rather " +
				"than assumed from the connection string; readers are not blocked " +
				"while a writer holds the lock, which is what write-ahead logging " +
				"buys; the pool is configured so writes serialise instead of " +
				"colliding; a write that still meets a locked database retries " +
				"with backoff a bounded number of times; a lock that outlasts the " +
				"timeout is answered 503 rather than 500, because it is a wait, " +
				"not a defect; nothing in the program treats a busy error as data " +
				"loss. The program starts the server and prints one line per " +
				"step, in this order: the journal mode read back from the " +
				"database, the words reader not blocked if a read completed while " +
				"a writer held the lock, the number of concurrent writes that " +
				"succeeded followed by the word writes and the number that " +
				"errored followed by the word errors, the words busy retried if " +
				"at least one write had to retry, the status and body of a write " +
				"made with the busy timeout set to zero while the lock is held, " +
				"and the word restored once the timeout is put back and the write " +
				"succeeds. It then shuts down and exits, writing nothing to " +
				"standard error.",
			expected: "wal\nreader not blocked\n100 writes 0 errors\nbusy retried\n" +
				"503 busy\nrestored",
			minPackages: 3,
			mustAppear:  []string{"journal_mode", "busy_timeout"},
		},
		{
			name: "168 shows a reader a consistent view of the database",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. The database is real SQLite through database/sql with " +
				"modernc.org/sqlite in write-ahead-log mode. The acceptance " +
				"criteria are: a read transaction sees the database as it was " +
				"when it began and is not disturbed by a commit that lands while " +
				"it is open; a writer begins with an immediate transaction rather " +
				"than upgrading a deferred one part-way, so two writers cannot " +
				"deadlock trying to upgrade at the same time; a connection reads " +
				"its own uncommitted writes; a reader that starts after the " +
				"commit sees the new value; nothing depends on the connection " +
				"pool handing back the same connection, which it does not " +
				"promise. The program starts the server and prints one line per " +
				"step, in this order: the word snapshot and the value a read " +
				"transaction sees when it begins, the word committed and the " +
				"value a writer commits while that read is still open, the words " +
				"snapshot still and the value the open read sees afterwards, the " +
				"words after commit and the value a new read sees, the words read " +
				"your write and the value the writing connection reads before it " +
				"commits, and the words no upgrade deadlock if two writers " +
				"contending at once both completed. It then shuts down and exits, " +
				"writing nothing to standard error.",
			expected: "snapshot 1\ncommitted 2\nsnapshot still 1\nafter commit 2\n" +
				"read your write 2\nno upgrade deadlock",
			minPackages: 4,
		},
		{
			name: "169 proves its queries use the indexes it built",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. The database is real SQLite through database/sql with " +
				"modernc.org/sqlite, holding an owners table and a notes table " +
				"with an index named notes_by_owner on the owner and identifier " +
				"columns. The acceptance criteria are: the listing query is " +
				"checked at startup with EXPLAIN QUERY PLAN and the check fails " +
				"if any step of the plan scans the notes table instead of " +
				"searching it, so a query that quietly stopped using its index is " +
				"caught before it is served; loading ten owners together with " +
				"their notes costs a fixed number of statements rather than one " +
				"per owner, which means one query for the owners and one for " +
				"their notes gathered by an IN clause; the number of statements " +
				"executed is counted by the database layer, not estimated; an " +
				"index the queries need but the schema lacks is reported by name " +
				"at startup rather than discovered under load. The program starts " +
				"the server and prints one line per step, in this order: the " +
				"first word of the listing query's plan, the words no scan if no " +
				"step of it scans the table, the word statements and the number " +
				"executed loading ten owners with their notes, the words n plus " +
				"one avoided if that number does not grow when the owner count " +
				"doubles, the name of the index the startup check reports missing " +
				"when it is dropped, and the status and row count of the listing " +
				"route. It then shuts down and exits, writing nothing to standard " +
				"error.",
			expected: "SEARCH\nno scan\nstatements 2\nn plus one avoided\n" +
				"notes_by_owner\n200 10",
			minPackages: 4,
			mustAppear:  []string{"EXPLAIN QUERY PLAN"},
		},
		{
			name: "170 searches its own documents through a text index",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. The database is real SQLite through database/sql with " +
				"modernc.org/sqlite and the index is an FTS5 virtual table over " +
				"the documents table. The acceptance criteria are: the index is " +
				"kept in step with the source table by triggers on insert, update " +
				"and delete, so no code path can update a document and forget the " +
				"index; a term query, a quoted phrase query and a prefix query " +
				"are all supported and return the matching document identifiers " +
				"in ascending order, which keeps the answer stable however the " +
				"ranking function scores them; a match can be returned with the " +
				"matched term marked; a query whose syntax FTS5 rejects is " +
				"answered 400 rather than being allowed to reach the database as " +
				"an error; results are paged with a stable order and the response " +
				"says which page of how many; rebuilding the index produces the " +
				"same content. The program starts the server and prints one line " +
				"per step, in this order: the word indexed and the number of " +
				"documents, the identifiers matching the term quick, the " +
				"identifiers matching the phrase quick brown, the word " +
				"highlighted if the marked term came back wrapped in markers, the " +
				"identifiers matching the prefix quic, the status and message for " +
				"a malformed query, the page description for the first page of " +
				"two, the word indexed and the count after a rebuild, and the " +
				"words in sync if updating a document changed what the index " +
				"returns. It then shuts down and exits, writing nothing to " +
				"standard error.",
			expected: "indexed 3\n1 3\n1\nhighlighted\n1 3\n400 bad query\n" +
				"page 1 of 2\nindexed 3\nin sync",
			minPackages: 4,
			mustAppear:  []string{"fts5"},
		},
		{
			name: "171 stores documents the database can still query",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. The database is real SQLite through database/sql with " +
				"modernc.org/sqlite. A document is stored in one JSON column. The " +
				"acceptance criteria are: the column carries a CHECK constraint " +
				"using json_valid, so a malformed document cannot be stored even " +
				"by a statement the handlers never issue, and the route reports " +
				"422; a field the service filters on is exposed as a generated " +
				"column extracted from the JSON with an index on it, and the plan " +
				"for the filtering query searches that index rather than " +
				"scanning; reading one field is done with json_extract in the " +
				"query, not by decoding the whole document in Go; updating one " +
				"field is one statement using json_set and leaves every other " +
				"field byte for byte as it was; querying inside an array uses " +
				"json_each rather than loading and looping. The program starts " +
				"the server on 127.0.0.1 on a port the operating system chooses " +
				"and prints one line per step, in this order: store a document, " +
				"read one field from it, the first word of the filtering query's " +
				"plan, the status and row count of the filtered route, the status " +
				"and message for a malformed document, the status and the new " +
				"value after patching one field, the word unchanged if every " +
				"other field survived the patch untouched, and the status and " +
				"count of a query over an array inside the document. It then " +
				"shuts down and exits, writing nothing to standard error.",
			expected: "201 1\n200 ann\nSEARCH\n200 1\n422 invalid json\n200 30\n" +
				"unchanged\n200 2",
			minPackages: 4,
			mustAppear:  []string{"json_valid", "json_extract"},
		},
		{
			name: "172 writes a row whether or not it was already there",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. The database is real SQLite through database/sql with " +
				"modernc.org/sqlite. The acceptance criteria are: creating or " +
				"replacing a record is one INSERT with an ON CONFLICT clause that " +
				"updates, never a SELECT followed by a decision, because between " +
				"the two another request could have written; the conflict target " +
				"names the columns of a real unique index, and the startup check " +
				"reports by name any upsert whose target has no unique index " +
				"behind it; the statement uses RETURNING so the identifier and " +
				"the stored values come back without a second query; a route that " +
				"must not overwrite uses ON CONFLICT DO NOTHING and reports that " +
				"nothing changed rather than pretending it wrote; a hundred " +
				"concurrent upserts of the same key leave exactly one row and " +
				"lose no update. The program starts the server and prints one " +
				"line per step, in this order: the status, identifier and name of " +
				"the first upsert, the same for a second upsert of the same key " +
				"with a new name, the word statements and the number of " +
				"statements one upsert executed, the status and identifier of the " +
				"do-nothing route against an existing key, the words missing " +
				"unique index followed by nothing else when the startup check " +
				"runs against a schema with the index dropped, and the number of " +
				"concurrent upserts followed by the word upserts and the " +
				"resulting row count followed by the word row. It then shuts down " +
				"and exits, writing nothing to standard error.",
			expected: "201 1 ann\n200 1 bob\nstatements 1\n200 1\n" +
				"missing unique index\n100 upserts 1 row",
			minPackages: 4,
			mustAppear:  []string{"ON CONFLICT", "RETURNING"},
		},
		{
			name: "173 pages and ranks without asking the database twice",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. The database is real SQLite through database/sql with " +
				"modernc.org/sqlite. The acceptance criteria are: the highest " +
				"scoring row per group is found with ROW_NUMBER over a partition " +
				"in one query, not by loading every row and grouping in Go; a " +
				"thread of replies is walked with a recursive common table " +
				"expression that reports each row's depth; pages are taken by " +
				"keyset, comparing against the last row's sort key, and the SQL " +
				"contains no OFFSET, because OFFSET reads and discards the rows " +
				"before it and drifts when a row is inserted mid-pagination; a " +
				"row inserted between two pages never causes a row to be repeated " +
				"or skipped; the sort key is unique, taking the identifier as a " +
				"tie-break, so no two rows can compare equal. The program starts " +
				"the server and prints one line per step, in this order: the word " +
				"top followed by the leading name of each group in ascending " +
				"group order, the word depth and the greatest depth the recursive " +
				"query reported, the three pages of a five-row collection read " +
				"two at a time as the identifiers followed by the word next and " +
				"an equals sign and the cursor, the words no repeats if reading " +
				"the pages again with a row inserted between them repeated and " +
				"skipped nothing, and the words no offset if no statement it " +
				"issued contained one. It then shuts down and exits, writing " +
				"nothing to standard error.",
			expected: "top ann bob\ndepth 3\n1 2 next=2\n3 4 next=4\n5 next=\n" +
				"no repeats\nno offset",
			minPackages: 4,
			mustAppear:  []string{"ROW_NUMBER", "WITH RECURSIVE"},
		},
		{
			name: "174 refuses to hold a row whose parent is gone",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. The database is real SQLite through database/sql with " +
				"modernc.org/sqlite. The acceptance criteria are: foreign keys " +
				"are enforced, which in SQLite means the pragma is set on every " +
				"connection the pool opens rather than once at startup, and the " +
				"program proves it by reading the pragma back on a connection the " +
				"pool created later; a child row naming a parent that does not " +
				"exist is refused, and the route reports 409 rather than 500, " +
				"because it is a rule and not a fault; one relation cascades on " +
				"delete and deleting the parent removes its children in the same " +
				"statement; another relation restricts, and deleting a parent " +
				"that still has children is refused; a query looking for orphaned " +
				"rows returns none at any point in the journey. The program " +
				"starts the server and prints one line per step, in this order: " +
				"the foreign-key pragma read back, the status and message of " +
				"inserting a child with no parent, the status of deleting a " +
				"cascading parent followed by the word cascaded and the number of " +
				"children removed, the status and message of deleting a " +
				"restricted parent, the word orphans and the count the orphan " +
				"query returns, and the pragma read back once more from a " +
				"connection opened after the pool has churned. It then shuts down " +
				"and exits, writing nothing to standard error.",
			expected: "on\n409 no parent\n204 cascaded 3\n409 restricted\n" +
				"orphans 0\non",
			minPackages: 4,
			mustAppear:  []string{"foreign_keys"},
		},
		{
			name: "175 puts the rules where no statement can miss them",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. The database is real SQLite through database/sql with " +
				"modernc.org/sqlite. The acceptance criteria are: a denormalised " +
				"count on the parent row is maintained by triggers on insert and " +
				"delete rather than by the handlers, so it cannot drift when a " +
				"new code path forgets it; a trigger refuses an update to a " +
				"record marked closed by raising, and the route reports 409; " +
				"every change writes an audit row through a trigger, so a " +
				"statement issued outside the handlers is audited too; the " +
				"maintained count matches a count recomputed from the rows " +
				"themselves at the end. The program starts the server and prints " +
				"one line per step, in this order: the word count and the " +
				"maintained count after three inserts, the word count and the " +
				"maintained count after one delete, the status and message of " +
				"updating a closed record, the word audit and the number of audit " +
				"rows, the word matches if the maintained count equals the " +
				"recomputed one, and the words still and the maintained count " +
				"after a raw statement is executed against the table without " +
				"going through the handlers. It then shuts down and exits, " +
				"writing nothing to standard error.",
			expected:    "count 3\ncount 2\n409 closed\naudit 4\nmatches\nstill 2",
			minPackages: 5,
			mustAppear:  []string{"CREATE TRIGGER", "RAISE"},
		},
		{
			name: "176 never commits a change without its event",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. The database is real SQLite through database/sql with " +
				"modernc.org/sqlite, and a receiver runs as a second HTTP server " +
				"in the same process. The acceptance criteria are: the row and " +
				"its event are written in one transaction, so an event can never " +
				"describe a change that was rolled back and a change can never " +
				"exist with no event; a poller claims a batch by updating the " +
				"rows it takes and returning them in the same statement, so two " +
				"pollers running at once never take the same event; delivery is " +
				"at-least-once and the receiver refuses a repeat by the event " +
				"identifier; a failed delivery is retried with backoff and its " +
				"attempt count is recorded on the row; the outbox drains to " +
				"empty; an event committed but not yet delivered when the process " +
				"stops is still delivered afterwards. The program starts both " +
				"servers and prints one line per step, in this order: the status " +
				"and identifier of a create followed by the word outbox and the " +
				"number of undelivered events, the word claimed and the number " +
				"one poller took while another ran, the word delivered and the " +
				"count accepted, the word retry and the count retried after a " +
				"failure, the word delivered and the count accepted on the retry, " +
				"the word outbox and the remaining count, and the words no loss " +
				"if an event committed before a simulated stop was delivered " +
				"after it. It then shuts down and exits, writing nothing to " +
				"standard error.",
			expected: "201 1 outbox 1\nclaimed 1\ndelivered 1\nretry 1\n" +
				"delivered 1\noutbox 0\nno loss",
			minPackages: 5,
		},
		{
			name: "177 runs a work queue out of its own database",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. The queue is a table in a real SQLite database " +
				"through database/sql with modernc.org/sqlite. The acceptance " +
				"criteria are: a worker claims a job by writing its own " +
				"identifier and a lease expiry on the row in one statement, so " +
				"two workers never hold the same job; a job whose lease expires " +
				"because its worker died becomes claimable again without human " +
				"intervention; a failure schedules a retry with a growing delay " +
				"and records the attempt count; a job that exhausts its attempts " +
				"moves to a dead-letter table with the last error rather than " +
				"being deleted or retried forever; only the worker holding the " +
				"lease may complete a job, and another attempting it is refused " +
				"409; the queue depth is a query, not a counter that can drift. " +
				"Five jobs are enqueued, of which one always fails and one is " +
				"abandoned mid-flight. The program starts the server and prints " +
				"one line per step, in this order: the word queued and the count, " +
				"the word claimed and the number of distinct jobs two workers " +
				"took between them in one round, the word done and the number " +
				"that succeeded followed by the word failed and the number that " +
				"did not, the word requeued and the number recovered after a " +
				"lease expired, the word dlq and the number dead-lettered, the " +
				"status and message of a worker completing a job it does not " +
				"hold, and the word depth and the remaining queue depth. It then " +
				"shuts down and exits, writing nothing to standard error.",
			expected: "queued 5\nclaimed 2\ndone 3 failed 1\nrequeued 1\ndlq 1\n" +
				"409 not yours\ndepth 0",
			minPackages: 5,
		},
		{
			name: "178 backs itself up while it is still serving",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. The database is real SQLite through database/sql with " +
				"modernc.org/sqlite, in a temporary directory of its own that is " +
				"removed before the program exits. The acceptance criteria are: " +
				"the backup is taken with VACUUM INTO rather than by copying the " +
				"file, because copying a file that is being written produces a " +
				"copy that may not open; the backup is taken while requests are " +
				"being served and readers are not blocked while it runs; the copy " +
				"passes PRAGMA integrity_check before it is accepted; restoring " +
				"means opening the copy in a fresh directory and finding exactly " +
				"the rows the original held; a backup file that has been " +
				"truncated is refused with 422 and a clear message rather than " +
				"opening and behaving as if it were empty. The program starts the " +
				"server and prints one line per step, in this order: the words " +
				"backup consistent if the copy opened and matched, the word " +
				"integrity and the result of the check, the word restored and the " +
				"row count read from the restored copy, the status and message of " +
				"restoring a truncated file, and the number of reads that " +
				"completed during the backup followed by the word readers and the " +
				"number that errored followed by the word errors. It then shuts " +
				"down and exits, writing nothing to standard error.",
			expected: "backup consistent\nintegrity ok\nrestored 3\n422 truncated\n" +
				"readers 10 errors 0",
			minPackages: 4,
			mustAppear:  []string{"VACUUM INTO", "integrity_check"},
		},
		{
			name: "179 exports more rows than it could hold in memory",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. The database is real SQLite through database/sql with " +
				"modernc.org/sqlite and holds ten thousand rows seeded in one " +
				"transaction. The acceptance criteria are: the export streams " +
				"newline-delimited JSON, writing and flushing each row as it is " +
				"scanned, so the first row reaches the client long before the " +
				"last is read; no slice ever holds every row, and the rows are " +
				"walked with the result set rather than collected; the read is " +
				"one transaction, so a write that lands mid-export does not " +
				"appear in it and the export cannot contain half of a change; the " +
				"query is paged by keyset if it pages at all; a client that " +
				"disappears cancels the request context and the query stops " +
				"rather than running to completion for nobody. The program starts " +
				"the server and prints one line per step, in this order: the word " +
				"rows and the number of exported lines the client counted, the " +
				"word streamed if the first line arrived before the export " +
				"finished, the word bounded if no collection of every row was " +
				"built, the word cancelled if abandoning the request stopped the " +
				"query, and the word consistent if a row written during the " +
				"export did not appear in it. It then shuts down and exits, " +
				"writing nothing to standard error.",
			expected:    "rows 10000\nstreamed\nbounded\ncancelled\nconsistent",
			minPackages: 4,
		},
		{
			name: "180 survives a name that looks like an attack",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. The database is real SQLite through database/sql with " +
				"modernc.org/sqlite. The acceptance criteria are: every value " +
				"reaching the database is a bound parameter and no statement is " +
				"built by concatenating or formatting request data into SQL; the " +
				"parts of a query that cannot be parameters, meaning the sort " +
				"column and its direction, come from a fixed allow-list that maps " +
				"a request word to a constant fragment, and anything outside it " +
				"is refused 400 rather than escaped and used; a stored value " +
				"containing quotes, a semicolon and a comment marker comes back " +
				"byte for byte; the tables are still there afterwards; a search " +
				"for a literal percent sign escapes it for LIKE and matches only " +
				"the literal; the test file covers each of those inputs. The " +
				"program starts the server and prints one line per step, in this " +
				"order: the status and identifier of storing a hostile name, the " +
				"status and the name read back, the words tables intact if the " +
				"schema is unchanged, the status and message of sorting by a " +
				"column that is not allowed, the status and the word sorted for " +
				"an allowed one, the status and match count of the literal " +
				"percent search, and the word parameterised if no statement was " +
				"built from request data by concatenation. It then shuts down and " +
				"exits, writing nothing to standard error.",
			expected: "201 1\n200 O'Brien; DROP TABLE notes--\ntables intact\n" +
				"400 bad sort\n200 sorted\n200 1\nparameterised",
			minPackages: 4,
		},
		{
			name: "181 undoes a multi-step operation that failed halfway",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. An order runs three steps — reserve stock, charge, " +
				"ship — each with a compensating action, against a real SQLite " +
				"database through database/sql with modernc.org/sqlite. The " +
				"acceptance criteria are: the saga's progress is written in the " +
				"same transaction as each step's own effect, so no step can have " +
				"happened without the log saying so; a failure runs the " +
				"compensations for the completed steps in reverse order and stops " +
				"there; a compensation that runs twice has the same effect as " +
				"running once, since the process may stop between compensating " +
				"and recording it; a saga interrupted part-way is resumed from " +
				"its recorded position when the service starts again, not " +
				"restarted from the beginning; the final state of every saga is " +
				"either completed or compensated and never something in between. " +
				"The program starts the server on 127.0.0.1 on a port the " +
				"operating system chooses and prints one line per step, in this " +
				"order: the completed steps of a successful order separated by " +
				"spaces, the status and message of an order whose shipping fails, " +
				"the word compensated followed by the compensations in the order " +
				"they ran, the words compensated once if running them again " +
				"changed nothing, the words resumed at followed by the step a " +
				"restarted saga continued from, and the word completed and its " +
				"count followed by the word compensated and its count. It then " +
				"shuts down and exits, writing nothing to standard error.",
			expected: "reserved charged shipped\n409 shipping failed\n" +
				"compensated refunded released\ncompensated once\n" +
				"resumed at inventory\ncompleted 1 compensated 1",
			minPackages: 5,
		},
		{
			name: "182 separates what it writes from what it reads",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. Commands append events to an event table in a real " +
				"SQLite database through database/sql with modernc.org/sqlite; a " +
				"projection builds the read model in its own tables. The " +
				"acceptance criteria are: a command is answered 202 once its " +
				"event is committed, before the read model has caught up, and the " +
				"response says so rather than pretending the change is visible; " +
				"the projection records how far it has read in a checkpoint row " +
				"updated in the same transaction as the rows it derives, so it " +
				"can never skip or double-apply an event; the read model can be " +
				"dropped and rebuilt from the events alone and comes back " +
				"identical; the lag between the last event and the checkpoint is " +
				"a query; an event the projection cannot apply is moved aside " +
				"with its error instead of stopping the projection forever. The " +
				"program starts the server and prints one line per step, in this " +
				"order: the status and message of a command, the status and count " +
				"read before the projection runs, the same after it runs, the " +
				"word rebuilt and the number of events replayed into a dropped " +
				"read model, the word lag and its value, the word quarantined and " +
				"the number of events set aside, and the word deterministic if " +
				"rebuilding twice produced the same read model. It then shuts " +
				"down and exits, writing nothing to standard error.",
			expected: "202 accepted\n200 0\n200 1\nrebuilt 3\nlag 0\n" +
				"quarantined 1\ndeterministic",
			minPackages: 5,
		},
		{
			name: "183 pushes events to subscribers and lets them catch up",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. Events are rows with increasing identifiers in a real " +
				"SQLite database through database/sql with modernc.org/sqlite, " +
				"and subscribers read them as a server-sent event stream. The " +
				"acceptance criteria are: every event is committed before it is " +
				"published, so a subscriber can never see an event that a " +
				"reconnect would then fail to find; each event carries its " +
				"identifier, and a subscriber reconnecting with Last-Event-ID is " +
				"sent what it missed from the table and then joined to the live " +
				"stream with no gap and no repeat; a subscriber too slow to keep " +
				"up is disconnected rather than allowed to block the publisher, " +
				"and the disconnection is counted; a heartbeat is sent on an idle " +
				"stream so a dead connection is noticed; shutting down closes " +
				"every stream cleanly. The program starts the server and prints " +
				"one line per step, in this order: the name of the first " +
				"subscriber and how many events it received, the same for the " +
				"second, the same for a third that connected late with a " +
				"Last-Event-ID of one, the word dropped and the number of slow " +
				"subscribers disconnected, the word heartbeat if an idle stream " +
				"received one, and the word closed and the number of streams shut " +
				"down. It then shuts down and exits, writing nothing to standard " +
				"error.",
			expected:    "a 3\nb 3\nc 2\ndropped 1\nheartbeat\nclosed 3",
			minPackages: 4,
		},
		{
			name: "184 upgrades a connection and speaks its own framing",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. One package, which must be pure, implements the " +
				"WebSocket handshake and framing itself, with no third-party " +
				"library: the accept key is the base64 of the SHA-1 of the client " +
				"key joined to the protocol's fixed string, and frames carry an " +
				"opcode, a mask and a length in the one-byte, two-byte or " +
				"eight-byte form. The handler takes the connection over with the " +
				"ResponseWriter's Hijack. The acceptance criteria are: the " +
				"handshake answers 101 only when the version, the upgrade and the " +
				"key are all present, and the accept key is computed rather than " +
				"echoed; a text frame from the client is unmasked and echoed back " +
				"unmasked, because a server must not mask; a ping is answered " +
				"with a pong carrying the same payload; a frame larger than the " +
				"agreed maximum is refused by closing with the code 1009 rather " +
				"than by allocating what it asked for; a client close is answered " +
				"with a close of 1000 and the connection is then shut. The " +
				"program starts the server and prints one line per step, in this " +
				"order: the handshake status and the accept key it returned for " +
				"the client key dGhlIHNhbXBsZSBub25jZQ==, the word echo and the " +
				"text that came back, the word pong, the word closed and the code " +
				"for the oversized frame, and the word closed and the code for " +
				"the normal close. It then shuts down and exits, writing nothing " +
				"to standard error.",
			expected: "101 s3pPLMBiTxaQ9kYGzzhZRbK+xOo=\necho hello\npong\n" +
				"closed 1009\nclosed 1000",
			minPackages:  3,
			mustAppear:   []string{"Hijack()"},
			purePackages: 1,
		},
		{
			name: "185 answers a batch item by item",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. Storage is a real SQLite database through " +
				"database/sql with modernc.org/sqlite. The acceptance criteria " +
				"are: a batch answers 207 with one status per item in the order " +
				"they were sent, because one status for the whole batch cannot " +
				"describe a partial result; in the default mode the items that " +
				"succeeded are kept even though others failed, each in its own " +
				"transaction; in atomic mode the whole batch is one transaction " +
				"and a single failure leaves nothing behind, answering 409; a " +
				"batch with more items than the declared maximum is refused 413 " +
				"before any of it is applied; two items in one batch claiming the " +
				"same identifier are not both applied, and the second is answered " +
				"409. The program starts the server and prints one line per step, " +
				"in this order: the batch status followed by each item's status " +
				"for a batch of a valid, an invalid and a valid item, the word " +
				"stored and the row count, the status of an oversized batch, the " +
				"batch status and item statuses for a batch containing a " +
				"duplicate, the word stored and the row count, the status and " +
				"message of an atomic batch containing one failure, and the word " +
				"stored and the row count again. It then shuts down and exits, " +
				"writing nothing to standard error.",
			expected: "207 201 400 201\nstored 2\n413 too large\n207 201 409\n" +
				"stored 3\n409 rolled back\nstored 3",
			minPackages: 4,
		},
		{
			name: "186 serves two versions of itself at once",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. One stored record is presented in two shapes: version " +
				"one holds a single name, version two holds a first and a last " +
				"name. Storage is a real SQLite database through database/sql " +
				"with modernc.org/sqlite and holds one shape, not two. The " +
				"acceptance criteria are: both versions are served at once from " +
				"the same rows, and the translation lives in the version packages " +
				"rather than in the store; a request that names no version gets " +
				"the oldest supported one, so an existing caller never changes " +
				"behaviour by standing still; the deprecated version answers with " +
				"a Deprecation header and a Sunset date; a version that was never " +
				"served is refused 400 and one that has passed its sunset is " +
				"answered 410, which are different facts; no handler branches on " +
				"the version inside itself. The program starts the server and " +
				"prints one line per step, in this order: the status and the " +
				"version one shape, the status and the version two shape, the " +
				"status and the version chosen when none was asked for, the names " +
				"of the deprecation headers the version one answer carried " +
				"separated by a space, the status and message for an unknown " +
				"version, and the status and message for one past its sunset. It " +
				"then shuts down and exits, writing nothing to standard error.",
			expected: "200 name=ann\n200 first=ann last=lee\n200 v1\n" +
				"Deprecation Sunset\n400 unknown version\n410 gone",
			minPackages: 6,
		},
		{
			name: "187 reports every failure in one machine-readable shape",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. One package, which must be pure, builds the error " +
				"document described by RFC 7807, holding a type, a title, the " +
				"status, a detail and the instance, and for a validation failure " +
				"an array of the fields at fault. The acceptance criteria are: " +
				"every route answers a failure through that one function, so no " +
				"handler invents its own error shape; the content type is " +
				"application/problem+json; the instance carries the request " +
				"identifier, which makes an error report traceable to a log line; " +
				"an unexpected error answers 500 with a document that names " +
				"nothing internal — no file path, no SQL, no driver text — while " +
				"the detail is written to the log; a validation failure lists " +
				"every field at fault rather than the first. The program starts " +
				"the server and prints one line per step, in this order: the " +
				"status of a validation failure followed by the number of fields " +
				"it listed and the word fields, the status and the word problem " +
				"for a missing resource, the same for a conflict, the status and " +
				"the words no leak if the 500 document contains no internal " +
				"detail, the content type the last answer carried, and the " +
				"instance the last document held. It then shuts down and exits, " +
				"writing nothing to standard error.",
			expected: "422 2 fields\n404 problem\n409 problem\n500 no leak\n" +
				"application/problem+json\ninstance=req-5",
			minPackages:  4,
			purePackages: 1,
		},
		{
			name: "188 publishes a contract that matches its own routes",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. The routes are declared once in a table that the " +
				"router and the specification are both built from. The acceptance " +
				"criteria are: the OpenAPI document is generated from that table " +
				"rather than written by hand, so it cannot describe a route that " +
				"does not exist; a route registered on the mux but absent from " +
				"the table is reported by name at startup, since that is the " +
				"direction the drift actually happens; a request is validated " +
				"against the declared schema before the handler sees it, and a " +
				"failure names the field; the answer is validated against the " +
				"declared response schema in tests, so a handler that returns a " +
				"shape the contract does not describe fails the suite; generating " +
				"the document twice produces identical bytes, which is what lets " +
				"it be committed and diffed. The program starts the server and " +
				"prints one line per step, in this order: the word paths and the " +
				"number the document describes, the word covered and the number " +
				"of registered routes the document covers, the word undocumented " +
				"and the number of registered routes it does not, the status and " +
				"the field named by a request that fails validation, the words " +
				"response valid if the answer matched its declared schema, and " +
				"the word stable if two generations were byte for byte the same. " +
				"It then shuts down and exits, writing nothing to standard error.",
			expected: "paths 4\ncovered 4\nundocumented 1\n400 name\n" +
				"response valid\nstable",
			minPackages: 4,
		},
		{
			name: "189 measures itself without measuring everything",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. One package implements a counter and a histogram " +
				"itself, safe for concurrent use, and renders them in the " +
				"Prometheus text exposition format. The acceptance criteria are: " +
				"every request is counted by route, method and status; the " +
				"duration histogram uses a declared set of buckets and its counts " +
				"are cumulative, which is what that format means; the route label " +
				"is the registered pattern and never the raw path, because " +
				"labelling by path turns one series into one per identifier and " +
				"eventually exhausts memory; a label value that has not been seen " +
				"before is dropped once the declared series limit is reached, and " +
				"the drop is itself counted; the exposition parses and the " +
				"counters read back match what was served. The program starts the " +
				"server and prints one line per step, in this order: the request " +
				"counter's name and value after three requests, the word buckets " +
				"and how many the histogram declares, the route label recorded " +
				"for a request to an item by identifier, the word metrics and the " +
				"number of distinct series exposed, the word dropped and the " +
				"number of series refused by the limit, and the status of the " +
				"exposition endpoint followed by the word exposition. It then " +
				"shuts down and exits, writing nothing to standard error.",
			expected: "http_requests_total 3\nbuckets 5\nroute=/items/{id}\n" +
				"metrics 4\ndropped 1\n200 exposition",
			minPackages: 3,
		},
		{
			name: "190 follows one request through three services",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. Three servers run in the same process on 127.0.0.1 on " +
				"ports the operating system chooses: a calls b, and b calls c. " +
				"One package, which must be pure, parses and renders the W3C " +
				"traceparent and tracestate headers itself. The acceptance " +
				"criteria are: the trace identifier is carried unchanged through " +
				"all three services while each creates its own span identifier " +
				"and records its parent, so the three spans form a chain and not " +
				"three unrelated traces; the sampling decision is taken once, at " +
				"the edge, and propagated rather than re-decided, since " +
				"re-deciding produces traces with holes in them; baggage set at " +
				"the edge is readable in the last service; a service that fails " +
				"records the error on its own span and the failure is " +
				"attributable to it; a malformed traceparent is replaced with a " +
				"new trace rather than propagated or rejected. The program starts " +
				"the three servers and prints one line per step, in this order: " +
				"the three service names in call order followed by the word same " +
				"if they all saw one trace identifier, the word depth and the " +
				"length of the parent chain, the word sampled and the decision " +
				"the last service saw, the baggage the last service read, the " +
				"words error at and the service that failed, and the status and " +
				"body of the successful call. It then shuts down and exits, " +
				"writing nothing to standard error.",
			expected: "a b c same\ndepth 3\nsampled true\ntenant=acme\n" +
				"error at c\n200 ok",
			minPackages:  3,
			purePackages: 1,
		},
		{
			name: "191 logs enough to debug and not enough to leak",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. That package must be pure. The acceptance criteria " +
				"are: a field named password, token, secret or authorization is " +
				"never written to the log, whatever nesting it appears at, and " +
				"the redaction happens in one place rather than at each call " +
				"site; an email address is written with the first character of " +
				"its local part kept and the rest of that part replaced by two " +
				"asterisks, so a log is still useful for support without holding " +
				"the address; debug lines are sampled at a declared rate and the " +
				"sampling is deterministic for a given request identifier, so a " +
				"sampled request keeps all of its lines rather than a scattering " +
				"of them; every line carries the request identifier; a failure to " +
				"write a log line never fails the request. The program starts the " +
				"server on 127.0.0.1 on a port the operating system chooses and " +
				"prints one line per step, in this order: the word redacted if a " +
				"request carrying a password produced no log line containing it, " +
				"the masked form of the address ann@example.test as it was " +
				"logged, the word logged and the number of debug lines kept out " +
				"of ten followed by the words of 10, the word correlated and the " +
				"number of lines carrying the request identifier for one request, " +
				"and the status and body of a request served while the log writer " +
				"was failing. It then shuts down and exits, writing nothing to " +
				"standard error.",
			expected: "redacted\na**@example.test\nlogged 1 of 10\ncorrelated 3\n" +
				"200 ok",
			minPackages:  4,
			purePackages: 1,
		},
		{
			name: "192 answers three different questions about its health",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. The service declares two dependencies: its SQLite " +
				"database, which is required, and a cache server running in the " +
				"same process, which is not. The acceptance criteria are: the " +
				"startup probe answers 503 until the first migration and the " +
				"first successful database query have both happened, and 200 " +
				"afterwards, never flapping back; the liveness probe reports only " +
				"whether the process is stuck, so a dependency being down does " +
				"not make it fail and does not get the process restarted for " +
				"something a restart cannot fix; the readiness probe fails while " +
				"a required dependency is down and names the dependency; with " +
				"only the optional dependency down the service serves reads and " +
				"refuses writes rather than refusing everything; each probe is " +
				"checked with a timeout of its own so a hanging dependency cannot " +
				"hang the probe. The program starts the server and prints one " +
				"line per step, in this order: the status and body of the startup " +
				"probe while warming, the same once warm, the status and body of " +
				"the liveness probe with the cache down, the status and the named " +
				"dependency from the readiness probe with the database down, the " +
				"status and the word read for a read while degraded, the status " +
				"and the word write for a write while degraded, and the status " +
				"and body of the readiness probe once everything is back. It then " +
				"shuts down and exits, writing nothing to standard error.",
			expected: "503 starting\n200 started\n200 alive\n503 cache\n200 read\n" +
				"503 write\n200 ready",
			minPackages: 4,
		},
		{
			name: "193 keeps one slow dependency from taking everything down",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. Two dependencies run as servers in the same process: " +
				"one fast, one deliberately slow. The acceptance criteria are: " +
				"each dependency is called through a pool of its own with a fixed " +
				"size, so requests waiting on the slow one cannot consume the " +
				"capacity the fast one needs; when a pool and its queue are both " +
				"full the request is refused at once with 503 rather than queued " +
				"indefinitely, because a queue nobody drains is an outage that " +
				"looks like latency; requests carry a priority and the low " +
				"priority ones are refused first, so the important traffic is " +
				"what survives; every dependency call has a timeout and one that " +
				"overruns is answered 504 and released, never left holding a " +
				"slot; where a dependency has a sensible default the service " +
				"answers from it rather than failing. The program starts the " +
				"servers and prints one line per step, in this order: the status " +
				"and body of a call to the fast dependency while the slow one is " +
				"saturated, the status and the word shed for a call that found " +
				"the queue full, the word shed followed by the number of " +
				"low-priority and the number of high-priority requests refused " +
				"under load, the status and message of a call that overran its " +
				"timeout, and the status and body of a call answered from the " +
				"fallback. It then shuts down and exits, writing nothing to " +
				"standard error.",
			expected: "200 fast\n503 shed\nshed low 3 high 0\n" +
				"504 upstream timeout\n200 fallback",
			minPackages: 3,
		},
		{
			name: "194 retries without turning a slowdown into an outage",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. Two backends run in the same process, one slow and " +
				"one fast. The acceptance criteria are: retries are drawn from a " +
				"budget expressed as a fraction of the successful requests, so a " +
				"backend that starts failing everything cannot be sent several " +
				"times the traffic that made it fail; once the budget is spent a " +
				"failure is returned rather than retried, and that is reported " +
				"rather than hidden; a request that has taken longer than a " +
				"declared threshold is hedged by sending a second one, the first " +
				"answer wins and the loser is cancelled through its context " +
				"rather than left running; a request that is not idempotent is " +
				"never retried or hedged, whatever the budget says, since a " +
				"duplicate charge is worse than a slow one; every delay is " +
				"jittered and stays within its declared bounds. The program " +
				"starts the servers and prints one line per step, in this order: " +
				"the word retries and the number made under the budget, the words " +
				"budget spent when it is exhausted, the word hedged and the " +
				"backend whose answer won, the words loser cancelled if the other " +
				"was cancelled rather than completed, the words no retry for a " +
				"failed non-idempotent request, and the words within bounds if " +
				"every delay fell inside the declared range. It then shuts down " +
				"and exits, writing nothing to standard error.",
			expected: "retries 1\nbudget spent\nhedged b\nloser cancelled\n" +
				"no retry\nwithin bounds",
			minPackages: 3,
		},
		{
			name: "195 changes version without dropping a request",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. Two instances of the service run in the same process, " +
				"one reporting the version blue and one green, behind a router " +
				"that decides which receives each request. The acceptance " +
				"criteria are: the share of traffic each version receives is set " +
				"at runtime and the split is exact rather than approximate over a " +
				"known number of requests; draining an instance stops it " +
				"receiving new requests immediately while the ones already in " +
				"flight run to completion and are answered; a drained instance is " +
				"only stopped once its in-flight count reaches zero or a declared " +
				"grace period expires; rolling back is the same operation in the " +
				"other direction and needs no restart of the router; a version " +
				"endpoint reports which instance answered, so the transcript can " +
				"prove where a request went. The program starts the router and " +
				"both instances and prints one line per step, in this order: the " +
				"status and version of a request while all traffic goes to blue, " +
				"the word blue and its count followed by the word green and its " +
				"count for ten requests under an even split, the status and body " +
				"of a request that was in flight when blue began draining, the " +
				"status and version of a request made after the drain, and the " +
				"status and version after rolling back. It then shuts down and " +
				"exits, writing nothing to standard error.",
			expected:    "200 blue\nblue 5 green 5\n200 done\n200 green\n200 blue",
			minPackages: 3,
		},
		{
			name: "196 takes new configuration and a new certificate while running",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. The server serves HTTPS with a certificate it " +
				"generates itself. The acceptance criteria are: configuration is " +
				"held behind an atomic value that readers take a snapshot of, so " +
				"a reload never shows a handler half of the old settings and half " +
				"of the new; a reload does not close existing connections; a new " +
				"document that fails validation is refused and the running " +
				"configuration is left exactly as it was, which is reported " +
				"rather than silently partially applied; the certificate is " +
				"fetched through the TLS configuration's callback rather than " +
				"fixed at start, so replacing it takes effect on the next " +
				"handshake with no restart; a connection established before the " +
				"rotation keeps working. The program starts the server and prints " +
				"one line per step, in this order: the word reloaded followed by " +
				"the word connections and the number still open across the " +
				"reload, the status and message of an invalid reload followed by " +
				"the words still and the version still in force, the word rotated " +
				"followed by the word serial and the serial number the next " +
				"handshake presented, and the status and body of a request made " +
				"over the connection that predates the rotation. It then shuts " +
				"down and exits, writing nothing to standard error.",
			expected: "reloaded connections 1\n422 invalid still v1\n" +
				"rotated serial 2\n200 ok",
			minPackages: 4,
			mustAppear:  []string{"GetCertificate"},
		},
		{
			name: "197 gives a person their data back and then forgets them",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. Storage is a real SQLite database through " +
				"database/sql with modernc.org/sqlite holding a person's account " +
				"and the rows that reference it. The acceptance criteria are: an " +
				"export gathers every row about the person by walking the " +
				"declared relations, so a table added later without being " +
				"declared is reported rather than quietly omitted; a deletion " +
				"removes them all in one transaction and the cascade is enforced " +
				"by foreign keys, not by a list in Go; a person under legal hold " +
				"cannot be deleted and the refusal says so with 423; a retention " +
				"policy purges rows past their declared age on a schedule and " +
				"reports how many; the audit record of a deletion survives it, " +
				"holding who asked and when but nothing about the person; an " +
				"export after a deletion returns nothing at all. The program " +
				"starts the server and prints one line per step, in this order: " +
				"the word export and the number of records returned, the status " +
				"and message of deleting a person under legal hold, the word " +
				"deleted and the number of rows removed once the hold is lifted, " +
				"the word purged and the number the retention policy removed, the " +
				"word audit and the number of surviving audit rows followed by " +
				"the words no pii if none holds personal data, and the word " +
				"export and the number returned afterwards. It then shuts down " +
				"and exits, writing nothing to standard error.",
			expected: "export 3\n423 legal hold\ndeleted 3\npurged 2\n" +
				"audit 1 no pii\nexport 0",
			minPackages: 4,
		},
		{
			name: "198 lets an operator act without letting them hide",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. Storage is a real SQLite database through " +
				"database/sql with modernc.org/sqlite. The acceptance criteria " +
				"are: an operator can search accounts and act on one, and every " +
				"administrative action writes an audit row in the same " +
				"transaction as its effect; impersonation issues a session that " +
				"carries both the operator and the impersonated account, and the " +
				"answer says on whose behalf it acted; an impersonated session " +
				"has the impersonated account's permissions and never the " +
				"operator's, so impersonation cannot be used to escalate; a " +
				"suspended account is refused everywhere except the routes that " +
				"explain the suspension; a quota override is recorded with who " +
				"granted it and takes effect immediately; the audit rows cannot " +
				"be written by the same route that reads them. The program starts " +
				"the server and prints one line per step, in this order: the word " +
				"found and the number of accounts the search returned, the status " +
				"and the words as and the impersonated name, the word audited if " +
				"that action wrote its audit row, the status and message of an " +
				"administrative action attempted from the impersonated session, " +
				"the status and message of a request from the suspended account, " +
				"the status and the new limit after an override, and the word " +
				"audit and the number of audit rows the journey wrote. It then " +
				"shuts down and exits, writing nothing to standard error.",
			expected: "found 2\n200 as ann\naudited\n403 admin action\n" +
				"403 suspended\n200 100\naudit 4",
			minPackages: 5,
		},
		{
			name: "199 refuses the requests that are trying it on",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. That package must be pure. The acceptance criteria " +
				"are: the header size and count are bounded by the server's own " +
				"settings and an oversized set is answered 431 rather than " +
				"buffered; a client that sends its headers a byte at a time is " +
				"disconnected by a read-header timeout instead of holding a " +
				"connection forever; a name is normalised to one Unicode form and " +
				"case-folded before uniqueness is decided, so two names that " +
				"render identically cannot both be registered; a path that tries " +
				"to escape the served directory is refused whether it is written " +
				"plainly, escaped once or escaped twice; a URL the service is " +
				"asked to fetch is resolved and refused if it points at a private " +
				"or loopback address, and the check is made on the address " +
				"actually dialled rather than on the text of the URL; a value " +
				"holding a null byte is refused rather than truncated. The " +
				"program starts the server and prints one line per step, in this " +
				"order: the status and message for an oversized header, the same " +
				"for too many headers, the words closed slow if the dribbling " +
				"client was disconnected, the status and message of registering a " +
				"name that normalises onto an existing one, the status of a " +
				"traversal attempt, the status and message of a fetch of a " +
				"private address, and the status and message of a value " +
				"containing a null byte. It then shuts down and exits, writing " +
				"nothing to standard error.",
			expected: "431 header too large\n431 too many headers\nclosed slow\n" +
				"409 taken\n404\n403 blocked\n400 null byte",
			minPackages:  4,
			purePackages: 1,
		},
		{
			name: "200 runs the whole platform end to end",
			requirement: "Write a program in the module codeflux.test/workspace. The " +
				"layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. This is every rung of this band assembled into one " +
				"service. Package domain is pure and may not import net/http or " +
				"database/sql. Storage is a real SQLite database through " +
				"database/sql with modernc.org/sqlite in a temporary directory of " +
				"its own, in write-ahead-log mode with foreign keys on, its " +
				"schema applied by four embedded migrations at startup, every " +
				"statement parameterised. The service is multi-tenant with the " +
				"tenant resolved from a header and carried on every row; it " +
				"authorises bearer tokens; it enforces a per-tenant rate limit " +
				"and a daily quota, which are different refusals; it serves a " +
				"JSON resource with optimistic concurrency through ETags; it " +
				"writes an outbox event in the same transaction as every change " +
				"and delivers it to a receiver running in the same process; it " +
				"keeps an FTS5 index in step with the rows by triggers; it " +
				"appends a hash-chained audit entry for every change; it exposes " +
				"metrics and a readiness probe; it propagates a trace across its " +
				"own internal call; and it shuts down gracefully. The router is " +
				"tested with net/http/httptest as well as being driven live. The " +
				"program prints one line per step, in this order: the word " +
				"migrated and the number applied; the status and message of a " +
				"request with no token; the status and the word tenant and the " +
				"tenant a valid request resolved to; the status and identifier of " +
				"creating a note; the status and name of reading it; the status " +
				"and message of updating it with a stale version; the status and " +
				"the new version of updating it with the current one; the status " +
				"and message of reading it as another tenant; the status and " +
				"Retry-After of a rate-limited request; the status and message of " +
				"a request past the daily quota; the word outbox and the number " +
				"of undelivered events after delivery; the word search and the " +
				"number of documents matching the note; the content encoding and " +
				"status of a gzipped read; the words audit chain intact if the " +
				"chain verifies; the word export and the number of rows exported " +
				"for the tenant; the word metrics and the number of series " +
				"exposed; the words trace propagated if the internal call carried " +
				"the trace identifier; the words backup integrity ok after a " +
				"backup is taken and checked; the status and body of the " +
				"readiness probe; and the words shutdown complete. It removes its " +
				"directory and exits, writing nothing to standard error.",
			expected: "migrated 4\n401 missing token\n200 tenant acme\n201 1\n" +
				"200 ann\n409 conflict\n200 2\n404 not found\n429 1\n" +
				"402 quota exceeded\noutbox 0\nsearch 1\ngzip 200\n" +
				"audit chain intact\nexport 2\nmetrics 6\ntrace propagated\n" +
				"backup integrity ok\n200 ready\nshutdown complete",
			minPackages:  10,
			mustAppear:   []string{"httptest.", "journal_mode", "foreign_keys"},
			purePackages: 1,
		},
		// From here the difficulty is somebody else's code.
		//
		// Everything above is written from nothing, so it can be as clean as the
		// run cares to make it. Real work is not: it is a program held together
		// by libraries that print when they feel like it, keep global state,
		// start goroutines, write files, return their own error types and change
		// shape between versions. That code cannot be pure, and asking it to be
		// is how a ladder stops describing the job.
		//
		// What is asked for instead is containment. A library may be reachable
		// from one package — the adapter — and the rest of the program talks to
		// an interface the run declared rather than to the library's types. Then
		// the answer has to come out the same twice, and nothing may be left on
		// disk afterwards. Those three are what keep integration code from
		// becoming the program, and none of them require the code to be tidy.
		{
			name: "201 puts its first dependency behind something it owns",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours: nothing here names a file, a package or a " +
				"function, and how the work is grouped is part of what is being " +
				"asked for. It generates identifiers with github.com/google/uuid, " +
				"which may be imported by exactly one package; everything else " +
				"asks an interface the run declares for an identifier and cannot " +
				"tell what produces them. The acceptance criteria are: a hundred " +
				"generated identifiers are all distinct and thirty-six characters " +
				"long; the same code path, given a generator that returns a fixed " +
				"value, produces that value, which is what having the interface " +
				"is for; a well-formed identifier parses and a malformed one is " +
				"refused with an error rather than a zero value; no package but " +
				"the adapter mentions the library or its types. The program " +
				"prints one line per step, in this order: the length of a " +
				"generated identifier followed by the word characters, the word " +
				"distinct and how many of a hundred were unique, the word fixed " +
				"and what the substituted generator produced, the word parsed and " +
				"the identifier read back, and the word refused for the malformed " +
				"one. It writes nothing to standard error and leaves nothing on " +
				"disk.",
			expected: "36 characters\ndistinct 100\n" +
				"fixed 11111111-1111-1111-1111-111111111111\n" +
				"parsed 11111111-1111-1111-1111-111111111111\nrefused",
			minPackages:   3,
			minInterfaces: 1,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "github.com/google/uuid", packages: 1},
			},
		},
		{
			name: "202 reads a configuration file it did not write",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It decodes YAML with gopkg.in/yaml.v3, " +
				"which may be imported by exactly one package: the rest of the " +
				"program receives a settings value of the run's own design and " +
				"never one of the library's nodes. The acceptance criteria are: a " +
				"document is decoded into typed settings and a missing field " +
				"takes a declared default rather than a zero value; a field the " +
				"settings do not declare is refused by name instead of ignored, " +
				"because a typo in a configuration file that starts anyway is a " +
				"defect that surfaces hours later somewhere else; a document that " +
				"is not valid YAML is refused with the line it failed on; a value " +
				"of the wrong type is refused naming the field; encoding the " +
				"settings again and decoding them returns the same value. The " +
				"program prints one line per step, in this order: the word loaded " +
				"with the decoded name and port, the word default and the port a " +
				"document that omits it receives, the word unknown and the field " +
				"that was refused, the word invalid and the line a malformed " +
				"document failed on, the word wrong and the field whose type did " +
				"not match, and the words round trip identical. It writes nothing " +
				"to standard error and leaves nothing on disk.",
			expected: "loaded ann 9090\ndefault 8080\nunknown colour\n" +
				"invalid line 3\nwrong port\nround trip identical",
			minPackages:   3,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "gopkg.in/yaml.v3", packages: 1},
			},
		},
		{
			name: "203 exposes itself as a command with subcommands",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It builds its command surface with " +
				"github.com/spf13/cobra, which may be imported by exactly one " +
				"package: the work each subcommand does lives elsewhere and is " +
				"callable without cobra, so it can be tested without building a " +
				"command. The acceptance criteria are: there are three " +
				"subcommands and each declares its own flags; a subcommand's work " +
				"is a function taking its arguments and returning a result and an " +
				"error, never one that prints and exits; an unknown subcommand is " +
				"refused with a non-zero code and a message naming it; a flag " +
				"with a bad value is refused before any work happens; the command " +
				"writes to a writer the caller supplies rather than to standard " +
				"output directly, which is what makes the transcript below " +
				"possible and what makes the command testable at all. The program " +
				"drives its own command surface in-process and prints one line " +
				"per step, in this order: the word commands and how many are " +
				"registered, the result of adding 2 and 3 through the add " +
				"subcommand, the word verbose and the flag's value once it is " +
				"set, the word unknown and the subcommand that was refused, and " +
				"the word refused and the flag that failed to parse. It writes " +
				"nothing to standard error and leaves nothing on disk.",
			expected: "commands 3\nadd 5\nverbose true\nunknown flavour\n" +
				"refused --count",
			minPackages:   3,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "github.com/spf13/cobra", packages: 1},
			},
		},
		{
			name: "204 settles where each setting came from",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It resolves configuration with " +
				"github.com/spf13/viper, which may be imported by exactly one " +
				"package, and hands the rest of the program a plain settings " +
				"value. The acceptance criteria are: the precedence is declared " +
				"and observed — a flag beats an environment variable, which beats " +
				"a file, which beats a declared default; removing the winning " +
				"source falls back to the next one rather than to zero; the " +
				"resolved settings can say where each value came from, because a " +
				"service whose port is wrong and cannot say why is a service " +
				"nobody can operate; the library's package-level instance is not " +
				"used, since a global makes two tests in one binary fight over " +
				"one configuration; the file it reads is written into a temporary " +
				"directory of its own and removed. The program prints one line " +
				"per step, in this order: the port and where it came from with " +
				"only the default set, then with a file added, then with an " +
				"environment variable added, then with a flag added, and finally " +
				"after the flag and the variable are taken away again. It writes " +
				"nothing to standard error and leaves nothing on disk.",
			expected:      "8080 default\n9090 file\n7070 env\n6060 flag\n9090 file",
			minPackages:   3,
			deterministic: true,
			leavesNothing: true,
			mustNotAppear: []string{"viper.Get", "viper.Set", "viper.ReadIn"},
			containedImports: []containment{
				{importPath: "github.com/spf13/viper", packages: 1},
			},
		},
		{
			name: "205 logs through a hole of its own shape",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It logs with go.uber.org/zap, which may be " +
				"imported by exactly one package. Everything else logs through an " +
				"interface the run declares, taking a message and typed fields, " +
				"and no other package may mention the library's types. The " +
				"acceptance criteria are: a line is written as JSON holding the " +
				"message, the level and the fields; the level is configurable and " +
				"a line below it appears nowhere and costs nothing to skip; a " +
				"field whose name marks it as a secret is written redacted by the " +
				"adapter rather than by each caller remembering to; the logger " +
				"writes to a writer the caller supplies, so the transcript can be " +
				"read back and parsed; the logger is flushed before the program " +
				"exits, because a buffered logger that is never flushed loses " +
				"exactly the lines written just before a crash. The program " +
				"prints one line per step, in this order: the word logged and the " +
				"level of the line it read back, the word fields and how many that " +
				"line carried, the word dropped and the level that was filtered " +
				"out, the value the secret field was written as, and the word " +
				"flushed. It writes nothing to standard error and leaves nothing " +
				"on disk.",
			expected:      "logged info\nfields 3\ndropped debug\nredacted\nflushed",
			minPackages:   3,
			minInterfaces: 1,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "go.uber.org/zap", packages: 1},
			},
		},
		{
			name: "206 swaps one library for another and moves nothing else",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It logs through one interface of the run's " +
				"own declaring, with two adapters behind it: one on " +
				"go.uber.org/zap and one on github.com/rs/zerolog. Each library " +
				"may be imported by exactly one package, and those two packages " +
				"are the only places either name appears. The acceptance criteria " +
				"are: the same calling code produces the same record through " +
				"either adapter — the same message, the same level and the same " +
				"field names, whatever shape each library would have chosen; " +
				"which one is used is decided once, where the program is " +
				"assembled, and no caller can tell which it got; both adapters " +
				"pass the same test, written once against the interface; a field " +
				"the interface does not describe cannot be logged through it, " +
				"which is what stops the two drifting apart. The program prints " +
				"one line per step, in this order: the level and message read back " +
				"from the first adapter, the same from the second, the word " +
				"identical if the two records agree field for field, and the word " +
				"both if the shared test passed against each. It writes nothing " +
				"to standard error and leaves nothing on disk.",
			expected:      "info ann\ninfo ann\nidentical\nboth",
			minPackages:   4,
			minInterfaces: 1,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "go.uber.org/zap", packages: 1},
				{importPath: "github.com/rs/zerolog", packages: 1},
			},
		},
		{
			name: "207 keeps its test library out of the program",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. Its tests are written with " +
				"github.com/stretchr/testify, which may appear in test files " +
				"only: no package the program builds may import it, because a " +
				"test dependency that reaches the program ships an assertion " +
				"library to production. The acceptance criteria are: the work " +
				"itself is a calculation over its arguments, in a package that " +
				"reaches nothing outside itself; the tests are table-driven with " +
				"each case named, so a failure names the case rather than a line " +
				"number; the table covers the boundaries the requirement lists " +
				"and asserts errors by type rather than by their message text. " +
				"The calculation splits a bill of a whole number of cents evenly " +
				"between n people, giving the remainder out one cent at a time to " +
				"the earliest shares, and refuses a zero or negative n. The " +
				"program prints one line per step, in this order: the shares of " +
				"100 between 3 separated by spaces, the shares of 100 between 1, " +
				"the word refused for a split between zero, the word sums if " +
				"every split it tried adds back to the total it started from, and " +
				"the word cases and how many the table holds. It writes nothing " +
				"to standard error and leaves nothing on disk.",
			expected:          "34 33 33\n100\nrefused\nsums\ncases 6",
			minPackages:       3,
			purePackages:      1,
			deterministic:     true,
			leavesNothing:     true,
			mustAppearInTests: []string{"testify"},
			containedImports: []containment{
				{importPath: "github.com/stretchr/testify", packages: 0},
			},
		},
		{
			name: "208 says exactly how two values differ",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It compares values with " +
				"github.com/google/go-cmp, which may be imported by exactly one " +
				"package. The acceptance criteria are: two records differing in " +
				"one field produce a difference naming that field rather than " +
				"printing both values whole; a field the comparison is told to " +
				"ignore does not appear in the difference; unexported fields are " +
				"handled by a declared option rather than by the panic the " +
				"library raises when it meets them unprepared; a value compared " +
				"with itself produces nothing at all; the comparison is exposed " +
				"as a function returning a description, so nothing else needs the " +
				"library to ask whether two values are the same. The program " +
				"prints one line per step, in this order: the word same for a " +
				"value compared with itself, the word differs and the field that " +
				"did, the word ignored and the field the option excluded, the " +
				"word unexported followed by ok if comparing them succeeded, and " +
				"the word differences and how many were found between two lists. " +
				"It writes nothing to standard error and leaves nothing on disk.",
			expected: "same\ndiffers Port\nignored Updated\nunexported ok\n" +
				"differences 2",
			minPackages:   3,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "github.com/google/go-cmp", packages: 1},
			},
		},
		{
			name: "209 runs work in parallel and stops it all together",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It runs concurrent work with " +
				"golang.org/x/sync, which may be imported by exactly one package: " +
				"callers hand that package a list of jobs and receive results and " +
				"an error. The acceptance criteria are: every job runs " +
				"concurrently and the group waits for all of them; the first " +
				"failure cancels the context the others run under and they stop " +
				"rather than running to completion for nothing; the error " +
				"returned is that first failure, and the cancellations that " +
				"followed are not reported as failures of their own; the number " +
				"running at once is bounded and the bound holds; no goroutine " +
				"outlives the call, which the tests assert with " +
				"go.uber.org/goleak. Each job takes about five milliseconds. The " +
				"program prints one line per step, in this order: the word " +
				"completed and how many of ten jobs finished when all succeed, " +
				"the word peak and the greatest number that ran at once, the word " +
				"failed and the message of the first failure, the word stopped " +
				"and how many jobs saw the cancellation, and the word clean if no " +
				"goroutine was left behind. It writes nothing to standard error " +
				"and leaves nothing on disk.",
			expected: "completed 10\npeak 3\nfailed job 4 refused\nstopped 6\n" +
				"clean",
			minPackages:       3,
			minInterfaces:     1,
			deterministic:     true,
			leavesNothing:     true,
			mustAppearInTests: []string{"goleak."},
			containedImports: []containment{
				{importPath: "golang.org/x/sync", packages: 1},
				{importPath: "go.uber.org/goleak", packages: 0},
			},
		},
		{
			name: "210 does the same expensive thing once",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It collapses duplicate work with " +
				"golang.org/x/sync, which may be imported by exactly one package. " +
				"The acceptance criteria are: a hundred concurrent callers asking " +
				"for one key cause the underlying work to run once and all " +
				"hundred receive that result; callers asking for different keys " +
				"do not wait for each other; a failure is delivered to every " +
				"caller that shared the call and is not remembered, so the next " +
				"caller tries again; the key is derived from the request rather " +
				"than supplied by the caller, because two callers who disagree " +
				"about the key get two calls; the shared result cannot be " +
				"modified by one caller in a way the others can see. The program " +
				"prints one line per step, in this order: the word callers and " +
				"how many received an answer, the word calls and how many times " +
				"the underlying work ran, the word keys and how many distinct " +
				"ones ran concurrently, the word failed and how many callers " +
				"received the shared failure, and the word retried if the call " +
				"after that failure ran again. It writes nothing to standard " +
				"error and leaves nothing on disk.",
			expected:      "callers 100\ncalls 1\nkeys 4\nfailed 100\nretried",
			minPackages:   3,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "golang.org/x/sync", packages: 1},
			},
		},
		{
			name: "211 lets only so much happen at once",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It bounds concurrency with a weighted " +
				"semaphore from golang.org/x/sync, which may be imported by " +
				"exactly one package. The acceptance criteria are: work is " +
				"admitted up to a declared total weight and heavier work takes " +
				"more of it, so three light jobs and one heavy job cannot both be " +
				"admitted; a caller that cannot be admitted waits rather than " +
				"failing, and is admitted as soon as there is room; every " +
				"acquisition is released even when the work fails, which means " +
				"the release is deferred and not written at the end of the happy " +
				"path; a caller whose context is cancelled while waiting gives up " +
				"and does not take a slot afterwards; asking for more than the " +
				"total weight is refused at once rather than waiting forever. " +
				"Each job takes about five milliseconds. The program prints one " +
				"line per step, in this order: the word admitted and how many of " +
				"ten jobs were admitted without waiting, the word waited and how " +
				"many had to, the word peak and the greatest weight held at once, " +
				"the word released and how many releases happened, the word " +
				"cancelled for a caller that gave up waiting, and the word " +
				"refused for one that asked for more than exists. It writes " +
				"nothing to standard error and leaves nothing on disk.",
			expected: "admitted 3\nwaited 7\npeak 3\nreleased 10\ncancelled\n" +
				"refused",
			minPackages:   3,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "golang.org/x/sync", packages: 1},
			},
		},
		{
			name: "212 stores a password the way the library intends",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It derives password verifiers with " +
				"golang.org/x/crypto, which may be imported by exactly one " +
				"package; the rest of the program asks an interface to derive and " +
				"to verify and never sees a hash. The acceptance criteria are: " +
				"the same password hashed twice gives two different verifiers, " +
				"because the salt is part of what is stored; verification " +
				"succeeds against either of them; a wrong password is refused, " +
				"and the refusal is a false answer rather than an error, since a " +
				"caller that cannot tell those apart writes the wrong branch; the " +
				"cost is declared rather than left to the library's default and " +
				"can be read back out of an existing verifier, so a stored one " +
				"can be found too cheap and re-derived at the next successful " +
				"login; something that is not a verifier at all is refused with " +
				"an error, not a false. The program prints one line per step, in " +
				"this order: the word length and how many bytes a verifier is, " +
				"the word distinct if two derivations of one password differed, " +
				"the word verified, the word rejected for the wrong password, the " +
				"word cost and the cost read back, and the word malformed. It " +
				"writes nothing to standard error and leaves nothing on disk.",
			expected: "length 60\ndistinct\nverified\nrejected\ncost 10\n" +
				"malformed",
			minPackages:   3,
			minInterfaces: 1,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "golang.org/x/crypto", packages: 1},
			},
		},
		{
			name: "213 seals data so that changing it is noticed",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It derives a key with Argon2 and seals data " +
				"with a secretbox, both from golang.org/x/crypto, which may be " +
				"imported by exactly one package. The acceptance criteria are: " +
				"the key is derived from a passphrase and a salt with declared " +
				"parameters, and deriving twice from the same inputs gives the " +
				"same key; the nonce is fresh for every sealing and stored beside " +
				"the ciphertext, because reusing one under the same key destroys " +
				"the guarantee entirely; sealing one plaintext twice gives two " +
				"different ciphertexts; opening returns the original bytes; a " +
				"ciphertext with one byte changed is refused rather than " +
				"returning damaged plaintext, and so is one whose nonce was " +
				"changed; neither the passphrase nor the key is ever printed. The " +
				"program prints one line per step, in this order: the word key " +
				"and the derived length in bytes, the word deterministic if " +
				"deriving twice agreed, the word distinct if two sealings " +
				"differed, the word opened and the recovered text, the word " +
				"tampered for the altered ciphertext, and the word nonce for the " +
				"altered nonce. It writes nothing to standard error and leaves " +
				"nothing on disk.",
			expected: "key 32\ndeterministic\ndistinct\nopened alpha\ntampered\n" +
				"nonce",
			minPackages:   3,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "golang.org/x/crypto", packages: 1},
			},
		},
		{
			name: "214 decides when two names are the same name",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It normalises text and matches languages " +
				"with golang.org/x/text, which may be imported by exactly one " +
				"package. The acceptance criteria are: two spellings of one word " +
				"differing only in Unicode composition are the same name, which " +
				"means normalising to one form before comparing rather than " +
				"comparing bytes; a word using a character that merely looks like " +
				"another is not the same name, so normalisation is not mistaken " +
				"for a defence against confusable characters; the normalised form " +
				"is what gets stored, so the comparison cannot disagree with the " +
				"index; a requested language is matched against those supported " +
				"and the best is chosen, falling back to the first supported when " +
				"none matches; a language tag is canonicalised rather than " +
				"trusted as written. The program prints one line per step, in " +
				"this order: the word equal for the two compositions, the word " +
				"different for the confusable pair, the word stored and the " +
				"length in bytes of what was stored, the word matched and the " +
				"base of the language chosen for a request of fr-CA against " +
				"English and French, the word fallback and the base chosen for a " +
				"request of German, and the word canonical and the canonical form " +
				"of the tag EN-us. It writes nothing to standard error and leaves " +
				"nothing on disk.",
			expected: "equal\ndifferent\nstored 5\nmatched fr\nfallback en\n" +
				"canonical en-US",
			minPackages:   3,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "golang.org/x/text", packages: 1},
			},
		},
		{
			name: "215 sorts names the way a reader expects",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It sorts text with a collator from " +
				"golang.org/x/text, which may be imported by exactly one package; " +
				"the rest of the program is handed a comparison function and does " +
				"not know where it came from. The acceptance criteria are: the " +
				"collated order differs from the byte order for the same input, " +
				"which is the entire reason the library is there; a lower-case " +
				"letter sorts before the upper-case form of a later letter, " +
				"rather than after every upper-case letter as byte order would " +
				"have it; an accented letter sorts beside its unaccented form " +
				"rather than after every unaccented word; the collator is built " +
				"once and reused, because building one per comparison is the " +
				"performance defect this library is known for; the sort is " +
				"stable, so equal keys keep their input order. The program prints " +
				"one line per step, in this order: the word differ if collating " +
				"changed the order, the words a before B, the word beside if the " +
				"accented form landed next to its unaccented one, the word " +
				"collators and how many were built, and the word stable if equal " +
				"keys kept their order. It writes nothing to standard error and " +
				"leaves nothing on disk.",
			expected:      "differ\na before B\nbeside\ncollators 1\nstable",
			minPackages:   3,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "golang.org/x/text", packages: 1},
			},
		},
		{
			name: "216 compresses with somebody else's compressor",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It compresses with " +
				"github.com/klauspost/compress, which may be imported by exactly " +
				"one package; the rest of the program is handed a reader and a " +
				"writer. The acceptance criteria are: compressing and " +
				"decompressing a repetitive input returns exactly the input " +
				"bytes; the compressed form is smaller; a higher level produces " +
				"something no larger than a lower one for that input; the data is " +
				"streamed rather than buffered whole, so the memory used does not " +
				"grow with the input; the writer is closed before the compressed " +
				"bytes are read, because a compressor that is never closed " +
				"silently truncates its last block; a truncated compressed stream " +
				"is refused with an error rather than returning what it managed " +
				"to read. The program prints one line per step, in this order: " +
				"the word identical if the round trip returned the input, the " +
				"word smaller if it compressed, the word levels if the higher " +
				"level was no larger, the word streamed and the number of bytes " +
				"handled, and the word truncated for the damaged stream. It " +
				"writes nothing to standard error and leaves nothing on disk.",
			expected: "identical\nsmaller\nlevels\nstreamed 1000000\n" +
				"truncated",
			minPackages:   3,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "github.com/klauspost/compress", packages: 1},
			},
		},
		{
			name: "217 encodes to a format it did not design",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It encodes with github.com/fxamacker/cbor, " +
				"which may be imported by exactly one package. The acceptance " +
				"criteria are: a record round-trips through the encoding " +
				"unchanged, including a map, a byte string and an optional field " +
				"that is absent; the encoding is canonical, so encoding one value " +
				"twice gives the same bytes and two maps holding the same entries " +
				"in different orders encode identically — without that, a " +
				"signature over the encoding is worthless; the encoded form is " +
				"smaller than the equivalent JSON; a field the decoder does not " +
				"know is refused when strictness is asked for and kept when it is " +
				"not, and which applies is the program's decision rather than the " +
				"library's default; a truncated encoding is refused with an error " +
				"and does not panic. The program prints one line per step, in " +
				"this order: the word identical for the round trip, the word " +
				"canonical if two encodings of equal values matched byte for " +
				"byte, the word smaller if it beat JSON, the word strict followed " +
				"by what happened to the unknown field, and the word truncated. " +
				"It writes nothing to standard error and leaves nothing on disk.",
			expected: "identical\ncanonical\nsmaller\nstrict refused\n" +
				"truncated",
			minPackages:   3,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "github.com/fxamacker/cbor", packages: 1},
			},
		},
		{
			name: "218 signs and encrypts with a library that offers both",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It signs and encrypts with " +
				"github.com/go-jose/go-jose, which may be imported by exactly one " +
				"package; the rest of the program hands over claims and receives " +
				"claims. The acceptance criteria are: a signed token verifies " +
				"against the public key and its claims come back; a token whose " +
				"payload was altered fails verification; a token declaring an " +
				"algorithm other than the one expected is refused before any key " +
				"is applied, because accepting whatever the token names is how " +
				"algorithm confusion works; a token carrying no signature at all " +
				"is refused; an encrypted token decrypts to the same claims and " +
				"tells a holder without the key nothing about them. The program " +
				"prints one line per step, in this order: the word signed, the " +
				"word verified and the subject that came back, the word tampered, " +
				"the word algorithm for the one declaring another, the word " +
				"unsigned, and the word decrypted with the subject recovered from " +
				"the encrypted token. It writes nothing to standard error and " +
				"leaves nothing on disk.",
			expected: "signed\nverified ann\ntampered\nalgorithm\nunsigned\n" +
				"decrypted ann",
			minPackages:   3,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "github.com/go-jose/go-jose", packages: 1},
			},
		},
		{
			name: "219 holds a socket open through somebody else's library",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It speaks WebSocket with " +
				"github.com/gorilla/websocket, which may be imported by exactly " +
				"one package: the rest of the program sends and receives messages " +
				"through an interface the run declares and never touches a " +
				"connection. It starts a server on 127.0.0.1 on a port the " +
				"operating system chooses, connects to itself and shuts down. The " +
				"acceptance criteria are: the upgrade succeeds and a text message " +
				"is echoed back; a ping is answered with a pong carrying the same " +
				"payload; a message larger than the declared read limit closes the " +
				"connection with the code meaning the message was too big, rather " +
				"than allocating what was asked for; the closing handshake is " +
				"completed in both directions rather than the socket simply being " +
				"dropped; every read carries a deadline, so a peer that stops " +
				"talking does not hold a goroutine forever; the tests assert with " +
				"go.uber.org/goleak that nothing was left running. The program " +
				"prints one line per step, in this order: the word connected, the " +
				"word echo and the text that came back, the word pong, the word " +
				"closed and the code for the oversized message, the word closed " +
				"and the code for the normal close, and the word clean. It writes " +
				"nothing to standard error and leaves nothing on disk.",
			expected: "connected\necho hello\npong\nclosed 1009\nclosed 1000\n" +
				"clean",
			minPackages:       3,
			minInterfaces:     1,
			deterministic:     true,
			leavesNothing:     true,
			mustAppearInTests: []string{"goleak."},
			containedImports: []containment{
				{importPath: "github.com/gorilla/websocket", packages: 1},
				{importPath: "go.uber.org/goleak", packages: 0},
			},
		},
		{
			name: "220 routes through a router it did not write",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It routes with github.com/go-chi/chi, which " +
				"may be imported by exactly one package: the handlers are " +
				"ordinary functions of the standard library's request and " +
				"response types, so they can be tested without the router and " +
				"would survive replacing it. It starts a server on 127.0.0.1 on a " +
				"port the operating system chooses, drives itself and shuts down. " +
				"The acceptance criteria are: routes are grouped, and a " +
				"middleware attached to a group runs for that group only; a URL " +
				"parameter is read through the router's own accessor in the one " +
				"package that knows about it and passed onward as a plain value; " +
				"an unknown path is answered 404 and a known path with the wrong " +
				"method is answered 405 with an Allow header, which is the " +
				"router's job and worth checking it does; middleware order is " +
				"declared and observed. The program prints one line per step, in " +
				"this order: the status and body of a route outside the group, " +
				"the status and body of one inside it carrying a parameter, the " +
				"status of an unknown path, the status and Allow header for the " +
				"wrong method, and the word middleware and how many ran for the " +
				"grouped route. It writes nothing to standard error and leaves " +
				"nothing on disk.",
			expected:      "200 alpha\n200 item 7\n404\n405 GET\nmiddleware 2",
			minPackages:   3,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "github.com/go-chi/chi", packages: 1},
			},
		},
		{
			name: "221 measures itself with a client it does not own",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It records metrics with " +
				"github.com/prometheus/client_golang, which may be imported by " +
				"exactly one package: the rest of the program increments and " +
				"observes through an interface the run declares. It starts a " +
				"server on 127.0.0.1 on a port the operating system chooses, " +
				"drives itself and shuts down. The acceptance criteria are: " +
				"metrics are registered against a registry the program created " +
				"rather than the library's default one, because the default is " +
				"global state that makes two of anything in one process collide; " +
				"a counter and a histogram are registered once and reused, since " +
				"registering the same name twice panics and doing it per request " +
				"is the usual way to find that out in production; the route " +
				"label is the registered pattern and never the raw path; the " +
				"exposition endpoint's output parses and the counter read back " +
				"matches the requests served; registering a duplicate collector " +
				"is handled and reported rather than allowed to panic. The " +
				"program prints one line per step, in this order: the word " +
				"requests and the counter read back after three, the word buckets " +
				"and how many the histogram declares, the word label and the " +
				"route label recorded for a request to an item by identifier, the " +
				"word series and how many the exposition holds, and the word " +
				"duplicate for the second registration of one name. It writes " +
				"nothing to standard error and leaves nothing on disk.",
			expected: "requests 3\nbuckets 5\nlabel /items/{id}\nseries 4\n" +
				"duplicate refused",
			minPackages:   3,
			minInterfaces: 1,
			deterministic: true,
			leavesNothing: true,
			mustNotAppear: []string{"prometheus.MustRegister", "DefaultRegisterer"},
			containedImports: []containment{
				{importPath: "github.com/prometheus/client_golang", packages: 1},
			},
		},
		{
			name: "222 traces itself and reads its own spans back",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It traces with go.opentelemetry.io/otel, " +
				"which may be imported by exactly one package, and exports spans " +
				"to a recorder held in memory so the program can read back what " +
				"it produced. The acceptance criteria are: the tracer provider is " +
				"created by the program and passed where it is needed rather than " +
				"installed globally, because a global provider is one process-wide " +
				"decision made by whichever package initialises first; a request " +
				"produces a span, and work it calls produces a child whose parent " +
				"is that span; attributes are set on the span rather than " +
				"concatenated into its name, which is what keeps the number of " +
				"distinct names bounded; a failure sets the span's status to an " +
				"error and records the error on it; the provider is shut down " +
				"before the program exits and the shutdown flushes, or the last " +
				"spans are lost exactly when they mattered. The program prints " +
				"one line per step, in this order: the word spans and how many " +
				"were recorded, the word nested if the second span's parent is " +
				"the first, the word attributes and how many the first carried, " +
				"the word status and the status of the failing span, and the word " +
				"flushed. It writes nothing to standard error and leaves nothing " +
				"on disk.",
			expected:      "spans 3\nnested\nattributes 2\nstatus error\nflushed",
			minPackages:   3,
			deterministic: true,
			leavesNothing: true,
			mustNotAppear: []string{"otel.SetTracerProvider", "otel.Tracer("},
			containedImports: []containment{
				{importPath: "go.opentelemetry.io/otel", packages: 1},
			},
		},
		{
			name: "223 serves a procedure call over gRPC",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It serves and calls with " +
				"google.golang.org/grpc and google.golang.org/protobuf, which " +
				"together may be imported by exactly two packages: the generated " +
				"code and the adapter that wraps it. The service definition is " +
				"the run's own; if it generates code from a schema it commits " +
				"both. The rest of the program calls a plain Go interface and " +
				"never sees a status, a code or a context deadline it did not " +
				"set. It listens on 127.0.0.1 on a port the operating system " +
				"chooses, calls itself and shuts down. The acceptance criteria " +
				"are: a successful call returns the answer; a request for " +
				"something absent comes back as the code meaning not found and " +
				"is translated at the adapter into an error of the program's own " +
				"vocabulary; an invalid argument is refused with the code that " +
				"says so rather than a generic failure; a call whose deadline " +
				"passes returns the deadline code and the server stops working on " +
				"it; the server is stopped gracefully and no goroutine outlives " +
				"it, which the tests assert with go.uber.org/goleak. The program " +
				"prints one line per step, in this order: the word ok and the " +
				"reply, the word code and the code for the absent thing, the word " +
				"code and the code for the invalid argument, the word code and " +
				"the code for the passed deadline, the word translated and the " +
				"program's own error for the absent thing, and the word clean. It " +
				"writes nothing to standard error and leaves nothing on disk.",
			expected: "ok pong\ncode NotFound\ncode InvalidArgument\n" +
				"code DeadlineExceeded\ntranslated not found\nclean",
			minPackages:       4,
			minInterfaces:     1,
			deterministic:     true,
			leavesNothing:     true,
			mustAppearInTests: []string{"goleak."},
			containedImports: []containment{
				{importPath: "google.golang.org/grpc", packages: 2},
				{importPath: "go.uber.org/goleak", packages: 0},
			},
		},
		{
			name: "224 streams over gRPC and gives up on time",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It extends the previous kind of service " +
				"with a server-streaming method, using google.golang.org/grpc, " +
				"contained as before. The acceptance criteria are: the client " +
				"receives every message the server sends and then a clean end of " +
				"stream rather than an error; a unary interceptor on the server " +
				"and one on the client each run once per call and can see the " +
				"method name; the interceptors are chained in a declared order " +
				"and that order is observable; a client that stops receiving " +
				"cancels the call, and the server's handler notices through its " +
				"context and stops producing rather than filling a buffer nobody " +
				"reads; the stream is drained or cancelled on every path out, so " +
				"no goroutine is left blocked on a send. The program prints one " +
				"line per step, in this order: the word received and how many " +
				"messages arrived, the word ended for the clean end of stream, " +
				"the word interceptors and how many ran for one call, the word " +
				"order and their names separated by spaces, the word stopped and " +
				"how many the server had produced when the client went away, and " +
				"the word clean. It writes nothing to standard error and leaves " +
				"nothing on disk.",
			expected: "received 5\nended\ninterceptors 2\norder outer inner\n" +
				"stopped 2\nclean",
			minPackages:       4,
			deterministic:     true,
			leavesNothing:     true,
			mustAppearInTests: []string{"goleak."},
			containedImports: []containment{
				{importPath: "google.golang.org/grpc", packages: 2},
			},
		},
		{
			name: "225 keeps a wire format compatible with its older self",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It encodes with " +
				"google.golang.org/protobuf, which may be imported by exactly two " +
				"packages: the generated code and the adapter around it. The " +
				"program holds two versions of one message: the older, and a " +
				"newer that adds a field and reserves nothing it removed. The " +
				"acceptance criteria are: a message written by the new version " +
				"and read by the old one keeps the fields the old one knows; a " +
				"message written by the old version and read by the new one " +
				"leaves the added field at its zero value rather than failing; a " +
				"field the reader does not know survives a round trip through it " +
				"unchanged, which is what lets an old service forward a message " +
				"it does not fully understand; two encodings of one message are " +
				"compared by unmarshalling rather than by bytes, because the " +
				"encoding is not canonical and comparing bytes is a test that " +
				"fails for no reason; the domain types are the program's own and " +
				"the generated types stop at the adapter. The program prints one " +
				"line per step, in this order: the word forward and the name the " +
				"old reader saw, the word backward and the added field's value " +
				"the new reader saw, the word preserved if the unknown field " +
				"survived, the word equal if the two encodings compared equal " +
				"after unmarshalling, and the word contained if no domain type " +
				"holds a generated one. It writes nothing to standard error and " +
				"leaves nothing on disk.",
			expected:      "forward ann\nbackward 0\npreserved\nequal\ncontained",
			minPackages:   4,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "google.golang.org/protobuf", packages: 2},
			},
		},
		{
			name: "226 serves the same service over a second protocol",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It serves the same service definition over " +
				"connectrpc.com/connect as well as gRPC, each library contained " +
				"in its own package, with one implementation behind both. The " +
				"acceptance criteria are: the service is implemented once against " +
				"the program's own interface and each protocol is a thin " +
				"translation; a successful call returns the same answer through " +
				"either; an error maps to the corresponding code in each " +
				"protocol, and the mapping is declared in one place rather than " +
				"written twice; the two servers can run at once on separate " +
				"ports; adding the second protocol changed nothing in the " +
				"implementation, which the run demonstrates by having one test " +
				"exercise the implementation directly with no protocol at all. " +
				"The program prints one line per step, in this order: the word " +
				"grpc and its reply, the word connect and its reply, the word " +
				"grpc and its code for the absent thing, the word connect and its " +
				"code for the same thing, and the word direct and the reply from " +
				"calling the implementation with no protocol. It writes nothing " +
				"to standard error and leaves nothing on disk.",
			expected: "grpc pong\nconnect pong\ngrpc NotFound\n" +
				"connect not_found\ndirect pong",
			minPackages:   5,
			minInterfaces: 1,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "connectrpc.com/connect", packages: 1},
				{importPath: "google.golang.org/grpc", packages: 2},
			},
		},
		{
			name: "227 evaluates rules it was given rather than compiled with",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It evaluates expressions with " +
				"github.com/google/cel-go, which may be imported by exactly one " +
				"package; callers pass a rule and a subject and receive a " +
				"decision. The acceptance criteria are: the environment declares " +
				"the variables a rule may use, and a rule referring to anything " +
				"else fails to compile with a message naming it rather than " +
				"failing at evaluation time on the first request that hits it; a " +
				"rule is compiled once and the compiled program is reused, " +
				"because compiling per evaluation is the mistake this library " +
				"invites; a rule that does not return a boolean is refused at " +
				"compile time; evaluation is bounded by a declared cost limit and " +
				"one that exceeds it is stopped rather than allowed to run; the " +
				"decision is a value of the program's own, not the library's. The " +
				"program prints one line per step, in this order: the word " +
				"allowed for a subject the rule admits, the word denied for one " +
				"it does not, the word compiled and how many times compilation " +
				"ran for a hundred evaluations, the word unknown and the variable " +
				"an invalid rule named, the word type for the rule that does not " +
				"return a boolean, and the word cost for the one stopped by the " +
				"limit. It writes nothing to standard error and leaves nothing on " +
				"disk.",
			expected:      "allowed\ndenied\ncompiled 1\nunknown colour\ntype\ncost",
			minPackages:   3,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "github.com/google/cel-go", packages: 1},
			},
		},
		{
			name: "228 retries on a schedule it can test without waiting",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It retries with " +
				"github.com/cenkalti/backoff, which may be imported by exactly " +
				"one package. The acceptance criteria are: the delays are " +
				"produced by the library and consumed through a seam the program " +
				"controls, so the tests observe the schedule without sleeping " +
				"through it; the schedule is bounded by a maximum number of " +
				"attempts and a maximum elapsed time, both declared; an error the " +
				"program classifies as permanent stops the retrying immediately " +
				"rather than being tried five more times, which means the " +
				"classification lives with the program's errors and not with the " +
				"call site; the last error is returned when the attempts run out, " +
				"not a generic one about giving up; nothing sleeps in the tests. " +
				"The program prints one line per step, in this order: the word " +
				"attempts and how many were made before success, the word delays " +
				"and the recorded delays separated by spaces, the word gave and " +
				"the word up with the number of attempts when it exhausted them, " +
				"the word permanent and the number of attempts made for an error " +
				"classified so, and the word slept and the total time actually " +
				"slept. It writes nothing to standard error and leaves nothing on " +
				"disk.",
			expected: "attempts 3\ndelays 10ms 20ms\ngave up 5\npermanent 1\n" +
				"slept 0s",
			minPackages:   3,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "github.com/cenkalti/backoff", packages: 1},
			},
		},
		{
			name: "229 takes a lock another process can see",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It takes a file lock with " +
				"github.com/gofrs/flock, which may be imported by exactly one " +
				"package. It works inside a temporary directory of its own and " +
				"removes it before exiting. The acceptance criteria are: the lock " +
				"is held by the process rather than by a variable, so a second " +
				"attempt from another goroutine using a second lock handle on the " +
				"same file is refused rather than granted; a non-blocking attempt " +
				"reports that it did not get the lock instead of waiting; a " +
				"blocking attempt with a context gives up when the context is " +
				"cancelled; the lock is released on every path out, including the " +
				"failing one, which means a deferred release and not one at the " +
				"end; releasing a lock that is not held is not an error the " +
				"program treats as fatal; the lock file is removed with the " +
				"directory. The program prints one line per step, in this order: " +
				"the word acquired, the word refused for the non-blocking second " +
				"attempt, the word cancelled for the blocking one that gave up, " +
				"the word released, the word acquired for the attempt that " +
				"follows the release, and the word removed. It writes nothing to " +
				"standard error and leaves nothing on disk.",
			expected:      "acquired\nrefused\ncancelled\nreleased\nacquired\nremoved",
			minPackages:   3,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "github.com/gofrs/flock", packages: 1},
			},
		},
		{
			name: "230 caches behind an interface it can measure",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It caches with " +
				"github.com/hashicorp/golang-lru, which may be imported by " +
				"exactly one package; callers see a get, a put and a set of " +
				"counters. The acceptance criteria are: the cache is bounded and " +
				"the least recently used entry is evicted, which the program " +
				"observes through an eviction callback rather than by inferring " +
				"it; the callback does not run while a lock is held in a way that " +
				"lets a caller deadlock by touching the cache from inside it; hits " +
				"and misses are counted and the counts are correct under " +
				"concurrent use; a value stored is not modifiable through the " +
				"reference the caller kept, or two callers share one value and " +
				"the second sees the first's changes; the cache can be emptied " +
				"and reports how many it dropped. The program prints one line per " +
				"step, in this order: the word evicted and the key the callback " +
				"reported, the word hits and the count, the word misses and the " +
				"count, the word concurrent and the total of hits and misses " +
				"after a hundred concurrent operations, the word isolated if a " +
				"caller could not change a stored value, and the word purged and " +
				"how many were dropped. It writes nothing to standard error and " +
				"leaves nothing on disk.",
			expected: "evicted a\nhits 2\nmisses 1\nconcurrent 100\nisolated\n" +
				"purged 2",
			minPackages:   3,
			minInterfaces: 1,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "github.com/hashicorp/golang-lru", packages: 1},
			},
		},
		{
			name: "231 reports every fault rather than the first",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It gathers faults with " +
				"github.com/hashicorp/go-multierror, which may be imported by " +
				"exactly one package; callers receive an ordinary error. The " +
				"acceptance criteria are: validating a record reports every " +
				"field at fault rather than stopping at the first, because a " +
				"caller who fixes one and resubmits to learn the next is being " +
				"made to do the program's work; the gathered error still answers " +
				"errors.Is for each fault it holds and errors.As for a typed one, " +
				"so nothing downstream has to know it is a gathered error at all; " +
				"gathering nothing returns a nil error rather than an empty " +
				"non-nil one, which is the classic way this pattern produces a " +
				"failure that is not a failure; nesting a gathered error inside " +
				"another flattens rather than nesting; the message lists the " +
				"faults in the order they were found. The program prints one line " +
				"per step, in this order: the word faults and how many the " +
				"gathered error holds, the word nil if gathering nothing produced " +
				"one, the word is if errors.Is found a sentinel inside it, the " +
				"word as and the field a typed fault named, and the word " +
				"flattened and how many the nested gathering holds. It writes " +
				"nothing to standard error and leaves nothing on disk.",
			expected:      "faults 3\nnil\nis\nas port\nflattened 5",
			minPackages:   3,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "github.com/hashicorp/go-multierror", packages: 1},
			},
		},
		{
			name: "232 runs work on a schedule without waiting for the clock",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It parses and follows schedules with " +
				"github.com/robfig/cron, which may be imported by exactly one " +
				"package. The acceptance criteria are: the schedule is parsed " +
				"once and its next times are computed from an instant the program " +
				"supplies, so the whole thing is testable without waiting for a " +
				"real minute to pass; a run that is still going when the next is " +
				"due is not started twice, and the skip is counted rather than " +
				"silently dropped; time that passed while the process was down is " +
				"caught up to a declared limit rather than either ignored or " +
				"replayed forever; an invalid schedule is refused at startup with " +
				"a message naming it, not at the first firing; stopping waits for " +
				"the running job to finish. The program prints one line per step, " +
				"in this order: the word next and the next two firing times of a " +
				"five-minute schedule from midnight, separated by a space; the " +
				"word ran and how many firings executed; the word skipped and how " +
				"many were skipped because the previous was still running; the " +
				"word caught and the word up with how many missed firings were " +
				"replayed; the word invalid for the schedule that would not " +
				"parse; and the word stopped. It writes nothing to standard error " +
				"and leaves nothing on disk.",
			expected: "next 00:05 00:10\nran 3\nskipped 1\ncaught up 2\ninvalid\n" +
				"stopped",
			minPackages:   3,
			minInterfaces: 1,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "github.com/robfig/cron", packages: 1},
			},
		},
		{
			name: "233 finds the goroutine it forgot to stop",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It uses go.uber.org/goleak in its tests, " +
				"which may not be imported by any package the program builds. The " +
				"program contains a worker that starts a goroutine per subscriber " +
				"and, as first written, leaks one when a subscriber goes away " +
				"without closing. The acceptance criteria are: a test asserts at " +
				"the end of the package that no goroutine outlives it, and that " +
				"test fails against the leaking version — a leak check that has " +
				"never failed is a leak check nobody has verified; the leak is " +
				"then fixed by giving the goroutine a way to be told to stop " +
				"rather than by making the test ignore it; the fix is a context " +
				"or a closed channel, not a sleep; goroutines started by the test " +
				"harness or by a library at initialisation are excluded by name " +
				"rather than by turning the check off. The program demonstrates " +
				"both versions in-process and prints one line per step, in this " +
				"order: the word before and how many goroutines the leaking " +
				"version left, the word named and whether the report identified " +
				"the leaking function, the word after and how many the fixed " +
				"version left, the word waited if stopping waited for the " +
				"goroutine to actually return, and the word clean. It writes " +
				"nothing to standard error and leaves nothing on disk.",
			expected:          "before 1\nnamed yes\nafter 0\nwaited\nclean",
			minPackages:       3,
			deterministic:     true,
			leavesNothing:     true,
			mustAppearInTests: []string{"goleak."},
			containedImports: []containment{
				{importPath: "go.uber.org/goleak", packages: 0},
			},
		},
		{
			name: "234 keeps the database driver out of the program",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It stores records in SQLite through " +
				"database/sql with modernc.org/sqlite. Both the driver and " +
				"database/sql may be imported by exactly one package, and the " +
				"rest of the program depends on a repository interface the run " +
				"declares. It works inside a temporary directory of its own and " +
				"removes it. The acceptance criteria are: no type from either " +
				"package appears in any signature outside the adapter — no rows, " +
				"no transaction, no null string, no driver error; the adapter " +
				"translates the driver's errors into the program's own, so a " +
				"unique-constraint violation becomes an already-exists error that " +
				"the caller can test for with errors.Is; a transaction is offered " +
				"as a function taking a callback rather than as an object the " +
				"caller must remember to close, because the version that can be " +
				"forgotten will be; the connection is closed and the file " +
				"released before the directory is removed; the same tests run " +
				"against the interface and not against SQLite. The program prints " +
				"one line per step, in this order: the word saved and the " +
				"identifier, the word loaded and the name, the word exists for a " +
				"duplicate, the word rolled and the word back with the row count " +
				"after a failing transaction, the word missing for an absent row, " +
				"and the word closed. It writes nothing to standard error and " +
				"leaves nothing on disk.",
			expected: "saved 1\nloaded ann\nexists\nrolled back 1\nmissing\n" +
				"closed",
			minPackages:   4,
			minInterfaces: 1,
			purePackages:  1,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "modernc.org/sqlite", packages: 1},
				{importPath: "database/sql", packages: 1},
			},
		},
		{
			name: "235 proves two very different stores behave the same",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. One repository interface has two " +
				"implementations behind it: one on SQLite through database/sql " +
				"with modernc.org/sqlite, contained to its own package, and one " +
				"holding everything in memory. The acceptance criteria are: one " +
				"suite of tests is written against the interface and run against " +
				"both implementations, so a behaviour the in-memory one invented " +
				"is caught rather than shipped; the suite covers the cases the " +
				"two are most likely to disagree on — ordering without an " +
				"explicit sort, what a missing row returns, what a duplicate " +
				"returns, and what happens to a change inside a transaction that " +
				"then fails; which implementation is used is decided where the " +
				"program is assembled and nothing else can tell; the in-memory " +
				"one is safe for concurrent use, because the SQLite one is and a " +
				"test double that is not will produce failures that look like " +
				"defects in the code under test. The program prints one line per " +
				"step, in this order: the word memory and the name it read back, " +
				"the word sqlite and the name it read back, the word cases and " +
				"how many the shared suite holds, the word agreed if both passed " +
				"every one, the word ordering and what both returned for an " +
				"unsorted list, and the word closed. It writes nothing to " +
				"standard error and leaves nothing on disk.",
			expected: "memory ann\nsqlite ann\ncases 8\nagreed\nordering 1 2 3\n" +
				"closed",
			minPackages:   5,
			minInterfaces: 1,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "modernc.org/sqlite", packages: 1},
				{importPath: "database/sql", packages: 1},
			},
		},
		{
			name: "236 wires three libraries into one composition root",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It serves over gRPC with " +
				"google.golang.org/grpc, traces with go.opentelemetry.io/otel and " +
				"logs with go.uber.org/zap. Each library is contained: grpc and " +
				"its generated code in two packages, tracing in one, logging in " +
				"one. The acceptance criteria are: nothing is installed globally " +
				"— no global tracer provider, no global logger, no package-level " +
				"registry — and everything is constructed in one place and passed " +
				"down, which is what makes two of these in one test binary " +
				"possible; a call produces a span, and the log line written " +
				"during that call carries the same trace identifier, because a " +
				"log that cannot be joined to a trace is two systems and not " +
				"one; a failure sets the span's status and is logged at error " +
				"level with the program's own error, not the library's; shutdown " +
				"happens in the reverse order of construction and flushes the " +
				"tracer before closing the logger. The program prints one line " +
				"per step, in this order: the word ok and the reply, the word " +
				"spans and how many were recorded, the word joined if the log " +
				"line carried the span's trace identifier, the word error and the " +
				"logged level for the failing call, the word order and the " +
				"shutdown order as three words, and the word clean. It writes " +
				"nothing to standard error and leaves nothing on disk.",
			expected: "ok pong\nspans 2\njoined\nerror error\n" +
				"order tracer logger server\nclean",
			minPackages:       6,
			minInterfaces:     1,
			deterministic:     true,
			leavesNothing:     true,
			mustAppearInTests: []string{"goleak."},
			containedImports: []containment{
				{importPath: "google.golang.org/grpc", packages: 2},
				{importPath: "go.opentelemetry.io/otel", packages: 1},
				{importPath: "go.uber.org/zap", packages: 1},
			},
		},
		{
			name: "237 puts a whole request pipeline together",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It routes with github.com/go-chi/chi, " +
				"counts with github.com/prometheus/client_golang and logs with " +
				"github.com/rs/zerolog, each contained to one package, and starts " +
				"a server on 127.0.0.1 on a port the operating system chooses. " +
				"The acceptance criteria are: one request passes through every " +
				"middleware in a declared order and each is a plain handler " +
				"wrapper that could be tested alone; the metric's route label is " +
				"the registered pattern, taken from the router in the one place " +
				"that knows about the router; the log line for a request carries " +
				"its identifier, its status and its duration, and the identifier " +
				"is the same one the response carries back; a panic in a handler " +
				"is recovered, counted, logged at error level and answered 500, " +
				"and the server keeps serving; metrics are registered against the " +
				"program's own registry. The program prints one line per step, in " +
				"this order: the status and body of a good request, the word " +
				"logged and the status the log line recorded, the word requests " +
				"and the counter's value, the word label and the route label, the " +
				"status of the request that panicked, and the word serving if the " +
				"next request after it succeeded. It writes nothing to standard " +
				"error and leaves nothing on disk.",
			expected: "200 alpha\nlogged 200\nrequests 1\nlabel /items/{id}\n" +
				"500\nserving",
			minPackages:   5,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "github.com/go-chi/chi", packages: 1},
				{importPath: "github.com/prometheus/client_golang", packages: 1},
				{importPath: "github.com/rs/zerolog", packages: 1},
			},
		},
		{
			name: "238 fans messages out and shuts the whole thing down",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It speaks WebSocket with " +
				"github.com/gorilla/websocket and runs its goroutines with " +
				"golang.org/x/sync, each contained to one package. It starts a " +
				"server on 127.0.0.1 on a port the operating system chooses, " +
				"connects three subscribers to itself and shuts down. The " +
				"acceptance criteria are: every subscriber receives every " +
				"broadcast message in order; each connection is served by one " +
				"reader and one writer goroutine and writes go through the writer " +
				"alone, because two goroutines writing to one connection is a " +
				"corruption the library will not protect against; a subscriber " +
				"that stops reading is disconnected on a declared timeout rather " +
				"than allowed to block the broadcast; shutdown closes every " +
				"connection with a close message and waits for every goroutine, " +
				"which the tests assert with go.uber.org/goleak. The program " +
				"prints one line per step, in this order: the word subscribers " +
				"and how many connected, the word delivered and how many messages " +
				"arrived in total, the word ordered if each subscriber saw them " +
				"in order, the word dropped and how many slow subscribers were " +
				"disconnected, the word closed and how many close messages were " +
				"sent, and the word clean. It writes nothing to standard error " +
				"and leaves nothing on disk.",
			expected: "subscribers 3\ndelivered 9\nordered\ndropped 1\nclosed 3\n" +
				"clean",
			minPackages:       4,
			minInterfaces:     1,
			deterministic:     true,
			leavesNothing:     true,
			mustAppearInTests: []string{"goleak."},
			containedImports: []containment{
				{importPath: "github.com/gorilla/websocket", packages: 1},
				{importPath: "golang.org/x/sync", packages: 1},
			},
		},
		{
			name: "239 assembles its whole configuration surface",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. Its command surface is " +
				"github.com/spf13/cobra, its configuration resolution is " +
				"github.com/spf13/viper and its file format is gopkg.in/yaml.v3, " +
				"each contained to one package. The acceptance criteria are: the " +
				"three meet in one place that produces a single settings value, " +
				"and everything downstream takes that value as an argument; the " +
				"precedence is flag, then environment variable, then file, then " +
				"declared default, and each is demonstrated; a setting that is " +
				"invalid whatever its source is refused once, in the settings, " +
				"rather than separately at each place it is used; the help output " +
				"lists every setting with its default and where it can be set, " +
				"because a configuration surface nobody can enumerate is one " +
				"nobody can operate; no global state is used by any of the three. " +
				"The program prints one line per step, in this order: the port " +
				"and its source with everything set, then after the flag is " +
				"removed, then after the variable is removed, then with only the " +
				"default; the word refused for an invalid value; and the word " +
				"documented and how many settings the help output lists. It " +
				"writes nothing to standard error and leaves nothing on disk.",
			expected: "6060 flag\n7070 env\n9090 file\n8080 default\nrefused\n" +
				"documented 4",
			minPackages:   5,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "github.com/spf13/cobra", packages: 1},
				{importPath: "github.com/spf13/viper", packages: 1},
				{importPath: "gopkg.in/yaml.v3", packages: 1},
			},
		},
		{
			name: "240 builds an authentication stack out of three libraries",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It derives password verifiers with " +
				"golang.org/x/crypto, mints identifiers with " +
				"github.com/google/uuid and issues tokens with " +
				"github.com/go-jose/go-jose, each contained to one package, with " +
				"the account rules in a package that reaches nothing outside " +
				"itself. It starts a server on 127.0.0.1 on a port the operating " +
				"system chooses. The acceptance criteria are: registering stores " +
				"a verifier and never the password; logging in with the right " +
				"password issues a signed token carrying the account identifier " +
				"and an expiry; the token is verified on each request and a " +
				"tampered one is refused; the signing key is rotated and tokens " +
				"issued under the previous key still verify until they expire, " +
				"which means keys are identified in the token and looked up " +
				"rather than assumed; a token past its expiry is refused even " +
				"under a valid key; every one of those decisions is made in the " +
				"pure package and the three libraries only carry them out. The " +
				"program prints one line per step, in this order: the word " +
				"registered, the word logged and the word in, the word verified " +
				"and the subject, the word tampered, the word rotated followed by " +
				"the word verified if the older token still passed, and the word " +
				"expired. It writes nothing to standard error and leaves nothing " +
				"on disk.",
			expected: "registered\nlogged in\nverified ann\ntampered\n" +
				"rotated verified\nexpired",
			minPackages:   6,
			minInterfaces: 1,
			purePackages:  1,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "golang.org/x/crypto", packages: 1},
				{importPath: "github.com/google/uuid", packages: 1},
				{importPath: "github.com/go-jose/go-jose", packages: 1},
			},
		},
		{
			name: "241 takes its policy from files it can reload",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. Policies are written in YAML, read with " +
				"gopkg.in/yaml.v3, and their conditions are expressions evaluated " +
				"with github.com/google/cel-go, each library contained to one " +
				"package. It works inside a temporary directory of its own and " +
				"removes it. The acceptance criteria are: every rule is compiled " +
				"when the file is loaded, so a file with a broken rule is refused " +
				"whole and the running policy is left in force rather than " +
				"half-replaced; the rule that failed is reported by its position " +
				"in the file; rules are evaluated in a declared order and the " +
				"first match decides, and the decision says which rule it was; " +
				"reloading swaps the whole set atomically, so no request is ever " +
				"evaluated against half of one set and half of another; a " +
				"decision is a value of the program's own and callers never see " +
				"the expression library. The program prints one line per step, in " +
				"this order: the word allowed and the rule that decided, the word " +
				"denied and the rule that decided, the word rules and how many " +
				"loaded, the word reloaded and how many after a good reload, the " +
				"word invalid and the position of the rule that failed to " +
				"compile, and the word unchanged and how many are in force after " +
				"that failure. It writes nothing to standard error and leaves " +
				"nothing on disk.",
			expected: "allowed admins\ndenied default\nrules 3\nreloaded 4\n" +
				"invalid rule 2\nunchanged 4",
			minPackages:   5,
			minInterfaces: 1,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "github.com/google/cel-go", packages: 1},
				{importPath: "gopkg.in/yaml.v3", packages: 1},
			},
		},
		{
			name: "242 stores documents encoded and compressed",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. Documents are encoded with " +
				"github.com/fxamacker/cbor, compressed with " +
				"github.com/klauspost/compress and stored in SQLite through " +
				"database/sql with modernc.org/sqlite, each contained to one " +
				"package. It works inside a temporary directory of its own and " +
				"removes it. The acceptance criteria are: what is stored is a " +
				"blob and what the caller passes and receives is a typed " +
				"document, so no caller knows either format; the encoding is " +
				"canonical and the stored digest is over the encoded bytes, so " +
				"the same document always stores identically and the digest can " +
				"be compared; the compressed form is smaller and the round trip " +
				"returns the document unchanged; a stored blob whose digest does " +
				"not match is refused on read rather than decoded into something " +
				"plausible; a document too large for a declared limit is refused " +
				"before it is encoded, not after. The program prints one line per " +
				"step, in this order: the word stored and the identifier, the " +
				"word smaller if compression helped, the word loaded and a field " +
				"of the document that came back, the word digest and the word ok, " +
				"the word corrupt for the blob whose digest did not match, and " +
				"the word refused for the oversized document. It writes nothing " +
				"to standard error and leaves nothing on disk.",
			expected: "stored 1\nsmaller\nloaded alpha\ndigest ok\ncorrupt\n" +
				"refused",
			minPackages:   5,
			minInterfaces: 1,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "github.com/fxamacker/cbor", packages: 1},
				{importPath: "github.com/klauspost/compress", packages: 1},
				{importPath: "modernc.org/sqlite", packages: 1},
				{importPath: "database/sql", packages: 1},
			},
		},
		{
			name: "243 shields a flaky dependency behind three techniques",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. Calls to an unreliable dependency are " +
				"retried with github.com/cenkalti/backoff, collapsed with " +
				"golang.org/x/sync and cached with " +
				"github.com/hashicorp/golang-lru, each contained to one package. " +
				"The dependency runs as a server in the same process on " +
				"127.0.0.1 on a port the operating system chooses, and fails its " +
				"first two calls. The acceptance criteria are: a hundred " +
				"concurrent callers asking for one key produce one call to the " +
				"dependency; that call is retried on the schedule the library " +
				"produces, with the delays observed through a seam rather than " +
				"slept; the answer is cached and the callers that follow are " +
				"served without another call; a failure is not cached, so the " +
				"next caller tries again; when the cached entry expires and the " +
				"dependency is failing, the stale value is served and the " +
				"staleness is reported rather than the caller being given an " +
				"error it cannot act on. The program prints one line per step, in " +
				"this order: the word callers and how many were served, the word " +
				"calls and how many reached the dependency, the word attempts and " +
				"how many the retrying made, the word cached and how many were " +
				"served without a call, the word stale and what was served when " +
				"the dependency was down, and the word closed. It writes nothing " +
				"to standard error and leaves nothing on disk.",
			expected: "callers 100\ncalls 1\nattempts 3\ncached 99\nstale alpha\n" +
				"closed",
			minPackages:   5,
			minInterfaces: 1,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "github.com/cenkalti/backoff", packages: 1},
				{importPath: "golang.org/x/sync", packages: 1},
				{importPath: "github.com/hashicorp/golang-lru", packages: 1},
			},
		},
		{
			name: "244 asserts its own observability in its tests",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It traces with go.opentelemetry.io/otel, " +
				"counts with github.com/prometheus/client_golang and tests with " +
				"github.com/stretchr/testify, the first two contained to one " +
				"package each and the third confined to test files. It starts a " +
				"server on 127.0.0.1 on a port the operating system chooses. The " +
				"acceptance criteria are: the tests assert what was observed and " +
				"not merely that observing did not crash — a span with the " +
				"expected name and attributes, a counter with the expected value " +
				"and labels; the recorders they read are in-memory ones the test " +
				"constructs, which is only possible because neither library was " +
				"installed globally; a handler that fails is asserted to produce " +
				"both an error span and an incremented failure counter, since the " +
				"two going out of step is the defect that makes dashboards lie; " +
				"the assertions name what differed rather than reporting false. " +
				"The program prints one line per step, in this order: the word " +
				"requests and the counter's value, the word spans and how many " +
				"were recorded, the word joined if a log line carried the trace " +
				"identifier, the word failures and the failure counter's value, " +
				"the word asserted and how many assertions the suite makes, and " +
				"the word clean. It writes nothing to standard error and leaves " +
				"nothing on disk.",
			expected:          "requests 3\nspans 3\njoined\nfailures 1\nasserted 6\nclean",
			minPackages:       5,
			deterministic:     true,
			leavesNothing:     true,
			mustAppearInTests: []string{"testify"},
			containedImports: []containment{
				{importPath: "go.opentelemetry.io/otel", packages: 1},
				{importPath: "github.com/prometheus/client_golang", packages: 1},
				{importPath: "github.com/stretchr/testify", packages: 0},
			},
		},
		{
			name: "245 makes sure only one of it is running",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It holds a lock with github.com/gofrs/flock " +
				"and schedules work with github.com/robfig/cron, each contained " +
				"to one package, and its tests assert with go.uber.org/goleak. It " +
				"works inside a temporary directory of its own and removes it. " +
				"The acceptance criteria are: only the instance holding the lock " +
				"runs scheduled work, and a second instance starts, finds the " +
				"lock held, and waits rather than either running the work twice " +
				"or exiting; the lock is checked at each firing rather than only " +
				"at start, so an instance that lost it stops working instead of " +
				"carrying on; a firing that arrives while the previous is still " +
				"running is skipped and counted; releasing the lock lets the " +
				"waiting instance take over and it runs the next firing; " +
				"stopping waits for the running job and releases the lock, and " +
				"leaves no goroutine behind. The program prints one line per " +
				"step, in this order: the word leader and which instance holds " +
				"the lock, the word waiting and which does not, the word ran and " +
				"how many firings the leader executed, the word skipped and how " +
				"many overlapped, the word leader and which instance holds it " +
				"after the first releases, and the word clean. It writes nothing " +
				"to standard error and leaves nothing on disk.",
			expected:          "leader a\nwaiting b\nran 2\nskipped 1\nleader b\nclean",
			minPackages:       4,
			minInterfaces:     1,
			deterministic:     true,
			leavesNothing:     true,
			mustAppearInTests: []string{"goleak."},
			containedImports: []containment{
				{importPath: "github.com/gofrs/flock", packages: 1},
				{importPath: "github.com/robfig/cron", packages: 1},
				{importPath: "go.uber.org/goleak", packages: 0},
			},
		},
		{
			name: "246 chooses which implementation to use while it is running",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. One repository interface has two " +
				"implementations behind it — one in memory and one on SQLite " +
				"through database/sql with modernc.org/sqlite, contained to its " +
				"own package — and which is used is read from configuration at " +
				"start. It works inside a temporary directory of its own and " +
				"removes it. The acceptance criteria are: the choice is made in " +
				"one place and returns the interface, so no caller has a branch " +
				"on it; the same journey is run against each and produces the " +
				"same transcript, which is the only evidence that the substitution " +
				"is real; a configured name that matches neither is refused at " +
				"start with a message listing what is available, rather than " +
				"falling back to a default that will be discovered in production; " +
				"the SQLite one is closed on the way out and the in-memory one " +
				"needs no closing, and the interface accommodates both without " +
				"the caller knowing which it has. The program prints one line per " +
				"step, in this order: the word memory and the name it read back, " +
				"the word sqlite and the name it read back, the word identical if " +
				"the two journeys matched line for line, the word unknown and the " +
				"configured name that was refused, the word available and how " +
				"many the refusal listed, and the word closed. It writes nothing " +
				"to standard error and leaves nothing on disk.",
			expected: "memory ann\nsqlite ann\nidentical\nunknown postgres\n" +
				"available 2\nclosed",
			minPackages:   5,
			minInterfaces: 1,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "modernc.org/sqlite", packages: 1},
				{importPath: "database/sql", packages: 1},
			},
		},
		{
			name: "247 refuses to speak a library's error language",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It integrates two libraries with error " +
				"types of their own — SQLite through database/sql with " +
				"modernc.org/sqlite, and gRPC with google.golang.org/grpc — each " +
				"contained, and it defines its own errors. The acceptance " +
				"criteria are: no error from either library reaches a caller " +
				"outside its adapter, so nothing downstream imports either to " +
				"understand a failure; each adapter translates the failures it " +
				"knows into the program's own errors and wraps the original as a " +
				"cause, so the detail survives for a log without being part of " +
				"the contract; errors.Is finds the program's sentinel through the " +
				"wrapping and errors.As finds the library's type only inside the " +
				"adapter that owns it; a failure the adapter does not recognise " +
				"becomes a general error rather than being passed through raw; " +
				"the same domain error comes back from both adapters for the same " +
				"condition, so a caller handles it once. The program prints one " +
				"line per step, in this order: the word sqlite and the program's " +
				"error for a duplicate row, the word grpc and the program's error " +
				"for an absent record, the word same if those two are the same " +
				"error, the word cause if the original survived as a cause, the " +
				"word hidden if no library type escaped its adapter, and the word " +
				"unknown for the failure neither adapter recognised. It writes " +
				"nothing to standard error and leaves nothing on disk.",
			expected: "sqlite already exists\ngrpc not found\nsame\ncause\n" +
				"hidden\nunknown",
			minPackages:   6,
			minInterfaces: 1,
			purePackages:  1,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "modernc.org/sqlite", packages: 1},
				{importPath: "database/sql", packages: 1},
				{importPath: "google.golang.org/grpc", packages: 2},
			},
		},
		{
			name: "248 keeps a badly behaved library from taking it down",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It integrates a dependency that misbehaves " +
				"in three ways the program cannot fix: one call panics, one " +
				"blocks forever, and one starts a goroutine it never stops. The " +
				"dependency is a package of the run's own writing that stands in " +
				"for a third party, and everything reaches it through one " +
				"adapter. The acceptance criteria are: the panic is recovered at " +
				"the adapter and returned as an error, so a defect in somebody " +
				"else's code is a failed request and not a stopped process; the " +
				"call that blocks is bounded by a timeout and abandoned, and the " +
				"caller gets an error while the abandoned goroutine is accounted " +
				"for rather than pretended away; the leak is detected and " +
				"reported by count, since a leak that cannot be seen is one " +
				"nobody fixes; the service keeps answering after all three; none " +
				"of the three is handled by the callers, who see only ordinary " +
				"errors. The program prints one line per step, in this order: the " +
				"word panic and the error the caller received, the word timeout " +
				"and the error the caller received, the word abandoned and how " +
				"many goroutines are knowingly outstanding, the word leaked and " +
				"how many the check reported, the word serving if a request after " +
				"all three succeeded, and the word reported. It writes nothing to " +
				"standard error and leaves nothing on disk.",
			expected: "panic call failed\ntimeout call failed\nabandoned 1\n" +
				"leaked 1\nserving\nreported",
			minPackages:   4,
			minInterfaces: 1,
			deterministic: true,
			leavesNothing: true,
		},
		{
			name: "249 assembles eight libraries and keeps every one contained",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours. It integrates eight third-party libraries " +
				"at once: github.com/go-chi/chi for routing, " +
				"go.opentelemetry.io/otel for tracing, " +
				"github.com/prometheus/client_golang for metrics, " +
				"go.uber.org/zap for logging, modernc.org/sqlite with " +
				"database/sql for storage, github.com/google/uuid for " +
				"identifiers, gopkg.in/yaml.v3 for configuration and " +
				"golang.org/x/sync for concurrency. Each is reachable from " +
				"exactly one package. It starts a server on 127.0.0.1 on a port " +
				"the operating system chooses and works in a temporary directory " +
				"it removes. The acceptance criteria are: every one of the eight " +
				"is constructed in one composition root and passed down, and " +
				"nothing is installed into a package-level variable anywhere; the " +
				"program's own rules live in a package that reaches none of them; " +
				"construction reports which dependency failed when one does, " +
				"rather than a nil pointer three layers later; shutdown happens " +
				"in reverse order and every resource is released; the whole " +
				"assembly can be built twice in one process, which is what having " +
				"no globals actually buys and what the tests demonstrate. The " +
				"program prints one line per step, in this order: the word " +
				"assembled and how many dependencies were constructed, the word " +
				"serving with the status of one request, the word traced if that " +
				"request produced a span, the word counted and the request " +
				"counter, the word second if a second independent assembly was " +
				"built and served in the same process, the word failed and the " +
				"dependency named when one is made to fail, and the word " +
				"shutdown. It writes nothing to standard error and leaves nothing " +
				"on disk.",
			expected: "assembled 8\nserving 200\ntraced\ncounted 1\nsecond 200\n" +
				"failed storage\nshutdown",
			minPackages:   10,
			minInterfaces: 3,
			purePackages:  1,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "github.com/go-chi/chi", packages: 1},
				{importPath: "go.opentelemetry.io/otel", packages: 1},
				{importPath: "github.com/prometheus/client_golang", packages: 1},
				{importPath: "go.uber.org/zap", packages: 1},
				{importPath: "modernc.org/sqlite", packages: 1},
				{importPath: "database/sql", packages: 1},
				{importPath: "github.com/google/uuid", packages: 1},
				{importPath: "gopkg.in/yaml.v3", packages: 1},
				{importPath: "golang.org/x/sync", packages: 1},
			},
		},
		{
			name: "250 runs the whole integrated service end to end",
			requirement: "Write a program in the module codeflux.test/workspace. " +
				"The layout is yours, and this is everything at once: a service " +
				"routed with github.com/go-chi/chi, authenticated with tokens " +
				"from github.com/go-jose/go-jose over verifiers from " +
				"golang.org/x/crypto, identified with github.com/google/uuid, " +
				"stored in SQLite through database/sql with modernc.org/sqlite, " +
				"searched through that database, configured from YAML with " +
				"gopkg.in/yaml.v3, decided by policies compiled with " +
				"github.com/google/cel-go, traced with go.opentelemetry.io/otel, " +
				"counted with github.com/prometheus/client_golang, logged with " +
				"go.uber.org/zap, made concurrent with golang.org/x/sync and " +
				"retried with github.com/cenkalti/backoff. Every library is " +
				"reachable from exactly one package, the rules are in packages " +
				"that reach none of them, and nothing is global. It works in a " +
				"temporary directory it removes and starts on 127.0.0.1 on a port " +
				"the operating system chooses. The acceptance criteria are every " +
				"one already given for the libraries it uses, and one more: the " +
				"transcript below is produced twice in the same process by two " +
				"independently assembled instances, and is identical both times. " +
				"The program prints one line per step, in this order: the word " +
				"configured and how many settings were resolved; the word " +
				"migrated and how many migrations ran; the status of a request " +
				"with no token; the status and identifier of a create; the status " +
				"and name of a read; the word policy and the rule that allowed " +
				"it; the word denied and the rule that refused a second account; " +
				"the word search and how many rows matched; the word traced and " +
				"how many spans that journey produced; the word counted and the " +
				"request counter; the word logged and how many lines carried the " +
				"trace identifier; the word retried and how many attempts the " +
				"flaky dependency took; the word second if the second instance " +
				"produced the same transcript; and the word shutdown. It writes " +
				"nothing to standard error and leaves nothing on disk.",
			expected: "configured 6\nmigrated 3\n401\n201 1\n200 ann\n" +
				"policy admins\ndenied default\nsearch 1\ntraced 4\ncounted 4\n" +
				"logged 4\nretried 3\nsecond\nshutdown",
			minPackages:   12,
			minInterfaces: 4,
			purePackages:  2,
			deterministic: true,
			leavesNothing: true,
			containedImports: []containment{
				{importPath: "github.com/go-chi/chi", packages: 1},
				{importPath: "github.com/go-jose/go-jose", packages: 1},
				{importPath: "golang.org/x/crypto", packages: 1},
				{importPath: "github.com/google/uuid", packages: 1},
				{importPath: "modernc.org/sqlite", packages: 1},
				{importPath: "database/sql", packages: 1},
				{importPath: "gopkg.in/yaml.v3", packages: 1},
				{importPath: "github.com/google/cel-go", packages: 1},
				{importPath: "go.opentelemetry.io/otel", packages: 1},
				{importPath: "github.com/prometheus/client_golang", packages: 1},
				{importPath: "go.uber.org/zap", packages: 1},
				{importPath: "golang.org/x/sync", packages: 1},
				{importPath: "github.com/cenkalti/backoff", packages: 1},
			},
		},
	}
}

// ladder is how the rungs relate to one another across a run of the suite.
//
// There are two modes and they answer different questions.
//
// Isolated is the default and is what every rung above was written against: a
// fresh database, repository and project for each, so a rung passes or fails on
// its own and a failure is never somebody else's fault. It is the mode to use
// when judging whether the engine can build a thing.
//
// Shared runs the whole ladder against one project. That is the mode that
// exercises the part of the pipeline nothing else reaches: the recall stage
// searches the artifacts of earlier runs, and it can only find anything if
// there are earlier runs to find. Stepping through the rungs in order then
// means rung 62 is offered what rung 61 built, and by the time the bands that
// integrate libraries arrive there is a project's worth of work behind them.
// The recall stage's own evidence says how much was searched and how much was
// found, and this reports it per rung and in total.
//
// Set CODEFLUX_LADDER=shared for the second. CODEFLUX_LADDER_STRICT=1 then
// fails a rung that searched nothing after the first, which is how a
// regression in the reuse path would be caught rather than merely logged.
type ladder struct {
	key    string
	shared bool
	strict bool
	// engine is the one project every rung runs against, in shared mode.
	engine *engineFixture
	// What the ladder accumulated, for the account at the end.
	rungs    int
	searched int
	offered  int
	names    []string
}

// startLadder reads the mode and, when it is shared, opens the one project.
func startLadder(t *testing.T, key string) *ladder {
	t.Helper()
	run := &ladder{
		key:    key,
		shared: strings.EqualFold(strings.TrimSpace(os.Getenv("CODEFLUX_LADDER")), "shared"),
		strict: os.Getenv("CODEFLUX_LADDER_STRICT") != "",
	}
	if !run.shared {
		t.Log("ladder: isolated. Every rung gets its own database, repository " +
			"and project, so nothing any rung produces is visible to another " +
			"and the recall stage has nothing to search. Set " +
			"CODEFLUX_LADDER=shared to run them against one project instead.")
		return run
	}
	engine := startEscalatingEngineFixture(t, run.models())
	run.engine = &engine
	t.Log("ladder: shared. Every rung runs against one project, so what each " +
		"produces is there for the ones after it and the recall stage has a " +
		"history to search.")
	t.Cleanup(func() { run.report(t) })
	return run
}

// models builds the model factory both modes use.
//
// A factory rather than one model, so the ladder runs the escalation path the
// product runs, and the default style, because judging the pipeline under a
// setting nobody ships says nothing about what a person gets.
func (run *ladder) models() func(string) (agentloop.FixedModel, error) {
	style := pipeline.DefaultSettings().CodeStyle
	return func(named string) (agentloop.FixedModel, error) {
		return newDefaultAgentModel(run.key, named, style)
	}
}

// engineFor returns the project this rung runs against.
func (run *ladder) engineFor(t *testing.T) engineFixture {
	t.Helper()
	if run.engine != nil {
		return *run.engine
	}
	return startEscalatingEngineFixture(t, run.models())
}

// report accounts for what the shared ladder accumulated.
func (run *ladder) report(t *testing.T) {
	if !run.shared || run.rungs == 0 {
		return
	}
	t.Logf("ladder: %d rung(s) ran against one project. The recall stage "+
		"searched %d earlier artifact(s) in total and found %d function(s) "+
		"that already existed: %s", run.rungs, run.searched, run.offered,
		describeReused(run.names))
}

// describeReused renders the names, or says plainly that there were none.
func describeReused(names []string) string {
	if len(names) == 0 {
		return "none, so every rung built everything it needed from nothing"
	}
	return strings.Join(names, ", ")
}

// ticket is this rung's own identity for the requests it makes.
//
// The rung's number, which is unique across the ladder and stable as rungs are
// added, so a rerun of one rung reuses its own key rather than colliding with
// its neighbour's.
func (program generatedProgram) ticket() string {
	number, _, _ := strings.Cut(program.name, " ")
	return "rung-" + number
}

// artifactTables are what a finished run is supposed to leave behind.
//
// They are counted rather than asserted, and they are listed here whether or
// not anything writes to them yet, because a census that only names the tables
// that already fill would report a complete pipeline. Several of these are
// declared by the plan and written by nothing: printing them at zero beside the
// ones that do fill is the difference between a gap somebody can see and a gap
// that looks like an absence of interest.
var artifactTables = []string{
	"artifacts",
	"pipeline_stage_records",
	"evidence",
	"episodes",
	"episode_fact_references",
	"atom_names",
	"atom_documentation_revisions",
	"memory_artifacts",
	"memory_artifact_revisions",
	"final_evidence_reports",
	"agent_plan_step_graph_nodes",
}

// artifactCount is one table's contribution, or the fact that it is not there.
type artifactCount struct {
	table   string
	rows    int
	present bool
}

// census counts what the project holds now.
func (engine engineFixture) census() []artifactCount {
	counts := make([]artifactCount, 0, len(artifactTables))
	for _, table := range artifactTables {
		rows, err := engine.repositories.CountRowsForTest(table)
		counts = append(counts,
			artifactCount{table: table, rows: rows, present: err == nil})
	}
	return counts
}

// describeCensusDelta says what this rung added to the project.
func describeCensusDelta(before, after []artifactCount) string {
	var changed, empty []string
	for index, count := range after {
		switch {
		case !count.present:
			empty = append(empty, count.table+" (no such table)")
		case count.rows > before[index].rows:
			changed = append(changed, fmt.Sprintf("%s +%d (%d)",
				count.table, count.rows-before[index].rows, count.rows))
		case count.rows == 0:
			empty = append(empty, count.table)
		}
	}
	if len(changed) == 0 {
		return "nothing was stored; empty: " + strings.Join(empty, ", ")
	}
	written := strings.Join(changed, ", ")
	if len(empty) == 0 {
		return written
	}
	return written + "; still empty: " + strings.Join(empty, ", ")
}

// noteReuse reads what the recall stage found and records it on the ladder.
//
// The stage's own evidence is the source rather than a count taken here,
// because the question is what the pipeline knew, not what a test could work
// out afterwards. A run that searched nothing and a run that searched a hundred
// artifacts and matched none are different results, and only the first is a
// reason to doubt the wiring.
func (run *ladder) noteReuse(
	t *testing.T,
	engine engineFixture,
	taskID domain.TaskID,
) {
	t.Helper()
	run.rungs++
	recorded, err := engine.repositories.ListPipelineStages(
		context.Background(), taskID, 1)
	if err != nil {
		t.Logf("reuse: the flow ledger could not be read: %v", err)
		return
	}
	for _, record := range recorded {
		if record.Name != "recall" {
			continue
		}
		var evidence struct {
			Searched int      `json:"artifacts_searched"`
			Needed   int      `json:"functions_needed"`
			Existing []string `json:"already_in_project"`
		}
		if record.EvidenceJSON != "" {
			if err := json.Unmarshal(
				[]byte(record.EvidenceJSON), &evidence,
			); err != nil {
				t.Logf("reuse: the recall stage's evidence did not parse: %v", err)
				return
			}
		}
		run.searched += evidence.Searched
		run.offered += len(evidence.Existing)
		run.names = append(run.names, evidence.Existing...)
		t.Logf("reuse: %d function(s) needed; %d earlier artifact(s) searched; "+
			"%s", evidence.Needed, evidence.Searched,
			describeReused(evidence.Existing))
		// Only in the shared mode, and only after the first rung, is an empty
		// search a defect: in isolated mode it is the mode working correctly.
		if run.strict && run.shared && run.rungs > 1 && evidence.Searched == 0 {
			t.Errorf("this rung searched no earlier artifacts, so the project's "+
				"history did not reach it: %s", record.DetailRedacted)
		}
		return
	}
	if run.shared && run.strict {
		t.Error("the flow ledger holds no recall stage, so nothing looked for " +
			"work the project had already done")
	}
}

// generatedProgram is one program the engine is asked to write, and what
// running it must print.
type generatedProgram struct {
	name        string
	requirement string
	arguments   []string
	stdin       string
	expected    string
	// minPackages is how many packages the work must be spread across,
	// counting the command itself. Zero means the layout is not part of the
	// requirement.
	//
	// It is a count and not a list of paths. Naming the files in the
	// requirement would be dictating a design and then congratulating the run
	// for following it; what the requirement can honestly ask for is that the
	// work is separated at all, and what the check can honestly verify is that
	// it was. Which package holds what, and what any of them are called, is the
	// run's decision.
	minPackages int
	// purePackages is how many packages besides the command must reach nothing
	// outside themselves.
	//
	// It names neither the packages nor what belongs in them, because a
	// requirement that says "keep the rules separate from the plumbing" is a
	// real constraint and "put them in internal/domain" is a different, smaller
	// one. What can be checked without dictating a design is that the
	// separation exists: some package computes without printing, reading,
	// logging, serving or querying. Which one, and what it is called, is the
	// run's answer to give.
	purePackages int
	// pureSymbols name exported identifiers whose declaring package must
	// compute without reaching outside itself.
	//
	// The symbol is the anchor rather than a path, because the requirement
	// asks for a pure core and not for a file. The check finds where the
	// identifier was declared and judges the package it landed in, so a run
	// that put it somewhere sensible of its own choosing passes for the right
	// reason.
	pureSymbols []string
	// mustAppear are fragments the produced source has to hold somewhere, used
	// where the shape of the code is the point rather than only its output.
	// Where they appear is the run's business; that they appear is not.
	mustAppear []string
	// mustAppearInTests is the same, restricted to the tests, for requirements
	// that ask for a particular kind of test rather than a particular result.
	mustAppearInTests []string
	// mustNotAppear are fragments no package outside the command may hold.
	//
	// The command is exempt on purpose: assembling the world is what it is for.
	// A library package that does it on import, or a test that sleeps until a
	// race stops happening, is the habit this refuses.
	mustNotAppear []string
	// containedImports bound how far a dependency was allowed to spread.
	containedImports []containment
	// minInterfaces is how many interface types must be declared outside the
	// command, which is the closest mechanical reading of "there is somewhere
	// to substitute the library".
	minInterfaces int
	// deterministic runs the program twice and requires the same output.
	//
	// This is the check that earns its keep once third-party libraries are in
	// play. Their nondeterminism is not usually a bug in them — a map iterated,
	// an identifier generated, a timestamp formatted, a goroutine that wins a
	// race — it is the program failing to hold them still. Once is a result;
	// twice is a behaviour.
	deterministic bool
	// leavesNothing runs the program in an empty directory and requires it
	// still empty afterwards. Libraries write caches, logs, lock files and
	// sockets; a program that integrates one is responsible for what it leaves.
	leavesNothing bool
}

// containment is a bound on how far one dependency may spread.
type containment struct {
	importPath string
	// packages is the most that may import it, or anything beneath it. One is
	// an adapter; two is an adapter and a fake beside it.
	packages int
}

// buildAndRun drives the engine, then compiles and runs what it wrote.
func (program generatedProgram) buildAndRun(t *testing.T, run *ladder) {
	t.Helper()
	// The project this rung runs against: its own in the isolated mode, the
	// ladder's one project in the shared one.
	engine := run.engineFor(t)
	before := engine.census()
	// The engine is shown the same example the test will judge it by. Judging
	// a run against a criterion it was never told is not a test of the run: it
	// cannot refine toward something it cannot see, and every failure of that
	// kind was unfixable by any number of attempts.
	worktree, taskID := engine.carryOut(
		t, program.requirement+program.acceptanceBlock(), program.ticket())

	// What this rung added to the project, and what it could reuse from the
	// ones before it. In the isolated mode both are answers about one run; in
	// the shared mode they are the ladder accumulating.
	t.Logf("stored: %s", describeCensusDelta(before, engine.census()))
	run.noteReuse(t, engine, taskID)

	// Everything the run wrote is read and parsed once, and every structural
	// question is answered from that.
	work := readProducedWork(t, worktree)
	written, err := os.ReadFile(work.entry)
	if err != nil {
		t.Fatalf("the program the run reported writing is not there: %v", err)
	}
	t.Logf("the model wrote %s:\n%s", filepath.ToSlash(work.entry), written)

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

	program.verifyStructure(t, work)

	// The plan the run recorded is checked against the worktree it left behind,
	// so a run that declared what it would produce and then produced something
	// else is caught here rather than downstream.
	engine.verifyPlanThesis(t, taskID, worktree)

	// 1. It has to compile. This is the first thing that cannot be faked: a
	//    file of plausible Go that does not build is not a program.
	binary := filepath.Join(t.TempDir(), filepath.Base(work.command))
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.CommandContext(t.Context(),
		"go", "build", "-o", binary, "./"+work.command)
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
	//
	//    It runs in an empty directory of its own rather than in the worktree,
	//    so anything it drops is its own doing and can be seen.
	yard := t.TempDir()
	produced := program.runOnce(t, binary, yard)
	if got := normalizeProgramOutput(produced); got != program.expected {
		t.Fatalf("the executable printed:\n%s\nwant:\n%s\n--- source ---\n%s",
			got, program.expected, written)
	}
	t.Logf("built %s, ran it with %v, and it printed exactly what was asked",
		filepath.Base(binary), program.arguments)

	// 3. Where the answer had to be repeatable, it is run again.
	if program.deterministic {
		again := program.runOnce(t, binary, t.TempDir())
		if normalizeProgramOutput(again) != normalizeProgramOutput(produced) {
			t.Errorf("the second run printed something else, so the answer was a "+
				"result and not a behaviour:\n--- first ---\n%s\n--- second ---\n%s",
				normalizeProgramOutput(produced), normalizeProgramOutput(again))
		}
	}

	// 4. And where it was told to leave nothing behind, the directory it ran in
	//    is checked. Third-party libraries write caches, logs and lock files;
	//    what a program leaves is its own responsibility, not theirs.
	if program.leavesNothing {
		left, readErr := os.ReadDir(yard)
		if readErr != nil {
			t.Fatalf("reading the directory the program ran in failed: %v", readErr)
		}
		if len(left) > 0 {
			var names []string
			for _, entry := range left {
				names = append(names, entry.Name())
			}
			sort.Strings(names)
			t.Errorf("the program left %d thing(s) behind in the directory it ran "+
				"in: %v", len(names), names)
		}
	}

	// 5. And it has to survive input nobody intended. Everything above uses
	//    the one input the requirement described, which is the input least
	//    likely to break anything.
	program.probeAdversarially(t, binary)
}

// runOnce runs the built program in a directory of its own.
func (program generatedProgram) runOnce(
	t *testing.T,
	binary string,
	where string,
) string {
	t.Helper()
	run := exec.CommandContext(t.Context(), binary, program.arguments...)
	run.Dir = where
	if program.stdin != "" {
		run.Stdin = strings.NewReader(program.stdin)
	}
	produced, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated executable failed to run: %v\n%s", err, produced)
	}
	return string(produced)
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
//
// The ticket makes this rung's requests its own. The keys used to be constants,
// which is harmless while every rung has a database to itself and silently
// wrong the moment they share one: the second rung's intake would match the
// first rung's key, return the first rung's task, and the suite would report
// two hundred and fifty passes for one program built once.
func (engine engineFixture) carryOut(
	t *testing.T,
	requirement string,
	ticket string,
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
		// Empty rather than ".", which intake refuses: fingerprint's
		// shapeImportPath requires each segment to start with an
		// alphanumeric, so "." never matched and every rung died at intake
		// with "entries must match the field's documented syntax" before
		// reaching a provider. Empty is also the honest value — no rung
		// names a package, so there is no package hint to give. Non-nil,
		// because normalizedStrings rejects nil.
		AffectedPackages: []string{},
		IdempotencyKey:   ticket,
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
		ticket+"-bind",
	)
	if err != nil {
		t.Fatalf("binding the approved preflight failed: %v", err)
	}
	if _, err := engine.lifecycle.StartPreparedTask(ctx, transport.StartTaskCommand{
		TaskID:            created.TaskID,
		ExpectedRevision:  readyRevision,
		PreflightRevision: preflight.Revision,
		IdempotencyKey:    ticket + "-start",
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
	//
	// This task's own state is what is waited on, not the conversation. The
	// conversation belongs to the thread, and when a ladder runs many rungs
	// through one thread it already holds the word Finished from the rung
	// before: waiting for that text would return at once, every time, and the
	// suite would judge an empty worktree and report the model's failure as
	// its own.
	engine.waitFor(t, "the run to finish", func() bool {
		task, err := engine.repositories.GetTask(ctx, created.TaskID)
		if err != nil {
			return false
		}
		switch task.State {
		case domain.TaskStateCompleted, domain.TaskStateFailed,
			domain.TaskStateCancelled, domain.TaskStateRolledBack,
			domain.TaskStateRecoveryRequired, domain.TaskStateAwaitingReview,
			domain.TaskStatePaused:
			return true
		default:
			return false
		}
	})
	// What the run said is logged even when everything passes. A green result
	// with no record of how it was reached is the kind of evidence that stops
	// being worth anything the moment somebody doubts it.
	t.Logf("the run reported:\n%s", engine.narration())
	engine.reportFlow(t, created.TaskID)
	// Two results, reported separately, because they are two questions.
	//
	// The checks after this ask whether the produced program builds, runs and
	// prints what was asked. The task state asks whether the pipeline
	// converged. Rung 6 answered yes to the first and no to the second and the
	// suite reported PASS, which reads as "the platform worked" when what was
	// established is "the program was correct despite the platform not
	// finishing". A ladder that hides the second question cannot measure
	// progress on the thing it exists to measure.
	if task, taskErr := engine.repositories.GetTask(
		ctx, created.TaskID,
	); taskErr == nil {
		t.Logf("task state: %s", task.State)
		switch task.State {
		case domain.TaskStateAwaitingReview, domain.TaskStateCompleted:
			// Converged.
		case domain.TaskStatePaused:
			t.Errorf("the program is correct and the pipeline did not "+
				"converge: the task ended %s, meaning the completion floor "+
				"held and a required gate did not. This rung is not passed "+
				"until both do.", task.State)
		default:
			t.Errorf("the task ended %s: the pipeline did not converge",
				task.State)
		}
	}
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

// producedFile is one Go file the run wrote, as the compiler sees it.
//
// The facts here are read with go/parser rather than matched for. Every one of
// them — which package this is, what it imports, whether it declares an
// interface — is a question the language already answers exactly, and a
// substring search answers approximately: "net" appears inside "net/http", an
// import path appears inside a comment quoting it, and a slice of strings looks
// like an import block if the regex is loose enough. A structural check that is
// wrong occasionally is worse than no check, because the failure it reports is
// unfalsifiable by reading the code it names.
type producedFile struct {
	// path is slash-separated and relative to the worktree.
	path string
	// directory is the package it belongs to, which is all the identity a
	// package has here: the requirement named none of them.
	directory string
	test      bool
	body      string
	// packageName is the clause, not the folder. They usually agree and the
	// requirement never said they had to.
	packageName  string
	imports      []string
	declaresMain bool
	// interfaces is how many interface types this file declares, which is the
	// closest mechanical reading of "there is somewhere to substitute".
	interfaces int
	// declares are the top-level names, used to find where an idea landed
	// without having told the run where to put it.
	declares []string
}

// producedWork is everything the run wrote, read once.
//
// Every structural question is answered from this rather than from a path the
// requirement handed over, because the requirement hands none over. It is read
// once and passed around for a duller reason too: the checks grew from three to
// eight while the ladder grew from one band to five, and re-walking the tree
// per check was already the slowest thing in the fixture that is not a model
// call.
type producedWork struct {
	worktree string
	files    []producedFile
	// command is the import path of the package that declares main, and entry
	// the file it is declared in.
	command string
	entry   string
}

// filesTheRunWrote is the set of repository-relative paths this run added or
// changed, read from git rather than inferred.
//
// It returns nil when the answer cannot be established — an unreadable
// worktree, a git that will not run — and every caller then falls back to
// reading the whole tree, because a fixture that silently judged nothing
// would report every rung as producing no program at all. A wrong answer
// that announces itself is recoverable; an empty one that looks like a pass
// is not.
func filesTheRunWrote(t *testing.T, worktree string) map[string]bool {
	t.Helper()
	// Both halves are needed and they are different questions: --others finds
	// a file the run created, and the diff finds one it edited in place.
	written := map[string]bool{}
	for _, argument := range [][]string{
		{"ls-files", "--others", "--exclude-standard"},
		{"diff", "--name-only", "HEAD"},
	} {
		command := exec.Command("git", argument...)
		command.Dir = worktree
		output, err := command.Output()
		if err != nil {
			t.Logf("could not establish what the run wrote (%v %v): %v; "+
				"falling back to reading the whole worktree",
				"git", argument, err)
			return nil
		}
		for _, line := range strings.Split(string(output), "\n") {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				written[filepath.ToSlash(trimmed)] = true
			}
		}
	}
	return written
}

// readProducedWork reads the worktree and finds the program in it.
//
// The command is found rather than assumed. The requirement asks for a program
// and says nothing about where to put it, so this asks the source: exactly one
// package must declare a main function. None means no program was produced;
// several means there is no saying which was meant, and building whichever
// sorted first is how a suite ends up judging the wrong binary.
//
// "What the run wrote" is taken literally, against the worktree's own base
// revision, rather than meaning "every Go file present". Those are the same
// thing only on an empty repository, and they were not the same here: the
// fixture's repository ships a placeholder `main.go`, so a run that correctly
// added its own command produced two main packages and this refused to judge
// either. On a real repository — the case PIPE-122 exists for — there are
// always commands the run did not write, so reading them as the run's work
// would make this unusable there rather than merely wrong here.
func readProducedWork(t *testing.T, worktree string) producedWork {
	t.Helper()
	work := producedWork{worktree: worktree}
	authored := filesTheRunWrote(t, worktree)
	walk := func(where string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			// A run's own build output and its version control are not the work.
			switch entry.Name() {
			case ".git", "testdata", "vendor":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}
		relative, relErr := filepath.Rel(worktree, where)
		if relErr != nil {
			return relErr
		}
		body, readErr := os.ReadFile(where)
		if readErr != nil {
			return readErr
		}
		slashed := filepath.ToSlash(relative)
		if authored != nil && !authored[slashed] {
			// Present in the worktree, and not this run's doing. See this
			// function's own comment: judging it would judge somebody else's
			// program.
			return nil
		}
		work.files = append(work.files,
			describeProducedFile(slashed, path.Dir(slashed),
				strings.HasSuffix(entry.Name(), "_test.go"), string(body)))
		return nil
	}
	if err := filepath.WalkDir(worktree, walk); err != nil {
		t.Fatalf("reading what the run wrote failed: %v", err)
	}
	sort.Slice(work.files, func(i, j int) bool {
		return work.files[i].path < work.files[j].path
	})

	var commands []string
	for _, file := range work.files {
		if file.test || file.packageName != "main" || !file.declaresMain {
			continue
		}
		if work.command == "" {
			work.command = file.directory
			work.entry = filepath.Join(worktree, filepath.FromSlash(file.path))
		}
		if !slices.Contains(commands, file.directory) {
			commands = append(commands, file.directory)
		}
	}
	switch {
	case len(commands) == 0:
		t.Fatalf("the run wrote no program: no package declares main. It wrote: %v",
			work.paths())
	case len(commands) > 1:
		t.Fatalf("the run wrote %d programs and the requirement asked for one, so "+
			"there is no saying which was meant: %v", len(commands), commands)
	}
	return work
}

// describeProducedFile reads one file the way the compiler would.
//
// A file that does not parse is described as far as it got. The build step
// reports the syntax error properly a moment later, with the compiler's own
// message and position, so failing here would only replace a good error with a
// worse one.
func describeProducedFile(where, directory string, test bool, body string) producedFile {
	file := producedFile{path: where, directory: directory, test: test, body: body}
	parsed, err := parser.ParseFile(token.NewFileSet(), where, body, parser.SkipObjectResolution)
	if parsed == nil || err != nil && parsed.Name == nil {
		return file
	}
	if parsed.Name != nil {
		file.packageName = parsed.Name.Name
	}
	for _, imported := range parsed.Imports {
		unquoted, unquoteErr := strconv.Unquote(imported.Path.Value)
		if unquoteErr == nil {
			file.imports = append(file.imports, unquoted)
		}
	}
	for _, declaration := range parsed.Decls {
		switch typed := declaration.(type) {
		case *ast.FuncDecl:
			if typed.Name == nil {
				continue
			}
			if typed.Recv == nil {
				file.declares = append(file.declares, typed.Name.Name)
			}
			if typed.Recv == nil && typed.Name.Name == "main" {
				file.declaresMain = true
			}
		case *ast.GenDecl:
			for _, spec := range typed.Specs {
				switch specified := spec.(type) {
				case *ast.TypeSpec:
					file.declares = append(file.declares, specified.Name.Name)
					if _, isInterface := specified.Type.(*ast.InterfaceType); isInterface {
						file.interfaces++
					}
				case *ast.ValueSpec:
					for _, name := range specified.Names {
						file.declares = append(file.declares, name.Name)
					}
				}
			}
		}
	}
	return file
}

// paths lists what the run wrote, for a failure message that has to say so.
func (work producedWork) paths() []string {
	found := make([]string, 0, len(work.files))
	for _, file := range work.files {
		found = append(found, file.path)
	}
	return found
}

// packages lists the packages the run wrote, excluding the command when asked.
func (work producedWork) packages(includeCommand bool) []string {
	seen := map[string]bool{}
	var found []string
	for _, file := range work.files {
		if file.directory == work.command && !includeCommand {
			continue
		}
		if !seen[file.directory] {
			seen[file.directory] = true
			found = append(found, file.directory)
		}
	}
	sort.Strings(found)
	return found
}

// importers lists the packages whose non-test files import a path.
//
// A third-party path also matches everything beneath it, because a library is
// one dependency however many of its packages are used: a run that imports the
// driver in one package and the driver's errors package in six has spread the
// dependency across seven, and saying otherwise would let containment be
// satisfied by a technicality.
//
// A standard-library path matches only itself. net and net/http are two
// packages that share a prefix, not one library and its corner, and treating
// them as one would answer a question about net with a fact about net/http.
func (work producedWork) importers(importPath string) []string {
	leading, _, _ := strings.Cut(importPath, "/")
	wholeLibrary := strings.Contains(leading, ".")
	seen := map[string]bool{}
	var found []string
	for _, file := range work.files {
		if file.test {
			continue
		}
		for _, imported := range file.imports {
			if imported != importPath &&
				!(wholeLibrary && strings.HasPrefix(imported, importPath+"/")) {
				continue
			}
			if !seen[file.directory] {
				seen[file.directory] = true
				found = append(found, file.directory)
			}
			break
		}
	}
	sort.Strings(found)
	return found
}

// reachesOutward reports whether a file leaves its own package.
//
// The standard library's ways out are listed; a third party is a way out by
// definition, because a package that drives someone else's code cannot promise
// anything about what that code does. The workspace's own packages are not: a
// pure package built on another pure package is still pure.
func (file producedFile) reachesOutward() bool {
	for _, imported := range file.imports {
		if slices.Contains(outwardImports, imported) {
			return true
		}
		leading, _, _ := strings.Cut(imported, "/")
		if !strings.Contains(leading, ".") {
			continue // the standard library
		}
		if imported == workspaceModule ||
			strings.HasPrefix(imported, workspaceModule+"/") {
			continue
		}
		return true
	}
	return false
}

// workspaceModule is the module the generated programs declare. Rungs that ask
// for one say so in their requirement; nothing here depends on the layout
// inside it.
const workspaceModule = "codeflux.test/workspace"

// verifyStructure checks the shape a requirement asked for without having told
// the run where to put anything.
//
// Every check here is expressed as a property of the work rather than as a
// path: how many packages it was separated into, whether the package that ended
// up holding a named idea can reach the outside world, and whether a construct
// the requirement called for appears at all. A run that made a sensible layout
// of its own passes; a run that put everything in one file does not.
func (program generatedProgram) verifyStructure(t *testing.T, work producedWork) {
	t.Helper()

	if program.minPackages > 0 {
		packages := work.packages(true)
		if len(packages) < program.minPackages {
			t.Errorf("the requirement asked for the work to be separated into at "+
				"least %d packages and it is in %d: %v",
				program.minPackages, len(packages), work.paths())
		}
	}

	if program.purePackages > 0 {
		program.verifyPureSeparation(t, work)
	}

	for _, symbol := range program.pureSymbols {
		program.verifyPureSymbol(t, work, symbol)
	}

	program.verifyContainment(t, work)
	program.verifySeams(t, work)
	program.verifyFragments(t, work)
}

// verifyContainment checks that a dependency stayed where it was put.
//
// This is the check that survives contact with impure code. Purity is a fine
// thing to ask of a decision table and a meaningless thing to ask of a package
// whose entire job is to drive a database driver, a metrics client or a
// websocket library — and a ladder that only knows how to ask for purity has
// nothing to say about the code where most defects actually live.
//
// What can still be asked, and is worth more as the program grows, is
// containment: this library is reachable from at most N packages. One is the
// adapter. Two is an adapter and its test double. Five means the library's
// types are the program's types, its errors are the program's errors, and
// replacing or upgrading it is a rewrite. The rule names no package, no file
// and no direction — only how far the dependency was allowed to spread.
func (program generatedProgram) verifyContainment(t *testing.T, work producedWork) {
	t.Helper()
	for _, rule := range program.containedImports {
		importers := work.importers(rule.importPath)
		if len(importers) == 0 {
			t.Errorf("the requirement is built on %s and nothing imports it",
				rule.importPath)
			continue
		}
		if len(importers) > rule.packages {
			t.Errorf("%s was to be reachable from at most %d package(s) and %d "+
				"import it: %v. Its types have become the program's types",
				rule.importPath, rule.packages, len(importers), importers)
			continue
		}
		t.Logf("%s is contained in %d package(s): %v",
			rule.importPath, len(importers), importers)
	}
}

// verifySeams checks that something was left to substitute at.
//
// A program that reaches a third-party library through an interface it declared
// can be tested without the library, and can survive the library. One that
// calls into it directly from everywhere cannot do either. Counting the
// interfaces declared outside the command is a crude reading of that, and it is
// the difference between a seam and a wish: a run that declared none has
// written code that can only be tested by running the real thing.
func (program generatedProgram) verifySeams(t *testing.T, work producedWork) {
	t.Helper()
	if program.minInterfaces == 0 {
		return
	}
	declared, where := 0, []string{}
	for _, file := range work.files {
		if file.test || file.directory == work.command || file.interfaces == 0 {
			continue
		}
		declared += file.interfaces
		where = append(where, file.directory)
	}
	if declared < program.minInterfaces {
		t.Errorf("the requirement asked for at least %d interface(s) to substitute "+
			"at and the run declared %d outside the command, so the library it "+
			"integrates can only be tested by running it", program.minInterfaces,
			declared)
		return
	}
	t.Logf("%d interface(s) declared outside the command, in %v", declared, where)
}

// outwardImports are the ways a Go package reaches the world outside itself.
//
// The clock is not among them. A package that takes an instant as an argument
// still computes from its arguments alone, and a rule about when a token
// expires has to be able to say so; forbidding the type would forbid stating
// the rule rather than forbidding the dependency.
var outwardImports = []string{
	"fmt", "os", "os/exec", "bufio", "log", "log/slog",
	"net", "net/http", "database/sql", "math/rand",
}

// verifyPureSeparation counts the packages that reach nothing outside
// themselves, without being told which they should be.
func (program generatedProgram) verifyPureSeparation(t *testing.T, work producedWork) {
	t.Helper()
	reaches := map[string]bool{}
	for _, file := range work.files {
		if file.test || file.directory == work.command {
			continue
		}
		if file.reachesOutward() {
			reaches[file.directory] = true
		}
	}
	packages := work.packages(false)
	var pure []string
	for _, directory := range packages {
		if !reaches[directory] {
			pure = append(pure, directory)
		}
	}
	if len(pure) < program.purePackages {
		t.Errorf("the requirement asked for %d package(s) that reach nothing "+
			"outside themselves and %d of the %d it wrote do: %v",
			program.purePackages, len(packages)-len(pure), len(packages), pure)
		return
	}
	t.Logf("%d package(s) reach nothing outside themselves: %v", len(pure), pure)
}

// verifyPureSymbol judges the package a named idea landed in.
//
// Purity is checked mechanically rather than by reading the code: the package
// may not import what a Go program reaches the outside world through. That is
// narrower than "functional" and it is the part that matters — a core that
// cannot print, read, or ask the time is one whose result depends on its
// arguments alone, which is what makes it testable and reusable.
func (program generatedProgram) verifyPureSymbol(
	t *testing.T,
	work producedWork,
	symbol string,
) {
	t.Helper()
	home := ""
	for _, file := range work.files {
		if !file.test && slices.Contains(file.declares, symbol) {
			home = file.directory
			break
		}
	}
	if home == "" {
		t.Errorf("the requirement asked for %s and nothing the run wrote declares "+
			"it: %v", symbol, work.paths())
		return
	}
	t.Logf("%s was declared in %s", symbol, home)
	for _, file := range work.files {
		if file.directory != home || file.test {
			continue
		}
		for _, imported := range file.imports {
			if !strictlyImpure(imported) {
				continue
			}
			t.Errorf("%s holds %s and imports %s in %s, so its result does not "+
				"depend on its arguments alone", home, symbol, imported, file.path)
		}
	}
}

// strictlyImpure reports whether an import lets a result depend on something
// other than the arguments.
//
// It is the outward set plus the clock plus any third party. A core reached
// through a named symbol was asked for as a calculation, and a calculation that
// consults the time gives two answers to one question.
func strictlyImpure(imported string) bool {
	if slices.Contains(outwardImports, imported) || imported == "time" {
		return true
	}
	leading, _, _ := strings.Cut(imported, "/")
	if !strings.Contains(leading, ".") {
		return false // the standard library
	}
	return imported != workspaceModule &&
		!strings.HasPrefix(imported, workspaceModule+"/")
}

// verifyFragments checks that a construct the requirement called for is there.
//
// Checking for a fragment is crude, and it is still the difference between "it
// printed the right number" and "it did it the way that was asked for" — a
// requirement that says the index must be maintained by triggers is not met by
// a program that maintains it in Go and prints the same thing. Where the
// fragment appears is not checked, because that was never specified.
func (program generatedProgram) verifyFragments(t *testing.T, work producedWork) {
	t.Helper()
	var everything, tests, library strings.Builder
	for _, file := range work.files {
		everything.WriteString(file.body)
		everything.WriteString("\n")
		if file.test {
			tests.WriteString(file.body)
			tests.WriteString("\n")
			continue
		}
		if file.directory != work.command {
			library.WriteString(file.body)
			library.WriteString("\n")
		}
	}
	for _, fragment := range program.mustAppear {
		if !strings.Contains(everything.String(), fragment) {
			t.Errorf("nothing the run wrote contains %q, so the requirement was "+
				"met some other way or not at all", fragment)
		}
	}
	for _, fragment := range program.mustAppearInTests {
		if !strings.Contains(tests.String(), fragment) {
			t.Errorf("no test the run wrote contains %q", fragment)
		}
	}
	// Forbidden constructs are looked for outside the command and outside the
	// tests. Wiring up the world in main is what main is for; a library that
	// does it on import, or a test that sleeps to make a race go away, is the
	// habit worth refusing.
	for _, fragment := range program.mustNotAppear {
		if strings.Contains(library.String(), fragment) {
			t.Errorf("a package outside the command contains %q, which the "+
				"requirement rules out", fragment)
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
		t.Logf("adversarial: %s survived every hostile input",
			filepath.Base(binary))
		return
	}
	t.Errorf("adversarial findings for %s:\n  %s",
		filepath.Base(binary), strings.Join(findings, "\n  "))
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
