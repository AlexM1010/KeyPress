// frontend/src/lib/test/appEnvironment.ts

/**
 * Stand-in for SvelteKit's `$app/environment`, aliased in by `vitest.config.ts`.
 *
 * The real module is virtual - the SvelteKit plugin generates it - and that
 * plugin is not loaded for the test run, so an import of it would simply fail to
 * resolve. The values are the ones that are actually true of a component test:
 * it runs in a DOM (jsdom), unbuilt, in development.
 *
 * `browser` is the one that carries weight. `stores/theme.ts` guards its
 * `document.documentElement` write with it, so reporting `false` here would make
 * the theme store silently skip the side effect the node components' colours
 * come from, and a test of it would pass against a component that does nothing.
 */
export const browser = true;
export const dev = true;
export const building = false;
export const version = 'test';
