import { defineConfig } from 'vitest/config';

// Vitest's own config rather than a `test` block bolted onto `vite.config.ts`,
// which it would otherwise pick up. That file exists to serve the app: it loads
// the SvelteKit plugin and pins the dev server to the port Wails dials, and
// neither has anything to do with running these tests. Keeping them apart means
// the test run cannot perturb the dev server, and `vite.config.ts` stays a file
// about the app.
//
// Everything under test here is a plain `.ts` module - see `src/lib/utils` - so
// there is no Svelte compilation and no DOM to emulate. The default `node`
// environment is therefore the right one and is left alone; component testing
// would need `@testing-library/svelte` and jsdom, and none of it is needed to
// test a graph-ordering algorithm or a string of CSS.
export default defineConfig({
	test: {
		// Tests sit next to the module they cover, so a function and its spec are
		// read together and neither can be moved without the other being noticed.
		include: ['src/**/*.test.ts']
	}
});
