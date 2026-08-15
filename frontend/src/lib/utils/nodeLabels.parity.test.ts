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

// The graph walk that decides which nodes a run visits exists twice: once in
// `reachableFrom` (backend/execution.go), which decides what actually runs, and
// once here, which decides how the status panel names and orders nodes. If the
// two ever disagree the panel describes a run that did not happen - it names
// the wrong nodes as skipped, or sorts a stall report wrongly.
//
// So both are run over the same graphs, with the same expected answers, kept in
// `backend/testdata/reachability`. The Go half of this check is
// `TestReachableFromAgreesWithTheSharedFixtures`; the README in that directory
// documents the format. Adding a `.json` file there extends both suites - this
// test globs the directory rather than listing it, so there is nothing to edit
// here.
//
// This is the only test in the frontend that reaches outside `frontend/`. The
// alternative - a second copy of the graphs on this side - is exactly the drift
// the fixtures exist to close.

/** One fixture file. Mirrors `reachabilityFixture` in `execution_test.go`. */
type ReachabilityFixture = {
	description: string;
	flow: { nodes: LabellableNode[]; edges: LabellableEdge[] };
	reachable: string[];
};

// Resolved from this file rather than from the working directory, so the test
// passes whether vitest is run from `frontend/` or from the repository root.
const fixtureDir = resolve(
	dirname(fileURLToPath(import.meta.url)),
	'../../../../backend/testdata/reachability'
);

const fixtureFiles = readdirSync(fixtureDir)
	.filter((name) => name.endsWith('.json'))
	.sort();

const load = (name: string): ReachabilityFixture =>
	JSON.parse(readFileSync(join(fixtureDir, name), 'utf8')) as ReachabilityFixture;

describe('reachableNodes agrees with reachableFrom in Go', () => {
	it('found the shared fixtures', () => {
		// A wrong path would otherwise make this whole file pass by testing
		// nothing at all, quietly, for as long as it took someone to notice.
		expect(fixtureFiles.length).toBeGreaterThan(0);
	});

	for (const name of fixtureFiles) {
		const fixture = load(name);

		it(`${name}: ${fixture.description}`, () => {
			const reachable = reachableNodes(fixture.flow.nodes, fixture.flow.edges);

			expect([...reachable].sort()).toEqual([...fixture.reachable].sort());
		});
	}
});

describe('the labelling uses that same answer', () => {
	// `reachableNodes` is only worth testing if it is the answer the panel
	// actually acts on. `buildNodeLabels` shares `shapeGraph` and `walkForwards`
	// with it, and this pins the consequence of that: the reachable nodes are
	// the ones the ordering puts first, because everything unreachable is
	// numbered after them.
	for (const name of fixtureFiles) {
		const fixture = load(name);

		it(`${name}: numbers every reachable node before every unreachable one`, () => {
			const labels = buildNodeLabels(fixture.flow.nodes, fixture.flow.edges);
			const reachable = new Set(fixture.reachable);

			const steps = fixture.flow.nodes.map((node) => {
				const label = labels.get(node.id);
				if (label === undefined) throw new Error(`no label for ${node.id}`);
				return { id: node.id, step: label.step, reachable: reachable.has(node.id) };
			});

			const lastReachable = Math.max(
				0,
				...steps.filter((s) => s.reachable).map((s) => s.step)
			);
			const firstUnreachable = Math.min(
				Number.MAX_SAFE_INTEGER,
				...steps.filter((s) => !s.reachable).map((s) => s.step)
			);

			expect(lastReachable).toBeLessThan(firstUnreachable);
			// And the step numbers are a full 1..n, so "before" means something.
			expect(steps.map((s) => s.step).sort((a, b) => a - b)).toEqual(
				fixture.flow.nodes.map((_, index) => index + 1)
			);
		});
	}
});
