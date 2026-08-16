import { fileURLToPath } from 'node:url';
import { svelte, vitePreprocess } from '@sveltejs/vite-plugin-svelte';
import { defineConfig } from 'vitest/config';

const fromHere = (path: string) => fileURLToPath(new URL(path, import.meta.url));

// Vitest's own config rather than a `test` block bolted onto `vite.config.ts`,
// which it would otherwise pick up. That file exists to serve the app: it loads
// the SvelteKit plugin and pins the dev server to the port Wails dials, and
// neither has anything to do with running these tests. Keeping them apart means
// the test run cannot perturb the dev server, and `vite.config.ts` stays a file
// about the app.
//
// The suite is in two halves and the environment is what separates them. Most of
// what is tested here is a plain `.ts` module - see `src/lib/utils` - with no
// Svelte compilation and no DOM behind it, so the default `node` environment is
// the right one and stays the default. The component tests need a DOM, and say
// so *per file* with a `// @vitest-environment jsdom` docblock rather than being
// carved out by a glob here. Two reasons: a file declares its own needs where
// someone reading it will see them, and the docblock is the one mechanism that
// has survived every Vitest major - `environmentMatchGlob`, which is what a glob
// here would have used, was removed in Vitest 3. Vitest picks the transform to
// match the environment, so the component tests get the browser build of Svelte
// while the node half is untouched.
export default defineConfig({
	// The app's Svelte config is not reused. `svelte.config.js` is read by the
	// SvelteKit plugin and carries the static adapter with it, which would drag a
	// whole build pipeline into a test run; the preprocessor is the only part of
	// it these tests need, to get `<script lang="ts">` compiled.
	plugins: [svelte({ preprocess: vitePreprocess() })],
	resolve: {
		// Without this, `svelte` and `svelte/internal` resolve to two different
		// copies of the runtime and `onMount` never fires. A compiled component
		// takes its mount machinery - `current_component` and the render
		// callbacks - from `svelte/internal`, while its `import { onMount } from
		// 'svelte'` lands in the other copy, so the callback is queued onto a
		// component record nothing ever mounts. Nothing throws and nothing
		// renders differently; the callback is simply never called.
		//
		// That is silent for most of this suite and fatal for `StartNode`, whose
		// keydown/keyup listeners are registered in an `onMount` - every one of
		// its recording tests read as though the component were broken. The
		// browser condition is what an actual Vite app build resolves under, so
		// this is putting the test run on the same footing rather than teaching
		// it a trick.
		conditions: ['browser'],
		alias: {
			$lib: fromHere('./src/lib'),
			// `$app/*` is a virtual module the SvelteKit plugin supplies, and that
			// plugin is deliberately absent above - so anything reaching for one
			// has to be pointed somewhere real. Only `$app/environment` is on the
			// import graph the components pull in (`stores/theme.ts` reads
			// `browser` from it); the rest would be an unused stub, so they are
			// left to fail loudly if a test ever drags one in.
			'$app/environment': fromHere('./src/lib/test/appEnvironment.ts')
		}
	},
	test: {
		// Tests sit next to the module they cover, so a function and its spec are
		// read together and neither can be moved without the other being noticed.
		include: ['src/**/*.test.ts'],
		setupFiles: [fromHere('./src/lib/test/setup.ts')],
		coverage: {
			provider: 'v8',
			reporter: ['text', 'html'],
			reportsDirectory: fromHere('./coverage'),
			// `include` has to be spelled out. Vitest 4 removed `coverage.all` and
			// `coverage.extensions`, and what replaced them reports only the files
			// some test actually imported - so a module with no test at all is not
			// 0% in the table, it is absent from the table, and the percentages read
			// as though the untested half of the app did not exist. Naming the globs
			// puts every source file in the denominator whether a test reaches it or
			// not, which is the only number worth looking at.
			include: ['src/**/*.{ts,svelte}'],
			exclude: [
				// Written by `wails3 generate bindings -clean=true`. Not ours to test
				// and not ours to edit - the generator deletes the directory first.
				'src/lib/bindings/**',
				// The tests themselves, and the harness they mount components with.
				// Counting either inflates the total with code whose only caller is
				// the coverage run.
				'src/**/*.test.ts',
				'src/lib/test/**',
				// Config and SvelteKit's own scaffolding, which carry no logic.
				'src/**/*.d.ts',
				'src/app.d.ts',
				'src/hooks.*.ts'
			]
			// Deliberately no `thresholds`. This is here to be looked at, not to
			// fail a build - a threshold set at today's number turns every honest
			// commit that adds a file into a red CI run.
		}
	}
});
