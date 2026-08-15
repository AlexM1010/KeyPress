<!-- frontend/src/lib/test/NodeHarness.svelte -->
<script lang="ts">
    import type { Component, ComponentType, SvelteComponent } from 'svelte';
    import { SvelteFlowProvider } from '@xyflow/svelte';
    // Deep, relative, and into node_modules on purpose - see the note on the
    // contexts below. The package's `exports` map publishes only the root and
    // two stylesheets, so a bare `@xyflow/svelte/...` specifier for this would
    // not resolve; a path does, and resolves to the very module the library
    // imports itself, which is the whole point.
    import {
        setNodeIdContext,
        setNodeConnectableContext
    } from '../../../node_modules/@xyflow/svelte/dist/lib/store/context.js';

    /**
     * Enough of a Svelte Flow canvas for one custom node to mount outside one.
     *
     * `NodeWrapper` calls `useSvelteFlow()` and renders `<Handle>`, and both read
     * the flow store out of context - so a node rendered bare throws before it
     * paints anything. `<SvelteFlowProvider>` is the library's own answer to
     * that: it builds the same store `<SvelteFlow>` would and puts it in context,
     * without the renderer, the viewport or the pane. That is the whole reason
     * these tests can drive a node's controls without standing up a canvas, a
     * zoom transform and a `ResizeObserver` around it.
     *
     * The two node-level contexts are set here rather than by the provider
     * because `<SvelteFlow>` normally sets them per node as it renders it, and
     * nothing is rendering nodes here.
     *
     * They have to be set with the library's own setters, and that is why this
     * file reaches into `store/context.js` rather than naming a key. Svelte Flow
     * 1.x builds each context key as `const key = {}` inside a `createContext()`
     * closure - an object identity that lives only in that module and cannot be
     * reconstructed from outside it. The 0.1.x keys were plain strings
     * ('svelteflow__node_id'), which is what this used to pass and what silently
     * stopped matching on upgrade: every node test failed with "Handle must be
     * used within a Custom Node component", because a `setContext` under a
     * different key is indistinguishable from no context at all.
     */
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    type AnyNodeComponent = ComponentType<SvelteComponent<any, any, any>> | Component<any, any, any>;

    export let component: AnyNodeComponent;
    export let props: Record<string, unknown> = {};

    setNodeIdContext(String(props.id ?? 'test-node-1'));
    // `{ value }` rather than a store: 1.x reads connectability as a plain
    // property off a rune-backed object, where 0.1.x read `$connectable`.
    setNodeConnectableContext({ value: true });
</script>

<SvelteFlowProvider>
    <svelte:component this={component} {...props} />
</SvelteFlowProvider>
