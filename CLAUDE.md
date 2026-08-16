# Keypress

A Wails v3 desktop app: a Go backend driving the real mouse and keyboard, and a
SvelteKit frontend that draws the macro as a flowchart. Windows is the primary
target and the only one CI builds.

## Build the frontend before you touch Go

`main.go` does `//go:embed all:frontend/build`, and `frontend/build` is
gitignored. On a fresh checkout that directory does not exist, so **every Go
command that loads package main fails** - `go build`, `go vet ./...`,
golangci-lint, an editor's language server - with an embed error that says
nothing about the frontend. It is not a Go problem and no amount of reading Go
files will explain it.

```bash
cd frontend && npm install && npm run build
```

That is the fix, and it has to have happened at least once before any of the
above will run; `.github/workflows/main.yml` orders its steps this way for the
same reason. The output lands in `frontend/build`, not the `dist` a stock Wails
frontend uses - adapter-static puts it there, and that is the path embedded.

`go test ./backend/...` is the exception - the backend package does not import
package main, so the backend suite runs on a bare checkout.

## CGO_ENABLED=1 is not optional

robotgo reaches the OS input APIs through cgo. With cgo disabled it still
compiles, against its non-cgo stubs, and the link then fails on undefined
symbols (`Move`, `Bitmap`, ...). A stock Wails v3 app is pure Go on Windows, so
the generated platform Taskfiles default this to 0; the root `Taskfile.yml`
overrides it to 1, and CI sets it in the job env. Set it yourself for anything
run outside those. The race detector needs a working gcc for the same reason.

## Wails v3 is beta

`v3.0.0-beta.4`. v2 is the stable line, so most search results and answers
describe an API this project does not have, and v3's own API moves between
betas - read the module source before trusting anything written about it. Two
differences that have already caused bugs here: `Events.On` delivers one event
object per listener with the payload on its `.data`, and a Go slice is typed
`T[] | null` in the generated bindings, because that is what an omitted JSON
array actually arrives as.

## Commands

From the repo root (go-task; `wails3 task <name>` works too):

- `task build` - production `bin/Keypress.exe`, frontend and bindings included
- `task dev` - `wails3 dev` with hot reload, Vite on port 9245
- `task run` - runs the last build; `task package` - NSIS installer
- `go vet ./...` - needs `frontend/build` (see above)
- `go test -race ./backend/...` - the whole Go suite; needs cgo

From `frontend/`:

- `npm run build` - production build into `frontend/build`
- `npm run dev` - Vite alone, without the Go side
- `npm test` - Vitest, one pass (`test:watch` for the loop)
- `npm run check` - `svelte-check`; this is what gates types, not eslint
- `npm run lint` / `lint:fix` - ESLint
- `npm run format` / `format:check` - Prettier

CI runs format, lint, check and test before it builds anything, cheapest first.
Running those four in that order locally is the quickest way to know whether a
change survives.

## Generated code

`frontend/src/lib/bindings/` is written by `wails3 generate bindings` and is
committed. Do not hand-edit it: the generator runs with `-clean=true`, which
deletes the directory before writing, so an edit there survives until the next
build and no longer. Change the Go method and regenerate. It sits under
`src/lib`, rather than the default `frontend/bindings`, so SvelteKit's `$lib`
alias reaches it.

## The frontend is mid-migration to Svelte 5 runes

Both dialects are in the tree and a file is wholly one or the other. Check which
before editing:

- **Legacy**: `export let`, `$:`, `createEventDispatcher`, `svelte/store`. Most
  of `customNodes/` and `nodeComponents/`, and the panels under
  `routes/workspace/flowpanels/`.
- **Runes**: `$props`, `$state`, `$derived`, `$effect`. `Flow.svelte`,
  `NodeWrapper.svelte`, and the `.svelte.ts` stores.

Runes are only available in `.svelte`, `.svelte.ts` and `.svelte.js` files -
that is why `stores/flow.svelte.ts` and `stores/theme.svelte.ts` carry the
infix. Mixing the two dialects in one file is a compile error, so converting
means converting the whole file.

## The graph is deep `$state`, and that is load-bearing

`graph` in `stores/flow.svelte.ts` holds `nodes` and `edges` as **deep**
`$state`, deliberately not `$state.raw` (which Svelte Flow's own docs suggest).
Every node component edits its payload in place - `data.time = 2500` is how an
edit reaches the graph at all - and raw state does not notice a write that deep.
Consequences worth knowing before writing code against it:

- A node's `data` is a `$state` proxy, and `structuredClone` throws
  `DataCloneError` on a proxy. Anything copying a payload out - duplicating a
  node, handing one to the backend - has to take `$state.snapshot(node.data)`
  first.
- There is no "mark the graph edited" call to remember. The write is the
  notification; the dirty check re-runs off the state directly.

Dirtiness is decided by comparing `serializeMacro()` output against a baseline,
not by counting edits, because typing a value back to what it was saved at must
leave the macro clean. That function includes only the fields the Go `FlowData`
persists - selection and drag state are Svelte Flow's bookkeeping, and
including any of it would have clicking a node report unsaved changes.

## The macro is walked, and the walk is described twice

`backend/interpreter.go` runs a macro with a single execution token: it runs the
node the token is on, asks that node which output to leave by, and follows the
matching edges depth-first. A node runs **on arrival**, so a node on two paths
runs twice and a loop is nothing but an edge pointing backwards. Two rules of it
are load-bearing and easy to get wrong:

- A handler returning `next == ""` means "the only output", and takes **every**
  outgoing edge, in ascending `sourceHandle` order. It does *not* mean "the edges
  whose handle is empty" - every edge the app draws carries `sourceHandle:
  'right'`, so a handle-equality rule would strand every macro at the Start node.
- An output handle takes **exactly one** edge; fan-out is an explicit Sequence
  node. `startFlow` refuses a flowchart that breaks that, before anything runs.

`reachableFrom` in `backend/execution.go` no longer decides what runs - the token
simply never arrives at an unreachable node - and is now purely a lint: it is
what tells the user which wired-up nodes will never run.

Both of those are described a second time in
`frontend/src/lib/utils/nodeLabels.ts`, which decides how the status panel names
and orders nodes. Nothing but the shared fixtures in
`backend/testdata/reachability/` and `backend/testdata/walk/` keeps the two
agreeing - see the READMEs there before changing either side.
