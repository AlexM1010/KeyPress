import { describe, expect, it } from 'vitest';
import { describeBackendError, describeError, isBackendUnreachable } from './helpers';

describe('describeError', () => {
	it('passes a bare string through', () => {
		// Wails rejects a bound call with the Go error's text as a plain string
		// rather than an Error, and that text is the whole reason the user is
		// reading the message.
		expect(describeError('macro name already taken')).toBe('macro name already taken');
	});

	it('reads the message off an Error', () => {
		expect(describeError(new Error('disk full'))).toBe('disk full');
	});
});

describe('isBackendUnreachable', () => {
	it('recognises a refused connection, however the browser words it', () => {
		expect(isBackendUnreachable(new TypeError('Failed to fetch'))).toBe(true);
		expect(isBackendUnreachable('NetworkError when attempting to fetch resource')).toBe(true);
		expect(isBackendUnreachable('Load failed')).toBe(true);
	});

	it('recognises the app’s own page coming back instead of an answer', () => {
		// The `npm run dev` case, and the one that actually bites. SvelteKit's
		// SPA fallback answers the backend endpoint with 200 and index.html, so
		// nothing rejects at the network layer and the runtime throws with the
		// page source as its message. Recognising it is what puts "Not connected
		// to KeyPress" on screen instead of a document.
		const page = '<!doctype html>\r\n<html lang="en">\r\n\t<head>\r\n\t</head>\r\n</html>';
		expect(isBackendUnreachable(page)).toBe(true);
		expect(isBackendUnreachable(new Error(page))).toBe(true);
		expect(isBackendUnreachable('<html><body>nope</body></html>')).toBe(true);
	});

	it('leaves a real backend refusal alone', () => {
		// The failure the user *can* act on has to keep its own wording - these
		// are the messages that say which macro, and why.
		expect(isBackendUnreachable('a macro called "Login" already exists')).toBe(false);
		expect(isBackendUnreachable(new Error('could not write macro: disk full'))).toBe(false);
		// Not an HTML document just because it mentions one.
		expect(isBackendUnreachable('macro contains <html> in a keypress node')).toBe(false);
	});
});

describe('describeBackendError', () => {
	it('names an unreachable backend rather than repeating the runtime', () => {
		expect(describeBackendError('<!doctype html><html></html>')).toMatch(/Not connected to the KeyPress backend/);
	});

	it('passes a real backend error through untouched', () => {
		expect(describeBackendError('a macro called "Login" already exists')).toBe(
			'a macro called "Login" already exists'
		);
	});
});
