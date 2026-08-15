import { describe, expect, it } from 'vitest';
import {
	buildActiveGlowCss,
	buildSkippedGlowCss,
	DEFAULT_NODE_GLOW,
	NODE_TYPE_GLOWS,
	SELECTOR_SAFE_NODE_ID
} from './nodeGlow';
import { NODE_TYPE_TITLES } from './nodeLabels';

/** The rules of a generated stylesheet, split back apart for inspection. */
const rulesOf = (css: string): { selectors: string[]; body: string }[] =>
	css
		.split('}')
		.filter((rule) => rule.trim() !== '')
		.map((rule) => {
			const [selectorList, body] = rule.split('{');
			return { selectors: selectorList.split(','), body };
		});

describe('SELECTOR_SAFE_NODE_ID', () => {
	it('admits the ids the app itself mints', () => {
		// The drop handler in Flow.svelte uses `crypto.randomUUID()`, which is hex
		// and hyphens - the whole reason that switch was safe to make.
		expect(SELECTOR_SAFE_NODE_ID.test(crypto.randomUUID())).toBe(true);
		// And the decimals every macro saved before that switch still carries.
		expect(SELECTOR_SAFE_NODE_ID.test('0.8324719283')).toBe(true);
		// And the hand-written ids in the default graph.
		expect(SELECTOR_SAFE_NODE_ID.test('startnode-1')).toBe(true);
	});

	it('rejects anything that could close the attribute selector or the rule', () => {
		for (const id of ['a"]', "a']", 'a}b', 'a{b', 'a b', 'a\\b', 'a<b', 'a/*b', '']) {
			expect(SELECTOR_SAFE_NODE_ID.test(id), id).toBe(false);
		}
	});
});

describe('buildSkippedGlowCss', () => {
	it('is empty when nothing was skipped', () => {
		expect(buildSkippedGlowCss([])).toBe('');
	});

	it('marks a node by the data-id Svelte Flow already renders, scoped to the canvas', () => {
		expect(buildSkippedGlowCss(['abc'])).toBe(
			'.flow-container .svelte-flow__node[data-id="abc"]' +
				'{--node-skipped-glow:0 0 0 2px var(--skipped-glow),0 0 22px 6px var(--skipped-glow-soft)}'
		);
	});

	it('emits one rule for several ids', () => {
		const rules = rulesOf(buildSkippedGlowCss(['a', 'b', 'c']));
		expect(rules).toHaveLength(1);
		expect(rules[0].selectors).toHaveLength(3);
	});

	it('drops an id that could break out of the selector', () => {
		// The security property. Without the filter this id would close the
		// attribute selector and the rule, and everything after it would be CSS
		// of the id's own choosing applied to the whole app.
		const attack = 'x"]{color:red}.svelte-flow__node[data-id="y';

		const css = buildSkippedGlowCss([attack]);

		expect(css).toBe('');
	});

	it('keeps the safe ids when one of a list is hostile', () => {
		const css = buildSkippedGlowCss(['good-1', 'evil"] , * ', 'good-2']);

		const rules = rulesOf(css);
		expect(rules).toHaveLength(1);
		expect(rules[0].selectors).toEqual([
			'.flow-container .svelte-flow__node[data-id="good-1"]',
			'.flow-container .svelte-flow__node[data-id="good-2"]'
		]);
		// No trace of the hostile id survives anywhere in the output - not even
		// as a selector that matches nothing.
		expect(css).not.toContain('evil');
		expect(css).not.toContain('*');
	});

	it('is empty when every id is unsafe, rather than emitting a dangling rule', () => {
		expect(buildSkippedGlowCss(['a b', 'c}d'])).toBe('');
	});
});

describe('buildActiveGlowCss', () => {
	it('is empty when nothing is running', () => {
		expect(buildActiveGlowCss([], new Map())).toBe('');
	});

	it('lights a node in the colour frozen for this run', () => {
		const glows = new Map([['abc', '--node-glow-green']]);

		expect(buildActiveGlowCss(['abc'], glows)).toBe(
			'.flow-container .svelte-flow__node[data-id="abc"]' +
				'{--node-active-glow:0 0 26px 8px var(--node-glow-green)}'
		);
	});

	it('groups selectors by colour rather than emitting one rule per id', () => {
		// Three lit nodes, two colours - so two rules, not three. A run lights a
		// handful of nodes of a couple of colours, and a selector list per colour
		// is the smaller stylesheet.
		const glows = new Map([
			['a', '--node-glow-blue'],
			['b', '--node-glow-green'],
			['c', '--node-glow-blue']
		]);

		const rules = rulesOf(buildActiveGlowCss(['a', 'b', 'c'], glows));

		expect(rules).toHaveLength(2);
		expect(rules[0].selectors).toEqual([
			'.flow-container .svelte-flow__node[data-id="a"]',
			'.flow-container .svelte-flow__node[data-id="c"]'
		]);
		expect(rules[0].body).toBe('--node-active-glow:0 0 26px 8px var(--node-glow-blue)');
		expect(rules[1].selectors).toEqual(['.flow-container .svelte-flow__node[data-id="b"]']);
		expect(rules[1].body).toBe('--node-active-glow:0 0 26px 8px var(--node-glow-green)');
	});

	it('falls back to the neutral glow for an id the colour map does not know', () => {
		// A node reported by a run this canvas did not start, or one added after
		// the snapshot was frozen. It still lights up, just not in a hue of its
		// own.
		const css = buildActiveGlowCss(['stranger'], new Map());

		expect(css).toBe(
			'.flow-container .svelte-flow__node[data-id="stranger"]' +
				`{--node-active-glow:0 0 26px 8px var(${DEFAULT_NODE_GLOW})}`
		);
	});

	it('groups a fallback with the other neutral nodes', () => {
		const glows = new Map([['known', DEFAULT_NODE_GLOW]]);

		const rules = rulesOf(buildActiveGlowCss(['known', 'stranger'], glows));

		expect(rules).toHaveLength(1);
		expect(rules[0].selectors).toHaveLength(2);
	});

	it('drops an id that could break out of the selector', () => {
		const attack = 'x"]{color:red}.svelte-flow__node[data-id="y';
		const glows = new Map([[attack, '--node-glow-blue']]);

		expect(buildActiveGlowCss([attack], glows)).toBe('');
	});

	it('keeps the safe ids when one of a list is hostile', () => {
		const css = buildActiveGlowCss(
			['good', 'evil"] , * '],
			new Map([
				['good', '--node-glow-blue'],
				['evil"] , * ', '--node-glow-green']
			])
		);

		// Dropped before its colour is even looked up, so it cannot open a rule
		// of its own either.
		expect(rulesOf(css)).toHaveLength(1);
		expect(css).not.toContain('evil');
		expect(css).not.toContain('*');
	});

	it('emits a node once even if the backend reports it twice', () => {
		// `markNodeActive` guards against this, but the builder should not depend
		// on that: a duplicated selector is a wasted rule, not a bug worth
		// chasing from the stylesheet back to the event stream.
		const rules = rulesOf(buildActiveGlowCss(['a', 'a'], new Map()));

		expect(rules).toHaveLength(1);
		expect(rules[0].selectors).toEqual([
			'.flow-container .svelte-flow__node[data-id="a"]',
			'.flow-container .svelte-flow__node[data-id="a"]'
		]);
	});
});

describe('the two node-type tables', () => {
	it('cover the same node types', () => {
		// Both are copied from the node components - their titles and their header
		// gradients - so a node type added to one and forgotten in the other means
		// a node that is named but glows neutral, or glows but is called by its
		// registry key.
		expect(Object.keys(NODE_TYPE_GLOWS).sort()).toEqual(Object.keys(NODE_TYPE_TITLES).sort());
	});

	it('names only `--node-glow-*` custom properties', () => {
		for (const glow of [...Object.values(NODE_TYPE_GLOWS), DEFAULT_NODE_GLOW]) {
			expect(glow).toMatch(/^--node-glow-[a-z]+$/);
		}
	});
});
