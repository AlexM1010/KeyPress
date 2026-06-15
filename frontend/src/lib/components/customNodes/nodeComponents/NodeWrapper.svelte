<!-- frontend\src\lib\components\customNodes\NodeWrapper.svelte -->
<script lang="ts">
    import { slide } from 'svelte/transition';
    import { ChevronDown } from "lucide-svelte";
    import type { ComponentType } from "svelte";
    import { Handle, Position } from "@xyflow/svelte";
    import type { HandleConfig } from "../types";
    import ContextMenu from "./ContextMenu.svelte";
    import { cubicOut } from "svelte/easing";
    import { nodesData as nodes, type NodeData } from '$lib/stores/flow';
    import { createEventDispatcher } from 'svelte';
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
    export let data: NodeData;

    // Rest props to silence warnings
    $$restProps;

    const dispatch = createEventDispatcher();

    // UI State Management
    let isHovered = false;
    let isHeaderHovered = false;

    // Event Handlers
    function handleDuplicate() {
        dispatch('duplicate', { id });
    }

    function handleDelete() {
        dispatch('delete', { id });
    }

    function handleKeyDown(event: KeyboardEvent) {
        if (event.key === 'Enter' || event.key === ' ') {
            isExpanded = !isExpanded;
        }
    }

    // Reactive statement to update nodes when data changes
    $: if (data) {
        dispatch('updateNodeData', { id: data.id, data: data.data });
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
    {#if isHeaderHovered}
    <div 
        on:mouseenter={() => isHeaderHovered = true} 
        on:mouseleave={() => isHeaderHovered = false} 
        class="context-menu-wrapper"
        role="menu"
        tabindex="0"
    >
        <ContextMenu
            on:duplicate={handleDuplicate}
            on:delete={handleDelete}
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
        on:mouseenter={() => isHeaderHovered = true}
        on:mouseleave={() => isHeaderHovered = false} 
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
        min-width: 250px;
        transform-origin: center center;
        transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
        position: relative;
    }

    .node-wrapper:hover {
        box-shadow: 0 8px 40px rgba(0, 0, 0, 0.15);
        transform: scale(1.01);
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
