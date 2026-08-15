<script lang="ts">
  /**
   * CustomEdge.svelte
   * A customizable edge component for XYFlow that displays a delete button on hover.
   * Features dynamic positioning, smooth animations, and single-button display.
   * 
   *  TODO:
   *  - Expand x button hover but not visual radius 
   *  - Fix edge path hover detection - 
   */

  import type { Position } from "@xyflow/svelte";
  import {
    getBezierPath,
    BaseEdge,
    type EdgeProps,
    EdgeLabel,
    useEdges,
  } from "@xyflow/svelte";
  import { X } from "lucide-svelte";
  import { cubicOut } from "svelte/easing";
  import { fade } from "svelte/transition";

  // Types and Interfaces
  /**
   * The slice of an edge's `data` payload this component understands.
   *
   * `defaultEdgeOptions` in Flow.svelte stamps `{ color: 'var(--main-text)' }`
   * onto every edge, so this is how an edge says what colour it should be
   * drawn in. Read-only here: the payload is the very object held in
   * `graph.edges`, so a write would be a write to the user's graph - and now
   * that the graph is deep `$state`, one that silently marks the macro dirty.
   */
  interface CustomEdgeData {
    color?: string;
  }

  interface HoverPoint {
    x: number;
    y: number;
    timeoutId: ReturnType<typeof setTimeout> | null;
    isHovered: boolean;
    id: string;
  }

  interface CustomEdgeProps extends EdgeProps {
    id: string;
    sourceX: number;
    sourceY: number;
    sourcePosition: Position;
    targetX: number;
    targetY: number;
    targetPosition: Position;
    markerEnd?: string;
    style?: string;
  }

  // Configuration Constants
  const CONFIG = {
    CREATION_COOLDOWN: 300, // ms between new point creation attempts
    MIN_POINT_DISTANCE: 30, // minimum pixels between hover points
    PATH_SAMPLING_POINTS: 50, // number of points to sample when finding closest point
    HOVER_TIMEOUT: 300, // ms before removing hover points
    MAX_HOVER_POINTS: 1, // maximum number of hover points (buttons) to show
    FADE_DURATION: 200, // ms for fade animation
    BUTTON_SCALE: {
      DEFAULT: 0.8,
      HOVER: 1.2,
    },
    ICON_SCALE: {
      DEFAULT: "65%",
      HOVER: "75%",
    },
  } as const;

  /**
   * Colour used when an edge carries no `data.color` of its own - for instance
   * an edge restored from a save written before edges had a data payload.
   * `--main-text` is white on the dark theme and black on the light one, so the
   * line stays legible either way.
   *
   * Svelte Flow's own default is `--xy-edge-stroke-default`, which on the dark
   * theme is #3e3e3e: all but invisible against this app's near-black canvas.
   * Leaving the stroke unset is what made edges look like they were not
   * rendering at all.
   */
  const DEFAULT_EDGE_COLOR = "var(--main-text)";

  /** Stroke width, in SVG user units, of the visible edge line. */
  const EDGE_STROKE_WIDTH = 2;

  // Props
  type $$Props = EdgeProps;

  export let id: $$Props["id"];
  export let sourceX: $$Props["sourceX"];
  export let sourceY: $$Props["sourceY"];
  export let sourcePosition: $$Props["sourcePosition"];
  export let targetX: $$Props["targetX"];
  export let targetY: $$Props["targetY"];
  export let targetPosition: $$Props["targetPosition"];
  export let style: $$Props["style"] = undefined;
  export let interactionWidth: $$Props["interactionWidth"] = undefined;
  export let data: CustomEdgeData | undefined = undefined;

  // State
  let edgePath: string;
  let pathElement: SVGPathElement | null = null;
  let isEdgeHovered = false;
  let lastCreationTime = 0;
  let hoverPoints: HoverPoint[] = [];
  let globalCleanupTimeout: ReturnType<typeof setTimeout> | null = null;
  let pointIdCounter = 0;

  // Reactive declarations
  $: [edgePath] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  });

  /**
   * Colour and width are handed to `BaseEdge` as Svelte Flow's own custom
   * properties rather than as a plain `stroke:` declaration. Its stylesheet
   * already reads them (`stroke: var(--xy-edge-stroke, ...)`), and going
   * through the variable keeps the `.selected` rule - which sets `stroke`
   * directly - able to win, which an inline `stroke` could not.
   *
   * Any `style` the edge itself carries is appended last so it still overrides.
   */
  $: edgeStyle =
    `--xy-edge-stroke: ${data?.color ?? DEFAULT_EDGE_COLOR};` +
    ` --xy-edge-stroke-width: ${EDGE_STROKE_WIDTH};` +
    (style ? ` ${style}` : "");

  const edges = useEdges();

  // Helper Functions
  /**
   * Finds the closest point on the path to the given coordinates
   */
  function getClosestPoint(path: SVGPathElement, x: number, y: number) {
    const pathLength = path.getTotalLength();
    const increment = pathLength / CONFIG.PATH_SAMPLING_POINTS;

    let closestPoint = { x: 0, y: 0 };
    let minDistance = Infinity;

    for (let i = 0; i <= pathLength; i += increment) {
      const point = path.getPointAtLength(i);
      const distance = Math.hypot(point.x - x, point.y - y);
      if (distance < minDistance) {
        minDistance = distance;
        closestPoint = point;
      }
    }
    return closestPoint;
  }

  /**
   * Transforms mouse coordinates from screen space to SVG space
   */
  function transformCoordinates(event: MouseEvent, svgElement: SVGSVGElement) {
    const ctm = svgElement.getScreenCTM();
    if (!ctm) return null;

    const point = svgElement.createSVGPoint();
    point.x = event.clientX;
    point.y = event.clientY;

    return point.matrixTransform(ctm.inverse());
  }

  /**
   * Cleans up all timeouts and scheduled operations
   */
  function clearAllTimeouts() {
    if (globalCleanupTimeout !== null) {
      clearTimeout(globalCleanupTimeout);
      globalCleanupTimeout = null;
    }

    hoverPoints.forEach((point) => {
      if (point.timeoutId !== null) {
        clearTimeout(point.timeoutId);
      }
    });
  }

  /**
   * Schedules removal of hover points if no hover states are active
   */
  function schedulePointRemoval() {
    clearAllTimeouts();

    if (!isEdgeHovered && !hoverPoints.some((p) => p.isHovered)) {
      globalCleanupTimeout = setTimeout(() => {
        hoverPoints = [];
      }, CONFIG.HOVER_TIMEOUT);
    }
  }

  /**
   * Checks if enough time has passed to create a new hover point
   */
  function canCreateNewPoint(): boolean {
    const now = Date.now();
    if (now - lastCreationTime >= CONFIG.CREATION_COOLDOWN) {
      lastCreationTime = now;
      return true;
    }
    return false;
  }

  // Event Handlers
  function onEdgeClick() {
    edges.update((eds) => eds.filter((edge) => edge.id !== id));
  }

  function handleMouseMove(event: MouseEvent) {
    if (!isEdgeHovered) {
      isEdgeHovered = true;
      clearAllTimeouts();
    }

    if (!pathElement || !canCreateNewPoint()) return;

    const svgElement = (event.currentTarget as SVGElement).closest(
      "svg",
    ) as SVGSVGElement;
    if (!svgElement) return;

    const transformedPoint = transformCoordinates(event, svgElement);
    if (!transformedPoint) return;

    const nearestPoint = getClosestPoint(
      pathElement,
      transformedPoint.x,
      transformedPoint.y,
    );

    // Check if the new point would be too close to existing points
    const tooClose = hoverPoints.some(
      (p) =>
        Math.hypot(p.x - nearestPoint.x, p.y - nearestPoint.y) <
        CONFIG.MIN_POINT_DISTANCE,
    );

    if (!tooClose) {
      // Remove old points with fade animation
      if (hoverPoints.length >= CONFIG.MAX_HOVER_POINTS) {
        const oldPoints = [...hoverPoints];
        oldPoints.forEach((point) => {
          if (!point.isHovered) {
            setTimeout(() => {
              hoverPoints = hoverPoints.filter((p) => p.id !== point.id);
            }, CONFIG.FADE_DURATION);
          }
        });
      }

      // Add new point
      const newPointId = `point-${pointIdCounter++}`;
      hoverPoints = [
        ...hoverPoints,
        {
          x: nearestPoint.x,
          y: nearestPoint.y,
          timeoutId: null,
          isHovered: false,
          id: newPointId,
        },
      ];
    }
  }

  function handleMouseLeave() {
    isEdgeHovered = false;
    schedulePointRemoval();
  }

  function handleButtonMouseEnter(id: string) {
    const index = hoverPoints.findIndex((p) => p.id === id);
    if (index === -1) return;

    hoverPoints[index] = { ...hoverPoints[index], isHovered: true };
    hoverPoints = [...hoverPoints];
    clearAllTimeouts();
  }

  function handleButtonMouseLeave(id: string) {
    const index = hoverPoints.findIndex((p) => p.id === id);
    if (index === -1) return;

    hoverPoints[index] = { ...hoverPoints[index], isHovered: false };
    hoverPoints = [...hoverPoints];
    schedulePointRemoval();
  }

  // Lifecycle
  function bindPath(node: SVGPathElement) {
    pathElement = node;
    return {
      destroy: () => {
        clearAllTimeouts();
        pathElement = null;
      },
    };
  }

  $$restProps; // Silence warnings 
</script>

<!-- Arrow marker definition 
<svg style="position: absolute; width: 0; height: 0;">
  <defs>
    <marker
      id="arrow"
      viewBox="0 0 10 10"
      refX="9"
      refY="5"
      markerUnits="strokeWidth"
      markerWidth="8"
      markerHeight="8"
      orient="auto-start-reverse"
    >
      <path d="M 0 0 L 10 5 L 0 10 z" fill="#9ca3af" />
    </marker>
  </defs>
</svg> -->

<!-- Edge container with invisible hit area -->
<g
  class="edge-container"
  on:mouseenter={handleMouseMove}
  on:mousemove={handleMouseMove}
  on:mouseleave={handleMouseLeave}
  role="presentation"
>
  <path
    use:bindPath
    d={edgePath}
    fill="none"
    stroke="transparent"
    stroke-width="10"
    style="pointer-events: stroke;"
  />
  <BaseEdge path={edgePath} style={edgeStyle} {interactionWidth} /> <!-- {markerEnd} -->
</g>

<!--
  Hover points with delete buttons.

  `EdgeLabel` is Svelte Flow 1.x's replacement for `EdgeLabelRenderer`, and it
  is not a like-for-like swap. The old component was a bare portal: it hoisted
  whatever you put in it out of the SVG and into an HTML layer, and positioning
  was entirely yours - hence the `transform: translate(x, y)` and the pair of
  `-12px` margins that used to hand-centre a 24px box on the point. `EdgeLabel`
  takes `x` / `y` itself and centres on them with its own
  `translate(-50%, -50%)`, so both of those go away and the button is centred
  properly at any size rather than at one hard-coded one.

  `transparent` is required, not decorative: `.svelte-flow__edge-label` paints
  an opaque background by default (white on the light theme, near-black on the
  dark one), which without this draws a card behind the delete button and over
  the edge it sits on.

  The inner `<div>` survives because a Svelte transition can only be applied to
  an element, never to a component - `transition:fade` on `<EdgeLabel>` would
  not compile.
-->
{#each hoverPoints as point (point.id)}
  <EdgeLabel x={point.x} y={point.y} transparent class="nodrag nopan">
    <div
      class="hover-point"
      transition:fade={{ duration: CONFIG.FADE_DURATION, easing: cubicOut }}
    >
      <button
        class="delete-button"
        on:click={onEdgeClick}
        on:mouseenter={() => handleButtonMouseEnter(point.id)}
        on:mouseleave={() => handleButtonMouseLeave(point.id)}
        aria-label="Remove edge"
      >
        <X />
      </button>
    </div>
  </EdgeLabel>
{/each}

<style>
  .edge-container {
    position: relative;
    color: #000;
  }

  /* Placement is `EdgeLabel`'s job now - see the comment on it above. All this
     is left to do is give the button a small hover buffer around it, so the
     pointer does not have to land on 16px of circle to keep it alive. */
  .hover-point {
    display: flex;
    padding: 0.25rem;
    transform-origin: center;
  }

  .delete-button {
    width: 1rem;
    height: 1rem;
    background: #9ca3af;
    border: 0.125rem solid white;
    border-radius: 50%;
    cursor: pointer;
    position: relative;
    transition: all 0.2s ease;
    box-shadow: 0 0.125rem 0.25rem rgba(0, 0, 0, 0.15);
    display: flex;
    align-items: center;
    justify-content: center;
    color: white;
    padding: 0;
    transform-origin: center;
    transform: scale(0.8);
  }

  .delete-button:hover {
    background: #3b82f6;
    transform: scale(1.2);
  }

  .delete-button :global(svg) {
    width: 65%;
    height: 65%;
    transition: inherit;
  }

  .delete-button:hover :global(svg) {
    width: 75%;
    height: 75%;
  }
</style>
