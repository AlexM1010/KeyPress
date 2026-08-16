// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { fireEvent, screen } from '@testing-library/svelte';
import { persistedData, renderNode } from '$lib/test/nodeHarness';
import { asGraphState } from '$lib/test/graphState.svelte';
import LoopStartNode from './LoopStartNode.svelte';

/**
 * Mirrors the payload the component declares inline, the way the neighbouring
 * node tests do. The optionality is the subject of half this file: `count` is
 * `null` for "no count" and `operator` is *absent* for "no condition", and both
 * of those have to survive a round trip through the saved file.
 */
type LoopStartNodeData = {
	count?: number | null;
	variable?: string;
	operator?: string;
	value?: string;
	loopId?: string;
};

/**
 * Mounts the node with its payload in the deep `$state` the graph really holds
 * it in.
 *
 * Not decoration: this component is runes, so it reads its payload reactively
 * and a plain object would render the first summary line and then ignore every
 * edit - and the summary is exactly what tells "runs forever" apart from "runs
 * zero times". See `asGraphState`.
 */
const renderLoop = (data: LoopStartNodeData) => renderNode(LoopStartNode, asGraphState(data));

/**
 * The handle ids the node has actually rendered, in the order they appear.
 *
 * Read off the DOM rather than off any constant, because the DOM is what Svelte
 * Flow attaches edges to: `<Handle>` renders `data-handleid`, and an edge's
 * `sourceHandle` is that string - which is then what `executeLoopStartTask`
 * returns to choose between going round again and leaving.
 */
function handleIds(kind?: 'source' | 'target'): string[] {
	const selector = kind ? `.svelte-flow__handle.${kind}` : '.svelte-flow__handle';
	return [...document.querySelectorAll(selector)].map(
		(handle) => handle.getAttribute('data-handleid') ?? ''
	);
}

const countInput = () => screen.getByLabelText('Repeat count') as HTMLInputElement;
const typeCount = (text: string) => fireEvent.input(countInput(), { target: { value: text } });

const operatorSelect = () => screen.getByLabelText('Stop when') as HTMLSelectElement;
const chooseOperator = (operator: string) =>
	fireEvent.change(operatorSelect(), { target: { value: operator } });

const summary = () => screen.getByTestId('loop-summary').textContent ?? '';

describe('LoopStartNode', () => {
	it('backfills a payload that has none, with no count and no condition', () => {
		// A node dropped from the palette arrives with only the pairing id, because
		// `createLoopPair` deliberately does not duplicate this component's
		// defaults.
		const { data } = renderLoop({ loopId: 'loop-1' });

		expect(persistedData(data, 'LoopStartNode')).toEqual({
			count: null,
			variable: '',
			value: '',
			loopId: 'loop-1'
		});
		// The one assertion `toEqual` cannot make: an absent key and a key holding
		// `undefined` compare equal to it, and only one of the two is what is meant.
		expect('operator' in data).toBe(false);
	});

	it('leaves a saved loop alone when backfilling', () => {
		const { data } = renderLoop({
			count: 5,
			variable: 'spot.x',
			operator: 'greaterThan',
			value: '800',
			loopId: 'loop-1'
		});

		expect(persistedData(data, 'LoopStartNode')).toEqual({
			count: 5,
			variable: 'spot.x',
			operator: 'greaterThan',
			value: '800',
			loopId: 'loop-1'
		});
	});

	it('names its outputs exactly `body` and `done`, and takes one input', () => {
		renderLoop({});

		// These strings are the save format and the handler's return value at once:
		// `executeLoopStartTask` answers "body" or "done" and the walk takes the
		// single edge drawn from that handle. Anything else here is an edge the
		// interpreter would look for and never find.
		expect(handleIds('target')).toEqual(['left']);
		expect(handleIds('source')).toEqual(['body', 'done']);
	});

	it('draws the body output above the done one', () => {
		renderLoop({});

		// Rendered order is DOM order, which is the order they sit down the side of
		// the card - and the legend inside the card repeats it. Two wires off one
		// node that cannot be told apart on the canvas is a coin toss.
		expect(handleIds()).toEqual(['left', 'body', 'done']);
	});

	it('writes a typed count to the payload as a number', async () => {
		const { data } = renderLoop({});

		await typeCount('3');

		expect(data.count).toBe(3);
		// A number, not the string the input holds: `parseLoopCount` takes either,
		// but a count that is sometimes "3" and sometimes 3 is a payload whose shape
		// depends on which control last touched it.
		expect(typeof persistedData(data, 'LoopStartNode').count).toBe('number');
	});

	it('keeps a count of zero, which is a count and not the absence of one', async () => {
		const { data } = renderLoop({ count: 4 });

		await typeCount('0');

		// The distinction the backend keeps `count` and `hasCount` apart for. Zero
		// runs the body no times; folding it into "no count" would turn a loop the
		// user switched off into one that never stops.
		expect(data.count).toBe(0);
		expect(persistedData(data, 'LoopStartNode').count).toBe(0);
		expect(summary()).toContain('the body never runs');
	});

	it('writes null for an emptied count, which is how the count is turned off', async () => {
		const { data } = renderLoop({ count: 4 });

		await typeCount('');

		// `null` is one of the four spellings of "no count" `parseLoopCount` takes,
		// and the one an emptied number input produces. It has to be distinguishable
		// from the zero above - in the payload and on the card.
		expect(data.count).toBeNull();
		expect(persistedData(data, 'LoopStartNode').count).toBeNull();
		expect(summary()).toContain('Loops forever');
	});

	it('never writes a count the backend would refuse', async () => {
		const { data } = renderLoop({});

		await typeCount('-5');
		// `checkedLoopCount` rejects a negative outright rather than rounding it up,
		// so a macro carrying one cannot be run at all. The editor must not be able
		// to draw it.
		expect(data.count).toBe(0);

		await typeCount('2.7');
		// Iterations are whole. Truncated rather than rounded, so a fraction can
		// only ever cost the user the part of a turn that does not exist.
		expect(data.count).toBe(2);

		await typeCount('nonsense');
		expect(data.count).toBeNull();
	});

	it('offers exactly the six operators the backend knows, plus "no condition"', () => {
		renderLoop({});

		// The option *values* are asserted, not the labels: the value is what lands
		// in the payload and is compared by `conditionHolds`, which refuses an
		// operator outside this set. The leading empty entry is the dropdown's way
		// of saying "no condition" and is the one value that never reaches the
		// payload - see the next test.
		const values = [...operatorSelect().options].map((option) => option.value);

		expect(values).toEqual([
			'',
			'equals',
			'notEquals',
			'greaterThan',
			'lessThan',
			'contains',
			'isSet'
		]);
	});

	it('writes a chosen operator to the payload as the wire spelling', async () => {
		const { data } = renderLoop({ variable: 'colour', value: '00ff00' });

		await chooseOperator('notEquals');

		expect(data.operator).toBe('notEquals');
		expect(persistedData(data, 'LoopStartNode').operator).toBe('notEquals');
	});

	it('removes the operator key rather than writing an empty one', async () => {
		const { data } = renderLoop({ variable: 'colour', operator: 'equals', value: '00ff00' });

		await chooseOperator('');

		// Go reads an empty operator and an absent one the same way, so this is the
		// stricter of two accepted spellings - and the only one that matches the
		// payload's own type, which has no empty member. It also keeps the in-memory
		// graph and the saved file agreeing: `JSON.stringify` drops an undefined
		// value, so writing one would have the two disagree about this node's shape.
		expect('operator' in data).toBe(false);
		expect(Object.keys(persistedData(data, 'LoopStartNode'))).not.toContain('operator');
		// What the condition was about is kept, so switching it back on does not
		// cost the user what they typed.
		expect(data.variable).toBe('colour');
		expect(data.value).toBe('00ff00');
	});

	it('shows the condition fields only once there is a condition', async () => {
		renderLoop({});

		// Fields whose contents change no outcome are a lie about what the node
		// does, and with no operator chosen that is exactly what they would be.
		expect(screen.queryByLabelText('Variable')).toBeNull();
		expect(screen.queryByLabelText('Value')).toBeNull();

		await chooseOperator('equals');

		expect(screen.queryByLabelText('Variable')).not.toBeNull();
		expect(screen.queryByLabelText('Value')).not.toBeNull();
	});

	it('hides the value field for isSet, which ignores it', () => {
		renderLoop({ variable: 'matched', operator: 'isSet' });

		// `conditionHolds` does not read `value` on the isSet path - it asks only
		// whether anything ever wrote to the name.
		expect(screen.queryByLabelText('Variable')).not.toBeNull();
		expect(screen.queryByLabelText('Value')).toBeNull();
	});

	it('says a loop with neither a count nor a condition runs forever', () => {
		renderLoop({ loopId: 'loop-1' });

		// The state this whole summary line exists for. A loop with nothing set is
		// legal, supported and common - "click until I say stop" is the macro the
		// app exists for - and it looks exactly like a node nobody finished filling
		// in. Saying so is what tells the two apart.
		expect(summary()).toContain('Loops forever');
	});

	it('stops saying "forever" as soon as either reason to stop is set', async () => {
		renderLoop({});

		await typeCount('2');
		expect(summary()).not.toContain('Loops forever');
		expect(summary()).toContain('Runs its body 2 times');

		await typeCount('');
		expect(summary()).toContain('Loops forever');

		await chooseOperator('isSet');
		expect(summary()).not.toContain('Loops forever');
	});

	it('reports both reasons to stop when a loop carries both', () => {
		renderLoop({ count: 3, variable: 'spot.x', operator: 'greaterThan', value: '800' });

		// Whichever comes first is what the node really does: `executeLoopStartTask`
		// checks the condition and then the count on every arrival, and either one
		// ends the loop.
		expect(summary()).toContain('Runs its body 3 times');
		expect(summary()).toContain('spot.x is greater than 800');
		expect(summary()).toContain('whichever comes first');
	});
});
