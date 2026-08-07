export { };

// Wails v3 has no `window.runtime`: the runtime is the `@wailsio/runtime` npm
// package, which ships its own typings, and the Go bindings are the generated
// modules under `$lib/bindings`. Both are typed at their source, so there is
// nothing left for this file to declare - it stays only because SvelteKit
// picks up ambient declarations from `src`, and a future one belongs here.
