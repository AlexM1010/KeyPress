// src/lib/stores/theme.ts
import { derived, get } from 'svelte/store';
import { userPrefersMode, systemPrefersMode, setMode } from 'mode-watcher';
import type { ColorMode } from '@xyflow/svelte';
import { browser } from '$app/environment'; // Add this import

function updateThemeAttribute(theme: boolean): void {
  if (browser) {
    document.documentElement.setAttribute('data-theme', theme ? 'dark' : 'light');
  }
}

// Create a derived store that combines mode-watcher states.
// NOTE: the `data-theme` side effect lives here deliberately. mode-watcher also
// writes `data-theme` (for its own custom-theme feature, default ''), so this
// store must write last. It does, because the subscription is established by
// components mounted after <ModeWatcher /> — see ThemeToggle.svelte.
export const theme = derived(
    [userPrefersMode, systemPrefersMode],
    ([$userPrefersMode, $systemPrefersMode]) => {
        const theme = $userPrefersMode === 'dark' ||
        ($userPrefersMode === 'light' ? false : $systemPrefersMode === 'dark');
        updateThemeAttribute(theme);
        return theme;
    }
);

// Flip between light/dark, or force a specific mode when `value` is given.
// `true` === dark, mirroring the `theme` store above.
export const toggleTheme = (value?: boolean): void => {
    const next = typeof value === 'boolean' ? value : !get(theme);
    setMode(next ? 'dark' : 'light');
};

// More efficient store for Svelte Flow - directly derives from userPrefersMode
export const flowTheme = derived<typeof userPrefersMode, ColorMode>(
    userPrefersMode,
    ($userPrefersMode) => $userPrefersMode || 'system'
);