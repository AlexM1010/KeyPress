// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import { get } from 'svelte/store';
import { Keyboard } from 'lucide-svelte';
// Rendered through `NodeHarness` rather than through `renderNode`, because this
// wrapper deliberately takes no `data` prop - the payload belongs to the node
// component inside it.
import NodeHarness from '$lib/test/NodeHarness.svelte';
import { nodesData, type FlowNode } from '$lib/stores/flow';
import NodeWrapper from './NodeWrapper.svelte';


const NODE_ID = 'keypress-1';
const TITLE = 'Keypress';

/**
 * The node the wrapper under test is standing in front of, built fresh per test.
 *
 * `modifiers` is what the deep-copy assertion needs: a nested array is exactly
 * what a shallow copy would leave shared between the original and its duplicate.
 */
const seedNode = (): FlowNode => ({
	id: NODE_ID,
	type: 'KeyPressNode',
	position: { x: 100, y: 200 },
	data: { keyKind: 'Character', character: 'a', modifiers: ['Ctrl'] }
});

/**
 * Renders the wrapper the way `<SvelteFlow>` would - inside the flow context,
 * because it calls `useSvelteFlow()` and renders `<Handle>`, and both read the
 * flow store out of context.
 */
function renderWrapper(): void {
	render(NodeHarness, {
		props: {
			component: NodeWrapper,
			props: {
				id: NODE_ID,
				type: 'KeyPressNode',
				title: TITLE,
				icon: Keyboard,
				color: 'bg-orange-500',
				handles: []
			}
		}
	});
}

/**
 * The menu is mounted only while the header is hovered - that is the whole
 * affordance, so a test that skipped the hover would find no buttons at all.
 */
async function hoverHeader(): Promise<void> {
	await fireEvent.mouseEnter(screen.getByRole('button', { name: TITLE }));
}

describe('NodeWrapper', () => {
	beforeEach(() => {
		// The store is module state shared by every test in this file, and the
		// duplicate under test appends to it, so it is reseeded rather than added to.
		nodesData.set([seedNode()]);
	});

	it('keeps the context menu out of the way until the header is hovered', async () => {
		renderWrapper();

		// Duplicate and delete sit on top of the node, over the canvas, so they are
		// only there when the pointer says the user is interested in this node.
		expect(screen.queryByLabelText('Duplicate')).toBeNull();

		await hoverHeader();

		expect(screen.getByLabelText('Duplicate')).toBeDefined();
	});

	it('appends a copy of the node with its own id and an offset position', async () => {
		renderWrapper();
		await hoverHeader();
		await fireEvent.click(screen.getByLabelText('Duplicate'));

		const [original, copy] = get(nodesData);

		expect(get(nodesData)).toHaveLength(2);
		// A fresh uuid, the same id scheme a node dropped from the palette gets:
		// once it is on the canvas a duplicate has to be indistinguishable from any
		// other node, and a reused id would have Svelte Flow key two nodes the same.
		expect(copy.id).not.toBe(original.id);
		expect(copy.id).not.toBe('');
		// Offset, or the copy would land exactly under the original and look like
		// nothing happened.
		expect(copy.position).toEqual({ x: 140, y: 240 });
		// The original is left where it was; duplicating is not a move.
		expect(original.position).toEqual({ x: 100, y: 200 });
		expect(copy.type).toBe(original.type);
	});

	it('gives the copy a payload of its own, nested values included', async () => {
		renderWrapper();
		await hoverHeader();
		await fireEvent.click(screen.getByLabelText('Duplicate'));

		const [original, copy] = get(nodesData);

		expect(copy.data).toEqual(original.data);
		expect(copy.data).not.toBe(original.data);

		// The reason `structuredClone` is there rather than a spread. Every node
		// component mutates the payload it is handed in place - that by-reference
		// write is how an edit reaches the store at all - so a shallow copy would
		// leave the duplicate editing the original's nested arrays: ticking Alt on
		// the copy would tick it on the node it was copied from, and the user would
		// have no way to tell why.
		(copy.data.modifiers as string[]).push('Alt');

		expect(original.data.modifiers).toEqual(['Ctrl']);
	});
});
