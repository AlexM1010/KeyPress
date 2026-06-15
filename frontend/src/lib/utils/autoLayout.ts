// flow Utils
import dagre from "@dagrejs/dagre";
import {
    Position,
    type Node,
    type Edge,
} from "@xyflow/svelte";
import "@xyflow/svelte/dist/style.css";
import { get } from "svelte/store";
import { nodesData, edgesData, type NodeData } from "$lib/stores/flow";

// Auto rearrange constants
const dagreGraph = new dagre.graphlib.Graph();
dagreGraph.setDefaultEdgeLabel(() => ({}));
const nodeWidth = 172;
const nodeHeight = 36;

// Auto rearrange functions
function getLayoutedElements(
    nodes: Node[],
    edges: Edge[],
    direction = "TB",
) {
    const isHorizontal = direction === "LR";
    dagreGraph.setGraph({ rankdir: direction });

    nodes.forEach((node) => {
        dagreGraph.setNode(node.id, {
            width: nodeWidth,
            height: nodeHeight,
        });
    });

    edges.forEach((edge) => {
        dagreGraph.setEdge(edge.source, edge.target);
    });

    dagre.layout(dagreGraph);

    nodes.forEach((node) => {
        const nodeWithPosition = dagreGraph.node(node.id);
        node.targetPosition = isHorizontal ? Position.Left : Position.Top;
        node.sourcePosition = isHorizontal
            ? Position.Right
            : Position.Bottom;

        // We are shifting the dagre node position (anchor=center center) to the top left
        // so it matches the React Flow node anchor point (top left).
        node.position = {
            x: nodeWithPosition.x - nodeWidth / 2,
            y: nodeWithPosition.y - nodeHeight / 2,
        };
    });

    return { nodes, edges };
}


function onLayout(direction: string) {
    const nodesValue = get(nodesData) as Node[];
    const edgesValue = get(edgesData);
    const layoutedElements = getLayoutedElements(nodesValue, edgesValue, direction);
    nodesData.set(layoutedElements.nodes as NodeData[]);
    edgesData.set(layoutedElements.edges);
}

export { onLayout };
