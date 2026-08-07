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
    isMacroDirty,
    getSavedSnapshot,
    markMacroSaved,
    serializeMacro,
    type FlowNode,
  } from "$lib/stores/flow";
  import { onLayout } from "$lib/utils/autoLayout";
  import { describeBackendError, isBackendUnreachable } from "$lib/utils/helpers";

  // Generated Wails bindings for the Go backend
  import { App } from "$lib/bindings/Keypress/backend";
  import { Events } from "@wailsio/runtime";

  // Nodes
  import { nodeTypes } from "$lib/components/Workspace/customNodes/nodeTypes";
  //Edges
  import CustomEdge from "./CustomEdge.svelte";
  import ConnectionLine from "./ConnectionLine.svelte";

  import { onDestroy, onMount, tick } from "svelte";
  //Flow
  import { flowTheme } from "$lib/stores/theme";
  import "$lib/index.scss";
  import "./FlowStyle.css";

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

  /**
   * The glow colour a node shows while it is running, keyed by the same node
   * type as `NODE_TYPE_TITLES`.
   *
   * Each value is the hue of that node's own header gradient - Start and Delay
   * are blue, the two mouse nodes green, Keypress orange, Wait For Color
   * indigo - so a running card lights up in its own colour rather than in one
   * shared highlight. The gradients themselves are Tailwind classes on the node
   * components (`export let color`), which is no use as a shadow colour, so the
   * hues are named here as the `--node-glow-*` custom properties `index.scss`
   * defines. As with the titles above, the node components are the source of
   * truth: when one changes colour, this table follows.
   */
  const NODE_TYPE_GLOWS: Record<string, string> = {
    StartNode: "--node-glow-blue",
    DelayNode: "--node-glow-blue",
    MouseClickNode: "--node-glow-green",
    MouseMoveNode: "--node-glow-green",
    ColorPickerNode: "--node-glow-indigo",
    KeyPressNode: "--node-glow-orange",
  };

  /** For a node type that is not in the table above. */
  const DEFAULT_NODE_GLOW = "--node-glow-neutral";

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

  // Active-node highlight
  // ---------------------
  // The nodes the backend currently has in flight, tracked the same way and for
  // the same reasons as the skipped ids above: a stylesheet keyed on `data-id`,
  // never a marker written onto the node objects, so the graph the user saves
  // is untouched.
  //
  // A list rather than a single id because the run is concurrent - the worker
  // pool in `taskqueue.go` runs several tasks at once, so two branches of a
  // flow really can be lit at the same time, and that is worth seeing.
  let activeNodeIds: string[] = [];

  /**
   * id -> the `--node-glow-*` property that node lights up in, frozen for the
   * duration of one run.
   *
   * Built from the same snapshot as `nodeLabels`, for the same reason: it is
   * the graph the backend is about to be given, so every node it can report on
   * has an entry and no node changes colour half-way through a run because the
   * canvas was edited underneath it.
   */
  let nodeGlows = new Map<string, string>();

  /**
   * CSS lighting up the nodes that are executing, or "" when none are.
   *
   * Shaped exactly like `buildSkippedGlowCss` - same `.flow-container` scope,
   * same id filter, same handing-off to FlowStyle.css through a custom property
   * - but grouped by colour rather than emitted per id, because most runs light
   * one or two nodes of a handful of colours and a selector list per colour is
   * the smaller stylesheet.
   *
   * The glow is a halo with no solid ring, which is what distinguishes it from
   * the skipped mark; see the rule in FlowStyle.css.
   *
   * `glows` is a parameter rather than a read of `nodeGlows`, so the reactive
   * statement below rebuilds the stylesheet when a new run recolours the graph
   * and not only when the set of lit ids changes.
   */
  function buildActiveGlowCss(
    nodeIds: string[],
    glows: Map<string, string>
  ): string {
    const byGlow = new Map<string, string[]>();
    for (const id of nodeIds) {
      if (!SELECTOR_SAFE_NODE_ID.test(id)) continue;
      const glow = glows.get(id) ?? DEFAULT_NODE_GLOW;
      const selectors = byGlow.get(glow);
      const selector = `.flow-container .svelte-flow__node[data-id="${id}"]`;
      if (selectors) selectors.push(selector);
      else byGlow.set(glow, [selector]);
    }

    let css = "";
    for (const [glow, selectors] of byGlow) {
      css +=
        `${selectors.join(",")}{--node-active-glow:` +
        `0 0 26px 8px var(${glow})}`;
    }
    return css;
  }

  $: activeGlowCss = buildActiveGlowCss(activeNodeIds, nodeGlows);

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

  $: hasError = statusMessages.some((msg) => msg.type === "error");
  $: hasWarning = statusMessages.some((msg) => msg.type === "warning");
  $: hasSuccess = statusMessages.some(
    (msg) =>
      msg.type === "success" && msg.message.includes("Flow execution completed")
  );

  // Computed property to determine the current execution status and icon
  $: executionStatus = (() => {
    if (isExecuting) return { icon: Loader, color: "text-blue-500" };
    if (hasError) return { icon: X, color: "text-red-500" };
    if (hasWarning) return { icon: TriangleAlert, color: "text-yellow-500" };
    if (hasSuccess) return { icon: Play, color: "text-green-500" };
    return { icon: Play, color: "text-foreground" };
  })();

  /**
   * How long the run button holds the alert glyph after a run that errored or
   * warned. Long enough to be read without watching for it, short enough that
   * the button goes back to looking like something you press.
   */
  const RUN_ALERT_HOLD_MS = 5000;

  // The button is a control first and a status light second, so it only borrows
  // the alert glyph for a moment. Nothing is lost by handing it back to Play:
  // the status panel keeps its own icon on the failure, and its messages stay
  // until the next run clears them.
  let isRunAlertHeld = false;
  let runAlertTimer: ReturnType<typeof setTimeout> | undefined;

  $: hasRunAlert = !isExecuting && (hasError || hasWarning);

  $: {
    clearTimeout(runAlertTimer);
    if (hasRunAlert) {
      isRunAlertHeld = true;
      runAlertTimer = setTimeout(() => (isRunAlertHeld = false), RUN_ALERT_HOLD_MS);
    } else {
      isRunAlertHeld = false;
    }
  }

  $: runButtonStatus =
    hasRunAlert && !isRunAlertHeld
      ? { icon: Play, color: "text-foreground" }
      : executionStatus;

  onDestroy(() => clearTimeout(runAlertTimer));

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
      // show a glow that the status panel no longer explains. The active glow
      // is cleared as well - a run that ended without a terminal event (the app
      // was left mid-run) must not leave a node looking like it is still going.
      skippedNodeIds = [];
      clearActiveNodes();

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

      // Label and colour every node before the backend can report on any of
      // them: this is the same snapshot that is about to be sent, so both match
      // the run exactly and stay fixed even if the user edits the canvas
      // mid-run.
      nodeLabels = buildNodeLabels(currentFlowData.nodes, currentFlowData.edges);
      nodeGlows = new Map<string, string>(
        currentFlowData.nodes.map((node): [string, string] => [
          node.id,
          NODE_TYPE_GLOWS[node.type ?? ""] ?? DEFAULT_NODE_GLOW,
        ])
      );

      // Start execution via the Go backend
      const response = await App.StartExecution(JSON.stringify(currentFlowData));

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

  /**
   * The name field, so a refused save can put the cursor back in the thing that
   * has to change for the next one to work.
   */
  let nameInput: HTMLInputElement | undefined;

  /**
   * Longest macro name the backend will store, mirrored here so the field stops
   * at the limit instead of letting the user type past it and be told afterwards.
   * Must match `maxMacroNameLen` in backend/persistence.go - the backend still
   * enforces it, this only saves the round trip.
   */
  const MAX_MACRO_NAME_LEN = 200;

  // The macro's name and id live in `$lib/stores/flow` rather than here, because
  // the macro list can now open a macro straight into the workspace and its
  // identity has to arrive with its graph. See `openMacroInWorkspace`.

  // Clear a stale rejection as soon as the user starts fixing the name it was
  // about, so the message on screen always refers to what is in the field.
  function clearSaveError() {
    if (saveState.status === 'error') saveState = { status: 'idle' };
  }

  /**
   * How long the save button holds its tick before going back to the save
   * glyph. The unsaved-changes dot is what carries the state afterwards.
   */
  const SAVE_SUCCESS_HOLD_MS = 3000;

  let saveSuccessTimer: ReturnType<typeof setTimeout> | undefined;

  /**
   * Serialises the macro on screen exactly as it would be written to disk, so
   * it can be compared with the last thing that was.
   */
  function currentSnapshot(): string {
    const { nodes, edges } = toObject();
    return serializeMacro($macroName, nodes, edges);
  }

  /**
   * Records the graph on screen as the one on disk.
   *
   * Deferred by a frame on purpose. Node components backfill their newer fields
   * into the `data` object the store holds as they mount, so a graph just read
   * off disk keeps changing for a moment after it is put on the canvas; a
   * baseline taken before that settles would have every freshly opened macro
   * claiming unsaved changes it does not have. `tick()` waits for Svelte Flow to
   * mount the nodes and the frame waits for their first render.
   */
  async function captureSavedSnapshot() {
    await tick();
    await new Promise((resolve) => requestAnimationFrame(() => resolve(null)));
    try {
      markMacroSaved(currentSnapshot());
    } catch (error) {
      // The route can be torn down inside that frame. Losing the baseline only
      // costs the unsaved-changes warning until the next mount takes one.
      console.warn("Could not record the saved state of the macro:", error);
    }
  }

  async function handleSave() {
    // Ctrl+S can fire while a save is already in flight, and the disabled
    // button does not stop it. Two saves at once would race over which id the
    // workspace ends up holding.
    if (saveState.status === 'saving') return;

    // The name is trimmed for the backend, so trim it here too: otherwise the
    // field keeps spaces that are not in the saved macro, and the snapshot
    // taken below would differ from the file the moment it is written.
    const name = $macroName.trim();
    $macroName = name;

    // Answered here rather than by the backend, which enforces the same rule and
    // keeps doing so. A name is the one thing the user can see is missing, the
    // field for it is right there, and going to the backend to be told costs a
    // round trip whose every other failure mode - not least the bindings not
    // being there at all - would be reported in place of the plain answer.
    if (name === '') {
      saveState = { status: 'error', message: 'Give the macro a name before saving it.' };
      nameInput?.focus();
      return;
    }

    try {
      saveState = { status: 'saving' };

      const currentFlowData = toObject();

      // Serialised here rather than after the call, and that ordering is the
      // whole point: `toObject()` shares each node's `data` object with the
      // canvas, so a node edited while the save was in flight would otherwise
      // be folded into the baseline and never reported unsaved again. Taken
      // now, this string is exactly the bytes the call below sends.
      const sentSnapshot = serializeMacro(name, currentFlowData.nodes, currentFlowData.edges);

      // v3's generated models are plain interfaces rather than v2's classes, so
      // there is no `createFrom` to run the canvas object through. Mapped field
      // by field rather than cast, because Svelte Flow's types really are looser
      // than the Go structs and a cast would compile while sending values Go
      // cannot take: an unused handle is `null` on an edge, and a node's `type`
      // is optional. Both are narrowed here, once, at the boundary.
      //
      // `data` and `position` stay by reference, as they were before: this runs
      // after `sentSnapshot` is taken, which is what makes that ordering matter.
      const savedID = await App.SaveFile(
        {
          nodes: currentFlowData.nodes.map((node) => ({
            id: node.id,
            type: node.type ?? "",
            data: node.data,
            position: node.position,
          })),
          edges: currentFlowData.edges.map((edge) => ({
            id: edge.id,
            source: edge.source,
            target: edge.target,
            sourceHandle: edge.sourceHandle ?? undefined,
            targetHandle: edge.targetHandle ?? undefined,
            type: edge.type,
          })),
        },
        name,
        $macroID
      );

      // Now editing whatever we just wrote: saving again overwrites it instead
      // of colliding with it, and a rename lands on the new file.
      $macroID = savedID;
      markMacroSaved(sentSnapshot);

      saveState = { status: 'success' };
      clearTimeout(saveSuccessTimer);
      saveSuccessTimer = setTimeout(() => {
        if (saveState.status === 'success') saveState = { status: 'idle' };
      }, SAVE_SUCCESS_HOLD_MS);
    } catch (error) {
      // `describeBackendError`, not `describeError`: a save that fails because
      // nothing is listening on the Go side throws a bare "Failed to fetch",
      // and putting that under the name field tells the user their macro's name
      // is wrong when it is not.
      const errorMessage = describeBackendError(error);
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
      // A refused save leaves the macro exactly as unsaved as it was, and when
      // the backend is what refused it, the name is what has to change. Not
      // when the backend never heard the request: pointing at the name field
      // would blame it for a failure it had no part in.
      if (!isBackendUnreachable(error)) nameInput?.focus();
    }
  }

  /**
   * Ctrl+S / Cmd+S, the shortcut every user of an editor already has in their
   * fingers. Bound on the window rather than the canvas so it works while the
   * name field or a node input has focus - those are exactly the moments a save
   * is worth reaching for.
   */
  function handleKeydown(event: KeyboardEvent) {
    if (event.key !== 's' && event.key !== 'S') return;
    if (!(event.ctrlKey || event.metaKey) || event.altKey) return;
    // The browser's own "save page" would otherwise fire as well.
    event.preventDefault();
    handleSave();
  }

  // Written out rather than inlined in the markup so the button's tooltip and
  // its accessible name cannot drift apart.
  $: saveTitle =
    saveState.status === 'saving'
      ? "Saving..."
      : $isMacroDirty
        ? "Save macro (Ctrl+S) - unsaved changes"
        : "Save macro (Ctrl+S)";

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

  /**
   * Shortest time a node's glow stays on screen.
   *
   * Without it most of them would never be seen at all. A keypress or a click
   * takes a millisecond or two, so its `task-started` and `task-completed`
   * arrive close enough together to land in the same Svelte update - the glow
   * would be added and removed without ever being painted, and a flow of quick
   * actions would light nothing while it ran. Holding each one briefly turns
   * the run into something the eye can follow across the canvas.
   *
   * Short enough not to lie about it: a node that is still glowing when the
   * next one lights up is only ever a few frames behind, and the status panel
   * carries the exact sequence for anyone who needs it.
   */
  const MIN_ACTIVE_GLOW_MS = 220;

  /** When each currently-lit node was reported as started. */
  const activeSince = new Map<string, number>();

  /** Glows waiting out `MIN_ACTIVE_GLOW_MS` before they go out. */
  const idleTimers = new Map<string, ReturnType<typeof setTimeout>>();

  /** Lights a node up on the canvas for as long as the backend is running it. */
  function markNodeActive(nodeId: string) {
    // A node that is running again while the last run's glow was still being
    // held keeps the glow and starts its hold over.
    const pending = idleTimers.get(nodeId);
    if (pending !== undefined) {
      clearTimeout(pending);
      idleTimers.delete(nodeId);
    }

    activeSince.set(nodeId, Date.now());
    if (activeNodeIds.includes(nodeId)) return;
    activeNodeIds = [...activeNodeIds, nodeId];
  }

  /** Takes a node's glow off the canvas, hold or no hold. */
  function removeActiveNode(nodeId: string) {
    activeSince.delete(nodeId);
    activeNodeIds = activeNodeIds.filter((id) => id !== nodeId);
  }

  /**
   * Ends a node's glow, once it has been on screen long enough to have been
   * seen.
   *
   * A failing task reports `task-error` and is then still followed by
   * `task-completed`, so this is called twice for it; the second call finds the
   * hold already running - or the glow already gone - and does nothing.
   */
  function markNodeIdle(nodeId: string) {
    if (idleTimers.has(nodeId)) return;

    const since = activeSince.get(nodeId);
    // No start time means nothing lit it, so there is nothing to hold.
    const shown = since === undefined ? MIN_ACTIVE_GLOW_MS : Date.now() - since;
    if (shown >= MIN_ACTIVE_GLOW_MS) {
      removeActiveNode(nodeId);
      return;
    }

    idleTimers.set(
      nodeId,
      setTimeout(() => {
        idleTimers.delete(nodeId);
        removeActiveNode(nodeId);
      }, MIN_ACTIVE_GLOW_MS - shown)
    );
  }

  /**
   * Puts every glow out at once, on each of the events that end a run, and on
   * anything that replaces the graph.
   *
   * Belt and braces over `markNodeIdle`, and worth it: a run that is stopped
   * mid-task, or that ends on an error raised outside a task, can leave a node
   * that reported `task-started` without ever reporting anything else. A glow
   * left burning on a finished run would say the flow is still going.
   *
   * The minimum hold is deliberately cut short here rather than waited out. The
   * run is over, and a card still lit after that says otherwise.
   */
  function clearActiveNodes() {
    for (const timer of idleTimers.values()) clearTimeout(timer);
    idleTimers.clear();
    activeSince.clear();
    activeNodeIds = [];
  }

  // Unsubscribe functions for the backend listeners below, called on destroy.
  //
  // This is new in v3 and not housekeeping for its own sake: v2's `EventsOn`
  // had no per-listener removal, so the listeners simply leaked and a remount
  // stacked a second set on top. Every one of them writes to this component's
  // state, so a stale set means a destroyed component being written to on the
  // next macro run.
  let eventUnsubscribers: (() => void)[] = [];

  // Set up event listeners
  //
  // Wails v3 hands every listener one `WailsEvent` and puts what the backend
  // emitted on its `data`, where v2 spread the payload across the callback's
  // arguments - hence the `({ data })` on each of these.
  function setupEventListeners() {
    // The backend reports tasks by node id, which is a random decimal and
    // meaningless on screen. Every message below names the node the way the
    // canvas does and keeps the id in `nodeId` for the panel's tooltip.
    eventUnsubscribers.push(Events.On("task-started", ({ data }) => {
      const taskId = data as string;
      // Lit on the canvas as well as named here, for the same reason the
      // skipped nodes are: the panel says which node, the glow says where.
      markNodeActive(taskId);
      addStatusMessage({
        id: `task-started-${taskId}`,
        type: "info",
        message: `${nodeLabel(taskId)} started.`,
        nodeId: taskId,
      });
    }));

    eventUnsubscribers.push(Events.On("task-completed", ({ data }) => {
      const taskId = data as string;
      markNodeIdle(taskId);
      addStatusMessage({
        id: `task-completed-${taskId}`,
        type: "success",
        message: `${nodeLabel(taskId)} completed successfully.`,
        nodeId: taskId,
      });
    }));

    eventUnsubscribers.push(Events.On("task-error", ({ data }) => {
      const payload = data as { taskID: string; error: string };
      isExecuting = false;
      markNodeIdle(payload.taskID);
      addStatusMessage({
        id: `task-error-${payload.taskID}`,
        type: "error",
        message: `${nodeLabel(payload.taskID)} failed: ${payload.error}`,
        nodeId: payload.taskID,
      });
    }));

    eventUnsubscribers.push(Events.On("execution-error", ({ data }) => {
      const errorMsg = data as string;
      isExecuting = false;
      clearActiveNodes();
      addStatusMessage({
        id: `exec-error-${Date.now()}`,
        type: "error",
        message: `Flow execution error: ${errorMsg}`,
      });
    }));

    eventUnsubscribers.push(Events.On("execution-stopped", () => {
      isExecuting = false;
      clearActiveNodes();
      addStatusMessage({
        id: `exec-stopped-${Date.now()}`,
        type: "warning",
        message: "Flow execution was stopped.",
      });
    }));

    // Nodes the backend left out of the run because nothing connects them to
    // the Start node. They sit on the canvas looking live, so say so rather
    // than let the user wonder why that branch did nothing.
    //
    // They are named here *and* marked on the canvas: the names say which
    // nodes without the user leaving the panel, the orange glow says where
    // without the user hunting for them.
    eventUnsubscribers.push(Events.On("execution-nodes-skipped", ({ data }) => {
      const nodeIds = data as string[];
      skippedNodeIds = nodeIds;
      addStatusMessage({
        id: `exec-skipped-${Date.now()}`,
        type: "warning",
        message:
          `${nodeIds.length} ${nodeIds.length === 1 ? "node" : "nodes"} skipped` +
          ` - not reachable from Start: ${inFlowOrder(nodeIds).map(nodeLabel).join(", ")}`,
      });
    }));

    // The run reached a point where nothing was left running and nothing more
    // could ever start - a loop in the connections, in practice.
    eventUnsubscribers.push(Events.On("execution-stalled", ({ data }) => {
      const nodeIds = data as string[];
      isExecuting = false;
      clearActiveNodes();
      const stuck = inFlowOrder(nodeIds).map(nodeLabel);
      addStatusMessage({
        id: `exec-stalled-${Date.now()}`,
        type: "error",
        message:
          `Flow stopped - ${stuck.join(", ")} could never run.` +
          " Check the connections for a loop.",
      });
    }));

    eventUnsubscribers.push(Events.On("execution-completed", () => {
      isExecuting = false;
      isSuccess = true;
      clearActiveNodes();
      addStatusMessage({
        id: `exec-completed-${Date.now()}`,
        type: "success",
        message: "Flow execution completed.",
      });
      // Reset success state after 3 seconds
      setTimeout(() => {
        isSuccess = false;
      }, 1000);
    }));

    // Add save status listeners
    eventUnsubscribers.push(Events.On("save-success", ({ data }) => {
      addStatusMessage({
        id: `save-${Date.now()}`,
        type: "success",
        message: data as string
      });
    }));

    // A macro started from the tray menu or a global hotkey, rather than from
    // this canvas. The task events that follow belong to that macro, and it is
    // very likely not the one on screen - the window may not even have been
    // open - so say whose run this is instead of letting the node highlighting
    // imply it is this one. Nothing here is highlighted: the ids in those
    // events belong to a graph this canvas does not have, and `nodeLabel`
    // already falls back gracefully for an id it does not know.
    eventUnsubscribers.push(Events.On("macro-started", ({ data }) => {
      const macro = data as { id: string; name: string };
      isExecuting = true;
      addStatusMessage({
        id: `macro-started-${Date.now()}`,
        type: "info",
        message: macro.id === $macroID
          ? `"${macro.name}" started from Keypress itself.`
          : `"${macro.name}" started outside the workspace - this canvas is not the macro running.`,
      });
    }));

    // No "save-error" listener: the backend deliberately does not emit one,
    // because `SaveFile` already rejects the bound call with the reason and
    // `handleSave` reports it. Listening for an event nothing sends would only
    // suggest failures are handled somewhere they are not.
  }
  
  // Load the last opened file when the component mounts
  async function loadLastOpenedFile() {
    try {
      const data = await App.LoadLastFile();
      if (!data) {
        // Nothing saved yet. The canvas keeps the defaults from flow.ts, and
        // they count as hydrated - coming back from the macro list must not
        // wipe out whatever the user has built on top of them since.
        markWorkspaceHydrated();
        return;
      }

      // A different graph is about to replace this one, so the previous graph's
      // run marks go with it. Ids are per-graph and the highlights match on
      // them, so a stale mark could not land on a node of the new macro anyway
      // - but leaving the lists populated would keep a stylesheet in <head>
      // describing nodes that no longer exist.
      skippedNodeIds = [];
      clearActiveNodes();

      // Adopts the graph, the name and the id, and marks the workspace
      // hydrated. `data` is a fresh parse from the backend and nothing else
      // holds a reference to it, which is what this call requires.
      openMacroInWorkspace(data);
    } catch (error) {
      console.error("Failed to load last file:", error);
      addStatusMessage({
        id: `load-error-${Date.now()}`,
        type: "error",
        message: "Failed to load last file: " + describeBackendError(error)
      });
    } finally {
      // Every path above settles on a graph the user has not edited - the macro
      // that was read, or the defaults when there was none to read or reading
      // failed - and each is the state to measure later edits against. In a
      // `finally` because `openMacroInWorkspace` drops the baseline, so a
      // capture racing this call could otherwise leave the macro with none at
      // all and the unsaved-changes warning permanently silent.
      await captureSavedSnapshot();
    }
  }

  // Unsaved changes
  // ---------------
  // How often the canvas is compared with the macro on disk. Polling rather
  // than reacting to the stores is forced by the by-reference data contract at
  // the top of this file: node components mutate the `data` object the store
  // holds, so editing a delay changes no store reference and fires no
  // subscription. A comparison is a `JSON.stringify` of the graph, which at the
  // size a macro reaches is nothing, and this interval is far below the time it
  // takes to reach for the macro list.
  const DIRTY_POLL_MS = 500;

  let dirtyPollTimer: ReturnType<typeof setInterval> | undefined;

  /**
   * Updates the unsaved-changes flag.
   *
   * A macro with no baseline yet is reported clean: the graph on screen is
   * still the one that was loaded, and the baseline arrives a frame later - see
   * `captureSavedSnapshot`. Announcing unsaved changes in that gap would put
   * the warning on screen before the user had touched anything.
   */
  function refreshDirtyState() {
    const saved = getSavedSnapshot();
    isMacroDirty.set(saved !== null && currentSnapshot() !== saved);
  }

  // Initialize event listeners when the component mounts.
  //
  // The last opened macro is read once per app run, not once per mount. This is
  // a route, so every trip to the macro list and back remounts it, and reading
  // again here would throw away both the macro the user just opened from that
  // list and any unsaved edits they had made before leaving.
  //
  // The unsaved-changes baseline is kept for the same reason and in the same
  // place - the store, not this component - so it is only taken when there is
  // none. Retaking it on every mount would have a trip to the macro list and
  // back quietly declare the user's unsaved edits saved.
  onMount(() => {
    setupEventListeners();

    if (!isWorkspaceHydrated()) {
      // Takes its own baseline once the macro it reads is on the canvas.
      loadLastOpenedFile();
    } else if (getSavedSnapshot() === null) {
      // Hydrated with no baseline: the macro list just opened a macro and
      // navigated here. A baseline that survived the remount is left alone -
      // it belongs to edits the user has not saved yet.
      captureSavedSnapshot();
    }

    dirtyPollTimer = setInterval(refreshDirtyState, DIRTY_POLL_MS);
  });

  onDestroy(() => {
    for (const unsubscribe of eventUnsubscribers) unsubscribe();
    eventUnsubscribers = [];

    clearInterval(dirtyPollTimer);
    clearTimeout(saveSuccessTimer);
    // The glows go with the component, but the timers holding them would
    // outlive it and write to a destroyed component's state.
    clearActiveNodes();

    // One last look, so the flag the macro list reads describes the canvas as
    // the user left it rather than as it was up to half a second earlier.
    // Guarded because this runs while the route is being torn down: an unsaved
    // edit going unrecorded is worth a warning in the console, never a throw
    // out of a destroy.
    try {
      refreshDirtyState();
    } catch (error) {
      console.warn("Could not check for unsaved changes on leaving:", error);
    }
  });
</script>

<!-- The run marks: which nodes a run skipped, and which it is executing right
     now. Stylesheets rather than classes on the nodes, so the graph data - and
     therefore every save file - stays exactly as the user left it; see
     `buildSkippedGlowCss` and `buildActiveGlowCss`. Emptying the id lists
     empties the rules, and Svelte removes the elements with the component.

     `<svelte:element>` rather than a written-out tag: a literal style element
     here would be taken for the component's own style block. Its content is a
     text node, never markup, so a node id cannot escape into the document. -->
<svelte:head>
  {#if skippedGlowCss}
    <svelte:element this="style">{skippedGlowCss}</svelte:element>
  {/if}
  {#if activeGlowCss}
    <svelte:element this="style">{activeGlowCss}</svelte:element>
  {/if}
</svelte:head>

<!-- Ctrl+S from anywhere in the workspace, including from inside a node's own
     inputs - see `handleKeydown`. -->
<svelte:window on:keydown={handleKeydown} />

<div class="flow-container flex">
  <!-- Left Panel. Kept mounted so it can slide both ways; an `{#if}` here would
       make opening a pop rather than a slide. -->
  <LeftPanel {isLeftPanelExpanded} />

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
          <div class="nav-button-container flex-center flex-gap transition-transform duration-300">
            <!-- Macro name: decides which file on disk the save writes to.
                 Required, and unique across macros - a rejected save reports
                 why just below. -->
            <input
              class="flow-name-input"
              class:flow-name-input-invalid={saveState.status === 'error'}
              type="text"
              bind:this={nameInput}
              bind:value={$macroName}
              on:input={clearSaveError}
              placeholder="Macro name"
              aria-label="Macro name"
              maxlength={MAX_MACRO_NAME_LEN}
              aria-invalid={saveState.status === 'error'}
            />
            <!-- Run Flow Button -->
            <button
              class="flow-button"
              on:click={handleRunFlow}
              disabled={isExecuting}
            >
              <svelte:component
                this={runButtonStatus.icon}
                class="flow-icon {runButtonStatus.color}"
                style={isExecuting ? "animation: spin 1s linear infinite" : ""}
              />
            </button>
            <!-- Save Button. The dot marks edits that are not on disk, so the
                 button says what pressing it is for rather than only what it
                 did last. The title spells the same thing out, and carries the
                 shortcut - there is nothing else on screen that would teach
                 it. -->
            <button
              class="flow-button flow-save-button"
              class:flow-save-button-dirty={$isMacroDirty}
              on:click={handleSave}
              disabled={saveState.status === 'saving'}
              title={saveTitle}
              aria-label={saveTitle}
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
              {#if $isMacroDirty && saveState.status !== 'saving'}
                <span class="flow-save-dot" aria-hidden="true"></span>
              {/if}
            </button>
            <!-- Layout Button. `onLayout` rearranges the workspace stores
                 directly, left-to-right - the direction the nodes' own handles
                 point. -->
            <button
              class="flow-button"
              on:click={() => onLayout("LR")}
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
      <Controls showLock={false} />
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