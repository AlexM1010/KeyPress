<!-- frontend\src\lib\components\customNodes\NodeWrapper.svelte -->
<script lang="ts">
    import { slide } from 'svelte/transition';
    import { ChevronDown } from "lucide-svelte";
    import type { ComponentType } from "svelte";
    import { Handle, Position, useSvelteFlow } from "@xyflow/svelte";
    import type { HandleConfig } from "../types";
    import ContextMenu from "./ContextMenu.svelte";
    import { cubicOut } from "svelte/easing";
    import { onDestroy, getContext } from 'svelte';
    import { nodesData } from "$lib/stores/flow";
    import '$lib/index.scss';

    // Component Props
    export let icon: ComponentType;
    export let title: string;
    export let color: string;
    export let isExpanded: boolean = true;
    export let isConnectable: boolean = true;
    export let handles: HandleConfig[] = [];
    export let id: string;
    export let type: string;

    // NOTE: this wrapper deliberately takes no `data` prop. Svelte Flow hands each
    // custom node the very `data` object it holds in the `nodesData` store, so the
    // node components edit that payload in place (by reference) and `toObject()`
    // picks the edits up on save. Nothing needs to be forwarded back up from here.

    // Rest props to silence warnings
    $$restProps;

    // These actions are handled here rather than dispatched upwards. `<SvelteFlow>`
    // instantiates the custom node components itself from `nodeTypes`, so no node is
    // a child of `Flow.svelte` and a `createEventDispatcher` event from one has no
    // path to a listener there. This component *is* rendered inside the flow, so it
    // can reach the same stores `<SvelteFlow>` was handed.
    const { deleteElements } = useSvelteFlow();

    // The node palette renders these same components as static drag previews. There
    // is no node in the flow behind them, so duplicate/delete have nothing to act on
    // and the menu is only in the way - the palette sets this context to suppress it.
    const isPreview: boolean = getContext('nodePreview') === true;

    // UI State Management
    let isHovered = false;
    let isHeaderHovered = false;

    // The context menu floats above the header, so the pointer momentarily leaves
    // the header on its way to the buttons. Hiding is deferred (and cancelled when
    // the pointer lands on the menu) so the buttons stay reachable.
    let hideTimer: ReturnType<typeof setTimeout> | undefined;

    function showMenu() {
        if (isPreview) return;
        clearTimeout(hideTimer);
        isHeaderHovered = true;
    }

    function hideMenu() {
        clearTimeout(hideTimer);
        hideTimer = setTimeout(() => (isHeaderHovered = false), 200);
    }

    onDestroy(() => clearTimeout(hideTimer));

    // Event Handlers
    function handleDuplicate() {
        // Read from the store rather than `getNode`: `<SvelteFlow>` writes drag
        // positions straight back into it, so it holds the node's current position
        // and is already typed as a `FlowNode`.
        const node = $nodesData.find((n) => n.id === id);
        if (!node) return;

        // `data` must be deep-copied. Every node component mutates the payload it is
        // handed in place (that is how edits reach the store), so a shallow copy
        // would leave the clone editing the original's nested arrays and objects.
        $nodesData = [
            ...$nodesData,
            {
                ...node,
                // Same id scheme as a node dropped from the palette (`onDrop` in
                // Flow.svelte). Both sites have to agree: a duplicate is
                // indistinguishable from any other node once it is on the canvas.
                id: crypto.randomUUID(),
                position: { x: node.position.x + 40, y: node.position.y + 40 },
                data: structuredClone(node.data),
                selected: false,
            },
        ];
    }

    function handleDelete() {
        // Goes through the flow helper rather than filtering the store directly so
        // the edges connected to this node are torn down with it.
        deleteElements({ nodes: [{ id }] });
    }

    function handleKeyDown(event: KeyboardEvent) {
        if (event.key === 'Enter' || event.key === ' ') {
            isExpanded = !isExpanded;
        }
    }

    function getHandlePosition(handle: HandleConfig): string {
        const offsetX = handle.offsetX ?? 0;
        const offsetY = handle.offsetY ?? 0;
        const positionMap = {
            [Position.Left]: `top: ${offsetY}%; left: ${offsetX}%;`,
            [Position.Right]: `top: ${offsetY}%; right: ${offsetX}%;`,
            [Position.Top]: `left: ${offsetY}%; top: ${offsetX}%;`,
            [Position.Bottom]: `left: ${offsetY}%; bottom: ${offsetX}%;`,
        };
        return positionMap[handle.position] || "";
    }
</script>

<div
    class="node-wrapper relative transition-all duration-300 overflow-visible" 
    on:mouseenter={() => isHovered = true}
    on:mouseleave={() => isHovered = false}
    role="region"
    {...$$restProps}
>
    <!-- Slide-out Context Menu -->
    {#if isHeaderHovered && !isPreview}
    <div
        on:mouseenter={showMenu}
        on:mouseleave={hideMenu}
        class="context-menu-wrapper nodrag nopan"
        role="menu"
        tabindex="0"
    >
        <ContextMenu
            onDuplicate={handleDuplicate}
            onDelete={handleDelete}
        />
    </div>
    {/if}

    <!-- Connection Handles -->
    {#each handles as handle (handle.id)}
        <Handle
            type={handle.type}
            position={handle.position}
            id={handle.id}
            style={getHandlePosition(handle)}
            {isConnectable}
        />
    {/each}

    <!-- Node Header -->
    <div
        class={`flex items-center justify-between p-4 rounded-t-lg ${color} node-header cursor-pointer ${!isExpanded ? "rounded-bottom" : ""}`}
        style="background: {color}"
        on:click={() => (isExpanded = !isExpanded)}
        on:keydown={handleKeyDown}
        on:mouseenter={showMenu}
        on:mouseleave={hideMenu}
        role="button"
        tabindex="0"
    >
        <div class="flex items-center gap-3">
            <div class="p-2 bg-white bg-opacity-20 rounded-lg">
                <svelte:component this={icon} class="w-5 h-5 text-[--secondary-text]" />
            </div>
            <h3 class="text-sm font-semibold text-[--secondary-text]">{title}</h3>
        </div>
        <button
            class="text-[--secondary-text] hover:text-[--secondary-text-hover] p-2"
            aria-expanded={isExpanded}
        >
            <ChevronDown class={`w-5 h-5 transition-transform ${isExpanded ? 'rotate-180' : ''}`} />
        </button>
    </div>

    <!-- Expandable Content -->
    {#if isExpanded}
        <div class="content space-y-4 p-4" transition:slide={{ duration: 300, easing: cubicOut }}>
            <slot />
        </div>
    {/if}
</div>

<style>
    /* Glass morphism effect for node wrapper */
    .node-wrapper {
        border-radius: 1rem;
        background: rgba(255, 255, 255, 0.1);
        backdrop-filter: blur(10px);
        box-shadow: 0 4px 30px rgba(0, 0, 0, 0.1);
        /* A fixed width rather than a minimum, so every node on the canvas is the
           same size whatever it holds. Svelte Flow lays nodes out absolutely, so
           an auto width shrink-wraps to *max-content*: a node with a sentence of
           help text in it takes the sentence's unwrapped length as its width and
           ends up several times wider than its neighbours, since nothing
           constrains it enough to wrap. 300px is a little over the widest row any
           node currently has (the Clicks + delay pair in the Mouse Click node),
           so pinning it here costs the existing nodes nothing and gives prose
           somewhere to wrap. */
        width: 300px;
        transform-origin: center center;
        transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
        position: relative;
    }

    .node-wrapper:hover {
        box-shadow: 0 8px 40px rgba(0, 0, 0, 0.15);
        transform: scale(1.01);
    }

    /* Context menu sits directly on top of the header. The padding-bottom is a
       transparent bridge: it keeps the pointer inside this element while it
       travels from the header up to the buttons, so the menu never vanishes
       mid-journey. */
    .context-menu-wrapper {
        position: absolute;
        bottom: 100%;
        right: 0.5rem;
        padding-bottom: 0.5rem;
        min-width: fit-content;
        z-index: 100;
    }

    /* Header styling */
    .node-header {
        border-radius: 1rem 1rem 0 0;
        transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
    }

    .node-header.rounded-bottom {
        border-radius: 1rem;
    }

    /* Content section styling */
    .content {
        background: rgba(255, 255, 255, 0.05);
        border-top: 1px solid rgba(255, 255, 255, 0.2);
        border-radius: 0 0 1rem 1rem;
        transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
    }

    :global(.svelte-flow__handle) {
        background: var(--accent);
        width: 8px;
        height: 8px;
        border: 2px solid var(--main-text);
        position: absolute;
        transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
        box-shadow: 0 0 0 rgba(0, 0, 0, 0);
        transform-origin: center center;
    }

    :global(.svelte-flow__handle:hover) {
        background: var(--link);
        width: 10px;
        height: 10px;
        margin: -1px;
        box-shadow: 0 2px 4px var(--main-60);
    }

    /* Keep the existing animations */
    .expand-animation {
        animation: expand 0.5s ease-out forwards;
    }

    .retract-animation {
        animation: retract 0.5s ease-in forwards;
    }

    @keyframes expand {
        0% {
            max-height: 0;
            padding: 1rem;
        }
        99% {
            max-height: 500px;
            padding: 1rem;
        }
        100% {
            max-height: 500px;
            padding: 0;
        }
    }

    @keyframes retract {
        0% {
            max-height: 500px;
            padding: 1rem;
        }
        99% {
            max-height: 0;
            padding: 1rem;
        }
        100% {
            max-height: 0;
            padding: 0;
        }
    }
</style>
