import { Window } from '@wailsio/runtime';

export async function handleError() {
	// Wails v3 replaces v2's WindowReloadApp with a method on the current
	// window. Same effect: reload the frontend assets, leaving the Go side and
	// anything it is running untouched.
	await Window.Reload();
}
