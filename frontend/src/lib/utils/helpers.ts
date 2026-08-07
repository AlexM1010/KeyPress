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
 * Whether a failed call failed because the Go side was not reachable at all.
 *
 * Wails v3 calls the backend over HTTP rather than through an object the
 * runtime hangs off `window`, so "is Go there?" can no longer be answered up
 * front - it is only knowable from how a call failed. With nothing listening,
 * `fetch` rejects with a TypeError long before any Go code is reached, and that
 * is what this recognises.
 *
 * Deliberately loose about the wording: browsers phrase this differently
 * ("Failed to fetch", "NetworkError when attempting to fetch resource", "Load
 * failed"), and a missed match only costs a less specific message, never a
 * wrong one.
 */
export const isBackendUnreachable = (error: unknown): boolean => {
	if (error instanceof TypeError) return true;
	return /failed to fetch|networkerror|load failed/i.test(describeError(error));
};

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
	isBackendUnreachable(error)
		? 'Not connected to the KeyPress backend. The desktop app has to be running the current build - restart it and try again.'
		: describeError(error);
