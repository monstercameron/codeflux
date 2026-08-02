# Control-Flow APIs in Strict-Functional Standard Libraries

Research input for the functional graph layer (`docs/plan.md` §5, "Functional Graph" and "Core Graph Entities"). Not a specification, not a plan section, and not authorization to build anything: §5 is gated behind the deferred items POST-001..005 in `internal/deferred`.

## 1. Scope and method

**"Strict" means eager evaluation, not "strictly functional."** That excludes Haskell and PureScript-style laziness from the comparison and keeps the languages whose evaluation order a graph can represent as an ordered edge without also modelling thunk forcing.

Six languages, chosen to span the three design positions a strict-FP standard library can take on control flow:

| Language | Library surveyed | Position |
| --- | --- | --- |
| Standard ML | SML '97 Basis Library | Minimal, formally specified |
| OCaml | Stdlib 5.x | Minimal core plus effect handlers |
| F# | FSharp.Core (.NET) | Mid-size, computation expressions |
| Scala 3 | `scala.*` stdlib (3.3+) | Large, direct-style plus monadic |
| Elm | `elm/core` 0.19.1 | Deliberately tiny, no exceptions |
| Roc | builtins, current compiler | Tiny, effects live outside the stdlib |

**"Control flow API" means** anything the standard library exposes that decides *what runs next*: sequencing, branching, early exit, error propagation, exception handling, iteration, effect performance, concurrency, and cleanup. Data structures, numerics, string handling, and serialization are out of scope even where they carry higher-order functions.

Signatures below were verified against the sources listed in §7 except where marked. Where a language expresses a construct as syntax rather than as an API, that is stated — the absence is as informative as the presence.

## 2. The convergent core

Nine constructs, and how many of the six standardize each:

| Construct | 6/6 | Notes |
| --- | --- | --- |
| Branch on a sum type (`match`) | ✅ | Syntax in all six, never an API |
| Sequencing with failure short-circuit (`bind`/`andThen`) | ✅ | An API in all six |
| Map / recover over a failure channel | ✅ | `mapError` or equivalent everywhere |
| Fold over a finite structure | ✅ | The only iteration primitive all six share |
| Function composition and identity | ✅ | `o`, `>>`, `<<`, `identity`, `always` |
| Early exit from a computation | 5/6 | Elm has none; the rest differ wildly in mechanism |
| Exception raise/handle | 4/6 | Elm and Roc have no catchable exceptions at all |
| Resource cleanup that survives failure | 4/6 | Absent from SML Basis and Elm |
| Concurrency / deferred work | 5/6 | Absent from SML Basis |

**Not standardized by any of the six:** retry, backoff, reconciliation, compensation, bounded iteration with a static bound, explicit merge/join points, deduplication, or idempotency keys. Every one of those is library territory in every language surveyed. This is the single most load-bearing finding for the graph layer — see §6.

## 3. Per-language inventories

### 3.1 Standard ML — the floor

The Basis Library is the smallest control-flow surface of the six, and the only one with a formal specification. Its entire built-in control vocabulary is exceptions plus higher-order functions.

`General` (opened by default) is the whole of it:

```sml
type exn                              (* extensible: exception decls add constructors *)
exception Bind      (* val-binding pattern match failed *)
exception Match     (* case/application pattern match failed *)
exception Div | Domain | Overflow | Size | Subscript | Chr | Span
exception Fail of string
val exnName    : exn -> string
val exnMessage : exn -> string
datatype order = LESS | EQUAL | GREATER
val o      : ('b -> 'c) * ('a -> 'b) -> 'a -> 'c   (* composition *)
val before : 'a * unit -> 'a                       (* sequence, keep the first *)
val ignore : 'a -> unit
```

Error values, as opposed to exceptions, go through `Option`:

```sml
val getOpt         : 'a option * 'a -> 'a
val valOf          : 'a option -> 'a          (* raises Option *)
val mapPartial     : ('a -> 'b option) -> 'a option -> 'b option   (* this is bind *)
val join           : 'a option option -> 'a option
val composePartial : ('b -> 'c option) * ('a -> 'b option) -> 'a -> 'c option
```

Iteration is `List.foldl` / `List.foldr` / `List.find` / `List.mapPartial` / `List.tabulate`, and nothing else. There is no `Result`, no async, no cleanup combinator, and no continuation capture — `callcc`/`throw` are an SML/NJ extension (`SMLofNJ.Cont`), explicitly *not* Basis.

**Why it matters here:** SML is the proof that a *complete, specified, trusted* control-flow kernel fits in roughly a dozen names. If the Tier-0 kernel wants a defensible lower bound, this is it.

### 3.2 OCaml — minimal core plus a real effect system

The value-level surface is close to SML's, with `Result` and `Either` added:

```ocaml
val Result.bind      : ('a, 'e) result -> ('a -> ('b, 'e) result) -> ('b, 'e) result
val Result.map_error : ('e -> 'f) -> ('a, 'e) result -> ('a, 'f) result
val Result.fold      : ok:('a -> 'c) -> error:('e -> 'c) -> ('a, 'e) result -> 'c
val Either.fold      : left:('a -> 'c) -> right:('b -> 'c) -> ('a, 'b) Either.t -> 'c
val Option.bind      : 'a option -> ('a -> 'b option) -> 'b option
```

Binding-operator *syntax* (`let*`, `and*`) exists since 4.08, but the Stdlib does not ship the operators — each library defines its own. Sequencing is therefore a convention, not an API.

Cleanup is standardized, and its edge cases are specified:

```ocaml
val Fun.protect : finally:(unit -> unit) -> (unit -> 'a) -> 'a
exception Fun.Finally_raised of exn
```

`protect` runs `finally ()` whether the body returns or raises, re-raising afterwards. If `finally` itself raises, `Finally_raised` replaces the original exception — the original is *lost*. That failure mode is worth carrying into any compensation design: the compensating action can destroy the evidence of what it was compensating for.

Iteration gained a first-class lazy sequence type, which is where OCaml puts its loop vocabulary:

```ocaml
val Seq.unfold     : ('b -> ('a * 'b) option) -> 'b -> 'a Seq.t   (* producer *)
val Seq.iterate    : ('a -> 'a) -> 'a -> 'a Seq.t                 (* infinite *)
val Seq.forever    : (unit -> 'a) -> 'a Seq.t                     (* infinite *)
val Seq.cycle      : 'a Seq.t -> 'a Seq.t                         (* infinite *)
val Seq.take_while : ('a -> bool) -> 'a Seq.t -> 'a Seq.t         (* the bound *)
val Seq.scan       : ('b -> 'a -> 'b) -> 'b -> 'a Seq.t -> 'b Seq.t
val Seq.fold_left  : ('acc -> 'a -> 'acc) -> 'acc -> 'a Seq.t -> 'acc  (* consumer *)
val Seq.once       : 'a Seq.t -> 'a Seq.t     (* single-consumption enforcement *)
val Seq.memoize    : 'a Seq.t -> 'a Seq.t
```

Note the shape: unbounded producers, and separately a `take_while` that imposes the bound. The bound is a *value*, never a type-level guarantee.

The distinctive API is effect handlers (OCaml 5), which are the only standardized delimited-continuation mechanism among the six:

```ocaml
type _ Effect.t = ..
val Effect.perform : 'a Effect.t -> 'a
exception Effect.Unhandled : 'a Effect.t -> exn
exception Effect.Continuation_already_resumed

(* deep: handles every effect until the computation terminates *)
val Effect.Deep.match_with : ('c -> 'a) -> 'c -> ('a, 'b) Effect.Deep.handler -> 'b
type ('a,'b) handler = { retc : 'a -> 'b; exnc : exn -> 'b;
                         effc : 'c. 'c Effect.t -> (('c,'b) continuation -> 'b) option }
val Effect.Deep.continue    : ('a,'b) continuation -> 'a -> 'b
val Effect.Deep.discontinue : ('a,'b) continuation -> exn -> 'b

(* shallow: handles exactly one effect; the continuation excludes the handler *)
val Effect.Shallow.continue_with    : ('c,'a) continuation -> 'c -> ('a,'b) handler -> 'b
val Effect.Shallow.discontinue_with : ('c,'a) continuation -> exn -> ('a,'b) handler -> 'b
```

`Continuation_already_resumed` is the important detail: continuations are **one-shot**. Concurrency is `Domain.spawn : (unit -> 'a) -> 'a Domain.t` / `Domain.join`.

**Why it matters here:** the handler record is a three-way join — normal return, exception, effect — over one computation. That is structurally the same fan-in the graph calls an explicit merge, and it is the only place in any of the six stdlibs where that join is a named, typed value rather than syntax.

### 3.3 F# — computation expressions as the extension point

`Result` and `Option` are ordinary modules; `Result` is small and total:

```fsharp
Result.bind         : ('T -> Result<'U,'E>) -> Result<'T,'E> -> Result<'U,'E>
Result.map          : ('T -> 'U) -> Result<'T,'E> -> Result<'U,'E>
Result.mapError     : ('E -> 'F) -> Result<'T,'E> -> Result<'T,'F>
Result.defaultValue : 'T -> Result<'T,'E> -> 'T
Result.defaultWith  : ('E -> 'T) -> Result<'T,'E> -> 'T
Result.fold         : ('S -> 'T -> 'S) -> 'S -> Result<'T,'E> -> 'S
Result.isOk / isError / contains / exists / forall / count / iter
Result.toOption / toValueOption / toList / toArray
```

Note what is *missing*: no `Result.traverse`, no applicative combination, no `Result.sequence`. Multi-input joins are not in FSharp.Core.

The real control-flow surface is computation expressions — `async { }`, `task { }`, `seq { }` — which desugar `let!`, `do!`, `return!`, `use!`, `try/with`, `try/finally`, `for`, and `while` into builder-method calls (`Bind`, `Return`, `TryWith`, `TryFinally`, `Using`, `While`, `Delay`, `Combine`, `Zero`). This is the most *reifiable* control flow of the six: the desugaring is a mechanical translation of every control construct into a named method on a value, which is very close to what lowering a control graph to source has to do in reverse.

`Async` carries the operational control:

```fsharp
Async.Catch       : Async<'T> -> Async<Choice<'T,exn>>
Async.Parallel    : seq<Async<'T>> -> Async<'T[]>
Async.Sequential  : seq<Async<'T>> -> Async<'T[]>
Async.StartChild  : Async<'T> -> Async<Async<'T>>
Async.OnCancel    : (unit -> unit) -> Async<IDisposable>
Async.TryCancelled: Async<'T> * (OperationCanceledException -> unit) -> Async<'T>
```

Cleanup is `use` / `IDisposable` (syntax, deterministic, exception-safe). Branching is extensible through **active patterns** — `(|Even|Odd|)` for total matches and `(|Int|_|)` for partial ones — which make `match` dispatch on a user-defined function rather than on the runtime representation. No other language here can do that in the standard language.

### 3.4 Scala 3 — three overlapping styles

Scala standardizes the most and agrees with itself the least. Three coexisting error channels:

```scala
Option[A]          // map, flatMap, filter, fold, getOrElse, orElse
Either[A, B]       // map, flatMap, fold, left, swap, cond, filterOrElse
Try[A]             // map, flatMap, recover, recoverWith, fold, toEither, transform
```

Early exit became structured in 3.3:

```scala
import scala.util.boundary, boundary.break
boundary:                       // establishes a labelled scope
  for (x, i) <- xs.zipWithIndex do
    if x == target then break(i)   // returns a value from the boundary
  -1
```

`break` throws a `Break` exception that extends `RuntimeException` with stack-trace generation suppressed, and the boundary label is a capability that can be passed inward. This replaces `scala.util.control.Breaks` and ad-hoc non-local returns, and it is the cleanest existing model of *escape as a scoped, typed capability* rather than as an unstructured jump.

Supporting control APIs:

```scala
scala.util.Using(resource)(f)   : Try[R]        // acquire/release, exception-safe
scala.util.Using.resource(r)(f) : R             // same, unwrapped
scala.util.Using.Manager        { m => ... }    // multiple resources
scala.util.control.NonFatal     : extractor separating recoverable from fatal
scala.util.control.TailCalls    : TailRec[A], tailcall, done, result  // trampolining
```

`Future` supplies the asynchronous joins that FSharp.Core omits: `flatMap`, `recover`, `recoverWith`, `transform`, `zip`, `fallbackTo`, `Future.sequence`, `Future.traverse`, `Future.firstCompletedOf`.

**Why it matters here:** `NonFatal` and `TailCalls` are the two ideas worth stealing. `NonFatal` says the failure channel is not uniform — some failures are recoverable and some must propagate untouched, and the *stdlib* draws that line rather than leaving it to each call site. `TailCalls` says unbounded recursion needs an explicit reified representation to be safe.

### 3.5 Elm — control flow with no escape hatch

Elm has no exceptions, no `throw`, no `catch`, no cleanup combinator, and no early return. Every failure is a value, and the compiler enforces that every failure is handled. The entire control-flow surface:

```elm
-- failure as a value
Maybe.andThen   : (a -> Maybe b) -> Maybe a -> Maybe b
Maybe.withDefault : a -> Maybe a -> a
Result.andThen  : (a -> Result x b) -> Result x a -> Result x b
Result.mapError : (x -> y) -> Result x a -> Result y a
Result.map2..map5, Result.fromMaybe, Result.toMaybe, Result.withDefault

-- deferred effects as a value
type Task x a
Task.succeed  : a -> Task x a
Task.fail     : x -> Task x a
Task.andThen  : (a -> Task x b) -> Task x a -> Task x b
Task.onError  : (x -> Task y a) -> Task x a -> Task y a      -- the recovery edge
Task.mapError : (x -> y) -> Task x a -> Task y a
Task.map2..map5                                              -- the merge/join
Task.sequence : List (Task x a) -> Task x (List a)
Task.perform  : (a -> msg) -> Task Never a -> Cmd msg        -- cannot fail
Task.attempt  : (Result x a -> msg) -> Task x a -> Cmd msg   -- may fail

-- scheduling
Process.sleep : Float -> Task x ()
Process.spawn : Task x a -> Task y Process.Id
Process.kill  : Id -> Task x ()
Platform.Cmd.batch / none / map ; Platform.Sub.batch / none / map
Basics.never  : Never -> a
```

Two APIs carry disproportionate weight.

`Task.perform : (a -> msg) -> Task Never a -> Cmd msg` versus `Task.attempt : (Result x a -> msg) -> Task x a -> Cmd msg` is a **type-level proof obligation discharged at a boundary**: `perform` is only callable when the error type is `Never`, i.e. when the type system has proved the task cannot fail. The caller does not choose whether to handle errors — the type does.

`Basics.never : Never -> a` is the witness that makes that work: an uninhabited type whose eliminator can produce anything, because it can never be called.

**Why it matters here:** this is the closest thing in any surveyed stdlib to codeflux's "guarantees attach to obligations, not to atoms." Elm attaches the guarantee to a *type at an edge*, and the graph's equivalent question is whether an obligation is a node, an edge annotation, or a type on a value.

### 3.6 Roc — the effect boundary lives outside the library

Roc is pre-1.0 and its effect model changed materially: `Task` was **removed** entirely in favour of effectful functions marked with a `!` suffix, and the success/failure type in the current compiler is `Try(ok, err)` with `Ok` / `Err` tags.

```roc
read_str! : Path => Try(Str, ReadFileErr)   -- => is an effectful function
parse     : Str  -> Try(Ast, ParseErr)      -- -> is pure

first_str = strings.first()?                -- ? early-returns the Err
value     = risky()? |_| CustomError(info)  -- ? with error replacement

match animals {
    ["bird", "crab", "lizard"] => 10
    ["bird", "crab", ..]       => 5
    _                          => 0
}

expect !digits.is_empty()   -- checked in dev/test builds, elided under --opt=speed
dbg x                       -- allowed inside pure functions
crash "unreachable"         -- not error handling; terminates
```

The structural decision matters more than the syntax: **Roc's standard library contains no effectful functions at all.** An application declares exactly one *platform*, and the platform supplies every effect:

```roc
app [main!] { pf: platform "https://..." }
```

Purity is therefore not a property a function claims — it is a property of *where the function came from*. And `crash` is deliberately not catchable: there is exactly one failure channel (`Try`) for recoverable failure and one non-channel for the rest.

**Why it matters here:** this is a working implementation of the capability boundary in `docs/plan.md` §5. It says the effect vocabulary is not part of the trusted core; it is injected at the edge, and the core is verified against its absence. It is also the strongest available argument that a Tier-0 kernel should contain *no* effect atoms whatsoever.

## 4. Where they diverge

Four forks, each of which the graph layer has to pick a side of.

**Fork 1 — is there a second, uncatchable failure channel?** SML, OCaml, F#, and Scala all have exceptions that bypass the value-level failure type. Elm and Roc do not: Elm has no exceptions, Roc has `crash` which is terminal by design. Scala is the only one whose stdlib draws the line explicitly, with `NonFatal`.

**Fork 2 — how does a computation escape early?** Six mechanisms, no convergence: SML raises; OCaml raises or `discontinue`s a continuation; F# uses exceptions inside computation expressions; Scala 3 uses a scoped `boundary`/`break` capability; Roc uses the `?` postfix operator; Elm has no escape at all and forces explicit `andThen` chaining. Scala's is the only one that is both structured *and* a value.

**Fork 3 — are effects values, handlers, or provenance?** Elm makes them values (`Task`, `Cmd`). OCaml makes them performable operations with handlers. Roc makes them a property of origin (platform-supplied). F# and Scala make them ambient and unmarked. These are genuinely different theories, and they produce different graphs: values want data edges, handlers want a join node, provenance wants a capability boundary at the region edge.

**Fork 4 — is cleanup standardized?** OCaml (`Fun.protect`), F# (`use`), and Scala (`Using`) standardize acquire/release. SML and Elm do not. Nobody standardizes *compensation* — undoing a completed effect is categorically different from releasing a held resource, and no surveyed stdlib attempts it.

## 5. The most important APIs, ranked

If only ten APIs from this survey inform the graph layer, these:

1. `Effect.Deep.match_with` + its `{ retc; exnc; effc }` handler record (OCaml) — a typed three-way join over one computation.
2. `Task.perform : ... -> Task Never a -> Cmd msg` (Elm) — an obligation discharged by a type at an edge.
3. `boundary` / `break` (Scala 3) — escape as a scoped capability rather than a jump.
4. Roc's platform declaration — the effect vocabulary injected at the boundary, absent from the core.
5. `Fun.protect ~finally` and `Fun.Finally_raised` (OCaml) — cleanup, and the documented way cleanup destroys evidence.
6. `NonFatal` (Scala) — the stdlib, not the call site, deciding which failures are recoverable.
7. F# computation-expression desugaring (`Bind`/`TryWith`/`TryFinally`/`Using`/`While`/`Combine`) — every control construct mechanically reified as a named method.
8. `Seq.unfold` + `Seq.take_while` (OCaml) — unbounded production separated from the value that bounds it.
9. `Result.mapError` / `Task.onError` (all six) — the failure channel is transformable, not just short-circuiting.
10. `General`'s dozen names (SML) — the defensible floor for a trusted control-flow kernel.

## 6. What this implies for the graph layer

**6.1 Three of the six planned edge classes have no stdlib precedent.** `docs/plan.md` §5 and `internal/graph/model.go:94-101` name `control`, `data-provenance`, `evidence-dependency`, `retry`, `reconciliation`, and `compensation`. Standard libraries across all six languages standardize sequencing, branching, failure propagation, and resource cleanup — and *none* of them standardize retry, reconciliation, or compensation. Those three are inventing vocabulary rather than adopting it. That is a legitimate thing to do; it is not legitimate to assume the invented terms carry the same shared understanding as the borrowed ones, and it means the disposable-graph experiment cannot lean on familiarity for them.

**6.2 "Bounded loops with stable iteration provenance" has no precedent either.** Every surveyed stdlib offers exactly two iteration shapes: fold over an already-finite structure, or general recursion with no bound. OCaml comes closest by separating the unbounded producer (`unfold`, `forever`, `cycle`) from the bounding consumer (`take_while`), but the bound is a runtime value, never a static guarantee. If the graph wants a *statically* bounded loop, that is novel work, not a port.

**6.3 Explicit merge is the right call and needs a non-stdlib source.** No stdlib names a join point — all six express it as syntax (`match` arms converging, `if` branches converging). The only exception is OCaml's handler record, which reifies a three-way join as a value. So the merge-node vocabulary should come from SSA/compiler literature, and OCaml's `{ retc; exnc; effc }` is the one API worth using as a shape reference.

**6.4 The kernel should contain no effect atoms.** Roc's platform model and SML's Basis both argue the same thing from opposite ends: the trusted core is defined by what it *cannot* do. `docs/plan.md` §6 already scopes the kernel as trusted primitive semantics; the survey supports scoping it as pure-only, with the entire effect vocabulary arriving through the capability boundary. It also supports a much smaller kernel than the plan currently implies — SML's specified control kernel is about a dozen names.

**6.5 One finding transfers to the shipped explanatory graph.** `Fun.protect`'s `Finally_raised` behaviour — the cleanup action destroying the record of the failure it was cleaning up after — is exactly the failure mode in `internal/coordinator/agent_graph.go:274-284`, where a rejected projection sets `available = false` and every subsequent fact in the run is silently dropped. The recorder does keep `failure`, which is the right instinct; the OCaml precedent says that keeping the *first* error and refusing to let a later one overwrite it is the property that matters.

## 7. Sources

- [OCaml manual — `Effect.Deep`](https://ocaml.org/manual/5.4/api/Effect.Deep.html), [`Effect.Shallow`](https://ocaml.org/manual/5.3/api/Effect.Shallow.html), [`Stdlib.Effect`](https://ocaml.org/manual/5.3/api/Stdlib.Effect.html), [effects language extension](https://ocaml.org/manual/5.5/effects.html)
- [OCaml manual — `Seq`](https://ocaml.org/manual/5.3/api/Seq.html)
- [OCaml — `Fun`](https://ocaml.org/api/Fun.html), [4.08 release notes](https://ocaml.org/releases/4.08.0), [`protect` double-exception discussion](https://github.com/ocaml/ocaml/pull/2118)
- [SML Basis Library — `General`](https://smlfamily.github.io/Basis/general.html)
- [FSharp.Core — `Result` module](https://fsharp.github.io/fsharp-core-docs/reference/fsharp-core-resultmodule.html)
- [Scala 3 — `scala.util.boundary`](https://www.scala-lang.org/api/current/scala/util/boundary$.html), [`scala.util.control`](https://www.scala-lang.org/api/current/scala/util/control.html), [boundary source at 3.3.0-RC4](https://github.com/scala/scala3/blob/3.3.0-RC4/library/src/scala/util/boundary.scala), [Scala 3 Book — Control Structures](https://docs.scala-lang.org/scala3/book/control-structures.html)
- [`elm/core` — `Task` source](https://raw.githubusercontent.com/elm/core/master/src/Task.elm)
- [Roc — mini tutorial, new compiler](https://github.com/roc-lang/roc/blob/main/docs/mini-tutorial-new-compiler.md), [roc-lang.org — Functional](https://www.roc-lang.org/functional), [Roc concepts explained](https://github.com/roc-lang/roc/wiki/Roc-concepts-explained), [Error Handling Basic](https://www.roc-lang.org/examples/ErrorHandlingBasic/README)
