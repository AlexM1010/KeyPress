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
  import { nodesData as nodes, edgesData as edges } from "$lib/stores/flow";
  import { onLayout } from "$lib/utils/autoLayout";

  // Nodes
  import { nodeTypes } from "$lib/components/customNodes/nodeTypes";
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

  function handleNodeDataUpdate(event: CustomEvent) {
    const { id, data: newData } = event.detail;
    $nodes = $nodes.map(node => {
        if (node.id === id) {
            return { ...node, data: newData };
        }
        return node;
    });
  }

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

  export let previewMode: boolean  = false;

  if (previewMode) {
    isLeftPanelExpanded = false;
    isStatusPanelExpanded = false;
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

    const newNode = {
      id: `${Math.random()}`,
      type,
      position,
      data: { label: `${type} node` },
    };

    $nodes = [...$nodes, newNode];
  };

  // Status messages
  let statusMessages: { id: string; type: string; message: string }[] = [];
  let isSuccess = false;

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

      // Start execution via the Go backend
      const response = await window.go.main.App.StartExecution(
        JSON.stringify(currentFlowData)
      );

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

  // Modified handleSave function
  async function handleSave() {
    try {
      saveState = { status: 'saving' };
      const currentFlowData = toObject();
      await window.go.main.App.SaveFile(currentFlowData);
      saveState = { status: 'success' };
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : 'Unknown error';
      saveState = { status: 'error', message: errorMessage };
      addStatusMessage({
        id: `save-error-${Date.now()}`,
        type: "error",
        message: "Failed to save flow: " + errorMessage
      });
    } finally {
      // Reset to idle after 3 seconds
      setTimeout(() => {
        saveState = { status: 'idle' };
      }, 3000);
    }
  }

  // Computed property to determine if the status panel should be shown
  $: hasStatusPanel = isStatusPanelExpanded || statusMessages.length > 0;

  // Function to add a status message
  function addStatusMessage(msg: {
    id: string;
    type: string;
    message: string;
  }) {
    statusMessages = [...statusMessages, msg];
    setTimeout(() => {
      statusMessages = statusMessages.filter((m) => m.id !== msg.id);
    }, 10000);
  }

  // Set up event listeners
  function setupEventListeners() {
    window.runtime.EventsOn("task-started", (taskId: string) => {
      addStatusMessage({
        id: `task-started-${taskId}`,
        type: "info",
        message: `Task ${taskId} started.`,
      });
    });

    window.runtime.EventsOn("task-completed", (taskId: string) => {
      addStatusMessage({
        id: `task-completed-${taskId}`,
        type: "success",
        message: `Task ${taskId} completed successfully.`,
      });
    });

    window.runtime.EventsOn(
      "task-error",
      (payload: { taskID: string; error: string }) => {
        isExecuting = false;
        addStatusMessage({
          id: `task-error-${payload.taskID}`,
          type: "error",
          message: `Task ${payload.taskID} failed: ${payload.error}`,
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

    window.runtime.EventsOn("execution-timed-out", () => {
      isExecuting = false;
      addStatusMessage({
        id: `exec-timed-out-${Date.now()}`,
        type: "warning",
        message: "Flow execution timed out.",
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
      const data = await window.go.main.App.LoadLastFile();
      if (data && data.nodes && data.nodes.length > 0) {
        $nodes = data.nodes;
        // Deduplicate edges by id to guard against corrupted saves missing handle data
        const seen = new Set<string>();
        $edges = (data.edges ?? []).filter((edge: { id: string }) => {
          if (seen.has(edge.id)) return false;
          seen.add(edge.id);
          return true;
        });
      }
      // If no data or empty data, keep the default nodes from flow.ts
    } catch (error) {
      console.error("Failed to load last file:", error);
      addStatusMessage({
        id: `load-error-${Date.now()}`,
        type: "error",
        message: "Failed to load last file: " + (error instanceof Error ? error.message : String(error))
      });
    }
  }

  // Initialize event listeners when the component mounts
  onMount(() => {
    setupEventListeners();
    loadLastOpenedFile();
  });
</script>

<div class="flow-container flex">
  <!-- Left Panel -->
  {#if isLeftPanelExpanded}
    <LeftPanel />
  {/if}

  {#if !previewMode}
    <!-- Left Panel Toggle Button -->
    <LeftPanelToggleButton
      {isLeftPanelExpanded}
      {toggleLeftPanel}
    />
  {/if}

  <!-- Main Flow Area -->
  <div
    class="h-full flex-1 transition-all duration-300"
    class:pl-80={isLeftPanelExpanded}
    class:pl-0={!isLeftPanelExpanded}
    class:pr-80={isStatusPanelExpanded}
    class:pr-0={!isStatusPanelExpanded}
  >
    <SvelteFlow
      {nodes}
      {nodeTypes}
      {edges}
      {edgeTypes}
      {colorMode}
      connectionMode={ConnectionMode.Loose}
      defaultEdgeOptions={defaultEdgeOptions}
      on:dragover={!previewMode ? onDragOver : undefined}
      on:drop={!previewMode ? onDrop : undefined}
      on:updateNodeData={handleNodeDataUpdate}
      proOptions={{ hideAttribution: previewMode }}
      fitView
    >
      <!-- Custom connection line -->
      <ConnectionLine slot="connectionLine" />
      <!-- Control Panel -->
      {#if !previewMode}
        <Panel position="top-right">
          <div class="flex items-center">
            <div class="nav-button-container flex-center flex-gap transition-transform duration-300 {expandedClass}">
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
              <!-- Layout Button -->
              <button
                class="flow-button"
                on:click={() => onLayout("TB")}
              >
                <LayoutDashboard class="flow-icon" />
                <span>Layout</span>
              </button>
            </div>
          </div>
        </Panel>
        <!-- Flow Controls -->
        <Controls />
        <MiniMap />
      {/if}
      <Background />
    </SvelteFlow>
  </div>

  {#if !previewMode}
    <!-- Status Panel Toggle Button -->
    <StatusPanelToggleButton
      {isStatusPanelExpanded}
      {hasStatusPanel}
      {toggleStatusPanel}
    />
  {/if}

  <!-- Status Panel -->
  <StatusPanel
    {isStatusPanelExpanded}
    {statusMessages}
    {executionStatus}
  />
</div>