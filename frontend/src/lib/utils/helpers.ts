// Small shared helpers.
//
// The date/duration helpers that used to live here only ever fed the portfolio
// template's project cards, and went when those did.

export const useTitle = (title: string, suffix: string): string => {
	return `${title} | ${suffix}`;
};

/**
 * Text of whatever a failed call threw.
 *
 * Wails rejects a bound call with the Go error's text as a plain string, not an
 * `Error`, so reading `.message` alone would throw away every backend reason -
 * exactly the ones the user needs when a save is refused or a macro will not
 * load.
 */
export const describeError = (error: unknown): string => {
	if (typeof error === 'string') return error;
	if (error instanceof Error) return error.message;
	return String(error);
};

/**
 * Whether the Go side of the app is actually there.
 *
 * The generated bindings reach straight into `window.go.backend.App` with no
 * check of their own, so when it is missing they throw a bare "Cannot read
 * properties of undefined (reading 'App')" - a message about the shape of an
 * object, handed to someone who was trying to save their work.
 *
 * Used to explain a failure, never to pre-empt one: calls are always attempted,
 * so a runtime that turns up in some way this check does not recognise still
 * gets to work.
 */
export const hasGoRuntime = (): boolean =>
	typeof window !== 'undefined' &&
	Boolean((window as { go?: { backend?: { App?: unknown } } }).go?.backend?.App);

/**
 * Text for a failed call to the backend.
 *
 * Same as `describeError`, except that the backend simply not being reachable -
 * the frontend served on its own with `npm run dev`, or a desktop build whose
 * bindings no longer match the Go package - is named as what it is rather than
 * passed on as the runtime's own wording. Nothing the user did caused it and
 * nothing about their macro will fix it, so the message says where to look.
 */
export const describeBackendError = (error: unknown): string =>
	hasGoRuntime()
		? describeError(error)
		: 'Not connected to the KeyPress backend. The desktop app has to be running the current build - restart it and try again.';
