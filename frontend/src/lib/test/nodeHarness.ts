// frontend/src/lib/test/nodeHarness.ts
import { render } from '@testing-library/svelte';
import type { Component, ComponentType, SvelteComponent } from 'svelte';
import NodeHarness from './NodeHarness.svelte';
import { serializeMacro } from '$lib/stores/flow.svelte';

/**
 * A custom node component, however the Svelte tooling of the day types one.
 *
 * The props have to be `any` rather than the node's own shape. Svelte 5's
 * `svelte2tsx` types a component as `__sveltets_2_IsomorphicComponent` - both
 * callable and constructible - and its constructor options are invariant in the
 * props, so a component with a required prop (every node has `id`) is not
 * assignable to a parameter typed for a component with arbitrary ones. Naming
 * the props here would only move the problem to the call site, and this helper's
 * whole job is to take *any* node.
 *
 * `nodeTypes.ts` used to need the same escape hatch and no longer does: Svelte
 * Flow 1.x types its node map as `Component<NodeProps & ...>` rather than as a
 * Svelte 4 component class, so the components go in there directly. That fixed
 * the map, not this - a helper that takes an arbitrary node still has nothing
 * more specific to say about its props than `any`.
 */
// The union covers both compilation modes, which this suite genuinely spans:
// the six node components are still legacy (a class), while `NodeWrapper` had to
// become runes (a function) and is rendered directly by its own test. Whichever
// one a caller hands over, it ends up at the same `<svelte:component>` in
// `NodeHarness.svelte`, so the two declarations have to agree.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
type AnyNodeComponent = ComponentType<SvelteComponent<any, any, any>> | Component<any, any, any>;

// Unmounting between tests is handled once for the whole suite, in `setup.ts`.

/**
 * Renders one custom node the way Svelte Flow would, and hands back the very
 * `data` object it was given.
 *
 * `data` is returned rather than copied because it is the whole point of the
 * exercise: every node component edits the payload it is handed in place, and
 * that by-reference write is how an edit reaches the graph at all. A test
 * therefore drives a control and then reads *this* object - if the component
 * had reassigned `data` instead of editing it, the graph's copy would go stale
 * in exactly the way this one returns unchanged.
 *
 * What the payload is *in* changed with Svelte Flow 1.x and this did not. The
 * graph now holds nodes in deep `$state`, so an in-place write is tracked and
 * needs no announcing; here the payload is a plain object passed straight to
 * the component, which is all this assertion has ever needed.
 */
export function renderNode<TData extends Record<string, unknown>>(
	component: AnyNodeComponent,
	data: TData,
	props: Record<string, unknown> = {}
): { data: TData } {
	render(NodeHarness, {
		props: {
			component,
			props: { id: 'test-node-1', data, ...props }
		}
	});

	return { data };
}

/**
 * The payload as the saved file would hold it.
 *
 * Goes through the real `serializeMacro` rather than `JSON.stringify` on its
 * own, because that function is what the workspace compares against the file and
 * so is the only honest answer to "does this edit persist, and as what". It is
 * also where a type slip surfaces: `bind:value` on an `<input type="number">`
 * yields a string if the binding is ever routed through a text field, JSON keeps
 * strings and numbers apart, and the Go side asserts `task.Data["time"].(float64)`
 * - so a number that became a string fails at run time on the user's real
 * keyboard, having passed every compile-time check on the way there.
 */
export function persistedData(data: unknown, type = 'TestNode'): Record<string, unknown> {
	const serialized = serializeMacro(
		'test-macro',
		[{ id: 'test-node-1', type, position: { x: 0, y: 0 }, data }],
		[]
	);

	return JSON.parse(serialized).nodes[0].data;
}
