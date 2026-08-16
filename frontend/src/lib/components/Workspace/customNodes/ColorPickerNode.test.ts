// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { fireEvent, screen } from '@testing-library/svelte';
import { persistedData, renderNode } from '$lib/test/nodeHarness';
import ColorPickerNode from './ColorPickerNode.svelte';

/**
 * Mirrors the payload the component declares inline. `ColorPickerNode.svelte`
 * keeps the type next to the only component that owns it and exports nothing,
 * so there is no shared declaration to import - restating it here is what lets
 * the partial payloads below be cast without `any`.
 */
type ColorPickerNodeData = {
	color: string;
	x: number;
	y: number;
	tolerance: number;
	timeoutMs: number;
	storeResultAs?: string;
};

/** A complete payload, so a test can vary one field without restating five. */
const savedPayload = (overrides: Partial<ColorPickerNodeData> = {}): ColorPickerNodeData => ({
	color: '#00ff00',
	x: 0,
	y: 0,
	tolerance: 10,
	timeoutMs: 30000,
	...overrides
});

/**
 * The handle ids the node has actually rendered, in the order they appear.
 *
 * Read off the DOM rather than off the component's own array, because the DOM is
 * what Svelte Flow attaches edges to: `<Handle>` renders `data-handleid`, and an
 * edge's `sourceHandle` is that string - which is what the Go handler compares
 * against `Task.WiredOutputs` to decide whether a timeout is handled.
 */
function handleIds(kind?: 'source' | 'target'): string[] {
	const selector = kind ? `.svelte-flow__handle.${kind}` : '.svelte-flow__handle';
	return [...document.querySelectorAll(selector)].map(
		(handle) => handle.getAttribute('data-handleid') ?? ''
	);
}

describe('ColorPickerNode', () => {
	it('backfills newer fields into a payload saved before they existed', () => {
		// A macro saved when the node only matched a colour, before it could be
		// pointed at a pixel. The backfill has to reach it in place, or the X/Y
		// boxes would mount bound to `undefined`.
		const { data } = renderNode(ColorPickerNode, { color: '#ff0000' } as ColorPickerNodeData);

		expect(data).toEqual({
			color: '#ff0000',
			x: 0,
			y: 0,
			tolerance: 10,
			timeoutMs: 30000
		});
	});

	it('leaves saved values alone when backfilling', () => {
		const { data } = renderNode(ColorPickerNode, {
			color: '#123456',
			x: 640,
			y: 480,
			tolerance: 3,
			timeoutMs: 5000
		});

		// Nothing the user saved is the default's to overwrite - a `tolerance` of
		// 3 is a deliberately strict match and must not spring back to 10.
		expect(persistedData(data, 'ColorPickerNode')).toEqual({
			color: '#123456',
			x: 640,
			y: 480,
			tolerance: 3,
			timeoutMs: 5000
		});
	});

	it('writes a picked colour back to the payload as a hex string', async () => {
		const { data } = renderNode(ColorPickerNode, {
			color: '#00ff00',
			x: 0,
			y: 0,
			tolerance: 10,
			timeoutMs: 30000
		});

		await fireEvent.input(screen.getByLabelText('Target color'), {
			target: { value: '#abcdef' }
		});

		// The backend parses this as `#rrggbb`; the swatch is also echoed as text
		// next to the picker, so a stale payload would show up as a label that
		// disagrees with the colour under it.
		expect(data.color).toBe('#abcdef');
		expect(screen.getByText('#abcdef')).toBeDefined();
		expect(persistedData(data, 'ColorPickerNode').color).toBe('#abcdef');
	});

	it('writes typed screen coordinates back to the payload as numbers', async () => {
		const { data } = renderNode(ColorPickerNode, {
			color: '#00ff00',
			x: 0,
			y: 0,
			tolerance: 10,
			timeoutMs: 30000
		});

		await fireEvent.input(screen.getByLabelText('X'), { target: { value: '1920' } });
		await fireEvent.input(screen.getByLabelText('Y'), { target: { value: '1080' } });

		// The assertion that matters is the *type*, not the value: the Go handler
		// asserts these as `float64`, and a string passes every compile-time check
		// on the way there before panicking at run time with the pixel already
		// being sampled. `toBe` is what catches it - `'1920'` would satisfy a
		// loose comparison.
		expect(data.x).toBe(1920);
		expect(data.y).toBe(1080);

		const persisted = persistedData(data, 'ColorPickerNode');

		expect(typeof persisted.x).toBe('number');
		expect(typeof persisted.y).toBe('number');
		expect(persisted.x).toBe(1920);
		expect(persisted.y).toBe(1080);
	});

	it('keeps X and Y bound to their own field', async () => {
		const { data } = renderNode(ColorPickerNode, {
			color: '#00ff00',
			x: 0,
			y: 0,
			tolerance: 10,
			timeoutMs: 30000
		});

		// Three `NumberInput`s share this node, and they used to share one
		// hard-coded DOM id as well - which made every `<label for=...>` resolve to
		// the first box. Reaching each field by its label is what proves the ids
		// are distinct, so typing into Y cannot land in X.
		await fireEvent.input(screen.getByLabelText('Y'), { target: { value: '42' } });

		expect(data.x).toBe(0);
		expect(data.y).toBe(42);
	});

	it('writes a typed tolerance back to the payload as a number', async () => {
		const { data } = renderNode(ColorPickerNode, {
			color: '#00ff00',
			x: 0,
			y: 0,
			tolerance: 10,
			timeoutMs: 30000
		});

		await fireEvent.input(screen.getByLabelText('Tolerance'), { target: { value: '32' } });

		expect(data.tolerance).toBe(32);
		expect(typeof persistedData(data, 'ColorPickerNode').tolerance).toBe('number');
		expect(persistedData(data, 'ColorPickerNode').tolerance).toBe(32);
	});

	it('clamps a tolerance above one RGB channel back to 255', async () => {
		const { data } = renderNode(ColorPickerNode, {
			color: '#00ff00',
			x: 0,
			y: 0,
			tolerance: 10,
			timeoutMs: 30000
		});

		await fireEvent.input(screen.getByLabelText('Tolerance'), { target: { value: '999' } });

		// Tolerance is compared per RGB channel, so anything past 255 matches every
		// possible pixel and the node would stop waiting instantly. The clamp has
		// to reach `data`, not just the box on screen - the saved file is what the
		// backend reads.
		expect(data.tolerance).toBe(255);
		expect(persistedData(data, 'ColorPickerNode').tolerance).toBe(255);
	});

	it('scales a timeout typed in seconds into the milliseconds the backend expects', async () => {
		const { data } = renderNode(ColorPickerNode, {
			color: '#00ff00',
			x: 0,
			y: 0,
			tolerance: 10,
			timeoutMs: 30000
		});

		// The box opens in seconds (`startingUnit="s"`), so the default 30000ms
		// shows as 30 - the unit on screen and the unit in the payload are not the
		// same, and only the payload's is the backend's business.
		expect((screen.getByLabelText('Timeout') as HTMLInputElement).value).toBe('30');

		await fireEvent.input(screen.getByLabelText('Timeout'), { target: { value: '5' } });

		// A typo here is a factor of a thousand: 5ms instead of 5s would make the
		// node give up before the screen had drawn a frame.
		expect(data.timeoutMs).toBe(5000);
		expect(typeof persistedData(data, 'ColorPickerNode').timeoutMs).toBe('number');
		expect(persistedData(data, 'ColorPickerNode').timeoutMs).toBe(5000);
	});

	it('carries a fractional timeout through the seconds conversion', async () => {
		const { data } = renderNode(ColorPickerNode, {
			color: '#00ff00',
			x: 0,
			y: 0,
			tolerance: 10,
			timeoutMs: 30000
		});

		await fireEvent.input(screen.getByLabelText('Timeout'), { target: { value: '2.5' } });

		// Half-second precision survives because the payload is in ms; a payload
		// kept in seconds would have to round and the user would silently get 2s
		// or 3s instead.
		expect(data.timeoutMs).toBe(2500);
	});

	it('switches the timeout unit without moving the payload', async () => {
		const { data } = renderNode(ColorPickerNode, {
			color: '#00ff00',
			x: 0,
			y: 0,
			tolerance: 10,
			timeoutMs: 30000
		});

		await fireEvent.click(screen.getByRole('button', { name: /Click to convert to minutes/ }));

		// Only the display unit changed. The stored value is untouched, so cycling
		// the unit button cannot on its own dirty the macro or shift the timeout.
		expect((screen.getByLabelText('Timeout') as HTMLInputElement).value).toBe('0.5');
		expect(data.timeoutMs).toBe(30000);

		await fireEvent.input(screen.getByLabelText('Timeout'), { target: { value: '2' } });

		expect(data.timeoutMs).toBe(120000);
	});

	it('offers a timeout output beside the match one, without renaming the match', () => {
		renderNode(ColorPickerNode, savedPayload());

		// `right` is load-bearing history: every edge this app has ever saved from
		// this node carries that handle id, so the match keeps it and the new output
		// is the one with a name of its own. Rename the match and every existing
		// macro's edge leaves a handle that no longer exists.
		expect(handleIds('source')).toEqual(['right', 'timeout']);
		expect(handleIds('target')).toEqual(['left']);
	});

	it('stores nothing by default, rather than storing an empty name', () => {
		const { data } = renderNode(ColorPickerNode, { color: '#ff0000' } as ColorPickerNodeData);

		// The distinction the whole run state rests on: a name that was never given
		// has to be *absent*, because "unset" is what a Branch node's `isSet` asks
		// about. An empty string would be a name, and one nobody chose.
		expect('storeResultAs' in data).toBe(false);
		expect('storeResultAs' in persistedData(data, 'ColorPickerNode')).toBe(false);
	});

	it('writes a typed result name into the payload', async () => {
		const { data } = renderNode(ColorPickerNode, savedPayload());

		await fireEvent.input(screen.getByLabelText('Store result as'), {
			target: { value: 'matched' }
		});

		expect(data.storeResultAs).toBe('matched');
		expect(persistedData(data, 'ColorPickerNode').storeResultAs).toBe('matched');
	});

	it('removes the result name when the box is cleared, rather than emptying it', async () => {
		const { data } = renderNode(ColorPickerNode, savedPayload({ storeResultAs: 'matched' }));

		await fireEvent.input(screen.getByLabelText('Store result as'), { target: { value: '' } });

		// `bind:value` alone cannot express this - it would write `""` - and `""` is
		// exactly the value the field must never take: `resultName` in Go reads a
		// blank name as "store nothing", so the payload would be carrying a name
		// that means the absence of one.
		expect('storeResultAs' in data).toBe(false);
		expect('storeResultAs' in persistedData(data, 'ColorPickerNode')).toBe(false);
	});

	it('treats a name of nothing but spaces as no name at all', async () => {
		const { data } = renderNode(ColorPickerNode, savedPayload({ storeResultAs: 'matched' }));

		await fireEvent.input(screen.getByLabelText('Store result as'), { target: { value: '   ' } });

		expect('storeResultAs' in data).toBe(false);
	});

	it('leaves a saved result name alone through an unrelated edit', async () => {
		const { data } = renderNode(ColorPickerNode, savedPayload({ storeResultAs: 'matched' }));

		expect((screen.getByLabelText('Store result as') as HTMLInputElement).value).toBe('matched');

		await fireEvent.input(screen.getByLabelText('Tolerance'), { target: { value: '32' } });

		expect(persistedData(data, 'ColorPickerNode')).toEqual({
			color: '#00ff00',
			x: 0,
			y: 0,
			tolerance: 32,
			timeoutMs: 30000,
			storeResultAs: 'matched'
		});
	});
});
