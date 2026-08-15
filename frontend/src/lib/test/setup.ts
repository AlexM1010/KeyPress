// frontend/src/lib/test/setup.ts
import { afterEach } from 'vitest';

/**
 * What every component test needs before it renders anything.
 *
 * All of it is guarded on there being a DOM at all, because this file runs for
 * *both* halves of the suite - the node-environment tests in `src/lib/utils`
 * load it too, and must not pay for or trip over a browser polyfill. The import
 * of `@testing-library/svelte` is dynamic for the same reason: it pulls in the
 * browser build of Svelte, which has no business being loaded to test a string
 * of CSS.
 */
if (typeof window !== 'undefined') {
	// jsdom implements no `window.matchMedia`, and the node components reach for
	// it without ever naming it: `ContextMenu` reads `$theme`, which derives from
	// mode-watcher, which asks the media query for the system colour scheme the
	// moment anything subscribes. `NodeWrapper` renders `ContextMenu`, so every
	// node in the app is downstream of this.
	//
	// Unstubbed it throws *inside the component's init*, which means the node
	// never mounts and the failure surfaces as a missing button rather than a
	// missing browser API - a long way from the cause. It lives here rather than
	// in the files that happen to need it today because which components are
	// theme-aware is not something the next test author should have to know.
	if (typeof window.matchMedia !== 'function') {
		window.matchMedia = ((query: string) => ({
			matches: false,
			media: query,
			onchange: null,
			addEventListener: () => {},
			removeEventListener: () => {},
			addListener: () => {},
			removeListener: () => {},
			dispatchEvent: () => false
		})) as unknown as typeof window.matchMedia;
	}

	// ResizeObserver is what Svelte Flow watches the canvas with. No test here
	// renders the canvas, but the library's store is built by `SvelteFlowProvider`
	// (see `NodeHarness.svelte`) and reaches for it on the way up. A no-op is
	// enough: nothing under test resizes.
	if (typeof globalThis.ResizeObserver === 'undefined') {
		globalThis.ResizeObserver = class {
			observe() {}
			unobserve() {}
			disconnect() {}
		} as unknown as typeof ResizeObserver;
	}

	// Unmount whatever the test rendered. `@testing-library/svelte` registers
	// this itself only when Vitest's globals are switched on, and they are not -
	// this suite imports what it uses. A setup file runs per test file, so the
	// hook lands on each file's root suite exactly as a hand-written
	// `afterEach(cleanup)` would, and no test file has to remember it.
	//
	// It matters more here than in a typical component suite: `StartNode` binds
	// keydown/keyup handlers to `window` and removes them in `onDestroy`, so a
	// node left mounted would keep answering keystrokes meant for the next test.
	const { cleanup } = await import('@testing-library/svelte');
	afterEach(cleanup);
}
