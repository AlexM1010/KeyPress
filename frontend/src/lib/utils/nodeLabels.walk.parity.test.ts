import { readFileSync, readdirSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import {
	buildNodeLabels,
	reachableNodes,
	type LabellableEdge,
	type LabellableNode
} from './nodeLabels';

// The *order* the execution token visits nodes in is described twice: once by
// the interpreter in `backend/interpreter.go`, which is what a run actually
// does, and once by `buildNodeLabels` here, which is what the status panel
// says a run is doing. The sibling file `nodeLabels.parity.test.ts` pins the
// other half of the pair - which nodes a run can reach at all - against
// `backend/testdata/reachability`; this one pins the order against
// `backend/testdata/walk`, whose README documents the format.
//
// Without it the panel can narrate a run in an order the token never took:
// name the second branch of a Sequence node first, or number a join before the
// branch that reaches it. The Go half of this check is
// `TestTheWalkAgreesWithTheSharedFixtures`.
//
// Adding a `.json` file to that directory extends both suites - this test globs
// the directory rather than listing it, so there is nothing to edit here.

/** One fixture file. Mirrors `walkFixture` in `execution_test.go`. */
type WalkFixture = {
	description: string;
	flow: { nodes: LabellableNode[]; edges: LabellableEdge[] };
	/** The ids the token runs, in order, **with repeats**. */
	visits: string[];
	/**
	 * The ids the backend's multipath lint flags. Read by the Go suite only -
	 * the lint has no frontend counterpart to keep honest - and declared here so
	 * this type is the whole file format rather than the part one test uses.
	 */
	multipath?: string[];
	/** The output handle a node leaves by, for the nodes that pick one. */
	outputs?: Record<string, string>;
};

// Resolved from this file rather than from the working directory, so the test
// passes whether vitest is run from `frontend/` or from the repository root.
const fixtureDir = resolve(
	dirname(fileURLToPath(import.meta.url)),
	'../../../../backend/testdata/walk'
);

const fixtureFiles = readdirSync(fixtureDir)
	.filter((name) => name.endsWith('.json'))
	.sort();

const load = (name: string): WalkFixture =>
	JSON.parse(readFileSync(join(fixtureDir, name), 'utf8')) as WalkFixture;

/** A fixture in which some node leaves by a named output - see below. */
const picksAnOutput = (fixture: WalkFixture): boolean =>
	Object.keys(fixture.outputs ?? {}).length > 0;

/**
 * The two sides say the same thing in two shapes, and this is the reduction
 * that lines them up.
 *
 * `visits` is a list of arrivals, with repeats: the token runs a diamond's join
 * once per branch, so the join appears twice, and a loop's body appears once per
 * turn until its count runs out. `buildNodeLabels` returns one entry per node,
 * numbered by the arrival that *first* reached it - a node is numbered when the
 * token first gets there and skipped on every later arrival, which is what makes
 * a cycle terminate the numbering.
 *
 * So dropping every arrival but the first at each node turns the one into the
 * other: what is left is the ids in the order the token first reached them,
 * which is exactly what the step numbers are. A `Set` built from the arrivals is
 * that reduction - it keeps insertion order and ignores a repeated insert - and
 * it is a reduction of the *fixture*, never of the labels, so the test cannot
 * quietly massage the two into agreement.
 */
const firstArrivals = (visits: string[]): string[] => [...new Set(visits)];

/**
 * The reachable nodes of the label map, in step order.
 *
 * The filter matters. `buildNodeLabels` numbers every node of the flow, not just
 * the ones a run reaches: unreachable-but-wired nodes continue the sequence
 * after them (the skipped-nodes warning is sorted by that number), and nodes in
 * no edge at all come last. No fixture's `visits` mentions any of those, because
 * the token never arrives at them - they are not a disagreement, they are a
 * second job the label map does. They are excluded by asking `reachableNodes`
 * which nodes the walk reaches, rather than by cutting the list down to the
 * length of `visits`, so a node the walk wrongly *did* reach still shows up as a
 * surplus entry and fails the comparison.
 */
const reachableInStepOrder = (fixture: WalkFixture): string[] => {
	const { nodes, edges } = fixture.flow;
	const labels = buildNodeLabels(nodes, edges);
	const reachable = reachableNodes(nodes, edges);

	return [...labels.entries()]
		.filter(([id]) => reachable.has(id))
		.sort((a, b) => a[1].step - b[1].step)
		.map(([id]) => id);
};

describe('buildNodeLabels orders nodes the way the interpreter walks them', () => {
	it('found the shared fixtures', () => {
		// A wrong path would otherwise make this whole file pass by testing
		// nothing at all, quietly, for as long as it took someone to notice.
		expect(fixtureFiles.length).toBeGreaterThan(0);
	});

	for (const name of fixtureFiles) {
		const fixture = load(name);
		if (picksAnOutput(fixture)) continue;

		it(`${name}: ${fixture.description}`, () => {
			// A fixture with a loop in it ends by its own count or condition - there
			// is no iteration budget any more, so a loop with no way out would hang
			// this suite rather than fail it, and the README in the fixture directory
			// says as much. Its repeated `visits` still reduce to the same first
			// arrivals the labeller numbers, which is what makes the equality hold
			// for a looping fixture as well as a linear one.
			expect(reachableInStepOrder(fixture)).toEqual(firstArrivals(fixture.visits));
		});
	}
});

describe('a fixture whose nodes pick an output bounds the labelling rather than matching it', () => {
	// `outputs` names the handle a node leaves by, and the interpreter then takes
	// that one edge and no other. `buildNodeLabels` cannot follow: it is built
	// once, when the run starts, before any node has run and therefore before any
	// of them has chosen an output - and it has to number the nodes on the branch
	// not taken too, since the panel names them either way. It is the static
	// answer, the same one `reachableFrom` gives in Go, and the fixture is one
	// dynamic run through it.
	//
	// That is a difference in what the two are for rather than a disagreement, so
	// what is asserted here is the relationship that must hold between them: a
	// run can only ever visit nodes the static walk numbered, because taking one
	// outgoing edge is taking a subset of all of them. If a node the token really
	// ran is missing from the labels, the panel has no name for a node the user
	// is watching run, and that is a genuine failure.
	for (const name of fixtureFiles) {
		const fixture = load(name);
		if (!picksAnOutput(fixture)) continue;

		it(`${name}: every node the token runs is numbered - ${fixture.description}`, () => {
			expect(reachableInStepOrder(fixture)).toEqual(
				expect.arrayContaining(firstArrivals(fixture.visits))
			);
		});
	}
});

describe('the comparison excludes unreachable nodes rather than trimming the list', () => {
	// Not a fixture: no graph in `backend/testdata/walk` has a dead branch or a
	// node in no edge at all, so nothing there exercises the filter in
	// `reachableInStepOrder` - it would pass just as well if it excluded nothing.
	// This graph is local to the frontend for that reason, and stays here rather
	// than being added to the backend's directory, which is the Go suite's input
	// and not this test's to extend.
	const nodes: LabellableNode[] = [
		{ id: 'start', type: 'StartNode', position: { x: 0, y: 0 } },
		{ id: 'ran', type: 'DelayNode', position: { x: 200, y: 0 } },
		{ id: 'dead', type: 'DelayNode', position: { x: 200, y: 200 } },
		{ id: 'dead-tail', type: 'DelayNode', position: { x: 400, y: 200 } },
		{ id: 'loose', type: 'DelayNode', position: { x: 0, y: 400 } }
	];
	const edges: LabellableEdge[] = [
		{ source: 'start', target: 'ran', sourceHandle: 'right' },
		{ source: 'dead', target: 'dead-tail', sourceHandle: 'right' }
	];

	it('numbers the wired-but-unreachable and the unwired, and leaves both out', () => {
		const labels = buildNodeLabels(nodes, edges);

		// All five are numbered - that is the second job the label map does.
		expect(labels.size).toBe(5);
		// The comparison sees only the two the token would reach, so a fixture
		// whose `visits` are `["start", "ran"]` is a match rather than being two
		// entries short.
		expect(reachableInStepOrder({ description: '', flow: { nodes, edges }, visits: [] })).toEqual([
			'start',
			'ran'
		]);
	});
});
