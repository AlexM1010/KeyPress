<!-- SVGNode.svelte -->
<script lang="ts">
  import { Handle, Position, type Node } from "@xyflow/svelte";
  import type { SVGNodeData, HandleConfig } from "./types";

  type $$Props = {
    id: string;
    data: SVGNodeData;
    width?: number;
    height?: number;
    dragHandle?: boolean;
    type?: string;
    selected?: boolean;
    isConnectable?: boolean;
    zIndex?: number;
    positionAbsolute: { x: number; y: number };
    dragging: boolean;
    targetPosition?: Position;
    sourcePosition?: Position;
  };

  export let id: $$Props["id"];
  export let data: $$Props["data"];
  export let selected: $$Props["selected"] = false;
  export let isConnectable: $$Props["isConnectable"] = true;

  $: nodeWidth = data.width || data.shape.defaultSize.width;
  $: nodeHeight = data.height || data.shape.defaultSize.height;

  $: styles = {
    fill: data.styles?.fill ?? "#f8fafc",
    stroke: data.styles?.stroke ?? "#60a5fa",
    strokeWidth: data.styles?.strokeWidth ?? 2,
  };

  $: handles = data.handles || data.shape.defaultHandles;

  const createClipPathId = () => `clip-path-${id}`;

  function getHandlePosition(handle: HandleConfig): string {
    const offsetX = handle.offsetX ?? 0;
    const offsetY = handle.offsetY ?? 0;

    // Convert pixel offsets to percentage of node size
    switch (handle.position) {
      case Position.Left:
        return `top: ${offsetY}%; left: ${offsetX}%;`;
      case Position.Right:
        return `top: ${offsetY}%; right: ${offsetX}%;`;
      case Position.Top:
        return `left: ${offsetY}%; top: ${offsetX}%;`;
      case Position.Bottom:
        return `left: ${offsetY}%; bottom: ${offsetX}%;`;
      default:
        return "";
    }
  }

  $$restProps; // Silence warnings 
</script>

<div
  class="node-wrapper"
  class:selected
  style="width: {nodeWidth}px; height: {nodeHeight}px"
>
  <svg width="0" height="0">
    <defs>
      <clipPath id={createClipPathId()}>
        <path d={data.shape.svgPath} />
      </clipPath>
    </defs>
  </svg>

  <div
    class="node-content"
    style="clip-path: url(#{createClipPathId()}); -webkit-clip-path: url(#{createClipPathId()});"
  >
    <svg
      width={nodeWidth}
      height={nodeHeight}
      viewBox="0 0 {nodeWidth} {nodeHeight}"
      class="node-svg"
    >
      <path
        d={data.shape.svgPath}
        class="svg-path"
        style="fill: {styles.fill}; stroke: {styles.stroke}; stroke-width: {styles.strokeWidth}px;"
      />
      {#if data.label}
        <text
          x="50%"
          y="50%"
          dominant-baseline="middle"
          text-anchor="middle"
          class="node-label"
        >
          {data.label}
        </text>
      {/if}
    </svg>
  </div>

  {#each handles as handle}
    <Handle
      type={handle.type}
      position={handle.position}
      id={handle.id}
      style={getHandlePosition(handle)}
      {isConnectable}
    />
  {/each}
</div>

<style>
  .node-wrapper {
    position: relative;
    display: flex;
    justify-content: center;
    align-items: center;
    transition: all 0.2s ease;
    opacity: 0.85;
    filter: drop-shadow(0 0 0 rgba(0, 0, 0, 0));
  }

  .node-wrapper:hover {
    transform: translateY(-2px);
    opacity: 0.9;
    filter: drop-shadow(0 4px 6px rgba(0, 0, 0, 0.1));
  }

  .node-content {
    width: 100%;
    height: 100%;
    position: relative;
  }

  .selected .node-content::after {
    content: "";
    position: absolute;
    top: -2px;
    left: -2px;
    right: -2px;
    bottom: -2px;
    background: rgba(59, 130, 246, 0.08);
    border: 2px solid #3b82f6;
    border-radius: inherit;
    pointer-events: none;
    clip-path: inherit;
    -webkit-clip-path: inherit;
    animation: pulse 2s cubic-bezier(0.4, 0, 0.6, 1) infinite;
  }

  @keyframes pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.5;
    }
  }

  .node-svg {
    width: 100%;
    height: 100%;
  }

  .node-label {
    fill: #374151;
    font-size: 12px;
    pointer-events: none;
  }

  :global(.svelte-flow__handle) {
    background: #9ca3af;
    width: 8px;
    height: 8px;
    border: 2px solid white;
    position: absolute;
    transition: all 0.2s ease;
    box-shadow: 0 0 0 rgba(0, 0, 0, 0);
  }

  :global(.svelte-flow__handle::before) {
    content: "";
    position: absolute;
    top: -12px;
    left: -12px;
    right: -12px;
    bottom: -12px;
    z-index: -1;
  }

  :global(.svelte-flow__handle:hover) {
    background: #3b82f6;
    width: 10px;
    height: 10px;
    margin: -1px;
    box-shadow: 0 2px 4px rgba(0, 0, 0, 0.15);
  }
</style>
