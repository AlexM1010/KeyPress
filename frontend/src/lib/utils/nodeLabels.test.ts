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

/**
 * `sourceHandle` is optional here for the same reason it is optional on the
 * type: most nodes have one output and their edges leave it unnamed. Where a
 * test cares which output an edge leaves by - which is the whole point of a
 * Sequence node - it says so.
 */
const edge = (source: string, target: string, sourceHandle?: string): LabellableEdge => ({
	source,
	target,
	sourceHandle
});

/** The ids in step order - what the label map is actually for. */
const orderOf = (labels: Map<string, NodeLabel>): string[] =>
	[...labels.entries()].sort((a, b) => a[1].step - b[1].step).map(([id]) => id);

const stepOf = (labels: Map<string, NodeLabel>, id: string): number => {
	const label = labels.get(id);
	if (label === undefined) throw new Error(`no label for ${id}`);
	return label.step;
};

describe('buildNodeLabels: the order is the order the token visits nodes', () => {
	it('follows a straight line of nodes in the order it is drawn', () => {
		const nodes = [
			node('S', 'StartNode', 0, 0),
			node('A', 'DelayNode', 100, 0),
			node('B', 'DelayNode', 200, 0)
		];

		const labels = buildNodeLabels(nodes, [edge('S', 'A'), edge('A', 'B')]);

		expect(orderOf(labels)).toEqual(['S', 'A', 'B']);
	});

	it('runs a Sequence output to its end before it starts the next one', () => {
		// S -> Q, and Q fans out: out-1 into A1 -> A2, out-2 into B1 -> B2.
		//
		// Depth-first is the claim. Breadth-first would give S, Q, A1, B1, A2, B2
		// - the two branches interleaved - which is precisely what the token
		// cannot do: it is one token, and it does not come back to Q until A2 has
		// finished.
		const nodes = [
			node('S', 'StartNode', 0, 0),
			node('Q', 'SequenceNode', 100, 0),
			node('A1', 'DelayNode', 200, 0),
			node('A2', 'DelayNode', 300, 0),
			node('B1', 'DelayNode', 200, 100),
			node('B2', 'DelayNode', 300, 100)
		];
		// Deliberately out of handle order in the array: the sort is what decides,
		// not the order the edges happen to arrive in.
		const edges = [
			edge('S', 'Q'),
			edge('Q', 'B1', 'out-2'),
			edge('B1', 'B2'),
			edge('Q', 'A1', 'out-1'),
			edge('A1', 'A2')
		];

		const labels = buildNodeLabels(nodes, edges);

		expect(orderOf(labels)).toEqual(['S', 'Q', 'A1', 'A2', 'B1', 'B2']);
	});

	it('numbers a join the FIRST time the token reaches it, not after every input', () => {
		// S -> Q, Q -out-1-> A -> J, Q -out-2-> B -> J.
		//
		// The old dependency engine ran J once, after both A and B, and the
		// labelling had to order it last to match. The token reaches J straight
		// out of A - and reaches it again out of B, where it runs a second time
		// but keeps the number it already has, because a node has one name.
		const nodes = [
			node('S', 'StartNode', 0, 0),
			node('Q', 'SequenceNode', 100, 0),
			node('A', 'DelayNode', 200, 0),
			node('B', 'DelayNode', 200, 100),
			node('J', 'DelayNode', 300, 50)
		];
		const edges = [
			edge('S', 'Q'),
			edge('Q', 'A', 'out-1'),
			edge('Q', 'B', 'out-2'),
			edge('A', 'J'),
			edge('B', 'J')
		];

		const labels = buildNodeLabels(nodes, edges);

		expect(orderOf(labels)).toEqual(['S', 'Q', 'A', 'J', 'B']);
		// The claim that distinguishes the walk from the longest-path scheduler it
		// replaced: J comes before B, not after everything that feeds it.
		expect(stepOf(labels, 'J')).toBeLessThan(stepOf(labels, 'B'));
		// And it is named once, however many times it runs.
		expect(labels.size).toBe(5);
	});

	it('sorts nine Sequence outputs the way a plain string sort does', () => {
		// The reason `SequenceNode` caps at nine: the handle ids are compared as
		// strings, so `out-10` would sort between `out-1` and `out-2`. Up to nine
		// the string order and the number order are the same, and this pins that
		// the walk really does take them 1..9.
		const outputs = [1, 2, 3, 4, 5, 6, 7, 8, 9];
		const nodes = [
			node('S', 'StartNode', 0, 0),
			node('Q', 'SequenceNode', 100, 0),
			// Positions run backwards up the canvas, so canvas order cannot be what
			// produces the answer.
			...outputs.map((n) => node(`n${n}`, 'DelayNode', 200, 1000 - n * 100))
		];
		const edges = [
			edge('S', 'Q'),
			// Reversed, again so the array order cannot be what produces the answer.
			...outputs
				.slice()
				.reverse()
				.map((n) => edge('Q', `n${n}`, `out-${n}`))
		];

		const labels = buildNodeLabels(nodes, edges);

		expect(orderOf(labels)).toEqual(['S', 'Q', ...outputs.map((n) => `n${n}`)]);
	});

	it('takes an edge with no handle before one that names an output', () => {
		// An unnamed handle is "" and sorts first. Not a shape the editor draws -
		// a node either has one unnamed output or a set of named ones - but a
		// saved macro can hold it, and the order must not be the array's.
		const nodes = [
			node('S', 'StartNode', 0, 0),
			node('Q', 'SequenceNode', 100, 0),
			node('named', 'DelayNode', 200, 0),
			node('unnamed', 'DelayNode', 200, 100)
		];
		const edges = [edge('S', 'Q'), edge('Q', 'named', 'out-1'), edge('Q', 'unnamed')];

		expect(orderOf(buildNodeLabels(nodes, edges))).toEqual(['S', 'Q', 'unnamed', 'named']);
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
		// The run begins at Start unconditionally, so an edge back into it is a
		// loop the walk has already been round rather than a way in.
		const nodes = [node('S', 'StartNode', 0, 0), node('A', 'DelayNode', 0, 100)];

		const labels = buildNodeLabels(nodes, [edge('S', 'A'), edge('A', 'S')]);

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
	it('terminates instead of spinning, and numbers each node once', () => {
		// A -> B -> C -> A. A cycle is a loop now rather than a deadlock, and the
		// walk visits each node once so it ends: the token would go round again,
		// but the node has one name and one number.
		const nodes = [
			node('S', 'StartNode', 0, 0),
			node('A', 'DelayNode', 0, 100),
			node('B', 'DelayNode', 0, 200),
			node('C', 'DelayNode', 0, 300)
		];
		const edges = [edge('S', 'A'), edge('A', 'B'), edge('B', 'C'), edge('C', 'A')];

		const labels = buildNodeLabels(nodes, edges);

		expect(labels.size).toBe(4);
		expect(orderOf(labels)).toEqual(['S', 'A', 'B', 'C']);
	});

	it('carries on past a loop to whatever hangs off it', () => {
		// A -> B -> A is the loop; D hangs off B. The old Kahn pass could never
		// release any of these and appended all three in canvas order; the token
		// walks straight through them.
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

	it('walks a self-edge once', () => {
		const nodes = [node('S', 'StartNode', 0, 0), node('L', 'DelayNode', 0, 100)];

		const labels = buildNodeLabels(nodes, [edge('S', 'L'), edge('L', 'L')]);

		expect(orderOf(labels)).toEqual(['S', 'L']);
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

	it('walks a dead branch from its roots, as the run would have walked it', () => {
		// P -> Q -> R -> Z, and P -> Z. Nothing starts this branch, so it has no
		// token order of its own - but walking it from the node nothing points at
		// still reads as a branch, where listing it by canvas position (P, Z, Q,
		// R) would put the join second.
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

	it('falls back to canvas order for a dead branch that is nothing but a loop', () => {
		// C1 <-> C2 has no root - every member is pointed at from inside the
		// group - so there is nowhere obvious to start and the topmost node is as
		// good an answer as any.
		const nodes = [
			node('S', 'StartNode', 0, 0),
			node('C2', 'DelayNode', 0, 300),
			node('C1', 'DelayNode', 0, 100)
		];
		const edges = [edge('C1', 'C2'), edge('C2', 'C1')];

		const labels = buildNodeLabels(nodes, edges);

		expect(orderOf(labels)).toEqual(['S', 'C1', 'C2']);
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

		// Nothing is reachable, so both are a dead branch - still named, and still
		// walked from the node nothing points at rather than listed by y, which
		// would put B first.
		expect(orderOf(labels)).toEqual(['A', 'B']);
	});
});

describe('buildNodeLabels: ties are broken deterministically', () => {
	it('orders two edges leaving one handle by target id, not by canvas position', () => {
		// The editor now refuses to draw this - one output takes one edge - but a
		// macro saved before that rule can hold it, and the two branches must
		// come out in the same order every time whatever the array says.
		const nodes = [
			node('S', 'StartNode', 0, 0),
			node('zeta', 'DelayNode', 0, 10),
			node('alpha', 'DelayNode', 0, 900)
		];
		const edges = [edge('S', 'zeta', 'right'), edge('S', 'alpha', 'right')];

		// Canvas order would say zeta first; the target id says alpha.
		expect(orderOf(buildNodeLabels(nodes, edges))).toEqual(['S', 'alpha', 'zeta']);
	});

	it('orders unwired nodes by y, then x, then id', () => {
		// The last bucket is the one canvas order still governs outright.
		const nodes = [
			node('S', 'StartNode', 0, -100),
			node('lower-right', 'DelayNode', 50, 10),
			node('lower-left', 'DelayNode', 10, 10),
			node('upper', 'DelayNode', 999, 0)
		];

		const labels = buildNodeLabels(nodes, []);

		expect(orderOf(labels)).toEqual(['S', 'upper', 'lower-left', 'lower-right']);
	});

	it('falls back to the id when two unwired nodes sit on exactly the same spot', () => {
		const nodes = [
			node('S', 'StartNode', 0, -100),
			node('m-b', 'DelayNode', 10, 10),
			node('m-a', 'DelayNode', 10, 10)
		];

		expect(orderOf(buildNodeLabels(nodes, []))).toEqual(['S', 'm-a', 'm-b']);
	});

	it('does not depend on the order the nodes and edges arrive in', () => {
		const nodes = [
			node('S', 'StartNode', 0, 0),
			node('Q', 'SequenceNode', 100, 0),
			node('A', 'DelayNode', 200, 0),
			node('B', 'DelayNode', 200, 100),
			node('J', 'DelayNode', 300, 50)
		];
		const edges = [
			edge('S', 'Q'),
			edge('Q', 'A', 'out-1'),
			edge('Q', 'B', 'out-2'),
			edge('A', 'J'),
			edge('B', 'J')
		];

		const forwards = orderOf(buildNodeLabels(nodes, edges));
		const backwards = orderOf(buildNodeLabels([...nodes].reverse(), [...edges].reverse()));

		expect(forwards).toEqual(['S', 'Q', 'A', 'J', 'B']);
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
			node('k', 'KeyPressNode', 0, 500),
			node('q', 'SequenceNode', 0, 600)
		];
		const edges = [
			edge('S', 'd'),
			edge('d', 'c'),
			edge('c', 'm'),
			edge('m', 'w'),
			edge('w', 'k'),
			edge('k', 'q')
		];

		const labels = buildNodeLabels(nodes, edges);

		expect(orderOf(labels).map((id) => labels.get(id)!.label)).toEqual([
			'Start',
			'Delay',
			'Mouse Click',
			'Mouse Move',
			'Wait For Color',
			'Keypress',
			'Sequence'
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
	// S -> Q, Q -out-1-> A, Q -out-2-> B. Steps: S 1, Q 2, A 3, B 4.
	const labels = buildNodeLabels(
		[
			node('S', 'StartNode', 0, 0),
			node('Q', 'SequenceNode', 100, 0),
			node('A', 'DelayNode', 200, 0),
			node('B', 'DelayNode', 200, 100)
		],
		[edge('S', 'Q'), edge('Q', 'A', 'out-1'), edge('Q', 'B', 'out-2')]
	);

	it('sorts reported ids into the order the flow would have run them', () => {
		expect(inFlowOrder(labels, ['B', 'A', 'S', 'Q'])).toEqual(['S', 'Q', 'A', 'B']);
	});

	it('does not mutate the list it was given', () => {
		const reported = ['B', 'A'];
		inFlowOrder(labels, reported);
		expect(reported).toEqual(['B', 'A']);
	});

	it('puts unknown ids last, in a stable order of their own', () => {
		expect(inFlowOrder(labels, ['zz', 'A', 'aa'])).toEqual(['A', 'aa', 'zz']);
	});

	it('keeps duplicates rather than collapsing them', () => {
		expect(inFlowOrder(labels, ['B', 'A', 'A'])).toEqual(['A', 'A', 'B']);
	});
});
