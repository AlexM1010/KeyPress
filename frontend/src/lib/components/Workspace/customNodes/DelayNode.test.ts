// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { fireEvent, screen } from '@testing-library/svelte';
import { persistedData, renderNode } from '$lib/test/nodeHarness';
import DelayNode from './DelayNode.svelte';
import type { DelayNodeData } from '$lib/stores/flow.svelte';

describe('DelayNode', () => {
	it('backfills newer fields into a payload saved before they existed', () => {
		// A macro saved when the node only had a fixed delay. The backfill has to
		// reach it in place, or the Random tab would open on `undefined`.
		const { data } = renderNode(DelayNode, { delayType: 'Fixed', time: 250 } as DelayNodeData);

		expect(data).toEqual({ delayType: 'Fixed', time: 250, minTime: 500, maxTime: 1500 });
	});

	it('leaves a saved value alone when backfilling', () => {
		const { data } = renderNode(DelayNode, {
			delayType: 'Random',
			time: 250,
			minTime: 10,
			maxTime: 20
		});

		expect(persistedData(data)).toEqual({
			delayType: 'Random',
			time: 250,
			minTime: 10,
			maxTime: 20
		});
	});

	it('writes a typed time back to the payload as a number', async () => {
		const { data } = renderNode(DelayNode, { delayType: 'Fixed', time: 1000 } as DelayNodeData);

		await fireEvent.input(screen.getByLabelText('Time'), { target: { value: '2500' } });

		// The assertion that matters is the *type*, not the value: the Go handler
		// does `task.Data["time"].(float64)`, which a string fails at run time
		// with the user's real keyboard already in play.
		expect(data.time).toBe(2500);
		expect(persistedData(data).time).toBe(2500);
	});

	it('scales a time typed in seconds into the milliseconds the backend expects', async () => {
		const { data } = renderNode(DelayNode, { delayType: 'Fixed', time: 1000 } as DelayNodeData);

		// The unit button cycles ms -> s -> min, and the payload stays in ms
		// whatever the box shows.
		await fireEvent.click(screen.getByRole('button', { name: /Click to convert to seconds/ }));
		await fireEvent.input(screen.getByLabelText('Time'), { target: { value: '3' } });

		expect(data.time).toBe(3000);
	});

	it('switches delay type through the button group', async () => {
		const { data } = renderNode(DelayNode, { delayType: 'Fixed', time: 1000 } as DelayNodeData);

		await fireEvent.click(screen.getByText('Random'));

		expect(data.delayType).toBe('Random');
		// Switching type swaps which inputs are on screen; the fixed time is kept
		// so flipping back and forth does not cost the user the value they typed.
		expect(screen.getByLabelText('Minimum Time')).toBeDefined();
		expect(data.time).toBe(1000);
	});

	it('keeps the min and max of a random delay apart', async () => {
		const { data } = renderNode(DelayNode, {
			delayType: 'Random',
			time: 1000,
			minTime: 500,
			maxTime: 1500
		});

		await fireEvent.input(screen.getByLabelText('Minimum Time'), { target: { value: '200' } });
		await fireEvent.input(screen.getByLabelText('Maximum Time'), { target: { value: '800' } });

		expect(persistedData(data)).toEqual({
			delayType: 'Random',
			time: 1000,
			minTime: 200,
			maxTime: 800
		});
	});
});
