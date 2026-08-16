// frontend\src\lib\utils\nodeLabels.ts
//
// Naming the nodes of a flow, and putting lists of them into the order the run
// walks the graph. Used by `Flow.svelte`, which holds the label map for the
// duration of one run and hands it back to the two lookups at the bottom of
// this file.

/**
 * Human-readable name for each node type, keyed by the registry key from
 * `customNodes/nodeTypes.ts` - the string that ends up in a saved node's
 * `type` and that the Go dispatcher in `tasks.go` switches on.
 *
 * The values are copies of the node components' own `title` prop defaults
 * (`StartNode.svelte`, `DelayNode.svelte`, `MouseClickNode.svelte`,
 * `MouseMoveNode.svelte`, `ColorPickerNode.svelte`, `KeyPressNode.svelte`,
 * `SequenceNode.svelte`, `BranchNode.svelte`, `LoopStartNode.svelte`,
 * `LoopEndNode.svelte`), so the status panel calls a node
 * exactly what the node's own header calls it on the canvas. Those components are the source of
 * truth: when one of them renames itself, this table follows. A type missing
 * from here still gets a label - see `buildNodeLabels`.
 */
export const NODE_TYPE_TITLES: Record<string, string> = {
	StartNode: 'Start',
	DelayNode: 'Delay',
	MouseClickNode: 'Mouse Click',
	MouseMoveNode: 'Mouse Move',
	ColorPickerNode: 'Wait For Color',
	KeyPressNode: 'Keypress',
	SequenceNode: 'Sequence',
	BranchNode: 'Branch',
	LoopStartNode: 'Loop Start',
	LoopEndNode: 'Loop End'
};

/** Shown when a reported node is not in the graph the run was started from. */
export const UNKNOWN_NODE_LABEL = 'Unknown node';

/**
 * The subset of a node this file needs to label it. Deliberately structural
 * rather than `FlowNode`: `toObject()` hands back Svelte Flow's own `Node`,
 * whose `type` is optional.
 */
export type LabellableNode = {
	id: string;
	type?: string;
	position: { x: number; y: number };
};

/**
 * The subset of an edge this file needs. Structural for the same reason:
 * `toObject()` returns Svelte Flow's `Edge`, whose `sourceHandle` is `null`
 * when the edge leaves a node's only output, and the Wails bindings type a Go
 * slice as `T[] | null` - so both flavours of "no handle" have to be taken.
 *
 * `sourceHandle` is not decoration: it is which output an edge leaves by, and
 * the walk below takes a node's outgoing edges in ascending order of it.
 */
export type LabellableEdge = {
	source: string;
	target: string;
	sourceHandle?: string | null;
};

/**
 * What a node is called on screen, plus its place in the sequence.
 *
 * `step` is the number in the label, kept separately so lists of reported
 * ids can be put back into flow order without parsing the text.
 */
export type NodeLabel = {
	label: string;
	step: number;
};

/**
 * A flow reduced to the shape both passes below work over. Built once, by
 * `shapeGraph`, so that "what the run can reach" and "what order the nodes come
 * in" can never read the same flowchart two different ways.
 */
type FlowGraph = {
	/** Every node of the flow, by id. */
	byId: Map<string, LabellableNode>;
	/** The node the run begins at, absent if the flow has none. */
	startId: string | undefined;
	/**
	 * Forward adjacency: which nodes each node hands the token on to, already in
	 * the order the token takes them - see `shapeGraph`.
	 */
	successors: Map<string, string[]>;
	/** The same edges backwards: which nodes point at each node. */
	predecessors: Map<string, string[]>;
	/**
	 * Every node that takes part in an edge, recorded before any edge is dropped
	 * from the adjacency, so it matches the backend's own "connected" test in
	 * `warnAboutSkippedNodes`.
	 */
	wired: Set<string>;
};

/**
 * Compares two strings by code unit, which is what a plain ascending sort of a
 * handle id means on the Go side (`sort.Strings` is bytewise). `localeCompare`
 * is the reflex and the wrong call here - it folds case and ignores
 * punctuation, so it would agree with the engine on `out-1` vs `out-2` and
 * disagree on ids that differ only in the parts it smooths over.
 */
const byCodeUnit = (a: string, b: string): number => (a < b ? -1 : a > b ? 1 : 0);

/**
 * Shapes the raw nodes and edges into a `FlowGraph`, keeping only the edges
 * that actually join two nodes of this flow and dropping the ones pointing
 * *into* the Start node - which the run begins at unconditionally, so an edge
 * back into it is a loop the walk has already been round, not a way in.
 *
 * Each node's outgoing edges come out **sorted by `sourceHandle` ascending,
 * then by target id**, because that is the order the interpreter follows them
 * in: a handler returning `""` means "the only output" and takes every outgoing
 * edge in handle order. The target id is the tie-break for two edges leaving
 * one handle - which the editor now refuses to draw, but a macro saved before
 * that rule (or drawn through Svelte Flow's loose connection mode) can still
 * hold, and the order of the two must not depend on which one happens to come
 * first in the array.
 *
 * `sortedSuccessors` in `backend/interpreter.go` breaks that tie the same way,
 * and did not always: it kept whatever order the file listed until a review
 * caught the two walks disagreeing. Nothing a user can run reaches the case -
 * `validateOutputHandles` refuses the graph before a run starts - but the walk
 * fixtures in `backend/testdata/walk` are driven straight into `run.step`,
 * bypassing that check, and both this suite and the Go one read them. A fixture
 * with two edges on one handle is exactly the drift those shared fixtures exist
 * to catch, so the two sides have to agree here even though no macro can.
 */
function shapeGraph(nodes: LabellableNode[], edges: LabellableEdge[]): FlowGraph {
	const byId = new Map<string, LabellableNode>();
	for (const node of nodes) byId.set(node.id, node);

	const startId = nodes.find((node) => node.type === 'StartNode')?.id;

	const outgoing = new Map<string, { handle: string; target: string }[]>();
	const predecessors = new Map<string, string[]>();
	const wired = new Set<string>();
	for (const edge of edges) {
		if (!byId.has(edge.source) || !byId.has(edge.target)) continue;
		wired.add(edge.source);
		wired.add(edge.target);
		// The Start node runs regardless of what points at it.
		if (edge.target === startId) continue;

		const out = outgoing.get(edge.source);
		// An unused handle is `null` on a Svelte Flow edge and `""` once
		// `serializeMacro` has been through it; both mean "the only output".
		const step = { handle: edge.sourceHandle ?? '', target: edge.target };
		if (out) out.push(step);
		else outgoing.set(edge.source, [step]);

		const back = predecessors.get(edge.target);
		if (back) back.push(edge.source);
		else predecessors.set(edge.target, [edge.source]);
	}

	const successors = new Map<string, string[]>();
	for (const [source, out] of outgoing) {
		out.sort((a, b) => byCodeUnit(a.handle, b.handle) || byCodeUnit(a.target, b.target));
		successors.set(
			source,
			out.map((step) => step.target)
		);
	}

	return { byId, startId, successors, predecessors, wired };
}

/**
 * The token's walk, from wherever it is put down: depth-first, outgoing edges
 * taken in the order `shapeGraph` sorted them into, and each node expanded at
 * most once so a cycle terminates the walk instead of spinning in it.
 *
 * Adds to `visited` in visit order, which is what makes a `Set` the return type
 * of the two callers rather than merely their answer: insertion order is
 * preserved, so the same object answers both "can the run reach this?" and "in
 * what order?".
 *
 * `within`, when given, confines the walk to one group of nodes - used for the
 * dead branches below, which are walked one weakly connected group at a time.
 */
function visitDepthFirst(
	successors: Map<string, string[]>,
	seeds: string[],
	visited: Set<string>,
	within?: Set<string>
): void {
	// An explicit stack rather than recursion: a saved macro's depth is the
	// user's business, and a long chain of nodes should not be able to overflow
	// the call stack of the thing that names them.
	const stack = [...seeds].reverse();
	while (stack.length > 0) {
		const id = stack.pop() as string;
		if (visited.has(id)) continue;
		if (within !== undefined && !within.has(id)) continue;
		visited.add(id);
		const next = successors.get(id) ?? [];
		// Pushed backwards so the first outgoing edge is the first one popped -
		// depth-first in handle order, exactly as the interpreter follows them.
		for (let i = next.length - 1; i >= 0; i--) stack.push(next[i]);
	}
}

/**
 * What the run can get to, walked exactly like `reachableFrom` in
 * `backend/execution.go`: forwards from the Start node, each node visited at
 * most once, and edges to ids that are not nodes of this flow ignored
 * (`shapeGraph` has already dropped those).
 *
 * "Exactly like" is asserted rather than hoped for. Both walks are run over the
 * shared fixtures in `backend/testdata/reachability` - see the README there -
 * by `nodeLabels.parity.test.ts` on this side and
 * `TestReachableFromAgreesWithTheSharedFixtures` on the Go side. If they ever
 * disagree the status panel starts describing a run that did not happen, naming
 * the wrong nodes as skipped, and those two tests are what catches it.
 *
 * The returned set is in visit order, so it doubles as the run's sequence.
 */
function walkForwards({ startId, successors }: FlowGraph): Set<string> {
	const reachable = new Set<string>();
	if (startId === undefined) return reachable;

	visitDepthFirst(successors, [startId], reachable);
	return reachable;
}

/**
 * The set of nodes a run of this flow would actually visit - the frontend's
 * answer to the question `reachableFrom` answers in Go.
 *
 * `buildNodeLabels` does not call this: it needs the shaped graph for its
 * dead-branch pass as well, so it shapes the flow once and walks it itself.
 * Both routes go through the same `shapeGraph` and `walkForwards`, so this
 * function cannot answer differently from the labelling - which is the point,
 * since it is what the parity test against the Go engine runs.
 */
export function reachableNodes(nodes: LabellableNode[], edges: LabellableEdge[]): Set<string> {
	return walkForwards(shapeGraph(nodes, edges));
}

/**
 * Names every node by its type - `Start`, `Delay`, `Mouse Click` - and records
 * its position in the order the run walks the graph. That position is never
 * displayed; it exists so a message listing several nodes can be sorted into
 * the order they would have run (see `inFlowOrder`). Two delays therefore read
 * alike in the panel: the canvas highlight and the id tooltip are what tell
 * them apart.
 *
 * The order is the order a single execution token visits nodes, because that
 * is now literally what a run is (`docs/control-flow-interpreter.md`):
 *
 * - The token starts on the Start node, which the run begins at unconditionally
 *   - so edges pointing *into* Start are ignored here, exactly as they are
 *   there.
 * - It leaves a node by its outgoing edges in ascending `sourceHandle` order,
 *   depth-first: the whole of the first branch runs before the second one
 *   starts. A Sequence node's `out-1`, `out-2`, `out-3` are that rule's reason
 *   for existing.
 * - A node is numbered the **first** time the token reaches it and skipped on
 *   every later arrival. That is what makes a cycle terminate, and it is why a
 *   node on two paths - which really does execute twice - is named once.
 *
 * There is no scheduling left in here. The previous version ordered by
 * longest-path Kahn because the engine's `canEnqueue` waited for every
 * prerequisite before releasing a node, so a join had to be numbered after all
 * of its inputs; the token has no such notion, and the ordering collapsed to
 * the walk itself.
 *
 * Nodes the run cannot reach still need a step, since they are named in the
 * skipped-nodes warning and that list is sorted by it. They continue the
 * sequence after everything reachable, one dead branch at a time: each weakly
 * connected group of unreachable-but-wired nodes is walked the same way, seeded
 * from the nodes nothing in the group points at, and canvas position (y, then
 * x, then id) decides which seed goes first. A dead branch has no token order
 * of its own - nothing runs it - so canvas order is the only sensible answer
 * there, and it is the tie-break nowhere else. Nodes in no edge at all are
 * last: the backend never reports them, and they are usually just something the
 * user has only dropped on the canvas so far.
 */
export function buildNodeLabels(
	nodes: LabellableNode[],
	edges: LabellableEdge[]
): Map<string, NodeLabel> {
	const graph = shapeGraph(nodes, edges);
	const { byId, successors, predecessors, wired } = graph;

	const positionOf = (id: string) => byId.get(id)?.position ?? { x: 0, y: 0 };
	const canvasOrder = (a: string, b: string) => {
		const pa = positionOf(a);
		const pb = positionOf(b);
		return pa.y - pb.y || pa.x - pb.x || a.localeCompare(b);
	};

	// In visit order, which is the run's sequence.
	const reachable = walkForwards(graph);
	const order: string[] = [...reachable];

	/**
	 * One dead branch, walked like the run would have walked it had anything
	 * started it. The seeds are the group's own roots - members nothing else in
	 * the group points at - taken top to bottom; a group that is nothing but a
	 * cycle has no roots, so whatever the walk did not reach is picked up
	 * afterwards, also top to bottom.
	 */
	const orderGroup = (members: Set<string>): string[] => {
		const inCanvasOrder = [...members].sort(canvasOrder);
		const roots = inCanvasOrder.filter(
			(id) => !(predecessors.get(id) ?? []).some((pred) => members.has(pred))
		);

		const visited = new Set<string>();
		visitDepthFirst(successors, roots, visited, members);
		visitDepthFirst(successors, inCanvasOrder, visited, members);
		return [...visited];
	};

	// Dead branches, one weakly connected group at a time.
	const deadEnds = [...byId.keys()].filter((id) => !reachable.has(id) && wired.has(id));
	const grouped = new Set<string>();
	for (const seed of deadEnds.slice().sort(canvasOrder)) {
		if (grouped.has(seed)) continue;
		const group = new Set<string>([seed]);
		grouped.add(seed);
		const frontier = [seed];
		for (let i = 0; i < frontier.length; i++) {
			const current = frontier[i];
			const neighbours = [...(successors.get(current) ?? []), ...(predecessors.get(current) ?? [])];
			for (const neighbour of neighbours) {
				if (reachable.has(neighbour) || group.has(neighbour)) continue;
				group.add(neighbour);
				grouped.add(neighbour);
				frontier.push(neighbour);
			}
		}
		order.push(...orderGroup(group));
	}

	// Anything in no edge at all.
	order.push(
		...[...byId.keys()].filter((id) => !reachable.has(id) && !wired.has(id)).sort(canvasOrder)
	);

	const labels = new Map<string, NodeLabel>();
	order.forEach((id, index) => {
		// An unregistered type has no title of its own. Its registry key is poor
		// UI text, but it still tells the user far more than a raw id.
		const type = byId.get(id)?.type ?? '';
		const title = NODE_TYPE_TITLES[type] ?? (type || UNKNOWN_NODE_LABEL);
		// The step is still computed and kept, but deliberately not shown: it is
		// what puts multi-node messages into flow order below. Status messages
		// name a node by type alone.
		labels.set(id, { label: title, step: index + 1 });
	});
	return labels;
}

/**
 * Label for a node id reported by the backend. Falls back to an obviously
 * degraded placeholder rather than `undefined` if the id is not in the graph
 * this run started from (a node deleted mid-run, or an event left over from a
 * run this component did not start).
 *
 * `labels` is a parameter rather than a module-level map: it is the snapshot
 * `Flow.svelte` froze when the run started, and it belongs to that component's
 * state - see the thin wrapper there that closes over it.
 */
export function nodeLabel(labels: Map<string, NodeLabel>, nodeId: string): string {
	return labels.get(nodeId)?.label ?? UNKNOWN_NODE_LABEL;
}

/**
 * Puts a list of reported ids back into flow order, so a message listing
 * several nodes reads in the order the flow would have run them.
 *
 * Sorting the finished labels as text would not do: labels are node types, so
 * a graph with three delays yields three identical strings and text order says
 * nothing about when they run. Unknown ids go last, in a stable order of their
 * own.
 *
 * Takes the label map for the same reason `nodeLabel` does.
 */
export function inFlowOrder(labels: Map<string, NodeLabel>, nodeIds: string[]): string[] {
	const step = (id: string) => labels.get(id)?.step ?? Number.MAX_SAFE_INTEGER;
	return nodeIds.slice().sort((a, b) => step(a) - step(b) || a.localeCompare(b));
}
