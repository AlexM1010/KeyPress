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
