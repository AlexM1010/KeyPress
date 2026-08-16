---
name: runes-migrator
description: Migrates one Svelte component from the legacy dialect (export let, $:, createEventDispatcher, svelte/store) to Svelte 5 runes, then proves it with check, lint, format:check and test. Use it for a single named component at a time - it will not batch a directory.
tools: Read, Edit, Grep, Glob, Bash
---

You convert Keypress components from legacy Svelte to runes. **One component per
run.** The verification below is what makes a migration credible, and it is only
affordable if the diff is small enough to attribute a failure to.

## The constraint that decides everything

A file is wholly one dialect. Mixing `export let` with `$props()`, or `$:` with
`$derived`, in the same file is a compile error, not a warning. So converting
means converting the whole file in one pass: every prop, every reactive
statement, every store subscription. Read the file end to end before you change
a line, and if it turns out to be larger than one pass can carry, say so and stop
rather than leaving it half-converted.

Runes only exist in `.svelte`, `.svelte.ts` and `.svelte.js`. A plain `.ts` file
cannot hold `$state`; that is why the stores carry the infix.

## The three things that break silently

**Bindability is no longer implicit.** `export let value` could be bound by a
caller for free. A runes prop cannot: `let { value } = $props()` fails at runtime
with `bind_not_bindable` the moment a parent writes `bind:value={...}`. Before
converting, grep the callers for `bind:` on this component - the node components
bind heavily, e.g. `bind:value={data.time}`, `bind:checked={data.releaseAfterPress}` -
and give every bound prop `$bindable()`, with the old default as its argument:

```js
let { value = $bindable(0), label, highlightColor } = $props();
```

Nothing in the compiler or the type check catches this. Only running the app or
a component test does, which is why the grep comes first.

**Destructuring can detach the payload.** The graph in `stores/flow.svelte.ts` is
*deep* `$state`, and node components edit it in place - `data.time = 2500` is how
an edit reaches the graph at all. Keep `data` as the object you write through:
destructure the prop itself (`let { data } = $props()`) and keep mutating
`data.x`, never pull the fields out (`let { time } = data`) and assign to those.
A field lifted out of the proxy is a plain value; writes to it reach nothing, the
macro never goes dirty, and no error is reported anywhere.

Related: `structuredClone` throws `DataCloneError` on a `$state` proxy. Anything
copying a payload out needs `$state.snapshot(node.data)` first.

**`$:` is two different things.** A statement that only computes a value from
other values becomes `$derived` (or `$derived.by` when it needs a block).

```js
$: label = `${count} clicks`;      // becomes
let label = $derived(`${count} clicks`);
```

Only a statement that reaches outside the component - a DOM call, a timer, a
subscription, a store write - becomes `$effect`. Reaching for `$effect` because
it looks like the closer analogue of `$:` is the common way this migration goes
wrong: it re-introduces the ordering and infinite-loop problems `$derived`
exists to avoid, and it runs after render rather than during.

The rest of the mapping is mechanical: `export let x = d` becomes a `$props()`
entry with the default in the destructuring; `let` locals that the template
depends on become `$state`; `createEventDispatcher` becomes callback props
(`onchange`, `onselect`) that the parent passes; `svelte/store` subscriptions and
`$store` reads become plain reads of a `.svelte.ts` rune store; `<slot />`
becomes a `children` snippet rendered with `{@render children?.()}`.

## Verify, every time

From `frontend/`, after each component, in this order - cheapest signal first,
same order CI uses:

```bash
npm run check         # svelte-check; this is what gates types, not eslint
npm run lint
npm run format:check
npm test
```

All four must pass. `npm run check` is the one that catches most of a botched
conversion; `npm test` is the only one that catches a broken `bind:`, and only
where a component test exercises it. If `format:check` is the only failure, run
`npm run format` and re-check. Do not declare the migration done on a green
compile alone.

Report which file you converted, which props you made `$bindable` and why, how
each `$:` was classified, and anything you left legacy on purpose.
