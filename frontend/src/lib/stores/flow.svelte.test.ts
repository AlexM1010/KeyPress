import { beforeEach, describe, expect, it } from 'vitest';
import { duplicateNodes, graph, selectedNodes, type FlowNode } from './flow.svelte';

/**
 * Two wired-up nodes, rebuilt per test.
 *
 * A function rather than a shared constant because `data` is a tree and the
 * whole point of these tests is what happens to nested values: a spread would
 * share `modifiers` between tests and the deep-copy assertion would pass for the
 * wrong reason.
 */
const seed = (): FlowNode[] => [
	{
		id: 'a',
		type: 'KeyPressNode',
		position: { x: 100, y: 200 },
		data: { character: 'a', modifiers: ['Ctrl'] }
	},
	{
		id: 'b',
		type: 'DelayNode',
		position: { x: 400, y: 200 },
		data: { delayType: 'Fixed', time: 1000 }
	}
];

describe('duplicateNodes', () => {
	beforeEach(() => {
		graph.nodes = seed();
		graph.edges = [];
	});

	it('appends one copy per id, offset from the node it came from', () => {
		const copies = duplicateNodes(['a']);

		expect(graph.nodes).toHaveLength(3);
		expect(copies).toHaveLength(1);
		// Offset, or the copy lands exactly under the original and nothing appears
		// to have happened.
		expect(copies[0].position).toEqual({ x: 140, y: 240 });
		// Duplicating is not a move: the original stays put.
		expect(graph.nodes[0].position).toEqual({ x: 100, y: 200 });
		expect(copies[0].type).toBe('KeyPressNode');
	});

	it('mints a fresh id for every copy', () => {
		const copies = duplicateNodes(['a', 'b']);
		const ids = graph.nodes.map((node) => node.id);

		// A reused id would have Svelte Flow key two nodes alike, and the save
		// file would carry two nodes claiming to be the same one.
		expect(new Set(ids).size).toBe(ids.length);
		for (const copy of copies) {
			expect(copy.id).not.toBe('a');
			expect(copy.id).not.toBe('b');
			expect(copy.id).not.toBe('');
		}
	});

	it('gives each copy a payload of its own, nested values included', () => {
		const [copy] = duplicateNodes(['a']);
		const original = graph.nodes[0];

		expect(copy.data).toEqual(original.data);

		// The reason the payload is snapshotted rather than spread. Node
		// components edit their payload in place, so a shallow copy would leave
		// the duplicate editing the original's nested arrays - ticking Alt on the
		// copy would tick it on the node it was copied from.
		(copy.data.modifiers as string[]).push('Alt');
		expect(original.data.modifiers).toEqual(['Ctrl']);
	});

	it('moves the selection onto the copies', () => {
		graph.nodes = graph.nodes.map((node) => ({ ...node, selected: true }));

		duplicateNodes(['a', 'b']);

		// Duplicating a selection and dragging the result aside is the point of
		// doing it to a selection at all, and that only works if the selection
		// follows the copies. It also stops a second Ctrl+D producing three copies
		// of the first node instead of a second pair.
		const selectedIds = selectedNodes().map((node) => node.id);
		expect(selectedIds).toHaveLength(2);
		expect(selectedIds).not.toContain('a');
		expect(selectedIds).not.toContain('b');
	});

	it('ignores ids that are not in the graph, and does nothing for none', () => {
		expect(duplicateNodes(['nope'])).toEqual([]);
		expect(graph.nodes).toHaveLength(2);

		expect(duplicateNodes([])).toEqual([]);
		expect(graph.nodes).toHaveLength(2);
	});

	it('leaves the edges alone', () => {
		graph.edges = [{ id: 'e1', source: 'a', target: 'b' }];

		duplicateNodes(['a', 'b']);

		// Deliberate: a duplicate lands offset on top of its original and the user
		// is about to move it. Re-creating the wiring would be guessing, in a
		// graph where an edge carries handle ids and decides execution order.
		expect(graph.edges).toHaveLength(1);
	});
});

describe('selectedNodes', () => {
	beforeEach(() => {
		graph.nodes = seed();
	});

	it('reports what Svelte Flow has selected, in graph order', () => {
		graph.nodes[1].selected = true;

		expect(selectedNodes().map((node) => node.id)).toEqual(['b']);
	});

	it('is empty when nothing is selected', () => {
		expect(selectedNodes()).toEqual([]);
	});
});
