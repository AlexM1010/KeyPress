// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import ContextMenu from './ContextMenu.svelte';


describe('ContextMenu', () => {
	it('calls back on duplicate', async () => {
		const onDuplicate = vi.fn();
		const onDelete = vi.fn();

		render(ContextMenu, { props: { onDuplicate, onDelete } });

		// By `aria-label`, because that is the only name these buttons have: both
		// are an icon and nothing else, so the label is what a screen reader reads
		// out and the only handle a test has on them.
		await fireEvent.click(screen.getByLabelText('Duplicate'));

		// Callback props rather than `createEventDispatcher`, which Svelte 5 removes.
		// The two are invoked identically from the outside, so this reads the same
		// before and after that migration - but if the props were ever reverted to
		// component events, nothing would arrive and this fails.
		expect(onDuplicate).toHaveBeenCalledTimes(1);
		// The two buttons sit next to each other and one of them is destructive, so
		// each is pinned to fire its own callback and only its own.
		expect(onDelete).not.toHaveBeenCalled();
	});

	it('calls back on delete', async () => {
		const onDuplicate = vi.fn();
		const onDelete = vi.fn();

		render(ContextMenu, { props: { onDuplicate, onDelete } });

		await fireEvent.click(screen.getByLabelText('Delete'));

		expect(onDelete).toHaveBeenCalledTimes(1);
		expect(onDuplicate).not.toHaveBeenCalled();
	});
});
