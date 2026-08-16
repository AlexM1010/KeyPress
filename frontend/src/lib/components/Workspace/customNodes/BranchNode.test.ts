// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { fireEvent, screen } from '@testing-library/svelte';
import { persistedData, renderNode } from '$lib/test/nodeHarness';
import { asGraphState } from '$lib/test/graphState.svelte';
import BranchNode from './BranchNode.svelte';

/**
 * Mirrors the payload the component declares inline, the way the neighbouring
 * node tests do: `BranchNode.svelte` keeps the type next to the only component
 * that owns it and exports nothing, so restating it here is what lets the
 * partial payloads below be cast without `any`.
 */
type BranchNodeData = {
	variable: string;
	operator: string;
	value: string;
};

/**
 * Mounts the node with its payload in the deep `$state` the graph really holds
 * it in.
 *
 * Not decoration: this component is runes, so it reads its payload reactively
 * and a plain object would render the first operator and then ignore every
 * change - hiding the value field on `isSet` is exactly the behaviour that would
 * silently stop working. See `asGraphState`.
 */
const renderBranch = (data: Partial<BranchNodeData>) =>
	renderNode(BranchNode, asGraphState(data as BranchNodeData));

/**
 * The handle ids the node has actually rendered, in the order they appear.
 *
 * Read off the DOM rather than off any constant in the component, because the
 * DOM is what Svelte Flow attaches edges to: `<Handle>` renders `data-handleid`,
 * and an edge's `sourceHandle` is that string - which is then what the Go
 * handler returns to choose a branch.
 */
function handleIds(kind?: 'source' | 'target'): string[] {
	const selector = kind ? `.svelte-flow__handle.${kind}` : '.svelte-flow__handle';
	return [...document.querySelectorAll(selector)].map(
		(handle) => handle.getAttribute('data-handleid') ?? ''
	);
}

const operatorSelect = () => screen.getByLabelText('Comparison') as HTMLSelectElement;

const chooseOperator = (operator: string) =>
	fireEvent.change(operatorSelect(), { target: { value: operator } });

describe('BranchNode', () => {
	it('backfills a whole condition into a payload that has none', () => {
		// A node dropped from the palette arrives with `data: {}` - the palette
		// deliberately does not duplicate each node's defaults.
		const { data } = renderBranch({});

		expect(persistedData(data, 'BranchNode')).toEqual({
			variable: '',
			operator: 'equals',
			value: ''
		});
	});

	it('leaves a saved condition alone when backfilling', () => {
		const { data } = renderBranch({ variable: 'spot.x', operator: 'greaterThan', value: '800' });

		expect(persistedData(data, 'BranchNode')).toEqual({
			variable: 'spot.x',
			operator: 'greaterThan',
			value: '800'
		});
	});

	it('names its two outputs exactly `true` and `false`', () => {
		renderBranch({});

		// These two strings are the save format and the handler's return value at
		// once: `executeBranchTask` answers "true" or "false" and the walk takes the
		// single edge drawn from that handle. Anything else here - `out-1`, `yes` -
		// is an edge the interpreter would look for and never find.
		expect(handleIds('target')).toEqual(['left']);
		expect(handleIds('source')).toEqual(['true', 'false']);
	});

	it('draws the true output above the false one', () => {
		renderBranch({});

		// Rendered order is DOM order, which is the order they sit down the side of
		// the card - and the legend inside the card repeats it. A branch whose two
		// wires cannot be told apart on the canvas is the main way this node goes
		// wrong.
		expect(handleIds()).toEqual(['left', 'true', 'false']);
	});

	it('offers exactly the six operators the backend knows', () => {
		renderBranch({});

		// The option *values* are asserted, not the labels: the value is what lands
		// in the payload and is compared in `conditionHolds`, which refuses an
		// operator outside this set outright rather than guessing a branch for it.
		// So a seventh entry here, or a mis-spelled one, is a node that fails the
		// macro at run time.
		const values = [...operatorSelect().options].map((option) => option.value);

		expect(values).toEqual(['equals', 'notEquals', 'greaterThan', 'lessThan', 'contains', 'isSet']);
	});

	it('writes the chosen operator to the payload as the wire spelling', async () => {
		const { data } = renderBranch({ variable: 'colour', operator: 'equals', value: '00ff00' });

		await chooseOperator('notEquals');

		expect(data.operator).toBe('notEquals');
		expect(persistedData(data, 'BranchNode').operator).toBe('notEquals');
	});

	it('shows the value field for every operator that compares against one', async () => {
		renderBranch({ variable: 'colour', operator: 'equals', value: '00ff00' });

		for (const operator of ['equals', 'notEquals', 'greaterThan', 'lessThan', 'contains']) {
			await chooseOperator(operator);
			expect(screen.queryByLabelText('Value'), operator).not.toBeNull();
		}
	});

	it('hides the value field for isSet, which ignores it', async () => {
		const { data } = renderBranch({ variable: 'colour', operator: 'equals', value: '00ff00' });

		expect(screen.queryByLabelText('Value')).not.toBeNull();

		await chooseOperator('isSet');

		// `conditionHolds` does not read `value` on the isSet path at all - it asks
		// only whether anything ever wrote to the name - so a box left on screen
		// there would be an input whose contents change no outcome.
		expect(screen.queryByLabelText('Value')).toBeNull();
		// What was typed is kept rather than cleared, so switching operator away and
		// back does not cost the user their value.
		expect(data.value).toBe('00ff00');

		await chooseOperator('equals');

		expect((screen.getByLabelText('Value') as HTMLInputElement).value).toBe('00ff00');
	});

	it('reads a saved isSet condition without a value field', () => {
		// The same state arrived at from disk rather than from the dropdown: a macro
		// saved with isSet must not mount a box the operator ignores.
		renderBranch({ variable: 'matched', operator: 'isSet', value: '' });

		expect(screen.queryByLabelText('Value')).toBeNull();
	});

	it('writes the variable name to the payload as typed', async () => {
		const { data } = renderBranch({});

		await fireEvent.input(screen.getByLabelText('Variable'), { target: { value: 'spot.x' } });

		// The dotted name is the point: a Mouse Move storing `spot` writes `spot.x`
		// and `spot.y`, so this is what a condition on a position actually reads.
		expect(data.variable).toBe('spot.x');
		expect(persistedData(data, 'BranchNode').variable).toBe('spot.x');
	});

	it('keeps a numeric comparison value a string in the payload', async () => {
		const { data } = renderBranch({ variable: 'spot.x', operator: 'greaterThan', value: '' });

		await fireEvent.input(screen.getByLabelText('Value'), { target: { value: '800' } });

		// Deliberately not coerced to a number. `asNumber` in the backend parses a
		// numeric string, so "800" and 800 compare identically against a stored
		// number - and one text box that always yields a string is a simpler
		// contract than a field whose type depends on what was typed into it.
		expect(data.value).toBe('800');
		expect(typeof persistedData(data, 'BranchNode').value).toBe('string');
	});
});
