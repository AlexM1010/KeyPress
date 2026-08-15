import { describe, expect, it } from 'vitest';
import {
	buildNodeLabels,
	inFlowOrder,
	nodeLabel,
	UNKNOWN_NODE_LABEL,
	type LabellableEdge,
	type LabellableNode,
	type NodeLabel
} from './nodeLabels';

// The doc comment on `buildNodeLabels` is the specification these tests check.
// Each `describe` below names the claim it is holding the code to, so a change
// to one of those rules fails the test that quotes it rather than an anonymous
// assertion somewhere in the file.

const node = (id: string, type: string, x = 0, y = 0): LabellableNode => ({
	id,
	type,
	position: { x, y }
});

const edge = (source: string, target: string): LabellableEdge => ({ source, target });

/** The ids in step order - what the label map is actually for. */
const orderOf = (labels: Map<string, NodeLabel>): string[] =>
	[...labels.entries()].sort((a, b) => a[1].step - b[1].step).map(([id]) => id);

const stepOf = (labels: Map<string, NodeLabel>, id: string): number => {
	const label = labels.get(id);
	if (label === undefined) throw new Error(`no label for ${id}`);
	return label.step;
};

describe('buildNodeLabels: order agrees with the engine', () => {
	it('orders a join after ALL of its inputs, not after the first one', () => {
		// S -> A -> J
		// S -> B -> C -> J
		//
		// Plain breadth-first would release J as soon as A was seen and place it
		// level with C. `canEnqueue` in the Go engine waits for every
		// prerequisite, so J really runs last, and the labelling has to say so.
		const nodes = [
			node('S', 'StartNode', 0, 0),
			node('A', 'DelayNode', 100, 0),
			node('B', 'DelayNode', 100, 100),
			node('C', 'DelayNode', 200, 100),
			node('J', 'DelayNode', 300, 50)
		];
		const edges = [edge('S', 'A'), edge('S', 'B'), edge('B', 'C'), edge('A', 'J'), edge('C', 'J')];

		const labels = buildNodeLabels(nodes, edges);

		expect(orderOf(labels)).toEqual(['S', 'A', 'B', 'C', 'J']);
		// The claim that distinguishes longest-path from breadth-first.
		expect(stepOf(labels, 'J')).toBeGreaterThan(stepOf(labels, 'C'));
	});

	it('starts at the Start node however deep in the graph it is drawn', () => {
		const nodes = [
			node('A', 'DelayNode', 0, 0),
			node('S', 'StartNode', 0, 500),
			node('B', 'DelayNode', 0, 900)
		];
		const labels = buildNodeLabels(nodes, [edge('S', 'A'), edge('A', 'B')]);

		expect(orderOf(labels)).toEqual(['S', 'A', 'B']);
	});

	it('ignores edges pointing INTO the Start node', () => {
		// `StartExecution` enqueues Start unconditionally, so a back edge into it
		// is not a prerequisite - and must not be read as one, or Start would
		// wait forever on a graph that runs perfectly well.
		const nodes = [node('S', 'StartNode', 0, 0), node('A', 'DelayNode', 0, 100)];

		const labels = buildNodeLabels(nodes, [edge('S', 'A'), edge('A', 'S')]);

		// Not a cycle as far as the run is concerned: both nodes are released.
		expect(orderOf(labels)).toEqual(['S', 'A']);
		expect(stepOf(labels, 'S')).toBe(1);
	});

	it('leaves a node that only points into Start unreachable', () => {
		// The edge is dropped from the adjacency, but X still counts as wired -
		// which matches the backend's own "connected" test, so X is reported as
		// skipped rather than ignored.
		const nodes = [
			node('S', 'StartNode', 0, 0),
			node('A', 'DelayNode', 0, 100),
			node('X', 'DelayNode', 0, 200)
		];

		const labels = buildNodeLabels(nodes, [edge('S', 'A'), edge('X', 'S')]);

		expect(orderOf(labels)).toEqual(['S', 'A', 'X']);
	});

	it('skips edges whose endpoints are not both in the graph', () => {
		const nodes = [node('S', 'StartNode', 0, 0), node('A', 'DelayNode', 0, 100)];

		const labels = buildNodeLabels(nodes, [edge('S', 'ghost'), edge('ghost', 'A')]);

		// Neither node is wired to anything real, so A is an unconnected node.
		expect(labels.has('ghost')).toBe(false);
		expect(orderOf(labels)).toEqual(['S', 'A']);
	});
});

describe('buildNodeLabels: cycles', () => {
	it('terminates instead of spinning, and still labels the cyclic remainder', () => {
		// A -> B -> C -> A. Kahn's algorithm never releases any of them; the
		// engine would report exactly this set as a stall, and the stall message
		// names them - so they must all have a label.
		const nodes = [
			node('S', 'StartNode', 0, 0),
			node('A', 'DelayNode', 0, 100),
			node('B', 'DelayNode', 0, 200),
			node('C', 'DelayNode', 0, 300)
		];
		const edges = [edge('S', 'A'), edge('A', 'B'), edge('B', 'C'), edge('C', 'A')];

		const labels = buildNodeLabels(nodes, edges);

		expect(labels.size).toBe(4);
		// Start is released; the cycle is appended after it in canvas order.
		expect(orderOf(labels)).toEqual(['S', 'A', 'B', 'C']);
	});

	it('appends a node stuck behind a cycle as well', () => {
		// D hangs off the cycle: it can never be released either.
		const nodes = [
			node('S', 'StartNode', 0, 0),
			node('A', 'DelayNode', 0, 100),
			node('B', 'DelayNode', 0, 200),
			node('D', 'DelayNode', 0, 300)
		];
		const edges = [edge('S', 'A'), edge('A', 'B'), edge('B', 'A'), edge('B', 'D')];

		const labels = buildNodeLabels(nodes, edges);

		expect(orderOf(labels)).toEqual(['S', 'A', 'B', 'D']);
	});
});

describe('buildNodeLabels: what the run cannot reach', () => {
	it('groups dead branches weakly, after everything reachable, top group first', () => {
		// Two wired-but-unreachable branches. They are numbered one branch at a
		// time rather than interleaved by y, so the skipped-nodes warning reads
		// as branches rather than as a scatter of nodes.
		const nodes = [
			node('S', 'StartNode', 0, 0),
			node('A', 'DelayNode', 0, 50),
			node('E1', 'DelayNode', 0, 100),
			node('E2', 'DelayNode', 0, 200),
			node('D1', 'DelayNode', 0, 300),
			node('D2', 'DelayNode', 0, 400)
		];
		const edges = [edge('S', 'A'), edge('E1', 'E2'), edge('D1', 'D2')];

		const labels = buildNodeLabels(nodes, edges);

		expect(orderOf(labels)).toEqual(['S', 'A', 'E1', 'E2', 'D1', 'D2']);
	});

	it('keeps a dead branch together even when another branch sits between its nodes', () => {
		// E1 (y 100) and E2 (y 400) are one branch; D1 (y 200) and D2 (y 300)
		// another. Ordering by y alone would interleave them.
		const nodes = [
			node('S', 'StartNode', 0, 0),
			node('E1', 'DelayNode', 0, 100),
			node('D1', 'DelayNode', 0, 200),
			node('D2', 'DelayNode', 0, 300),
			node('E2', 'DelayNode', 0, 400)
		];
		const edges = [edge('E1', 'E2'), edge('D1', 'D2')];

		const labels = buildNodeLabels(nodes, edges);

		expect(orderOf(labels)).toEqual(['S', 'E1', 'E2', 'D1', 'D2']);
	});

	it('walks a dead branch in the same longest-path order', () => {
		// The join rule holds inside a dead branch too: prerequisites are counted
		// within the group, not globally.
		const nodes = [
			node('S', 'StartNode', 0, 0),
			node('P', 'DelayNode', 0, 100),
			node('Q', 'DelayNode', 0, 200),
			node('R', 'DelayNode', 0, 300),
			node('Z', 'DelayNode', 0, 150)
		];
		const edges = [edge('P', 'Q'), edge('Q', 'R'), edge('P', 'Z'), edge('R', 'Z')];

		const labels = buildNodeLabels(nodes, edges);

		expect(orderOf(labels)).toEqual(['S', 'P', 'Q', 'R', 'Z']);
	});

	it('puts nodes in no edge at all last, however high on the canvas they sit', () => {
		// I is the topmost node by y and still comes last: the backend never
		// reports it, and it is usually just something freshly dropped.
		const nodes = [
			node('S', 'StartNode', 0, 0),
			node('A', 'DelayNode', 0, 100),
			node('W', 'DelayNode', 0, 200),
			node('X', 'DelayNode', 0, 300),
			node('I', 'DelayNode', 0, -500)
		];
		const edges = [edge('S', 'A'), edge('W', 'X')];

		const labels = buildNodeLabels(nodes, edges);

		expect(orderOf(labels)).toEqual(['S', 'A', 'W', 'X', 'I']);
	});

	it('labels every node when there is no Start node at all', () => {
		const nodes = [node('A', 'DelayNode', 0, 100), node('B', 'DelayNode', 0, 0)];

		const labels = buildNodeLabels(nodes, [edge('A', 'B')]);

		// Nothing is reachable, so both are a dead branch - still named, in
		// longest-path order within the branch.
		expect(orderOf(labels)).toEqual(['A', 'B']);
	});
});

describe('buildNodeLabels: ties are broken deterministically', () => {
	it('orders nodes that come ready together by y, then x, then id', () => {
		const nodes = [
			node('S', 'StartNode', 0, -100),
			node('lower-right', 'DelayNode', 50, 10),
			node('lower-left', 'DelayNode', 10, 10),
			node('upper', 'DelayNode', 999, 0)
		];
		const edges = [edge('S', 'lower-right'), edge('S', 'lower-left'), edge('S', 'upper')];

		const labels = buildNodeLabels(nodes, edges);

		expect(orderOf(labels)).toEqual(['S', 'upper', 'lower-left', 'lower-right']);
	});

	it('falls back to the id when two nodes sit on exactly the same spot', () => {
		const nodes = [
			node('S', 'StartNode', 0, -100),
			node('m-b', 'DelayNode', 10, 10),
			node('m-a', 'DelayNode', 10, 10)
		];
		const edges = [edge('S', 'm-b'), edge('S', 'm-a')];

		expect(orderOf(buildNodeLabels(nodes, edges))).toEqual(['S', 'm-a', 'm-b']);
	});

	it('does not depend on the order the nodes and edges arrive in', () => {
		const nodes = [
			node('S', 'StartNode', 0, 0),
			node('A', 'DelayNode', 100, 0),
			node('B', 'DelayNode', 100, 100),
			node('J', 'DelayNode', 200, 50)
		];
		const edges = [edge('S', 'A'), edge('S', 'B'), edge('A', 'J'), edge('B', 'J')];

		const forwards = orderOf(buildNodeLabels(nodes, edges));
		const backwards = orderOf(buildNodeLabels([...nodes].reverse(), [...edges].reverse()));

		expect(forwards).toEqual(['S', 'A', 'B', 'J']);
		expect(backwards).toEqual(forwards);
	});
});

describe('buildNodeLabels: what a node is called', () => {
	it('names a registered type the way the node itself does on the canvas', () => {
		const nodes = [
			node('S', 'StartNode', 0, 0),
			node('d', 'DelayNode', 0, 100),
			node('c', 'MouseClickNode', 0, 200),
			node('m', 'MouseMoveNode', 0, 300),
			node('w', 'ColorPickerNode', 0, 400),
			node('k', 'KeyPressNode', 0, 500)
		];
		const edges = [
			edge('S', 'd'),
			edge('d', 'c'),
			edge('c', 'm'),
			edge('m', 'w'),
			edge('w', 'k')
		];

		const labels = buildNodeLabels(nodes, edges);

		expect(orderOf(labels).map((id) => labels.get(id)!.label)).toEqual([
			'Start',
			'Delay',
			'Mouse Click',
			'Mouse Move',
			'Wait For Color',
			'Keypress'
		]);
	});

	it('falls back to the registry key for an unregistered type', () => {
		const nodes = [node('S', 'StartNode', 0, 0), node('f', 'FrobnicateNode', 0, 100)];

		const labels = buildNodeLabels(nodes, [edge('S', 'f')]);

		expect(labels.get('f')?.label).toBe('FrobnicateNode');
	});

	it('falls back to the placeholder for a node with no type at all', () => {
		const nodes: LabellableNode[] = [
			node('S', 'StartNode', 0, 0),
			{ id: 'untyped', position: { x: 0, y: 100 } }
		];

		const labels = buildNodeLabels(nodes, [edge('S', 'untyped')]);

		expect(labels.get('untyped')?.label).toBe(UNKNOWN_NODE_LABEL);
	});

	it('numbers the steps from 1 with no gaps', () => {
		const nodes = [
			node('S', 'StartNode', 0, 0),
			node('A', 'DelayNode', 0, 100),
			node('B', 'DelayNode', 0, 200)
		];

		const labels = buildNodeLabels(nodes, [edge('S', 'A'), edge('A', 'B')]);

		expect([...labels.values()].map((label) => label.step).sort()).toEqual([1, 2, 3]);
	});
});

describe('nodeLabel', () => {
	const labels = buildNodeLabels(
		[node('S', 'StartNode', 0, 0), node('d', 'DelayNode', 0, 100)],
		[edge('S', 'd')]
	);

	it('names a node the graph knows about', () => {
		expect(nodeLabel(labels, 'd')).toBe('Delay');
	});

	it('degrades visibly for an id the run did not start with', () => {
		// A node deleted mid-run, or an event left over from someone else's run.
		expect(nodeLabel(labels, 'deleted-mid-run')).toBe(UNKNOWN_NODE_LABEL);
	});

	it('degrades for an empty map rather than throwing', () => {
		expect(nodeLabel(new Map(), 'anything')).toBe(UNKNOWN_NODE_LABEL);
	});
});

describe('inFlowOrder', () => {
	const labels = buildNodeLabels(
		[
			node('S', 'StartNode', 0, 0),
			node('A', 'DelayNode', 100, 0),
			node('B', 'DelayNode', 100, 100),
			node('J', 'DelayNode', 200, 50)
		],
		[edge('S', 'A'), edge('S', 'B'), edge('A', 'J'), edge('B', 'J')]
	);

	it('sorts reported ids into the order the flow would have run them', () => {
		expect(inFlowOrder(labels, ['J', 'B', 'S', 'A'])).toEqual(['S', 'A', 'B', 'J']);
	});

	it('does not mutate the list it was given', () => {
		const reported = ['J', 'A'];
		inFlowOrder(labels, reported);
		expect(reported).toEqual(['J', 'A']);
	});

	it('puts unknown ids last, in a stable order of their own', () => {
		expect(inFlowOrder(labels, ['zz', 'A', 'aa'])).toEqual(['A', 'aa', 'zz']);
	});

	it('keeps duplicates rather than collapsing them', () => {
		expect(inFlowOrder(labels, ['J', 'A', 'A'])).toEqual(['A', 'A', 'J']);
	});
});
