import { beforeEach, describe, expect, it } from 'vitest';
import {
	createLoopPair,
	detachOutputEdges,
	duplicateNodes,
	graph,
	isOutputHandleFree,
	loopPartnerId,
	selectedNodes,
	withLoopPartners,
	LOOP_BACK_HANDLE,
	LOOP_END_NODE_TYPE,
	LOOP_START_NODE_TYPE,
	type FlowNode
} from './flow.svelte';

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

// The one-edge-per-output rule the canvas enforces through `isValidConnection`
// in `Flow.svelte`. Tested here rather than there because `Flow.svelte` is the
// whole workspace - the Wails bindings, the event listeners, the canvas - and
// none of that has anything to do with the rule. The component is a one-line
// wrapper over this function, which is the whole reason the rule lives here.
describe('isOutputHandleFree', () => {
	beforeEach(() => {
		graph.nodes = seed();
		graph.edges = [
			{ id: 'e1', source: 'a', sourceHandle: 'right', target: 'b', targetHandle: 'left' }
		];
	});

	it('refuses a second edge out of a handle that already has one', () => {
		// The token leaves a node by exactly one edge per output, so a second wire
		// off the same pin says nothing the interpreter can act on.
		expect(isOutputHandleFree('a', 'right')).toBe(false);
	});

	it('allows another edge out of a different handle on the same node', () => {
		// This is what a Sequence node is: several outputs, one edge each.
		expect(isOutputHandleFree('a', 'out-2')).toBe(true);
	});

	it('allows any number of edges INTO one node', () => {
		// The asymmetry is the model. Arriving twice is arriving twice, and the
		// token simply runs the node again - so a join stays drawable while an
		// ambiguous fan-out does not.
		graph.edges = [
			{ id: 'in-1', source: 'a', sourceHandle: 'right', target: 'b', targetHandle: 'left' },
			{ id: 'in-2', source: 'c', sourceHandle: 'right', target: 'b', targetHandle: 'left' }
		];

		expect(isOutputHandleFree('d', 'right')).toBe(true);
	});

	it('treats a null handle and an empty one as the same handle', () => {
		// An unused handle is `null` on a Svelte Flow edge and `''` once
		// `serializeMacro` has been through it, so a macro reopened from disk must
		// not suddenly allow a second wire where the live canvas refused one.
		graph.edges = [{ id: 'saved', source: 'a', sourceHandle: '', target: 'b' }];

		expect(isOutputHandleFree('a', null)).toBe(false);
		expect(isOutputHandleFree('a', undefined)).toBe(false);
	});

	it('allows the first edge out of an untouched node', () => {
		expect(isOutputHandleFree('b', 'right')).toBe(true);
	});
});

describe('detachOutputEdges', () => {
	beforeEach(() => {
		graph.nodes = seed();
		graph.edges = [
			{ id: 'gone', source: 'a', sourceHandle: 'out-2', target: 'b' },
			{ id: 'other-handle', source: 'a', sourceHandle: 'out-1', target: 'b' },
			{ id: 'other-node', source: 'b', sourceHandle: 'out-2', target: 'a' },
			{ id: 'incoming', source: 'b', sourceHandle: 'right', target: 'a' }
		];
	});

	it('drops only the edges leaving that node by that handle', () => {
		detachOutputEdges('a', 'out-2');

		expect(graph.edges.map((edge) => edge.id)).toEqual(['other-handle', 'other-node', 'incoming']);
	});

	it('matches a saved edge whose handle is the empty string', () => {
		graph.edges = [{ id: 'saved', source: 'a', sourceHandle: '', target: 'b' }];

		detachOutputEdges('a', '');

		expect(graph.edges).toEqual([]);
	});

	it('leaves the graph alone when nothing is attached', () => {
		const before = graph.edges.map((edge) => edge.id);

		detachOutputEdges('a', 'out-9');

		expect(graph.edges.map((edge) => edge.id)).toEqual(before);
	});
});

// The loop pair
// -------------
// A loop is two nodes and the edge between them, and the user draws none of it -
// the editor creates the pair, deletes the pair, and duplicates the pair. These
// tests are here rather than in a canvas test for the same reason
// `isOutputHandleFree`'s are: `Flow.svelte` is the whole workspace, and every one
// of these rules is a change to the graph this module owns. `Flow.svelte` is a
// one-line wrapper over each of them (its drop handler and its `onbeforedelete`).

/** A whole loop wired up the way `createLoopPair` and the user together leave it. */
const seedLoop = (loopId: string): void => {
	graph.nodes = [
		{ id: 'before', type: 'DelayNode', position: { x: 0, y: 0 }, data: {} },
		{ id: 'start', type: LOOP_START_NODE_TYPE, position: { x: 200, y: 0 }, data: { loopId } },
		{ id: 'body', type: 'MouseClickNode', position: { x: 400, y: 0 }, data: {} },
		{ id: 'end', type: LOOP_END_NODE_TYPE, position: { x: 620, y: 0 }, data: { loopId } }
	];
	graph.edges = [
		{ id: 'in', source: 'before', sourceHandle: 'right', target: 'start', targetHandle: 'left' },
		{ id: 'to-body', source: 'start', sourceHandle: 'body', target: 'body', targetHandle: 'left' },
		{ id: 'to-end', source: 'body', sourceHandle: 'right', target: 'end', targetHandle: 'left' },
		{
			id: 'back',
			source: 'end',
			sourceHandle: LOOP_BACK_HANDLE,
			target: 'start',
			targetHandle: 'left'
		}
	];
};

const nodeIds = (): string[] => graph.nodes.map((node) => node.id);
const edgeIds = (): string[] => graph.edges.map((edge) => edge.id);
const loopIdOf = (node: FlowNode): unknown => node.data.loopId;
const nodesOfType = (type: string): FlowNode[] => graph.nodes.filter((node) => node.type === type);
const backEdges = () =>
	graph.edges.filter((edge) => (edge.sourceHandle ?? '') === LOOP_BACK_HANDLE);

describe('createLoopPair', () => {
	beforeEach(() => {
		graph.nodes = [];
		graph.edges = [];
	});

	it('puts both halves and the edge between them on the canvas at once', () => {
		const { start, end, edge } = createLoopPair({ x: 100, y: 50 });

		expect(nodeIds()).toEqual([start.id, end.id]);
		expect(start.type).toBe(LOOP_START_NODE_TYPE);
		expect(end.type).toBe(LOOP_END_NODE_TYPE);
		// The edge the user never draws. It leaves the end by `back` and lands on
		// the start's input, which is what makes the walk go round.
		expect(graph.edges).toEqual([edge]);
		expect(edge.source).toBe(end.id);
		expect(edge.sourceHandle).toBe(LOOP_BACK_HANDLE);
		expect(edge.target).toBe(start.id);
		expect(edge.targetHandle).toBe('left');
		// No `type` on the edge: `defaultEdgeOptions` is merged *under* each edge,
		// so a type named here would beat `customedge` and the edge would render as
		// a built-in with no delete button.
		expect(edge.type).toBeUndefined();
	});

	it('gives the two halves one pairing id, and the end its own place', () => {
		const { start, end } = createLoopPair({ x: 100, y: 50 });

		expect(loopIdOf(start)).toBe(loopIdOf(end));
		expect(loopIdOf(start)).toBeTruthy();
		// Dropped clear of its own start rather than on top of it, so there is
		// somewhere to put the body.
		expect(start.position).toEqual({ x: 100, y: 50 });
		expect(end.position.x).toBeGreaterThan(start.position.x + 300);
		expect(end.position.y).toBe(start.position.y);
	});

	it('makes each loop a loop of its own', () => {
		const first = createLoopPair({ x: 0, y: 0 });
		const second = createLoopPair({ x: 0, y: 300 });

		// Two loops that believed they were the same loop would have a delete on one
		// take a node out of the other.
		expect(loopIdOf(first.start)).not.toBe(loopIdOf(second.start));
		expect(graph.nodes).toHaveLength(4);
		expect(backEdges()).toHaveLength(2);
	});

	it('leaves whatever was already on the canvas alone', () => {
		graph.nodes = seed();
		graph.edges = [{ id: 'e1', source: 'a', target: 'b' }];

		createLoopPair({ x: 0, y: 0 });

		expect(nodeIds().slice(0, 2)).toEqual(['a', 'b']);
		expect(edgeIds()[0]).toBe('e1');
	});
});

describe('loopPartnerId', () => {
	beforeEach(() => seedLoop('loop-1'));

	it('finds the other half from either end', () => {
		expect(loopPartnerId('start')).toBe('end');
		expect(loopPartnerId('end')).toBe('start');
	});

	it('has no answer for a node that is not half of a loop', () => {
		expect(loopPartnerId('body')).toBeUndefined();
		expect(loopPartnerId('nope')).toBeUndefined();
	});

	it('has no answer for a half whose partner is gone', () => {
		graph.nodes = graph.nodes.filter((node) => node.id !== 'end');
		graph.edges = graph.edges.filter((edge) => edge.id !== 'back');

		expect(loopPartnerId('start')).toBeUndefined();
	});

	it('falls back to the back edge when there is no pairing id', () => {
		// A macro saved before the pairing id existed, or edited outside the app.
		// The wiring is the only statement of the pair there is, and reading it is
		// what keeps such a file editable as a pair instead of coming apart the
		// first time the user deletes one end.
		for (const node of graph.nodes) delete node.data.loopId;

		expect(loopPartnerId('start')).toBe('end');
		expect(loopPartnerId('end')).toBe('start');
	});

	it('prefers the pairing id to the wiring', () => {
		// Two ends, and the back edge points at the wrong one - the user dragged the
		// halves around and rewired something. The id is what the editor created the
		// pair with, so it wins.
		graph.nodes = [
			...graph.nodes,
			{
				id: 'stray-end',
				type: LOOP_END_NODE_TYPE,
				position: { x: 800, y: 0 },
				data: { loopId: 'loop-2' }
			}
		];
		graph.edges = graph.edges.map((edge) =>
			edge.id === 'back' ? { ...edge, source: 'stray-end' } : edge
		);

		expect(loopPartnerId('start')).toBe('end');
	});
});

describe('withLoopPartners', () => {
	beforeEach(() => seedLoop('loop-1'));

	it('takes the whole loop when the start is deleted', () => {
		const doomed = withLoopPartners(['start'], ['in', 'to-body', 'back']);

		expect(doomed.nodes.map((node) => node.id).sort()).toEqual(['end', 'start']);
		// Every edge attached to either half, including the one the partner brought
		// with it: an edge whose endpoint has gone is wiring the user can neither
		// see nor remove.
		expect(doomed.edges.map((edge) => edge.id).sort()).toEqual(['back', 'in', 'to-body', 'to-end']);
	});

	it('takes the whole loop when the end is deleted', () => {
		// Svelte Flow hands the hook the edges it already collected for the node it
		// was asked to delete; the start's own edges arrive only because this widens
		// the deletion to it.
		const doomed = withLoopPartners(['end'], ['to-end', 'back']);

		expect(doomed.nodes.map((node) => node.id).sort()).toEqual(['end', 'start']);
		expect(doomed.edges.map((edge) => edge.id).sort()).toEqual(['back', 'in', 'to-body', 'to-end']);
	});

	it('leaves an ordinary deletion exactly as it was', () => {
		const doomed = withLoopPartners(['body'], ['to-body', 'to-end']);

		expect(doomed.nodes.map((node) => node.id)).toEqual(['body']);
		expect(doomed.edges.map((edge) => edge.id).sort()).toEqual(['to-body', 'to-end']);
	});

	it('does not reach into another loop', () => {
		graph.nodes = [
			...graph.nodes,
			{
				id: 'start-2',
				type: LOOP_START_NODE_TYPE,
				position: { x: 0, y: 400 },
				data: { loopId: 'loop-2' }
			},
			{
				id: 'end-2',
				type: LOOP_END_NODE_TYPE,
				position: { x: 400, y: 400 },
				data: { loopId: 'loop-2' }
			}
		];

		const doomed = withLoopPartners(['start'], []);

		expect(doomed.nodes.map((node) => node.id).sort()).toEqual(['end', 'start']);
	});

	it('deletes a half whose partner has already gone', () => {
		graph.nodes = graph.nodes.filter((node) => node.id !== 'end');

		const doomed = withLoopPartners(['start'], []);

		// Degrades rather than throwing: half a pair is still a node, and refusing
		// to delete it would leave the user with something they cannot get rid of.
		expect(doomed.nodes.map((node) => node.id)).toEqual(['start']);
	});
});

describe('duplicateNodes, on a loop', () => {
	beforeEach(() => seedLoop('loop-1'));

	it('gives a copied pair a pairing id and a back edge of its own', () => {
		const added = duplicateNodes(['start', 'end']);

		expect(added).toHaveLength(2);
		const copiedStart = added.find((node) => node.type === LOOP_START_NODE_TYPE) as FlowNode;
		const copiedEnd = added.find((node) => node.type === LOOP_END_NODE_TYPE) as FlowNode;

		// The trap this exists for. The copies carry their originals' `loopId`, so
		// left alone the canvas would hold two loops that believe they are the same
		// loop - and deleting one of them could take a node out of the other.
		expect(loopIdOf(copiedStart)).toBe(loopIdOf(copiedEnd));
		expect(loopIdOf(copiedStart)).not.toBe('loop-1');

		// `duplicateNodes` copies no edges between duplicated nodes, but the back
		// edge is the editor's rather than the user's, so it is re-created.
		expect(backEdges()).toHaveLength(2);
		expect(loopPartnerId(copiedStart.id)).toBe(copiedEnd.id);
		expect(loopPartnerId(copiedEnd.id)).toBe(copiedStart.id);
		// And the loop that was copied is untouched.
		expect(loopPartnerId('start')).toBe('end');
	});

	it('promotes half a copied pair into a whole new pair', () => {
		const added = duplicateNodes(['start']);

		// Refusing would have Ctrl+D on a Loop Start do nothing visible, or silently
		// drop the loop out of a mixed selection. Promoting keeps the invariant the
		// whole design rests on - a loop half always has a partner - and it is what
		// every other entry point already does.
		expect(added).toHaveLength(2);
		expect(nodesOfType(LOOP_START_NODE_TYPE)).toHaveLength(2);
		expect(nodesOfType(LOOP_END_NODE_TYPE)).toHaveLength(2);

		const [copy, minted] = added;
		expect(copy.type).toBe(LOOP_START_NODE_TYPE);
		expect(minted.type).toBe(LOOP_END_NODE_TYPE);
		expect(loopIdOf(copy)).toBe(loopIdOf(minted));
		expect(loopIdOf(copy)).not.toBe('loop-1');
		expect(loopPartnerId(copy.id)).toBe(minted.id);
		// Selected with the copy, so dragging the result apart moves the whole loop
		// rather than the half the user happened to pick.
		expect(minted.selected).toBe(true);
		// The minted Loop End has no payload to inherit - the backend reads none -
		// so it arrives with the pairing id and nothing else.
		expect(minted.data).toEqual({ loopId: loopIdOf(copy) });
	});

	it('promotes a copied Loop End the same way, into a loop that runs forever', () => {
		const added = duplicateNodes(['end']);

		const [copy, minted] = added;
		expect(copy.type).toBe(LOOP_END_NODE_TYPE);
		expect(minted.type).toBe(LOOP_START_NODE_TYPE);
		// The minted start is left at the defaults its component backfills - no
		// count and no condition, i.e. a loop that runs until the user stops it.
		// Copying the count off the half the user did *not* select would be
		// inventing a configuration they never asked for.
		expect(minted.data).toEqual({ loopId: loopIdOf(copy) });
		// Placed to the left of the end it belongs to, which is the way the pair
		// reads on the canvas.
		expect(minted.position.x).toBeLessThan(copy.position.x);
	});

	it('carries the loop settings onto the copied start', () => {
		graph.nodes[1].data = { loopId: 'loop-1', count: 3, operator: 'isSet', variable: 'matched' };

		const [copy] = duplicateNodes(['start']);

		// Everything but the pairing id is the user's configuration and is copied
		// like any other payload; the pairing id is the one field the copy must not
		// keep.
		expect(copy.data.count).toBe(3);
		expect(copy.data.operator).toBe('isSet');
		expect(copy.data.loopId).not.toBe('loop-1');
	});

	it('re-pairs each copied loop separately', () => {
		graph.nodes = [
			...graph.nodes,
			{
				id: 'start-2',
				type: LOOP_START_NODE_TYPE,
				position: { x: 0, y: 400 },
				data: { loopId: 'loop-2' }
			},
			{
				id: 'end-2',
				type: LOOP_END_NODE_TYPE,
				position: { x: 400, y: 400 },
				data: { loopId: 'loop-2' }
			}
		];
		graph.edges = [
			...graph.edges,
			{
				id: 'back-2',
				source: 'end-2',
				sourceHandle: LOOP_BACK_HANDLE,
				target: 'start-2',
				targetHandle: 'left'
			}
		];

		const added = duplicateNodes(['start', 'end', 'start-2', 'end-2']);

		const ids = added.map((node) => loopIdOf(node));
		// Four copies, two loops, two ids - and neither of them either original.
		expect(new Set(ids).size).toBe(2);
		expect(ids).not.toContain('loop-1');
		expect(ids).not.toContain('loop-2');
		expect(backEdges()).toHaveLength(4);
	});
});
