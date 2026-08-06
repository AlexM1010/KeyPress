<!-- frontend\src\routes\workspace\Flow.svelte -->
<script lang="ts">
  // Import necessary components and utilities from @xyflow/svelte
  import {
    SvelteFlow,
    Controls,
    Background,
    Panel,
    MiniMap,
    ConnectionMode,
    useSvelteFlow,
  } from "@xyflow/svelte";
  import type { DefaultEdgeOptions } from "@xyflow/svelte";

  // Import custom nodes, edges, and utilities
  import {
    nodesData,
    edgesData,
    macroName,
    macroID,
    isWorkspaceHydrated,
    markWorkspaceHydrated,
    openMacroInWorkspace,
    type FlowNode,
  } from "$lib/stores/flow";
  import { onLayout } from "$lib/utils/autoLayout";
  import { describeError } from "$lib/utils/helpers";

  // Generated Wails bindings for the Go backend
  import {
    LoadLastFile,
    SaveFile,
    StartExecution,
  } from "$lib/wailsjs/go/backend/App";
  import { backend } from "$lib/wailsjs/go/models";

  // Nodes
  import { nodeTypes } from "$lib/components/Workspace/customNodes/nodeTypes";
  //Edges
  import CustomEdge from "./CustomEdge.svelte";
  import ConnectionLine from "./ConnectionLine.svelte";

  import { onMount } from "svelte";
  //Flow
  import { flowTheme } from "$lib/stores/theme";
  import "$lib/index.scss";
  import "./FlowStyle.css";
  import { isExpanded } from '$lib/stores/navbar';

  // Import icons from Lucide Svelte
  import {
    Check,
    Save,
    X,
    Play,
    Loader,
    TriangleAlert,
    LayoutDashboard,
  } from "lucide-svelte";

  import LeftPanel from './flowpanels/LeftPanel.svelte';
  import LeftPanelToggleButton from './flowpanels/LeftPanelToggleButton.svelte';
  import StatusPanel from './flowpanels/StatusPanel.svelte';
  import StatusPanelToggleButton from './flowpanels/StatusPanelToggleButton.svelte';

  $: expandedClass = $isExpanded ? 'expanded' : '';

  // NOTE: node edits do not travel back up through Svelte events. `<SvelteFlow>`
  // instantiates the custom node components itself from `nodeTypes`, so they are not
  // children of this component and a `createEventDispatcher` event from one of them
  // has no path to a listener here. Instead, Svelte Flow hands each node component
  // the very `data` object held in the `nodesData` store, and the components mutate
  // that payload in place - so `toObject()` below already sees the user's edits.

  // Reactive statement to sync color mode with flow theme
  $: colorMode = $flowTheme;

  // Destructure helper functions from useSvelteFlow
  const { toObject, screenToFlowPosition } = useSvelteFlow();

  // State variables
  let isStatusPanelExpanded = false;
  let isExecuting = false;
  let isLeftPanelExpanded = true;

  // Toggle the status panel expansion
  function toggleStatusPanel() {
    isStatusPanelExpanded = !isStatusPanelExpanded;
  }

  // Toggle the left panel expansion
  function toggleLeftPanel() {
    isLeftPanelExpanded = !isLeftPanelExpanded;
  }

  // Handle drag over event to allow dropping nodes onto the flow
  const onDragOver = (event: DragEvent) => {
    event.preventDefault();
    event.dataTransfer && (event.dataTransfer.dropEffect = "move");
  };

  // Handle drop event to add new nodes to the flow
  const onDrop = (event: DragEvent) => {
    event.preventDefault();
    const type = event.dataTransfer?.getData("application/svelteflow");
    if (!type) return;

    const position = screenToFlowPosition({
      x: event.clientX,
      y: event.clientY,
    });

    // The payload starts empty on purpose. Each node component backfills its
    // own defaults into the object Svelte Flow hands it, and none of them
    // renders a `label` - every node takes its title from its own props - so
    // the `${type} node` string this used to stamp on was never displayed and
    // only ended up in every save file.
    const newNode: FlowNode = {
      id: `${Math.random()}`,
      type,
      position,
      data: {},
    };

    $nodesData = [...$nodesData, newNode];
  };

  // Status messages
  //
  // `nodeId` is the raw node id the backend reported the event for. It is kept
  // out of `message` on purpose - node ids are random decimals and mean nothing
  // to a user - but carried alongside it so the panel can still surface it for
  // debugging (see StatusPanel's tooltip).
  let statusMessages: {
    id: string;
    type: string;
    message: string;
    nodeId?: string;
  }[] = [];
  let isSuccess = false;

  /**
   * Human-readable name for each node type, keyed by the registry key from
   * `customNodes/nodeTypes.ts` - the string that ends up in a saved node's
   * `type` and that the Go dispatcher in `tasks.go` switches on.
   *
   * The values are copies of the node components' own `title` prop defaults
   * (`StartNode.svelte`, `DelayNode.svelte`, `MouseClickNode.svelte`,
   * `MouseMoveNode.svelte`, `ColorPickerNode.svelte`, `KeyPressNode.svelte`),
   * so the status panel calls a node exactly what the node's own header calls
   * it on the canvas. Those components are the source of truth: when one of
   * them renames itself, this table follows. A type missing from here still
   * gets a label - see `buildNodeLabels`.
   */
  const NODE_TYPE_TITLES: Record<string, string> = {
    StartNode: "Start",
    DelayNode: "Delay",
    MouseClickNode: "Mouse Click",
    MouseMoveNode: "Mouse Move",
    ColorPickerNode: "Wait For Color",
    KeyPressNode: "Keypress",
  };

  /** Shown when a reported node is not in the graph the run was started from. */
  const UNKNOWN_NODE_LABEL = "Unknown node";

  // Skipped-node highlight
  // ----------------------
  // Ids the backend reported as connected but unreachable from Start on the
  // most recent run. Held on their own, deliberately NOT on the nodes: Svelte
  // Flow's `toObject()` shallow-copies the very objects in the `nodesData`
  // store, so a `class` (or any other marker) written onto a node would be
  // handed straight to `SaveFile` and live in the user's macro JSON forever.
  // The same rule that forbids it is the by-reference data contract - node
  // components mutate their `data` payload in place, so `data` is the user's,
  // not a place to park display state.
  //
  // Instead the ids are turned into a stylesheet keyed on the `data-id`
  // attribute Svelte Flow already renders on every `.svelte-flow__node`
  // wrapper (NodeWrapper.svelte: `data-id={id}`), which marks exactly the
  // reported nodes without the graph data being touched at all.
  let skippedNodeIds: string[] = [];

  /**
   * Node ids the app generates are `Math.random()` decimals, but ids also
   * arrive off disk from saved macros, so nothing guarantees their shape. Only
   * ids built from characters that are inert inside an attribute-selector
   * string become selectors; anything else is dropped, so the generated
   * stylesheet can never be anything but the CSS intended here. A dropped id
   * merely loses its glow - the status message still names it.
   */
  const SELECTOR_SAFE_NODE_ID = /^[A-Za-z0-9._:-]+$/;

  /**
   * CSS marking the skipped nodes, or "" when there are none. It is the *body*
   * of a stylesheet: the `<style>` element that carries it is in the markup
   * below, because a literal `<style>` string in this block would be mistaken
   * for the component's own style block by the preprocessor.
   *
   * Scoped to `.flow-container` rather than left bare because a stylesheet in
   * `<head>` is global: confining it to a canvas keeps it from reaching any
   * other element that happens to carry a matching `data-id`.
   *
   * `--node-skipped-glow` is consumed by the `.svelte-flow__node` rule in
   * FlowStyle.css, which owns what the mark looks like.
   */
  function buildSkippedGlowCss(nodeIds: string[]): string {
    const selectors = nodeIds
      .filter((id) => SELECTOR_SAFE_NODE_ID.test(id))
      .map((id) => `.flow-container .svelte-flow__node[data-id="${id}"]`);
    if (selectors.length === 0) return "";
    return (
      `${selectors.join(",")}{--node-skipped-glow:` +
      `0 0 0 2px var(--skipped-glow),0 0 22px 6px var(--skipped-glow-soft)}`
    );
  }

  $: skippedGlowCss = buildSkippedGlowCss(skippedNodeIds);

  /**
   * The subset of a node this file needs to label it. Deliberately structural
   * rather than `FlowNode`: `toObject()` hands back Svelte Flow's own `Node`,
   * whose `type` is optional.
   */
  type LabellableNode = {
    id: string;
    type?: string;
    position: { x: number; y: number };
  };

  /**
   * The subset of an edge this file needs. Structural for the same reason:
   * `toObject()` returns Svelte Flow's `Edge`, and only the direction matters
   * here.
   */
  type LabellableEdge = {
    source: string;
    target: string;
  };

  /**
   * What a node is called on screen, plus its place in the sequence.
   *
   * `step` is the number in the label, kept separately so lists of reported
   * ids can be put back into flow order without parsing the text.
   */
  type NodeLabel = {
    label: string;
    step: number;
  };

  /**
   * id -> label, frozen for the duration of one run.
   *
   * Built from the very graph snapshot that is handed to the backend, so every
   * task the backend can report on has an entry and no node changes label
   * half-way through a run.
   */
  let nodeLabels = new Map<string, NodeLabel>();

  /**
   * Names every node by its type - `Start`, `Delay`, `Mouse Click` - and records
   * its position in the order the run walks the graph. That position is never
   * displayed; it exists so a message listing several nodes can be sorted into
   * the order they would have run (see `inFlowOrder`). Two delays therefore read
   * alike in the panel: the canvas highlight and the id tooltip are what tell
   * them apart.
   *
   * The order is built to agree with the engine (`execution.go`) rather than to
   * merely look plausible:
   *
   * - Edges are directed `source -> target`, and the run starts at the Start
   *   node, which `StartExecution` enqueues unconditionally - so edges pointing
   *   *into* Start are ignored here, exactly as they are there.
   * - A node's prerequisites are its predecessors that the run can also reach;
   *   `StartExecution` prunes edges out of dead branches for the same reason,
   *   which is why prerequisites are counted within a group rather than
   *   globally.
   * - `canEnqueue` waits for *every* prerequisite, so a join must be ordered
   *   after all of its inputs. Plain breadth-first would place it as soon as
   *   the first input was seen, so depth here is longest-path: a node sits one
   *   past the deepest node feeding it. Nodes that come ready together - the
   *   branches out of one node - are then ordered top to bottom by canvas
   *   position (y, then x, then id, purely so it is deterministic).
   *
   * Cycles: the depth pass is Kahn's algorithm, which only ever releases a node
   * once its last prerequisite has been released, so a node inside a cycle is
   * never released and the walk terminates instead of spinning. Whatever is
   * left over at the end is precisely the cyclic part - the engine would report
   * it as a stall - and it is appended in canvas order so it is still named.
   *
   * Nodes the run cannot reach still need a step, since they are named in the
   * skipped-nodes warning and that list is sorted by it. They continue the
   * sequence after everything reachable, one dead branch at a time:
   * each weakly connected group of unreachable-but-wired nodes is walked in the
   * same longest-path order, groups taken top to bottom by their highest node.
   * Nodes in no edge at all are last - the backend never reports them, and they
   * are usually just something the user has only dropped on the canvas so far.
   */
  function buildNodeLabels(
    nodes: LabellableNode[],
    edges: LabellableEdge[]
  ): Map<string, NodeLabel> {
    const byId = new Map<string, LabellableNode>();
    for (const node of nodes) byId.set(node.id, node);

    const startId = nodes.find((node) => node.type === "StartNode")?.id;

    // Adjacency over the edges that actually join two nodes of this graph.
    // `wired` records participation in an edge before anything is dropped, so
    // it matches the backend's own "connected" test in `warnAboutSkippedNodes`.
    const successors = new Map<string, string[]>();
    const predecessors = new Map<string, string[]>();
    const wired = new Set<string>();
    const link = (map: Map<string, string[]>, from: string, to: string) => {
      const list = map.get(from);
      if (list) list.push(to);
      else map.set(from, [to]);
    };
    for (const edge of edges) {
      if (!byId.has(edge.source) || !byId.has(edge.target)) continue;
      wired.add(edge.source);
      wired.add(edge.target);
      // The Start node runs regardless of what points at it.
      if (edge.target === startId) continue;
      link(successors, edge.source, edge.target);
      link(predecessors, edge.target, edge.source);
    }

    const positionOf = (id: string) => byId.get(id)?.position ?? { x: 0, y: 0 };
    const canvasOrder = (a: string, b: string) => {
      const pa = positionOf(a);
      const pb = positionOf(b);
      return pa.y - pb.y || pa.x - pb.x || a.localeCompare(b);
    };

    // What the run can get to, walked exactly like `reachableFrom` in Go.
    const reachable = new Set<string>();
    if (startId !== undefined) {
      reachable.add(startId);
      const queue = [startId];
      for (let i = 0; i < queue.length; i++) {
        for (const next of successors.get(queue[i]) ?? []) {
          if (reachable.has(next)) continue;
          reachable.add(next);
          queue.push(next);
        }
      }
    }

    /**
     * One set of nodes in longest-path order, prerequisites counted only within
     * the set. Anything the walk cannot release (a cycle, or a node behind one)
     * is appended in canvas order rather than dropped or looped over.
     */
    const orderGroup = (members: Set<string>): string[] => {
      const waiting = new Map<string, number>();
      for (const id of members) {
        let count = 0;
        for (const pred of predecessors.get(id) ?? []) {
          if (members.has(pred)) count += 1;
        }
        waiting.set(id, count);
      }

      const depth = new Map<string, number>();
      const released: string[] = [];
      for (const id of members) {
        if (waiting.get(id) === 0) {
          depth.set(id, 0);
          released.push(id);
        }
      }
      for (let i = 0; i < released.length; i++) {
        const current = released[i];
        const nextDepth = (depth.get(current) ?? 0) + 1;
        for (const next of successors.get(current) ?? []) {
          if (!members.has(next)) continue;
          const best = depth.get(next);
          if (best === undefined || best < nextDepth) depth.set(next, nextDepth);
          const left = (waiting.get(next) ?? 0) - 1;
          waiting.set(next, left);
          if (left === 0) released.push(next);
        }
      }

      const ordered = released
        .slice()
        .sort(
          (a, b) => (depth.get(a) ?? 0) - (depth.get(b) ?? 0) || canvasOrder(a, b)
        );
      const releasedSet = new Set(released);
      const stuck = [...members]
        .filter((id) => !releasedSet.has(id))
        .sort(canvasOrder);
      return [...ordered, ...stuck];
    };

    const order: string[] = [];
    if (reachable.size > 0) order.push(...orderGroup(reachable));

    // Dead branches, one weakly connected group at a time.
    const deadEnds = [...byId.keys()].filter(
      (id) => !reachable.has(id) && wired.has(id)
    );
    const grouped = new Set<string>();
    for (const seed of deadEnds.slice().sort(canvasOrder)) {
      if (grouped.has(seed)) continue;
      const group = new Set<string>([seed]);
      grouped.add(seed);
      const frontier = [seed];
      for (let i = 0; i < frontier.length; i++) {
        const current = frontier[i];
        const neighbours = [
          ...(successors.get(current) ?? []),
          ...(predecessors.get(current) ?? []),
        ];
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
      ...[...byId.keys()]
        .filter((id) => !reachable.has(id) && !wired.has(id))
        .sort(canvasOrder)
    );

    const labels = new Map<string, NodeLabel>();
    order.forEach((id, index) => {
      // An unregistered type has no title of its own. Its registry key is poor
      // UI text, but it still tells the user far more than a random decimal.
      const type = byId.get(id)?.type ?? "";
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
   */
  function nodeLabel(nodeId: string): string {
    return nodeLabels.get(nodeId)?.label ?? UNKNOWN_NODE_LABEL;
  }

  /**
   * Puts a list of reported ids back into flow order, so a message listing
   * several nodes reads in the order the flow would have run them.
   *
   * Sorting the finished labels as text would not do: labels are node types, so
   * a graph with three delays yields three identical strings and text order says
   * nothing about when they run. Unknown ids go last, in a stable order of their
   * own.
   */
  function inFlowOrder(nodeIds: string[]): string[] {
    const step = (id: string) => nodeLabels.get(id)?.step ?? Number.MAX_SAFE_INTEGER;
    return nodeIds.slice().sort((a, b) => step(a) - step(b) || a.localeCompare(b));
  }

  // Computed property to determine the current execution status and icon
  $: executionStatus = (() => {
    const hasError = statusMessages.some((msg) => msg.type === "error");
    const hasWarning = statusMessages.some((msg) => msg.type === "warning");
    const hasSuccess = statusMessages.some(
      (msg) =>
        msg.type === "success" && msg.message.includes("Flow execution completed")
    );

    if (isExecuting) return { icon: Loader, color: "text-blue-500" };
    if (hasError) return { icon: X, color: "text-red-500" };
    if (hasWarning) return { icon: TriangleAlert, color: "text-yellow-500" };
    if (hasSuccess) return { icon: Play, color: "text-green-500" };
    return { icon: Play, color: "text-foreground" };
  })();

  // Custom edge types
  const edgeTypes = {
    customedge: CustomEdge,
  };

  // Default options for edges
  export const defaultEdgeOptions: DefaultEdgeOptions = {
    type: "customedge",
    animated: true,
    deletable: true,
    selectable: true,
    data: { color: "var(--main-text)" },
    interactionWidth: 20,
  };

  // Function to handle running the entire flow
  async function handleRunFlow() {
    try {
      isExecuting = true;
      isSuccess = false;
      statusMessages = [];
      // Last run's marks go with last run's messages: the canvas must never
      // show a glow that the status panel no longer explains.
      skippedNodeIds = [];

      // Get the current flow data as an object
      const currentFlowData = toObject();

      // Check if the flow contains a Start node
      const hasStartNode = currentFlowData.nodes.some(
        (node) => node.type === "StartNode"
      );
      if (!hasStartNode) {
        alert("Flowchart must contain a Start node.");
        isExecuting = false;
        return;
      }

      // Label every node before the backend can report on any of them: this is
      // the same snapshot that is about to be sent, so the labels match the run
      // exactly and stay fixed even if the user edits the canvas mid-run.
      nodeLabels = buildNodeLabels(currentFlowData.nodes, currentFlowData.edges);

      // Start execution via the Go backend
      const response = await StartExecution(JSON.stringify(currentFlowData));

      console.log("Flow execution started:", response);
      addStatusMessage({
        id: `exec-${Date.now()}`,
        type: "info",
        message: "Flow execution started.",
      });
    } catch (error) {
      console.error("Failed to run flow:", error);
      isExecuting = false;
      addStatusMessage({
        id: `error-${Date.now()}`,
        type: "error",
        message: "Failed to run flow. Check the console for details.",
      });
    }
  }

  // Function to handle saving the flow
  type SaveState = 
    | { status: 'idle' }
    | { status: 'saving' }
    | { status: 'success' }
    | { status: 'error', message: string };

  let saveState: SaveState = { status: 'idle' };

  // The macro's name and id live in `$lib/stores/flow` rather than here, because
  // the macro list can now open a macro straight into the workspace and its
  // identity has to arrive with its graph. See `openMacroInWorkspace`.

  // Clear a stale rejection as soon as the user starts fixing the name it was
  // about, so the message on screen always refers to what is in the field.
  function clearSaveError() {
    if (saveState.status === 'error') saveState = { status: 'idle' };
  }

  async function handleSave() {
    try {
      saveState = { status: 'saving' };
      const currentFlowData = toObject();
      const savedID = await SaveFile(
        backend.FlowData.createFrom(currentFlowData),
        $macroName.trim(),
        $macroID
      );
      // Now editing whatever we just wrote: saving again overwrites it instead
      // of colliding with it, and a rename lands on the new file.
      $macroID = savedID;
      saveState = { status: 'success' };
      setTimeout(() => {
        if (saveState.status === 'success') saveState = { status: 'idle' };
      }, 3000);
    } catch (error) {
      const errorMessage = describeError(error);
      // Two surfaces on purpose: the status panel is the established home for
      // backend failures, but it is collapsed by default, and a rejected save
      // the user cannot see is the bug this whole rule set exists to avoid. The
      // toolbar message sits next to the field that caused it and stays until
      // the name changes or the next attempt.
      saveState = { status: 'error', message: errorMessage };
      addStatusMessage({
        id: `save-error-${Date.now()}`,
        type: "error",
        message: "Failed to save flow: " + errorMessage
      });
    }
  }

  // Computed property to determine if the status panel should be shown
  $: hasStatusPanel = isStatusPanelExpanded || statusMessages.length > 0;

  /**
   * Appends a status message. Messages are not expired on a timer: the record
   * of a run has to still be there when the user looks up from the canvas, and
   * a message that erases itself after ten seconds is exactly the failure the
   * panel exists to prevent. The list is emptied by `handleRunFlow`, which is
   * the only thing that starts a run - so what the panel shows is always the
   * whole of the current run and nothing from the last one.
   */
  function addStatusMessage(msg: {
    id: string;
    type: string;
    message: string;
    nodeId?: string;
  }) {
    statusMessages = [...statusMessages, msg];
  }

  // Set up event listeners
  function setupEventListeners() {
    // The backend reports tasks by node id, which is a random decimal and
    // meaningless on screen. Every message below names the node the way the
    // canvas does and keeps the id in `nodeId` for the panel's tooltip.
    window.runtime.EventsOn("task-started", (taskId: string) => {
      addStatusMessage({
        id: `task-started-${taskId}`,
        type: "info",
        message: `${nodeLabel(taskId)} started.`,
        nodeId: taskId,
      });
    });

    window.runtime.EventsOn("task-completed", (taskId: string) => {
      addStatusMessage({
        id: `task-completed-${taskId}`,
        type: "success",
        message: `${nodeLabel(taskId)} completed successfully.`,
        nodeId: taskId,
      });
    });

    window.runtime.EventsOn(
      "task-error",
      (payload: { taskID: string; error: string }) => {
        isExecuting = false;
        addStatusMessage({
          id: `task-error-${payload.taskID}`,
          type: "error",
          message: `${nodeLabel(payload.taskID)} failed: ${payload.error}`,
          nodeId: payload.taskID,
        });
      }
    );

    window.runtime.EventsOn("execution-error", (errorMsg: string) => {
      isExecuting = false;
      addStatusMessage({
        id: `exec-error-${Date.now()}`,
        type: "error",
        message: `Flow execution error: ${errorMsg}`,
      });
    });

    window.runtime.EventsOn("execution-stopped", () => {
      isExecuting = false;
      addStatusMessage({
        id: `exec-stopped-${Date.now()}`,
        type: "warning",
        message: "Flow execution was stopped.",
      });
    });

    // Nodes the backend left out of the run because nothing connects them to
    // the Start node. They sit on the canvas looking live, so say so rather
    // than let the user wonder why that branch did nothing.
    //
    // They are named here *and* marked on the canvas: the names say which
    // nodes without the user leaving the panel, the orange glow says where
    // without the user hunting for them.
    window.runtime.EventsOn("execution-nodes-skipped", (nodeIds: string[]) => {
      skippedNodeIds = nodeIds;
      addStatusMessage({
        id: `exec-skipped-${Date.now()}`,
        type: "warning",
        message:
          `${nodeIds.length} ${nodeIds.length === 1 ? "node" : "nodes"} skipped` +
          ` - not reachable from Start: ${inFlowOrder(nodeIds).map(nodeLabel).join(", ")}`,
      });
    });

    // The run reached a point where nothing was left running and nothing more
    // could ever start - a loop in the connections, in practice.
    window.runtime.EventsOn("execution-stalled", (nodeIds: string[]) => {
      isExecuting = false;
      const stuck = inFlowOrder(nodeIds).map(nodeLabel);
      addStatusMessage({
        id: `exec-stalled-${Date.now()}`,
        type: "error",
        message:
          `Flow stopped - ${stuck.join(", ")} could never run.` +
          " Check the connections for a loop.",
      });
    });

    window.runtime.EventsOn("execution-completed", () => {
      isExecuting = false;
      isSuccess = true;
      addStatusMessage({
        id: `exec-completed-${Date.now()}`,
        type: "success",
        message: "Flow execution completed.",
      });
      // Reset success state after 3 seconds
      setTimeout(() => {
        isSuccess = false;
      }, 1000);
    });

    // Add save status listeners
    window.runtime.EventsOn("save-success", (message) => {
      addStatusMessage({
        id: `save-${Date.now()}`,
        type: "success",
        message: message
      });
    });

    window.runtime.EventsOn("save-error", (message) => {
      addStatusMessage({
        id: `save-error-${Date.now()}`,
        type: "error",
        message: message
      });
    });
  }
  
  // Load the last opened file when the component mounts
  async function loadLastOpenedFile() {
    try {
      const data = await LoadLastFile();
      if (!data) {
        // Nothing saved yet. The canvas keeps the defaults from flow.ts, and
        // they count as hydrated - coming back from the macro list must not
        // wipe out whatever the user has built on top of them since.
        markWorkspaceHydrated();
        return;
      }

      // A different graph is about to replace this one, so the previous graph's
      // skip marks go with it. Ids are per-graph and the highlight matches on
      // them, so a stale mark could not land on a node of the new macro anyway
      // - but leaving the list populated would keep a stylesheet in <head>
      // describing nodes that no longer exist.
      skippedNodeIds = [];

      // Adopts the graph, the name and the id, and marks the workspace
      // hydrated. `data` is a fresh parse from the backend and nothing else
      // holds a reference to it, which is what this call requires.
      openMacroInWorkspace(data);
    } catch (error) {
      console.error("Failed to load last file:", error);
      addStatusMessage({
        id: `load-error-${Date.now()}`,
        type: "error",
        message: "Failed to load last file: " + describeError(error)
      });
    }
  }

  // Initialize event listeners when the component mounts.
  //
  // The last opened macro is read once per app run, not once per mount. This is
  // a route, so every trip to the macro list and back remounts it, and reading
  // again here would throw away both the macro the user just opened from that
  // list and any unsaved edits they had made before leaving.
  onMount(() => {
    setupEventListeners();
    if (!isWorkspaceHydrated()) loadLastOpenedFile();
  });
</script>

<!-- The skipped-node marks. A stylesheet rather than a class on the nodes, so
     the graph data - and therefore every save file - stays exactly as the user
     left it; see `buildSkippedGlowCss`. Emptying `skippedNodeIds` empties the
     rule, and Svelte removes the element with the component.

     `<svelte:element>` rather than a written-out tag: a literal style element
     here would be taken for the component's own style block. Its content is a
     text node, never markup, so a node id cannot escape into the document. -->
<svelte:head>
  {#if skippedGlowCss}
    <svelte:element this="style">{skippedGlowCss}</svelte:element>
  {/if}
</svelte:head>

<div class="flow-container flex">
  <!-- Left Panel -->
  {#if isLeftPanelExpanded}
    <LeftPanel />
  {/if}

  <!-- Left Panel Toggle Button -->
  <LeftPanelToggleButton
    {isLeftPanelExpanded}
    {toggleLeftPanel}
  />

  <!-- Main Flow Area -->
  <div
    class="h-full flex-1 transition-all duration-300"
    class:pl-80={isLeftPanelExpanded}
    class:pl-0={!isLeftPanelExpanded}
    class:pr-80={isStatusPanelExpanded}
    class:pr-0={!isStatusPanelExpanded}
  >
    <SvelteFlow
      nodes={nodesData}
      {nodeTypes}
      edges={edgesData}
      {edgeTypes}
      {colorMode}
      connectionMode={ConnectionMode.Loose}
      defaultEdgeOptions={defaultEdgeOptions}
      on:dragover={onDragOver}
      on:drop={onDrop}
      deleteKey="Backspace"
      fitView
    >
      <!-- Custom connection line -->
      <ConnectionLine slot="connectionLine" />
      <!-- Control Panel -->
      <Panel position="top-right">
        <div class="flex flex-col items-end">
          <div class="nav-button-container flex-center flex-gap transition-transform duration-300 {expandedClass}">
            <!-- Macro name: decides which file on disk the save writes to.
                 Required, and unique across macros - a rejected save reports
                 why just below. -->
            <input
              class="flow-name-input"
              class:flow-name-input-invalid={saveState.status === 'error'}
              type="text"
              bind:value={$macroName}
              on:input={clearSaveError}
              placeholder="Macro name"
              aria-label="Macro name"
              aria-invalid={saveState.status === 'error'}
            />
            <!-- Run Flow Button -->
            <button
              class="flow-button"
              on:click={handleRunFlow}
              disabled={isExecuting}
            >
              <svelte:component
                this={executionStatus.icon}
                class="flow-icon {executionStatus.color}"
                style={isExecuting ? "animation: spin 1s linear infinite" : ""}
              />
            </button>
            <!-- Save Button -->
            <button
              class="flow-button"
              on:click={handleSave}
              disabled={saveState.status === 'saving'}
            >
              <svelte:component
                this={
                  saveState.status === 'saving' ? Loader :
                  saveState.status === 'error' ? X :
                  saveState.status === 'success' ? Check :
                  Save
                }
                class="flow-icon"
                style={saveState.status === 'saving' ? "animation: spin 1s linear infinite" : ""}
              />
            </button>
            <!-- Layout Button. `onLayout` rearranges the workspace stores
                 directly. -->
            <button
              class="flow-button"
              on:click={() => onLayout("TB")}
            >
              <LayoutDashboard class="flow-icon" />
              <span>Layout</span>
            </button>
          </div>
          {#if saveState.status === 'error'}
            <!-- Below the toolbar row, which already carries the navbar
                 offset, so this needs no `expanded` class of its own. -->
            <p class="flow-save-error" role="alert">
              {saveState.message}
            </p>
          {/if}
        </div>
      </Panel>
      <!-- Flow Controls -->
      <Controls />
      <MiniMap />
      <Background />
    </SvelteFlow>
  </div>

  <!-- Status Panel Toggle Button -->
  <StatusPanelToggleButton
    {isStatusPanelExpanded}
    {hasStatusPanel}
    {toggleStatusPanel}
  />

  <!-- Status Panel -->
  <StatusPanel
    {isStatusPanelExpanded}
    {statusMessages}
    {executionStatus}
  />
</div>