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
  import type { DefaultEdgeOptions, Edge } from "@xyflow/svelte";

  // Import custom nodes, edges, and utilities
  import {
    graph,
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
  } from "$lib/stores/flow.svelte";
  import { onLayout } from "$lib/utils/autoLayout";
  import { describeBackendError, isBackendUnreachable } from "$lib/utils/helpers";

  // Naming the nodes of a run, and the run marks painted onto the canvas. Both
  // are pure functions of a graph snapshot, so they live outside the component
  // and are tested directly - see `nodeLabels.test.ts` and `nodeGlow.test.ts`.
  import {
    buildNodeLabels,
    inFlowOrder as orderIdsByStep,
    nodeLabel as labelForNodeId,
    type NodeLabel,
  } from "$lib/utils/nodeLabels";
  import {
    buildActiveGlowCss,
    buildSkippedGlowCss,
    DEFAULT_NODE_GLOW,
    NODE_TYPE_GLOWS,
  } from "$lib/utils/nodeGlow";

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
  import { flowTheme } from "$lib/stores/theme.svelte";
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
  // children of this component and an event dispatched from one of them has no path
  // to a listener here. Instead, Svelte Flow hands each node component the very
  // `data` object held in `graph.nodes`, and the components mutate that payload in
  // place - so a snapshot of `graph` already carries the user's edits.
  //
  // Mutating used to be only half of the contract. With the `nodesData` store an
  // in-place edit changed no store reference and so announced nothing, and every
  // node component had to follow the edit with a `markGraphEdited()` whose identity
  // update fired the store's subscriptions. `graph` is deep `$state` now, so the
  // write to `data` is itself a tracked write: the unsaved-changes effect below
  // re-reads the graph *because of* it, with nothing in between. `markGraphEdited`
  // no longer exists.
  //
  // Traffic runs the other way too. Svelte Flow writes drag positions, selection
  // and deletions back into the same arrays - it shallow-copies the node and keeps
  // the same `data` object, so a payload a node component is holding survives the
  // replacement. That is what `bind:nodes` / `bind:edges` below are for: without
  // the `bind:` those writes have nowhere to land.

  // Reactive statement to sync color mode with flow theme
  const colorMode = $derived($flowTheme);

  // Destructure helper functions from useSvelteFlow
  const { screenToFlowPosition } = useSvelteFlow();

  /**
   * The graph on the canvas as plain objects, detached from the state the canvas
   * holds.
   *
   * This is what `toObject()` used to be, and it had to stop being `toObject()`:
   * 1.x implements that as `structuredClone({ nodes: [...store.nodes], ... })`,
   * and every node in there is a `$state` proxy - `structuredClone` throws
   * `DataCloneError` on a proxy. `$state.snapshot` is the deep copy that
   * understands them, so it is what every path out of this component goes
   * through: both calls to the Go backend, and the serialisation the
   * unsaved-changes check compares.
   *
   * Nothing is lost by dropping `toObject`. Its `nodes` and `edges` were copies
   * of the very arrays bound to `<SvelteFlow>`, so reading `graph` reads the
   * same graph one step earlier; its third field, the viewport, was never read -
   * it rode along in the JSON handed to `StartExecution`, where Go's decoder
   * ignored it.
   */
  function currentGraph(): { nodes: FlowNode[]; edges: Edge[] } {
    // Widened on the way in and narrowed again on the way out. `Snapshot<T>` is
    // a recursive conditional type, and asking TypeScript to compute it for a
    // Svelte Flow node is asking it to walk the whole `Node` type - it gives up
    // with "type instantiation is excessively deep and possibly infinite".
    // `unknown[]` stops it descending. The values are the same either way; only
    // the type of the expression differs, and this function's signature is what
    // states it.
    return {
      nodes: $state.snapshot(graph.nodes as unknown[]) as FlowNode[],
      edges: $state.snapshot(graph.edges as unknown[]) as Edge[],
    };
  }

  // State variables
  let isStatusPanelExpanded = $state(false);
  let isExecuting = $state(false);
  let isLeftPanelExpanded = $state(true);

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
      id: crypto.randomUUID(),
      type,
      position,
      data: {},
    };

    graph.nodes = [...graph.nodes, newNode];
  };

  // Status messages
  //
  // `nodeId` is the raw node id the backend reported the event for. It is kept
  // out of `message` on purpose - node ids are random UUIDs and mean nothing
  // to a user - but carried alongside it so the panel can still surface it for
  // debugging (see StatusPanel's tooltip).
  let statusMessages = $state<{
    id: string;
    type: string;
    message: string;
    nodeId?: string;
  }[]>([]);

  // Skipped-node highlight
  // ----------------------
  // Ids the backend reported as connected but unreachable from Start on the
  // most recent run. Held on their own, deliberately NOT on the nodes: the
  // node objects in `graph.nodes` are the ones handed to `SaveFile`, so a
  // `class` (or any other marker) written onto a node would live in the user's
  // macro JSON forever. The same rule that forbids it is the by-reference data
  // contract - node components mutate their `data` payload in place, so `data`
  // is the user's, not a place to park display state.
  //
  // Instead the ids are turned into a stylesheet keyed on the `data-id`
  // attribute Svelte Flow already renders on every `.svelte-flow__node`
  // wrapper (NodeWrapper.svelte: `data-id={id}`), which marks exactly the
  // reported nodes without the graph data being touched at all.
  let skippedNodeIds = $state<string[]>([]);

  const skippedGlowCss = $derived(buildSkippedGlowCss(skippedNodeIds));

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
  let activeNodeIds = $state<string[]>([]);

  /**
   * id -> the `--node-glow-*` property that node lights up in, frozen for the
   * duration of one run.
   *
   * Built from the same snapshot as `nodeLabels`, for the same reason: it is
   * the graph the backend is about to be given, so every node it can report on
   * has an entry and no node changes colour half-way through a run because the
   * canvas was edited underneath it.
   */
  let nodeGlows = $state(new Map<string, string>());

  const activeGlowCss = $derived(buildActiveGlowCss(activeNodeIds, nodeGlows));

  /**
   * id -> label, frozen for the duration of one run.
   *
   * Built from the very graph snapshot that is handed to the backend, so every
   * task the backend can report on has an entry and no node changes label
   * half-way through a run.
   */
  let nodeLabels = $state(new Map<string, NodeLabel>());

  // `nodeLabel` and `inFlowOrder` are thin bindings of the pure lookups in
  // `nodeLabels.ts` to the snapshot above. They are kept as one-argument
  // wrappers so every call site below - and `.map(nodeLabel)` in particular -
  // reads as it did when the map was module state. The rules they follow, and
  // why the ordering agrees with the engine, are documented there.
  const nodeLabel = (nodeId: string) => labelForNodeId(nodeLabels, nodeId);
  const inFlowOrder = (nodeIds: string[]) => orderIdsByStep(nodeLabels, nodeIds);

  const hasError = $derived(statusMessages.some((msg) => msg.type === "error"));
  const hasWarning = $derived(statusMessages.some((msg) => msg.type === "warning"));
  const hasSuccess = $derived(
    statusMessages.some(
      (msg) =>
        msg.type === "success" && msg.message.includes("Flow execution completed")
    )
  );

  /**
   * The icon-and-colour pair the run button and the status panel share.
   *
   * Annotated with one concrete icon type rather than left to inference. Every
   * Lucide icon has the same props, but the four branches below infer as four
   * different component types, and a union of component constructors is not
   * something a `<RunIcon />` tag can be typed against.
   */
  type ExecutionStatus = { icon: typeof Play; color: string };

  // Computed property to determine the current execution status and icon
  const executionStatus = $derived.by((): ExecutionStatus => {
    if (isExecuting) return { icon: Loader, color: "text-blue-500" };
    if (hasError) return { icon: X, color: "text-red-500" };
    if (hasWarning) return { icon: TriangleAlert, color: "text-yellow-500" };
    if (hasSuccess) return { icon: Play, color: "text-green-500" };
    return { icon: Play, color: "text-foreground" };
  });

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
  let isRunAlertHeld = $state(false);

  const hasRunAlert = $derived(!isExecuting && (hasError || hasWarning));

  // The timer is owned by the effect that starts it, so it needs no variable of
  // its own and no line in `onDestroy`: Svelte runs the returned cleanup both
  // when `hasRunAlert` changes again and when the component goes away, which is
  // exactly the set of moments a pending hold has to be cancelled.
  $effect(() => {
    if (!hasRunAlert) {
      isRunAlertHeld = false;
      return;
    }

    isRunAlertHeld = true;
    const timer = setTimeout(() => (isRunAlertHeld = false), RUN_ALERT_HOLD_MS);
    return () => clearTimeout(timer);
  });

  const runButtonStatus = $derived(
    hasRunAlert && !isRunAlertHeld
      ? { icon: Play, color: "text-foreground" }
      : executionStatus
  );

  /**
   * The run button's glyph, pulled out of `runButtonStatus` so the markup can
   * render it as a tag. `{@const}` would be the obvious way to name a component
   * in the template, but it is only allowed as the immediate child of a block,
   * and this one sits inside a `<button>`.
   */
  const RunIcon = $derived(runButtonStatus.icon);

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
      statusMessages = [];
      // Last run's marks go with last run's messages: the canvas must never
      // show a glow that the status panel no longer explains. The active glow
      // is cleared as well - a run that ended without a terminal event (the app
      // was left mid-run) must not leave a node looking like it is still going.
      skippedNodeIds = [];
      clearActiveNodes();

      // Get the current flow data as plain objects
      const currentFlowData = currentGraph();

      // Check if the flow contains a Start node
      const hasStartNode = currentFlowData.nodes.some(
        (node) => node.type === "StartNode"
      );
      if (!hasStartNode) {
        // Reported through the status panel like every other run failure,
        // rather than a browser `alert()`. An alert is modal - it stops the app
        // until it is dismissed, and it looks nothing like the rest of the
        // window - and it leaves no record, where a refused run is exactly the
        // thing the panel exists to keep on screen. The panel's own toggle
        // button carries the error state, so this cannot go unnoticed either.
        isExecuting = false;
        addStatusMessage({
          id: `no-start-${Date.now()}`,
          type: "error",
          message: "Flowchart must contain a Start node.",
        });
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

  let saveState = $state<SaveState>({ status: 'idle' });

  /**
   * The save button's glyph. Named for the same reason `RunIcon` is - see there.
   * Annotated with one icon type because the four branches infer as four.
   */
  const SaveIcon: typeof Save = $derived(
    saveState.status === 'saving' ? Loader :
    saveState.status === 'error' ? X :
    saveState.status === 'success' ? Check :
    Save
  );

  /**
   * The name field, so a refused save can put the cursor back in the thing that
   * has to change for the next one to work.
   */
  let nameInput = $state<HTMLInputElement | undefined>(undefined);

  /**
   * Longest macro name the backend will store, mirrored here so the field stops
   * at the limit instead of letting the user type past it and be told afterwards.
   * Must match `maxMacroNameLen` in backend/persistence.go - the backend still
   * enforces it, this only saves the round trip.
   */
  const MAX_MACRO_NAME_LEN = 200;

  // The macro's name and id live in `$lib/stores/flow.svelte` rather than here,
  // because the macro list can now open a macro straight into the workspace and
  // its identity has to arrive with its graph. See `openMacroInWorkspace`.

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
    const { nodes, edges } = currentGraph();
    return serializeMacro($macroName, nodes, edges);
  }

  /**
   * Records the graph on screen as the one on disk.
   *
   * Deferred by a frame on purpose. Node components backfill their newer fields
   * into the `data` object the graph holds as they mount, so a graph just read
   * off disk keeps changing for a moment after it is put on the canvas; a
   * baseline taken before that settles would have every freshly opened macro
   * claiming unsaved changes it does not have. `tick()` waits for Svelte Flow to
   * mount the nodes and the frame waits for their first render - and, now that
   * those backfills are tracked writes rather than silent ones, for the effects
   * they schedule, which are flushed in microtasks and so are all done by the
   * time a `requestAnimationFrame` callback runs.
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

      const currentFlowData = currentGraph();

      // `currentGraph()` is a `$state.snapshot`, so this is a graph detached
      // from the canvas: a node edited while the save is in flight cannot reach
      // it, and the string below is therefore exactly the bytes the call sends.
      // That used to depend on ordering - `toObject()` shared each node's `data`
      // object with the canvas, so the snapshot had to be taken before the await
      // or a mid-flight edit would be folded into the baseline and never
      // reported unsaved again. The detachment is now structural.
      const sentSnapshot = serializeMacro(name, currentFlowData.nodes, currentFlowData.edges);

      // v3's generated models are plain interfaces rather than v2's classes, so
      // there is no `createFrom` to run the canvas object through. Mapped field
      // by field rather than cast, because Svelte Flow's types really are looser
      // than the Go structs and a cast would compile while sending values Go
      // cannot take: an unused handle is `null` on an edge, and a node's `type`
      // is optional. Both are narrowed here, once, at the boundary.
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
  const saveTitle = $derived(
    saveState.status === 'saving'
      ? "Saving..."
      : $isMacroDirty
        ? "Save macro (Ctrl+S) - unsaved changes"
        : "Save macro (Ctrl+S)"
  );

  // Computed property to determine if the status panel should be shown
  const hasStatusPanel = $derived(isStatusPanelExpanded || statusMessages.length > 0);

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
    // The backend reports tasks by node id, which is a random UUID and
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
      clearActiveNodes();
      addStatusMessage({
        id: `exec-completed-${Date.now()}`,
        type: "success",
        message: "Flow execution completed.",
      });
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
        // Nothing saved yet. The canvas keeps the defaults from
        // flow.svelte.ts, and they count as hydrated - coming back from the
        // macro list must not wipe out whatever the user has built on top of
        // them since.
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
  // The canvas is compared with the macro on disk whenever the graph changes,
  // rather than on a timer. A node dropped or deleted, a node dragged, an edge
  // drawn, the name typed, a value edited inside a node's `data` payload - every
  // one of those is now an ordinary tracked write to `graph` (or to the
  // `macroName` store), so an effect that reads the graph through
  // `serializeMacro` re-runs on exactly the set of changes that can alter the
  // saved file and on nothing else. This used to be a 500ms poll, which meant a
  // save button that lit up as much as half a second after the keystroke and a
  // comparison run twice a second whether anything had happened or not; before
  // that it was three store subscriptions plus a `markGraphEdited` call at the
  // end of every node component.
  //
  // No debounce sits in front of it. A comparison is a `$state.snapshot` plus a
  // `JSON.stringify` of the graph: measured at roughly 15us for ten nodes,
  // 130us for a hundred and 0.8ms for five hundred - a fraction of the render
  // the edit had already caused, and dwarfed by what Svelte Flow itself does on
  // each of these changes (it rebuilds its internal node for every node in the
  // graph). The thing worth protecting is that the warning is right at the
  // moment the user reaches for the macro list, and a delay is exactly what
  // would take that away.

  /**
   * Updates the unsaved-changes flag.
   *
   * A macro with no baseline yet is reported clean: the graph on screen is
   * still the one that was loaded, and the baseline arrives a frame later - see
   * `captureSavedSnapshot`. Announcing unsaved changes in that gap would put
   * the warning on screen before the user had touched anything.
   *
   * The graph is read before the baseline is looked at, and unconditionally.
   * Written the other way round, `saved !== null && ...` would short-circuit
   * past the only reactive read in here on precisely the runs where there is no
   * baseline yet, and the effect below would be left tracking nothing at all -
   * so a freshly opened macro would never report an edit again.
   */
  function refreshDirtyState() {
    const current = currentSnapshot();
    const saved = getSavedSnapshot();
    isMacroDirty.set(saved !== null && current !== saved);
  }

  // Runs once on mount as well as on every later change, which is what makes
  // the arrival back from the macro list report the graph it arrives with; a
  // macro whose baseline is still a frame away reports clean either way - see
  // `refreshDirtyState`. Nothing needs to re-run it when the *baseline* moves:
  // `markMacroSaved` sets the flag itself, and it is the only thing that
  // changes a baseline.
  $effect(refreshDirtyState);

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
  });

  onDestroy(() => {
    for (const unsubscribe of eventUnsubscribers) unsubscribe();
    eventUnsubscribers = [];

    clearTimeout(saveSuccessTimer);
    // The glows go with the component, but the timers holding them would
    // outlive it and write to a destroyed component's state.
    clearActiveNodes();

    // One last look, so the flag the macro list reads describes the canvas as
    // the user left it. The effect above has already been torn down by this
    // point, and an edit can still land after that - a node component's own
    // teardown, or a final mutation that arrives while the route unwinds - so
    // this is the answer that has to be right rather than the last one that
    // happened to arrive. Guarded because this runs while the route is being
    // torn down: an unsaved edit going unrecorded is worth a warning in the
    // console, never a throw out of a destroy.
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
     `buildSkippedGlowCss` and `buildActiveGlowCss` in `$lib/utils/nodeGlow`.
     Emptying the id lists
     empties the rules, and Svelte removes the elements with the component.

     `<svelte:element>` rather than a written-out tag: a literal style element
     here would be taken for the component's own style block. Its content is a
     text node, never markup, so a node id cannot escape into the document. -->
<svelte:head>
  {#if skippedGlowCss}
    <svelte:element this={'style'}>{skippedGlowCss}</svelte:element>
  {/if}
  {#if activeGlowCss}
    <svelte:element this={'style'}>{activeGlowCss}</svelte:element>
  {/if}
</svelte:head>

<!-- Ctrl+S from anywhere in the workspace, including from inside a node's own
     inputs - see `handleKeydown`. -->
<svelte:window onkeydown={handleKeydown} />

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
    <!-- `bind:` on both graphs, not plain props. Svelte Flow owns a node's
         position, its selection and its removal, and it publishes all three by
         writing the array back - without the binding a drag would snap back and
         a delete would never reach the file.

         The custom connection line is a component prop rather than the
         `slot="connectionLine"` it was in 0.1.x: 1.x replaced slots with
         snippets throughout, and this particular one is not a snippet either -
         `connectionLineComponent` takes the component itself and Svelte Flow
         instantiates it inside its own `<svg>`, passing no props. The line
         reads the connection in progress from `useConnection()` instead.

         The drop handlers are plain DOM attributes now. 1.x forwards everything
         it does not recognise onto its container `<div>`, so `ondragover` /
         `ondrop` land on the element the user is actually dragging over; the
         `on:` directives they replace only ever addressed component events,
         which Svelte 5 does not have. -->
    <SvelteFlow
      bind:nodes={graph.nodes}
      {nodeTypes}
      bind:edges={graph.edges}
      {edgeTypes}
      {colorMode}
      connectionMode={ConnectionMode.Loose}
      {defaultEdgeOptions}
      connectionLineComponent={ConnectionLine}
      ondragover={onDragOver}
      ondrop={onDrop}
      deleteKey="Backspace"
      fitView
    >
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
              oninput={clearSaveError}
              placeholder="Macro name"
              aria-label="Macro name"
              maxlength={MAX_MACRO_NAME_LEN}
              aria-invalid={saveState.status === 'error'}
            />
            <!-- Run Flow Button -->
            <button
              class="flow-button"
              onclick={handleRunFlow}
              disabled={isExecuting}
            >
              <RunIcon
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
              onclick={handleSave}
              disabled={saveState.status === 'saving'}
              title={saveTitle}
              aria-label={saveTitle}
            >
              <SaveIcon
                class="flow-icon"
                style={saveState.status === 'saving' ? "animation: spin 1s linear infinite" : ""}
              />
              {#if $isMacroDirty && saveState.status !== 'saving'}
                <span class="flow-save-dot" aria-hidden="true"></span>
              {/if}
            </button>
            <!-- Layout Button. `onLayout` rearranges the workspace graph
                 directly, left-to-right - the direction the nodes' own handles
                 point. -->
            <button
              class="flow-button"
              onclick={() => onLayout("LR")}
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
