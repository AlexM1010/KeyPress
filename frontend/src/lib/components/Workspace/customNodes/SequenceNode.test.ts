// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest';
import { fireEvent, screen } from '@testing-library/svelte';
import { persistedData, renderNode } from '$lib/test/nodeHarness';
import { asGraphState } from '$lib/test/graphState.svelte';
import { graph } from '$lib/stores/flow.svelte';
import SequenceNode from './SequenceNode.svelte';

/** The id `renderNode` gives the node it mounts. */
const NODE_ID = 'test-node-1';

/**
 * Mounts the node with its payload in the deep `$state` the graph really holds
 * it in.
 *
 * The wrapping is not decoration. This component is runes, so it reads its
 * payload reactively and a plain object would render the first count and then
 * ignore every edit - which is a property of the harness, not of the node: in
 * the app `graph.nodes` is deep `$state` precisely so that an in-place payload
 * write is seen. See `asGraphState`.
 *
 * `Partial` on the way in because a node dropped from the palette really does
 * arrive with an empty payload, and the backfill is one of the things under
 * test here.
 */
const renderSequence = (data: Partial<{ outputs: number }>) =>
	renderNode(SequenceNode, asGraphState(data as { outputs: number }));

/**
 * The handle ids the node has actually rendered, in the order they appear.
 *
 * Read off the DOM rather than off any exported constant, because the DOM is
 * what Svelte Flow connects edges to: `<Handle>` renders `data-handleid`, and
 * an edge's `sourceHandle` is that string. Anything short of this would be
 * asserting the component's intentions rather than its output.
 */
function handleIds(kind?: 'source' | 'target'): string[] {
	const selector = kind ? `.svelte-flow__handle.${kind}` : '.svelte-flow__handle';
	return [...document.querySelectorAll(selector)].map(
		(handle) => handle.getAttribute('data-handleid') ?? ''
	);
}

const addOutput = () => fireEvent.click(screen.getByLabelText('Add output'));
const removeOutput = () => fireEvent.click(screen.getByLabelText('Remove output'));

describe('SequenceNode', () => {
	beforeEach(() => {
		// The graph is module state shared by every test in this file, and removing
		// an output writes to `graph.edges`.
		graph.edges = [];
	});

	it('backfills the default output count into a payload that has none', () => {
		// A node dropped from the palette arrives with `data: {}` - the palette
		// deliberately does not duplicate each node's defaults.
		const { data } = renderSequence({});

		expect(data.outputs).toBe(2);
		expect(persistedData(data, 'SequenceNode')).toEqual({ outputs: 2 });
	});

	it('leaves a saved output count alone when backfilling', () => {
		const { data } = renderSequence({ outputs: 4 });

		expect(persistedData(data, 'SequenceNode')).toEqual({ outputs: 4 });
	});

	it('declares one target handle and a numbered source handle per output', () => {
		renderSequence({ outputs: 3 });

		// One way in, however many ways out. The target is what makes a join
		// expressible; the numbered sources are what make the fan-out ordered.
		expect(handleIds('target')).toEqual(['left']);
		expect(handleIds('source')).toEqual(['out-1', 'out-2', 'out-3']);
	});

	it('lays the outputs out in walk order, top to bottom', () => {
		renderSequence({ outputs: 3 });

		// The interpreter takes a node's outgoing edges in ascending handle order,
		// so the handle the user sees highest has to be the one that runs first.
		// Rendered order is DOM order, which is the order they sit down the side.
		expect(handleIds()).toEqual(['left', 'out-1', 'out-2', 'out-3']);
	});

	it('adds an output, and the handle that goes with it', async () => {
		const { data } = renderSequence({ outputs: 2 });

		await addOutput();

		expect(data.outputs).toBe(3);
		expect(handleIds('source')).toEqual(['out-1', 'out-2', 'out-3']);
		// The count is what is saved; the handles are derived from it, so a macro
		// reopened has the same three outputs.
		expect(persistedData(data, 'SequenceNode')).toEqual({ outputs: 3 });
	});

	it('removes the last output, and the edge attached to it', async () => {
		graph.edges = [
			{ id: 'kept-first', source: NODE_ID, sourceHandle: 'out-1', target: 'x' },
			{ id: 'removed', source: NODE_ID, sourceHandle: 'out-3', target: 'y' },
			// Same handle id, different node. Nothing about removing an output here
			// may touch another node's wiring.
			{ id: 'kept-elsewhere', source: 'other-node', sourceHandle: 'out-3', target: 'z' },
			// Pointing *into* this node, which is untouched by all of this.
			{ id: 'kept-incoming', source: 'upstream', sourceHandle: 'right', target: NODE_ID }
		];

		const { data } = renderSequence({ outputs: 3 });

		await removeOutput();

		expect(data.outputs).toBe(2);
		expect(handleIds('source')).toEqual(['out-1', 'out-2']);
		// An edge whose `sourceHandle` names a handle that no longer exists is
		// drawn from nowhere and followed by nothing - dead wiring the user can
		// neither see nor delete - so it goes with the handle.
		expect(graph.edges.map((edge) => edge.id)).toEqual([
			'kept-first',
			'kept-elsewhere',
			'kept-incoming'
		]);
	});

	it('refuses to go below two outputs', async () => {
		const { data } = renderSequence({ outputs: 2 });

		// A Sequence with one output is an ordinary node with a longer name.
		expect(screen.getByLabelText('Remove output')).toHaveProperty('disabled', true);

		await removeOutput();

		expect(data.outputs).toBe(2);
		expect(handleIds('source')).toEqual(['out-1', 'out-2']);
	});

	it('refuses a tenth output, because out-10 would sort before out-2', async () => {
		const { data } = renderSequence({ outputs: 9 });

		// The ordering trap, and the reason for the cap. The interpreter sorts
		// `sourceHandle` as a plain string, so a tenth output would run second and
		// silently reorder the macro. Nine is the last number where the string
		// order and the number order agree.
		expect(screen.getByLabelText('Add output')).toHaveProperty('disabled', true);

		await addOutput();

		expect(data.outputs).toBe(9);
		expect(handleIds('source')).toHaveLength(9);
	});

	it('renders nine outputs in an order a plain string sort agrees with', () => {
		renderSequence({ outputs: 9 });

		const rendered = handleIds('source');

		expect(rendered).toEqual([
			'out-1',
			'out-2',
			'out-3',
			'out-4',
			'out-5',
			'out-6',
			'out-7',
			'out-8',
			'out-9'
		]);
		// The assertion that actually pins the cap: sorted as strings - which is
		// what `sort.Strings` does in Go and what `nodeLabels.ts` does here - the
		// ids come out in exactly the order they were drawn in. Add `out-10` to
		// this list and it fails, which is the point.
		expect([...rendered].sort()).toEqual(rendered);
	});

	it('clamps an output count saved outside the range', () => {
		// Nothing in the app writes these, but a hand-edited macro can, and a
		// negative count would render no handles at all.
		renderSequence({ outputs: 40 });
		expect(handleIds('source')).toHaveLength(9);
	});
});
