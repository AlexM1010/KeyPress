// src/lib/stores/theme.svelte.ts
import { readable, type Readable } from 'svelte/store';
import { userPrefersMode, systemPrefersMode, setMode } from 'mode-watcher';
import type { ColorMode } from '@xyflow/svelte';
import { browser } from '$app/environment';

// A `.svelte.ts` module rather than the plain `.ts` this used to be.
//
// mode-watcher 1.x stopped exporting Svelte stores: `userPrefersMode` and
// `systemPrefersMode` are now rune-backed objects read through `.current`, and
// nothing outside a reactive context hears them change. `derived([...])`, which
// is what this file used to be built on, has nothing to subscribe to.
//
// Runes are only available in a `.svelte.ts`/`.svelte.js` module, hence the
// rename - and the file keeps exporting plain Svelte stores on the other side,
// so the four places that read `$theme` / `$flowTheme` are untouched by any of
// this. Only the import specifier moved.

function updateThemeAttribute(theme: boolean): void {
  if (browser) {
    document.documentElement.setAttribute('data-theme', theme ? 'dark' : 'light');
  }
}

/** Whether dark mode is in force, resolving "system" against the OS setting. */
function prefersDark(): boolean {
  const preference = userPrefersMode.current;
  return preference === 'dark' || (preference === 'light' ? false : systemPrefersMode.current === 'dark');
}

/**
 * Whether the app is in dark mode. `true` === dark.
 *
 * NOTE: the `data-theme` side effect lives here deliberately. mode-watcher also
 * writes `data-theme` (for its own custom-theme feature, default ''), so this
 * store must write last. It does, because the subscription is established by
 * components mounted after <ModeWatcher /> — see ThemeToggle.svelte.
 *
 * The effect is wrapped in `$effect.root` because this is a module, not a
 * component: there is no component lifetime to hang an effect off, so one is
 * created explicitly and torn down when the last subscriber leaves - which is
 * exactly what `readable`'s stop function is for. The initial value is computed
 * and applied synchronously on subscribe rather than left to the effect's first
 * run, because effects are scheduled rather than immediate and a frame of the
 * wrong `data-theme` is a visible flash of the wrong colours.
 */
export const theme: Readable<boolean> = readable(false, (set) => {
  const publish = () => {
    const dark = prefersDark();
    updateThemeAttribute(dark);
    set(dark);
  };

  publish();
  return $effect.root(() => {
    $effect(publish);
  });
});

// Flip between light/dark, or force a specific mode when `value` is given.
// `true` === dark, mirroring the `theme` store above.
export const toggleTheme = (value?: boolean): void => {
    const next = typeof value === 'boolean' ? value : !prefersDark();
    setMode(next ? 'dark' : 'light');
};

/**
 * The mode Svelte Flow is asked to render in, which unlike `theme` above keeps
 * "system" as an answer in its own right - the library resolves it itself.
 */
export const flowTheme: Readable<ColorMode> = readable<ColorMode>('system', (set) => {
  const publish = () => set(userPrefersMode.current || 'system');

  publish();
  return $effect.root(() => {
    $effect(publish);
  });
});
