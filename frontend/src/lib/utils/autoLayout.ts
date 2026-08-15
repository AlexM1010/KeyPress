// flow Utils
import dagre from "@dagrejs/dagre";
import {
    type Node,
    type Edge,
} from "@xyflow/svelte";
import "@xyflow/svelte/dist/style.css";
import { graph, type FlowNode } from "$lib/stores/flow.svelte";

/**
 * Size to reserve for a node whose real size cannot be read yet - one that has
 * just been dropped and not laid out by the browser, or a graph laid out before
 * the canvas has mounted.
 *
 * The width matches the `300px` every node is pinned to in `NodeWrapper`. The
 * height is a deliberate over-estimate of an expanded node: reserving too much
 * space leaves a gap, reserving too little overlaps the neighbour, so the
 * fallback errs upwards.
 */
const FALLBACK_NODE_WIDTH = 300;
const FALLBACK_NODE_HEIGHT = 260;

/**
 * Gaps dagre leaves around each node, in canvas units.
 *
 * `ranksep` runs along the flow (so, horizontally for `LR`) and has to clear
 * the edge's delete button, which sits at the midpoint between two nodes.
 * `nodesep` separates siblings across the flow.
 */
const RANK_SEPARATION = 140;
const NODE_SEPARATION = 80;
const GRAPH_MARGIN = 48;

/**
 * The size a node actually occupies on the canvas.
 *
 * Svelte Flow measures every rendered node and writes the result back onto the
 * very object held in `graph.nodes` (`measured`), so that is the first and best
 * source. It is absent until the node has been through a render, hence the two
 * fallbacks: the node's own DOM box - `offsetWidth`/`offsetHeight`, which are
 * layout sizes and so unaffected by the canvas zoom transform or the hover
 * `scale()` - and finally the constants above.
 *
 * Getting this right is the whole game: dagre packs nodes to the size it is
 * told, so a size smaller than reality is not a loose layout, it is an
 * overlapping one.
 */
function getNodeSize(node: Node): { width: number; height: number } {
    const measured = node.measured;
    if (measured?.width && measured?.height) {
        return { width: measured.width, height: measured.height };
    }

    if (typeof document !== "undefined") {
        const el = document.querySelector<HTMLElement>(
            `.svelte-flow__node[data-id="${CSS.escape(node.id)}"]`,
        );
        if (el?.offsetWidth && el?.offsetHeight) {
            return { width: el.offsetWidth, height: el.offsetHeight };
        }
    }

    return {
        width: node.width ?? FALLBACK_NODE_WIDTH,
        height: node.height ?? FALLBACK_NODE_HEIGHT,
    };
}

// Auto rearrange functions
function getLayoutedElements(
    nodes: Node[],
    edges: Edge[],
    direction = "LR",
) {
    // A fresh graph per run. A module-level graph is never emptied, so nodes
    // and edges from earlier layouts pile up in it and keep reserving rank and
    // spacing for nodes the user has since deleted.
    const dagreGraph = new dagre.graphlib.Graph();
    dagreGraph.setDefaultEdgeLabel(() => ({}));
    dagreGraph.setGraph({
        rankdir: direction,
        ranksep: RANK_SEPARATION,
        nodesep: NODE_SEPARATION,
        // Keeps parallel edges between the same pair of nodes from being drawn
        // on top of each other.
        edgesep: 32,
        marginx: GRAPH_MARGIN,
        marginy: GRAPH_MARGIN,
        // Straightens long chains, which is what a macro mostly is: a run of
        // steps that should sit on one line rather than zig-zag.
        ranker: "network-simplex",
        align: "UL",
    });

    const sizes = new Map<string, { width: number; height: number }>();

    nodes.forEach((node) => {
        const size = getNodeSize(node);
        sizes.set(node.id, size);
        dagreGraph.setNode(node.id, size);
    });

    edges.forEach((edge) => {
        // An edge left over from a deleted node would make dagre lay out a
        // phantom, pushing real nodes aside to make room for nothing.
        if (dagreGraph.hasNode(edge.source) && dagreGraph.hasNode(edge.target)) {
            dagreGraph.setEdge(edge.source, edge.target);
        }
    });

    dagre.layout(dagreGraph);

    // New node objects rather than mutation. Deep `$state` would notice either
    // way now, so this is no longer about being seen; it is about not editing
    // the caller's nodes in a function that may yet decide to place them
    // elsewhere. `data` is spread through by reference on purpose: it is the
    // very `$state` proxy the node component writes its edits into, and copying
    // it here would cut those edits off from the graph.
    const layoutedNodes = nodes.map((node) => {
        const positioned = dagreGraph.node(node.id);
        if (!positioned) return node;

        const size = sizes.get(node.id) ?? {
            width: FALLBACK_NODE_WIDTH,
            height: FALLBACK_NODE_HEIGHT,
        };

        return {
            ...node,
            // Dagre anchors a node at its centre, Svelte Flow at its top-left.
            // This has to subtract *that node's* half-size, not a shared
            // constant, or nodes of differing heights end up misaligned
            // against the edges drawn between them.
            position: {
                x: positioned.x - size.width / 2,
                y: positioned.y - size.height / 2,
            },
        };
    });

    return { nodes: layoutedNodes, edges };
}

/**
 * Stacks the nodes that take no part in the graph in a column clear of it.
 *
 * Left to dagre these become their own components, which it lays out one under
 * the other - so a handful of stray nodes push the macro itself far up the
 * canvas and out of view. Placing them in a column to the right of everything
 * else keeps the flow the user actually cares about where they left it.
 */
function placeIsolatedNodes(laidOut: Node[], isolated: Node[]): Node[] {
    if (isolated.length === 0) return [];

    let columnX = GRAPH_MARGIN;
    let y = GRAPH_MARGIN;

    if (laidOut.length > 0) {
        let right = -Infinity;
        let top = Infinity;
        laidOut.forEach((node) => {
            const { width } = getNodeSize(node);
            right = Math.max(right, node.position.x + width);
            top = Math.min(top, node.position.y);
        });
        columnX = right + RANK_SEPARATION;
        y = top;
    }

    return isolated.map((node) => {
        const positioned = { ...node, position: { x: columnX, y } };
        y += getNodeSize(node).height + NODE_SEPARATION;
        return positioned;
    });
}

/**
 * Rearranges the workspace graph in place.
 *
 * Defaults to `LR`. Every node declares a `left` target handle and a `right`
 * source handle, so a left-to-right rank order is the one where an edge leaves
 * one node's right edge and arrives at the next node's left edge; `TB` puts
 * connected nodes above one another with their edges looping around the sides.
 *
 * Only the connected nodes are handed to dagre; the rest are parked beside the
 * result by `placeIsolatedNodes`.
 */
function onLayout(direction: string = "LR") {
    // Read straight off the graph rather than through `get(store)`: the nodes
    // and edges are `$state` fields on an exported object now, so a property
    // read *is* the current value. This module has no reactive machinery of its
    // own and needs none - `onLayout` is called from a click, does its work
    // once, and assigns the result back.
    const nodesValue = graph.nodes as Node[];
    const edgesValue = graph.edges;

    const nodeIds = new Set(nodesValue.map((node) => node.id));
    // An edge whose endpoints are not both on the canvas connects nothing, and
    // is dropped by `getLayoutedElements` too.
    const connectedIds = new Set<string>();
    edgesValue.forEach((edge) => {
        if (nodeIds.has(edge.source) && nodeIds.has(edge.target)) {
            connectedIds.add(edge.source);
            connectedIds.add(edge.target);
        }
    });

    const connected = nodesValue.filter((node) => connectedIds.has(node.id));
    const isolated = nodesValue.filter((node) => !connectedIds.has(node.id));

    const layoutedElements = getLayoutedElements(connected, edgesValue, direction);
    const placed = new Map<string, Node>();
    layoutedElements.nodes.forEach((node) => placed.set(node.id, node));
    placeIsolatedNodes(layoutedElements.nodes, isolated).forEach((node) =>
        placed.set(node.id, node),
    );

    // Rebuilt in the graph's own order: Svelte Flow paints nodes in array
    // order, so reordering them here would silently restack the canvas.
    const nextNodes = nodesValue.map((node) => placed.get(node.id) ?? node);

    graph.nodes = nextNodes as FlowNode[];
    graph.edges = layoutedElements.edges;
}

export { onLayout };
