// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { screen } from '@testing-library/svelte';
import { persistedData, renderNode } from '$lib/test/nodeHarness';
import { asGraphState } from '$lib/test/graphState.svelte';
import LoopEndNode from './LoopEndNode.svelte';

/**
 * Mirrors the payload the component declares inline: the editor's pairing id and
 * nothing else. `executeLoopEndTask` reads none of it, and that is the property
 * that lets the pair be an editor construct at all.
 */
type LoopEndNodeData = {
	loopId?: string;
};

/**
 * Mounts the node with its payload in the deep `$state` the graph really holds
 * it in - the harness catching up with the contract, as for every runes node.
 * See `asGraphState`.
 */
const renderLoopEnd = (data: LoopEndNodeData) => renderNode(LoopEndNode, asGraphState(data));

/**
 * The handle ids the node has actually rendered, in the order they appear.
 *
 * Read off the DOM rather than off any constant, because the DOM is what Svelte
 * Flow attaches edges to: `<Handle>` renders `data-handleid`, and the `back`
 * edge the editor draws names that string in its `sourceHandle`.
 */
function handleIds(kind?: 'source' | 'target'): string[] {
	const selector = kind ? `.svelte-flow__handle.${kind}` : '.svelte-flow__handle';
	return [...document.querySelectorAll(selector)].map(
		(handle) => handle.getAttribute('data-handleid') ?? ''
	);
}

describe('LoopEndNode', () => {
	it('names its one output exactly `back`, and takes one input', () => {
		renderLoopEnd({ loopId: 'loop-1' });

		// `back` is the save format: `createLoopPair` writes it into the
		// `sourceHandle` of the edge it draws to the partner Loop Start. The handler
		// returns "" - "the only output" - which takes every outgoing edge whatever
		// its handle is called, so the walk needs nothing else from this node.
		expect(handleIds('target')).toEqual(['left']);
		expect(handleIds('source')).toEqual(['back']);
	});

	it('writes nothing at all into its payload', () => {
		const { data } = renderLoopEnd({ loopId: 'loop-1' });

		// No defaults to backfill, because there is nothing to configure: the
		// backend reads no payload from this node. A field appearing here would be
		// a value nothing consumes, saved into every macro that has a loop in it.
		expect(persistedData(data, 'LoopEndNode')).toEqual({ loopId: 'loop-1' });
	});

	it('leaves an empty payload empty', () => {
		// A hand-edited or older macro can have a Loop End with no pairing id at
		// all. The store falls back to the back edge to find its partner, and this
		// component still has nothing to add.
		const { data } = renderLoopEnd({});

		expect(persistedData(data, 'LoopEndNode')).toEqual({});
	});

	it('says the edge back to the start was drawn for the user', () => {
		renderLoopEnd({ loopId: 'loop-1' });

		// The one thing about this node a user has to be told: they wire the body
		// *into* it, and the wire out of it is already drawn. n8n is the one
		// mainstream tool that makes the user draw that edge, and Loop Over Items is
		// reliably its most confusing node.
		expect(screen.getByText(/wired for you/)).not.toBeNull();
	});
});
