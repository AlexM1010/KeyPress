import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

// `MAX_MACRO_NAME_LEN` in `Flow.svelte` is a copy of `maxMacroNameLen` in
// `backend/persistence.go`: the field stops at the limit instead of letting the
// user type a name that is only rejected after a round trip. The backend is the
// one that enforces it, so drift is not a crash - it is a name the field
// happily accepts and the save then refuses, or a field that stops short of
// what the backend would have stored. Neither shows up in any other test.
//
// This reads both sources for the literal rather than sharing the value for
// real, which would mean a Wails binding (an App method returning the limit,
// regenerated into `frontend/bindings/`), a call at mount, and a fallback for
// before it resolves - a lot of machinery, and an async one, for the
// `maxlength` of one input. A grep is honest about what it is: a tripwire on
// two constants that have no reason to change often, and that names its own
// fix when it fires.

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, '../../../..');

const read = (path: string) => readFileSync(resolve(repoRoot, path), 'utf8');

/** The single capture group is the limit. */
const limitFrom = (source: string, where: string, pattern: RegExp): number => {
	const match = source.match(pattern);
	if (match === null) {
		throw new Error(
			`could not find the macro name limit in ${where} using ${pattern}. ` +
				`It was renamed or rewritten - update this test to match, and check the other side still agrees.`
		);
	}
	return Number(match[1]);
};

describe('the macro name limit is the same on both sides', () => {
	it('MAX_MACRO_NAME_LEN in Flow.svelte matches maxMacroNameLen in persistence.go', () => {
		const frontend = limitFrom(
			read('frontend/src/routes/workspace/Flow.svelte'),
			'Flow.svelte',
			/\bMAX_MACRO_NAME_LEN\s*=\s*(\d+)/
		);
		const backend = limitFrom(
			read('backend/persistence.go'),
			'persistence.go',
			/\bmaxMacroNameLen\s*=\s*(\d+)/
		);

		expect(frontend).toBe(backend);
	});
});
